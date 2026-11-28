package main

// Color conversion and settled-view resampling; all pixel work stays on background workers.
import (
	"context"
	"image"
	"math"
	"time"

	"github.com/nsgo-dev/nsgo/core"

	"github.com/OUBIGFA/FreeView/internal/colorprofile"
	"github.com/OUBIGFA/FreeView/internal/resample"
	"github.com/OUBIGFA/FreeView/internal/winmouse"
)

// needsTransform reports whether data embeds a profile other than sRGB.
func needsTransform(data []byte) bool {
	prof := colorprofile.Embedded(data)
	return prof != nil && !colorprofile.IsSRGB(prof)
}

// withBase calls fn with the shown image's full pixels, decoding them on
// a heavy worker the first time. Decoded under color management, they
// also replace what nsgo shows, which draws without it.
func (v *viewer) withBase(fn func(*image.RGBA)) {
	e := &v.extras
	s := v.img.Get()
	switch {
	case e.base != nil:
		fn(e.base)
		return
	case s.pic.Pixels != nil && !(v.cfg.Get().ColorManagement && s.pic.Data != nil):
		fn(s.pic.Pixels)
		return
	case s.pic.Data == nil || s.pic.Format == "svg":
		return
	}
	e.waiting = append(e.waiting, fn)
	if len(e.waiting) > 1 {
		return
	}
	seq, data := e.seq, s.pic.Data
	target := v.colorTarget()
	v.background(func() {
		to, converted := target(data)
		img, err := colorprofile.Convert(data, to)
		v.cx.Post(func() {
			if seq != e.seq {
				return
			}
			waiting := e.waiting
			e.waiting = nil
			if err != nil {
				return
			}
			e.base = img
			if converted && e.frames == nil {
				v.slots[v.front.Get()].Set(core.ImageSource{Pixels: img})
			}
			for _, fn := range waiting {
				fn(img)
			}
		})
	})
}

// colorTarget returns, for a heavy worker to call, what converts data's
// colors: the monitor's profile when color management is on and data
// embeds a profile other than sRGB (converted), else data's own profile,
// with which WIC changes no color. The monitor's profile is read once.
func (v *viewer) colorTarget() func(data []byte) (to []byte, converted bool) {
	e := &v.extras
	manage, display, read := v.cfg.Get().ColorManagement, e.display, e.displayRead
	hwnd := winmouse.Window()
	return func(data []byte) ([]byte, bool) {
		prof := colorprofile.Embedded(data)
		if !manage || prof == nil || colorprofile.IsSRGB(prof) {
			return prof, false
		}
		if !read {
			display, _ = colorprofile.Display(hwnd)
			v.cx.Post(func() { e.display, e.displayRead = display, true })
		}
		return display, true
	}
}

// smoothShown reports whether the resampled view is up to date, and so
// shows instead of nsgo's.
func (v *viewer) smoothShown() bool {
	e := &v.extras
	return v.cfg.Get().SmoothZoom && !e.smooth.Get().Empty() && e.smoothScale == v.currentScale()
}

// scheduleSmooth resamples the view with area averaging once it stays
// still at a scale below smoothBelow: Direct2D's linear filter aliases
// fine detail below 50%. As an effect it follows the image, the zoom and
// the window's size.
func (v *viewer) scheduleSmooth() {
	e := &v.extras
	if e.smoothCancel != nil {
		e.smoothCancel()
	}
	e.smoothSeq++
	s := v.img.Get()
	scale := v.currentScale()
	if !v.cfg.Get().SmoothZoom || s.err != nil || s.src.Empty() || e.animated || s.pic.Format == "svg" ||
		scale >= smoothBelow || scale == e.smoothScale {
		return
	}
	w, h := int(math.Round(float64(s.pic.Width)*scale)), int(math.Round(float64(s.pic.Height)*scale))
	if w < 1 || h < 1 || w*h > smoothMaxPixels {
		return
	}
	seq, mine := e.seq, e.smoothSeq
	ctx, cancel := context.WithCancel(e.work)
	e.smoothCancel = cancel
	time.AfterFunc(settleDelay, func() {
		v.cx.Post(func() {
			if seq != e.seq || mine != e.smoothSeq {
				return
			}
			done := func(out *image.RGBA) {
				v.cx.Post(func() {
					if seq == e.seq && mine == e.smoothSeq && out != nil && v.cfg.Get().SmoothZoom && v.currentScale() == scale {
						e.smoothScale = scale
						e.smooth.Set(core.ImageSource{Pixels: out})
					}
				})
			}
			src := e.base
			if src == nil {
				src = s.pic.Pixels
			}
			if src != nil {
				background(ctx, func() { done(resample.Region(src, src.Rect, w, h)) })
				return
			}
			// Encoded bytes are decoded straight at the size shown, so a
			// large photo is never decoded in full for this.
			data, target := s.pic.Data, v.colorTarget()
			background(ctx, func() {
				to, _ := target(data)
				out, err := colorprofile.ConvertScaled(data, to, w, h)
				if err == nil {
					done(out)
				}
			})
		})
	})
}

// smoothView is the resampled view over the shown one.
func (v *viewer) smoothView() core.Node {
	return core.Image{TestID: "image-smooth", SourceFunc: v.extras.smooth.Get, ResizeMode: core.Stretch,
		StyleFunc: func() core.Style {
			if v.smoothShown() {
				return v.imageStyle().Merge(core.Style{PointerEvents: core.PointerEventsNone})
			}
			return hiddenStyle
		}}
}

var hiddenStyle = core.Style{Position: core.Absolute, Left: core.Pt(0), Top: core.Pt(0), Width: core.Pt(1), Height: core.Pt(1),
	Opacity: core.Alpha(0), PointerEvents: core.PointerEventsNone}
