package resample

import (
	"image"
	"image/color"
	"math/rand/v2"
	"testing"
)

func filled(w, h int, c color.RGBA) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for i := 0; i < len(img.Pix); i += 4 {
		img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = c.R, c.G, c.B, c.A
	}
	return img
}

func TestBoxAverage2x2(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	img.SetRGBA(0, 0, color.RGBA{0, 0, 0, 255})
	img.SetRGBA(1, 0, color.RGBA{100, 0, 0, 255})
	img.SetRGBA(0, 1, color.RGBA{0, 200, 0, 255})
	img.SetRGBA(1, 1, color.RGBA{100, 200, 40, 255})
	got := Region(img, img.Rect, 1, 1).RGBAAt(0, 0)
	if want := (color.RGBA{50, 100, 10, 255}); got != want {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestBoxFractional4to3(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 4, 1))
	for x, v := range []uint8{0, 30, 60, 90} {
		img.SetRGBA(x, 0, color.RGBA{v, v, v, 255})
	}
	// Each output pixel covers 4/3 source pixels:
	// [0, 4/3): (0*1 + 30*1/3) * 3/4 = 7.5 -> 8
	// [4/3, 8/3): (30*2/3 + 60*2/3) * 3/4 = 45
	// [8/3, 4): (60*1/3 + 90*1) * 3/4 = 82.5 -> 83
	out := Region(img, img.Rect, 3, 1)
	for x, want := range []uint8{8, 45, 83} {
		if got := out.RGBAAt(x, 0); got != (color.RGBA{want, want, want, 255}) {
			t.Errorf("x=%d: got %v, want %d", x, got, want)
		}
	}
}

func TestEnlargeFlat(t *testing.T) {
	c := color.RGBA{60, 120, 30, 200}
	out := Region(filled(3, 2, c), image.Rect(0, 0, 3, 2), 17, 11)
	for i := 0; i < len(out.Pix); i += 4 {
		if got := (color.RGBA{out.Pix[i], out.Pix[i+1], out.Pix[i+2], out.Pix[i+3]}); got != c {
			t.Fatalf("pixel %d: got %v, want %v", i/4, got, c)
		}
	}
}

func TestPremultipliedValid(t *testing.T) {
	// Sharp edges between opaque and transparent pixels make Catmull-Rom
	// overshoot; the result must still be valid premultiplied color.
	rng := rand.New(rand.NewPCG(1, 2))
	img := image.NewRGBA(image.Rect(0, 0, 9, 7))
	for i := 0; i < len(img.Pix); i += 4 {
		if rng.IntN(2) == 0 {
			a := uint8(rng.IntN(256))
			img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = a, a/2, uint8(rng.IntN(int(a)+1)), a
		}
	}
	for _, size := range [][2]int{{31, 23}, {4, 3}, {20, 2}, {2, 20}} {
		out := Region(img, img.Rect, size[0], size[1])
		for i := 0; i < len(out.Pix); i += 4 {
			if a := out.Pix[i+3]; out.Pix[i] > a || out.Pix[i+1] > a || out.Pix[i+2] > a {
				t.Fatalf("%v: pixel %d %v not premultiplied", size, i/4, out.Pix[i:i+4])
			}
		}
	}
}

func TestVeryWide(t *testing.T) {
	c := color.RGBA{10, 20, 30, 255}
	out := Region(filled(33000, 40, c), image.Rect(0, 0, 33000, 40), 400, 1)
	if out.Rect != image.Rect(0, 0, 400, 1) {
		t.Fatalf("bounds %v", out.Rect)
	}
	for x := range 400 {
		if got := out.RGBAAt(x, 0); got != c {
			t.Fatalf("x=%d: got %v, want %v", x, got, c)
		}
	}
}

func TestClipAndEmpty(t *testing.T) {
	img := filled(4, 4, color.RGBA{0, 0, 0, 255})
	img.SetRGBA(3, 3, color.RGBA{200, 100, 0, 255})
	// Only the pixel (3,3) lies inside the bounds.
	if got := Region(img, image.Rect(3, 3, 10, 10), 1, 1).RGBAAt(0, 0); got != (color.RGBA{200, 100, 0, 255}) {
		t.Fatalf("clipped: got %v", got)
	}
	if Region(img, image.Rect(5, 5, 9, 9), 2, 2) != nil {
		t.Fatal("region outside bounds should give nil")
	}
	if Region(img, img.Rect, 0, 3) != nil || Region(nil, img.Rect, 1, 1) != nil {
		t.Fatal("empty input should give nil")
	}
}

func TestSubimageOrigin(t *testing.T) {
	// A sub-image whose Rect does not start at 0,0 is addressed in its own
	// coordinates.
	img := filled(8, 8, color.RGBA{0, 0, 0, 255})
	img.SetRGBA(6, 6, color.RGBA{255, 255, 255, 255})
	sub := img.SubImage(image.Rect(6, 6, 8, 8)).(*image.RGBA)
	if got := Region(sub, image.Rect(6, 6, 7, 7), 1, 1).RGBAAt(0, 0); got != (color.RGBA{255, 255, 255, 255}) {
		t.Fatalf("got %v", got)
	}
}

func BenchmarkRegion6000x4000(b *testing.B) {
	img := image.NewRGBA(image.Rect(0, 0, 6000, 4000))
	rng := rand.New(rand.NewPCG(3, 4))
	for i := range img.Pix {
		img.Pix[i] = uint8(rng.IntN(256))
	}
	for i := 3; i < len(img.Pix); i += 4 {
		img.Pix[i] = 255
	}
	for b.Loop() {
		Region(img, img.Rect, 1500, 1000)
	}
}
