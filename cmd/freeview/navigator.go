package main

// Overview geometry and rendering use the same transforms as the displayed image.
import (
	"image"
	"math"

	"github.com/nsgo-dev/nsgo/core"

	"github.com/OUBIGFA/FreeView/internal/picture"
	"github.com/OUBIGFA/FreeView/internal/thumbs"
)

// wantNav makes the overview's picture once the overview shows for an
// image. Files get the shell's thumbnail, which Explorer has likely
// cached; archive entries are scaled from their pixels.
func (v *viewer) wantNav() {
	e := &v.extras
	if !v.navShown() || e.navSeq == e.seq {
		return
	}
	e.navSeq = e.seq
	seq, s := e.seq, v.img.Get()
	cat := s.cat
	done := func(img *image.RGBA, upright bool) {
		v.cx.Post(func() {
			if seq == e.seq && img != nil {
				e.nav.Set(navThumb{src: core.ImageSource{Pixels: img}, upright: upright, seq: seq})
			}
		})
	}
	fromPixels := func() {
		v.withBase(func(src *image.RGBA) { v.background(func() { done(thumbs.Image(src, navSize*2), false) }) })
	}
	if cat.Archive != "" {
		fromPixels()
		return
	}
	v.background(func() {
		img, err := thumbs.File(s.item.Path, navSize*2)
		if err != nil {
			v.cx.Post(func() {
				if seq == e.seq {
					fromPixels()
				}
			})
			return
		}
		done(img, true)
	})
}

// navShown reports whether the overview shows: turned on, and the image
// overflows the window.
func (v *viewer) navShown() bool {
	return v.cfg.Get().Navigator && v.pannable()
}

// navGeometry lays the overview out: its size in points, the scale from
// the displayed image's points to it, and the image's displayed box.
func (v *viewer) navGeometry() (nw, nh, k, left, top, dw, dh float64) {
	x, y, w, h := v.imageBox()
	dw, dh = w, h
	if v.rotate.Get()%180 != 0 {
		dw, dh = h, w
	}
	k = navSize / math.Max(dw, dh)
	return dw * k, dh * k, k, x - dw/2, y - dh/2, dw, dh
}

// navTransform is the rotation and mirror that show the overview's
// picture as the image shows. A stored picture takes the image's own; an
// upright one, which the EXIF orientation (r0, m0) already turned, takes
// what remains: with T(r, m) mirroring then rotating, T(r, m) of the
// stored image is T(r-r0, false) of the upright one when m == m0, and
// T(r+r0, true) when not, since a mirror reverses a rotation.
func (v *viewer) navTransform(upright bool) (int, bool) {
	r, m := v.rotate.Get(), v.mirror.Get()
	if !upright {
		return r, m
	}
	r0, m0 := picture.OrientationTransform(v.img.Get().pic.Orientation)
	if m == m0 {
		return ((r-r0)%360 + 360) % 360, false
	}
	return (r + r0) % 360, true
}

// navigate centers the view on the point p of the overview.
func (v *viewer) navigate(p core.Point) {
	_, _, k, _, _, dw, dh := v.navGeometry()
	o := core.Point{X: core.Points(dw/2 - float64(p.X)/k), Y: core.Points(dh/2 - float64(p.Y)/k)}
	v.offset.Set(v.clampOffset(o, v.currentScale()))
}

func (v *viewer) navigator(cx *core.Scope) core.Node {
	e := &v.extras
	return core.View{TestID: "navigator",
		StyleFunc: func() core.Style {
			nw, nh, _, _, _, _, _ := v.navGeometry()
			return core.Style{Position: core.Absolute, Right: core.Pt(16), Bottom: core.Pt(v.bottomOverlayInset()),
				Width: core.Pt(core.Points(nw)), Height: core.Pt(core.Points(nh)), Overflow: core.Hidden,
				Background: core.RGBA(overlayColor, 0.96), CornerRadius: radiusControl, BorderWidth: 1, BorderColor: core.RGB(0x666666)}
		},
		OnPressDown: func(ev core.PressEvent) { v.navigate(ev.Position) },
		OnPan:       func(ev core.PanEvent) { v.navigate(ev.Position) },
		Children: []core.Node{
			core.Image{TestID: "navigator-image", SourceFunc: func() core.ImageSource {
				if t := e.nav.Get(); t.seq == e.seq {
					return t.src
				}
				return core.ImageSource{}
			}, ResizeMode: core.Stretch, StyleFunc: func() core.Style {
				nw, nh, _, _, _, _, _ := v.navGeometry()
				rot, mir := v.navTransform(e.nav.Get().upright)
				bw, bh := nw, nh
				if rot%180 != 0 {
					bw, bh = nh, nw
				}
				return core.Style{Position: core.Absolute, Left: core.Pt(core.Points((nw - bw) / 2)), Top: core.Pt(core.Points((nh - bh) / 2)),
					Width: core.Pt(core.Points(bw)), Height: core.Pt(core.Points(bh)), PointerEvents: core.PointerEventsNone,
					Transform: core.Transform{Rotate: float32(rot), ScaleX: pick[float32](mir, -1, 1), ScaleY: 1}}
			}},
			core.View{TestID: "navigator-view", StyleFunc: func() core.Style {
				_, _, k, left, top, dw, dh := v.navGeometry()
				vp := v.viewport.Get()
				x0, y0 := math.Max(0, -left), math.Max(0, -top)
				x1, y1 := math.Min(dw, float64(vp.Width)-left), math.Min(dh, float64(vp.Height)-top)
				return core.Style{Position: core.Absolute, Left: core.Pt(core.Points(x0 * k)), Top: core.Pt(core.Points(y0 * k)),
					Width: core.Pt(core.Points(math.Max(2, (x1-x0)*k))), Height: core.Pt(core.Points(math.Max(2, (y1-y0)*k))),
					BorderWidth: 1.5, BorderColor: core.RGB(0xffffff), Background: core.RGBA(0xffffff, 0.12),
					PointerEvents: core.PointerEventsNone}
			}},
		}}
}
