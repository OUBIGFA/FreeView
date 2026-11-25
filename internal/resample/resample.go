// Package resample scales a part of an image to a given size with the
// filters an image viewer needs to look right at any zoom: area averaging
// when shrinking, which Direct2D's linear interpolation does not do below
// 50% and so aliases, and Catmull-Rom when enlarging.
//
// golang.org/x/image/draw has no area-averaging filter (its kernels are
// widened when downscaling, which blurs and weighs pixels unevenly), so the
// resampler is written here: two separable passes, horizontal first into a
// 16-bit intermediate of the output width and the source height, then
// vertical into the result. Pixels stay premultiplied RGBA throughout, as
// image.RGBA is, so averaging transparent and opaque pixels needs no
// un-premultiplying.
package resample

import (
	"image"
	"math"
	"runtime"
	"sync"
)

// Region resamples the part src of img (in img pixel coords, clipped to
// bounds) to w×h pixels: area averaging (box filter over every source
// pixel) when shrinking, Catmull-Rom when enlarging, per axis. Returns nil
// for empty input. Beyond the output it allocates one intermediate of
// w×srcH 16-bit pixels and O(w+h) filter taps, so very wide or tall images
// (e.g. 33000×40) cost no more than their output.
func Region(img *image.RGBA, src image.Rectangle, w, h int) *image.RGBA {
	if img == nil || w <= 0 || h <= 0 {
		return nil
	}
	src = src.Intersect(img.Rect)
	if src.Empty() {
		return nil
	}
	sw, sh := src.Dx(), src.Dy()
	xs, ys := taps(sw, w), taps(sh, h)

	// Horizontal pass: each source row of the region to w pixels. The
	// intermediate keeps 16 bits per channel (8-bit values times 257) so
	// the result is rounded to 8 bits only once.
	mid := make([]uint16, w*sh*4)
	base := img.PixOffset(src.Min.X, src.Min.Y)
	parallel(sh, sh*max(sw, w), func(lo, hi int) {
		for y := lo; y < hi; y++ {
			row, out := base+y*img.Stride, mid[y*w*4:(y+1)*w*4]
			for x := range xs {
				store(out[x*4:x*4+4], gather(&xs[x], img.Pix, row, 4), 257, 65535)
			}
		}
	})

	// Vertical pass: each output row from the intermediate's columns.
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	parallel(h, w*max(sh, h), func(lo, hi int) {
		for y := lo; y < hi; y++ {
			out := dst.Pix[y*dst.Stride : y*dst.Stride+w*4]
			for x := range w {
				store(out[x*4:x*4+4], gather(&ys[y], mid, x*4, w*4), 1.0/257, 255)
			}
		}
	})
	return dst
}

// tap says which source pixels make one output pixel along an axis, and
// with what weights. A box tap covers start..start+n-1 with weight w[0] for
// the first pixel, w[2] for the last (both may be partly covered) and w[1]
// for every one between, so it takes constant space however many pixels it
// averages. A Catmull-Rom tap has up to four pixels, weighted w[0..n-1].
type tap struct {
	start, n int32
	box      bool
	w        [4]float32
}

// taps returns the taps resampling n source pixels to out pixels: area
// averaging when out <= n (an exact copy when equal), Catmull-Rom when
// out > n.
func taps(n, out int) []tap {
	t := make([]tap, out)
	scale := float64(n) / float64(out)
	if out <= n {
		for i := range t {
			// Computed from the integers so that whole-number boundaries
			// stay exact and no pixel gets a sliver of a neighbor.
			lo := float64(i*n) / float64(out)
			hi := float64((i+1)*n) / float64(out)
			j0 := i * n / out
			j1 := max(((i+1)*n+out-1)/out-1, j0)
			first := (math.Min(hi, float64(j0+1)) - lo) / scale
			last := (hi - math.Max(lo, float64(j1))) / scale
			t[i] = tap{start: int32(j0), n: int32(j1 - j0 + 1), box: true,
				w: [4]float32{float32(first), float32(1 / scale), float32(last)}}
		}
		return t
	}
	for i := range t {
		// Pixel centers line up: output center i+0.5 maps to source c+0.5.
		c := (float64(i)+0.5)*scale - 0.5
		f := int(math.Floor(c))
		var raw [4]float64
		var idx [4]int
		lo, hi, sum := n, -1, 0.0
		for k := range 4 {
			j := f - 1 + k
			raw[k] = catmullRom(c - float64(j))
			sum += raw[k]
			// Edge pixels repeat beyond the border; folding their weights
			// onto the clamped index keeps a tap within the source.
			idx[k] = min(max(j, 0), n-1)
			lo, hi = min(lo, idx[k]), max(hi, idx[k])
		}
		tp := tap{start: int32(lo), n: int32(hi - lo + 1)}
		for k := range 4 {
			// Normalized so a flat color stays exactly flat.
			tp.w[idx[k]-lo] += float32(raw[k] / sum)
		}
		t[i] = tp
	}
	return t
}

// catmullRom is the Catmull-Rom cubic (B = 0, C = 0.5).
func catmullRom(x float64) float64 {
	x = math.Abs(x)
	switch {
	case x < 1:
		return (1.5*x-2.5)*x*x + 1
	case x < 2:
		return ((-0.5*x+2.5)*x-4)*x + 2
	}
	return 0
}

// gather sums the RGBA pixels of p that t weighs, the first at offset off
// plus t.start pixels of step elements each.
func gather[T uint8 | uint16](t *tap, p []T, off, step int) (acc [4]float32) {
	o := off + int(t.start)*step
	if !t.box {
		for k := range int(t.n) {
			q, w := p[o:o+4:o+4], t.w[k]
			acc[0] += w * float32(q[0])
			acc[1] += w * float32(q[1])
			acc[2] += w * float32(q[2])
			acc[3] += w * float32(q[3])
			o += step
		}
		return acc
	}
	q, w := p[o:o+4:o+4], t.w[0]
	acc = [4]float32{w * float32(q[0]), w * float32(q[1]), w * float32(q[2]), w * float32(q[3])}
	if t.n == 1 {
		return acc
	}
	// The fully covered pixels share one weight: sum them first and
	// multiply once.
	var s [4]float32
	for range int(t.n) - 2 {
		o += step
		q = p[o : o+4 : o+4]
		s[0] += float32(q[0])
		s[1] += float32(q[1])
		s[2] += float32(q[2])
		s[3] += float32(q[3])
	}
	o += step
	q, w, m := p[o:o+4:o+4], t.w[2], t.w[1]
	for c := range 4 {
		acc[c] += m*s[c] + w*float32(q[c])
	}
	return acc
}

// store writes acc times scale to q, rounded and clamped so the pixel stays
// valid premultiplied color: alpha within 0..top and each channel within
// 0..alpha. Catmull-Rom overshoots near edges and would break that.
func store[T uint8 | uint16](q []T, acc [4]float32, scale, top float32) {
	a := min(max(acc[3]*scale, 0), top)
	for c := range 3 {
		q[c] = T(min(max(acc[c]*scale, 0), a) + 0.5)
	}
	q[3] = T(a + 0.5)
}

// parallel runs fn over the rows 0..n split across GOMAXPROCS goroutines,
// or on the calling goroutine when cost (about the pixels touched) is too
// small to repay starting them.
func parallel(n, cost int, fn func(lo, hi int)) {
	procs := min(runtime.GOMAXPROCS(0), n)
	if procs < 2 || cost < 1<<17 {
		fn(0, n)
		return
	}
	var wg sync.WaitGroup
	for i := range procs {
		lo, hi := n*i/procs, n*(i+1)/procs
		wg.Go(func() { fn(lo, hi) })
	}
	wg.Wait()
}
