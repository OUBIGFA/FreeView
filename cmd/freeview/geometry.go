package main

// Image size, zoom and pan calculations do not perform I/O or decode pixels.
import (
	"math"

	"github.com/nsgo-dev/nsgo/core"

	"github.com/OUBIGFA/FreeView/internal/settings"
)

// displaySize is the image's size in device pixels as shown, after its
// rotation, at scale 1.
func (v *viewer) displaySize() (w, h float64) {
	p := v.img.Get().pic
	w, h = float64(p.Width), float64(p.Height)
	if v.rotate.Get()%180 != 0 {
		w, h = h, w
	}
	return w, h
}

// ratio is the screen's device pixels per point.
func (v *viewer) ratio() float64 {
	if r := float64(v.cx.Env().PixelRatio()); r > 0 {
		return r
	}
	return 1
}

// fitScale is the scale that fits the image in the viewport under mode.
func (v *viewer) fitScale(mode settings.Fit) float64 {
	w, h := v.displaySize()
	vp := v.viewport.Get()
	if w == 0 || h == 0 || vp.Width == 0 || vp.Height == 0 {
		return 1
	}
	r := v.ratio()
	s := math.Min(float64(vp.Width)*r/w, float64(vp.Height)*r/h)
	switch mode {
	case settings.FitShrink:
		return math.Min(s, 1)
	case settings.FitOriginal:
		return 1
	}
	return s
}

// currentScale is the scale on screen.
func (v *viewer) currentScale() float64 {
	if mode := v.fit.Get(); mode != "" {
		return v.fitScale(mode)
	}
	return v.scale.Get()
}

// clampOffset keeps the image's center where the image covers the
// viewport: centered along an axis it fits in, never pulled past its
// edges along one it overflows.
func (v *viewer) clampOffset(o core.Point, scale float64) core.Point {
	w, h := v.displaySize()
	r := v.ratio()
	vp := v.viewport.Get()
	limit := func(o core.Points, size float64, view core.Points) core.Points {
		over := (size*scale/r - float64(view)) / 2
		if over <= 0 {
			return 0
		}
		return core.Points(math.Max(-over, math.Min(over, float64(o))))
	}
	return core.Point{X: limit(o.X, w, vp.Width), Y: limit(o.Y, h, vp.Height)}
}

// zoomTo sets the scale, keeping the image point under anchor (viewport
// coordinates) in place.
func (v *viewer) zoomTo(scale float64, anchor core.Point) {
	if v.img.Get().pic.Width == 0 {
		return
	}
	old := v.currentScale()
	scale = math.Max(minScale, math.Min(maxScale, scale))
	vp := v.viewport.Get()
	// The anchor relative to the viewport's center, and the image's
	// center relative to it, scale together.
	ax, ay := float64(anchor.X-vp.Width/2), float64(anchor.Y-vp.Height/2)
	o := v.clampOffset(v.offset.Get(), old)
	k := scale / old
	next := core.Point{X: core.Points(ax - (ax-float64(o.X))*k), Y: core.Points(ay - (ay-float64(o.Y))*k)}
	v.cx.Batch(func() {
		v.fit.Set("")
		v.scale.Set(scale)
		v.offset.Set(v.clampOffset(next, scale))
	})
}

func (v *viewer) center() core.Point {
	vp := v.viewport.Get()
	return core.Point{X: vp.Width / 2, Y: vp.Height / 2}
}
