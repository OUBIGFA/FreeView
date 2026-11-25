package main

// The features off the fast path. Showing an image stays what it was:
// read the file, hand the bytes to nsgo, show it; none of this file runs
// before the image is on screen, and nothing here blocks the engine's
// goroutine. Everything heavy (decoding pixels in full, color transforms,
// resampling, thumbnails, animation frames, metadata) runs on at most
// heavyWorkers goroutines, starts only when its feature is on and needed,
// and its result is dropped when the user has moved on meanwhile. While
// browsing quickly the timers keep restarting, so nothing heavy starts.

import (
	"bytes"
	"errors"
	"image"
	"image/draw"
	_ "image/gif" // decoders for archive thumbnails
	_ "image/jpeg"
	_ "image/png"
	"math"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	_ "golang.org/x/image/webp"

	"github.com/OUBIGFA/FreeView/cmd/freeview/locales"
	"github.com/OUBIGFA/FreeView/internal/anim"
	"github.com/OUBIGFA/FreeView/internal/catalog"
	"github.com/OUBIGFA/FreeView/internal/colorprofile"
	"github.com/OUBIGFA/FreeView/internal/livephoto"
	"github.com/OUBIGFA/FreeView/internal/meta"
	"github.com/OUBIGFA/FreeView/internal/picture"
	"github.com/OUBIGFA/FreeView/internal/resample"
	"github.com/OUBIGFA/FreeView/internal/settings"
	"github.com/OUBIGFA/FreeView/internal/thumbs"
	"github.com/OUBIGFA/FreeView/internal/winmouse"
)

const (
	// heavyWorkers bounds the goroutines doing heavy work, leaving the
	// other cores to nsgo's decoding and drawing of the shown image.
	heavyWorkers = 2
	// settleDelay is how long the view must stay still before it is
	// resampled: zooming and panning use Direct2D's quick filtering.
	settleDelay = 250 * time.Millisecond
	// smoothBelow is the scale under which the view is resampled; above
	// it Direct2D's linear filtering looks the same.
	smoothBelow = 0.95
	// smoothMaxPixels bounds a resampled view, about a 5K screen.
	smoothMaxPixels = 16 << 20
	// navSize is the overview's longest side in points.
	navSize = 180
	// stripHeight, stripCell and thumbSize lay out the strip of
	// thumbnails; thumbCache is how many thumbnails stay.
	stripHeight = 96
	stripCell   = 80
	thumbSize   = 144
	thumbCache  = 400
	// promptShown bounds the prompt in the information overlay.
	promptShown = 600
)

var heavy = make(chan struct{}, heavyWorkers)

// background runs fn on a heavy worker, waiting for one to be free, at
// below-normal priority: the fast path never waits for a core behind it.
func background(fn func()) {
	go func() {
		heavy <- struct{}{}
		defer func() { <-heavy }()
		lowPriority(fn)
	}()
}

// extras is the state of the features off the fast path; the engine's
// goroutine owns it.
type extras struct {
	seq      uint64 // bumped for every image committed; results check it
	animated bool   // the shown image is an animated GIF, APNG or WebP
	// base is the shown image's pixels decoded in full, color-managed
	// when it has a profile and color management is on; nil until a
	// feature needed it. waiting are the features waiting for it.
	base    *image.RGBA
	waiting []func(*image.RGBA)

	smooth      *core.Signal[core.ImageSource] // the resampled view
	smoothScale float64                        // the scale smooth is for, 0 for none
	smoothSeq   uint64

	nav    *core.Signal[navThumb]
	navSeq uint64 // the image nav is for or being made for

	meta    *core.Signal[details]
	metaSeq uint64

	stripOn     *core.Signal[bool]
	thumbs      map[string]*image.RGBA
	thumbOrder  []string
	thumbAsked  map[string]bool
	thumbsAdded *core.Signal[int]

	player   *anim.Player
	frames   *anim.Animation
	paused   bool
	tickAt   time.Time
	tickSeq  uint64
	decoding bool
	pending  []func()

	renaming *core.Signal[bool]

	display     []byte // the monitor's ICC profile, nil for sRGB
	displayRead bool
}

// navThumb is the overview's picture: upright when it came from the
// shell, which applies the EXIF orientation, else as the file stores it.
type navThumb struct {
	src     core.ImageSource
	upright bool
	seq     uint64
}

// details are what the information overlay adds once it opens.
type details struct {
	info meta.Info
	live bool
	seq  uint64
}

func (v *viewer) initExtras(cx *core.Scope) {
	e := &v.extras
	e.smooth = core.NewSignalFunc(cx, core.ImageSource{}, never[core.ImageSource])
	e.nav = core.NewSignalFunc(cx, navThumb{}, never[navThumb])
	e.meta = core.NewSignalFunc(cx, details{}, never[details])
	e.stripOn = core.NewSignal(cx, false)
	e.thumbsAdded = core.NewSignal(cx, 0)
	e.renaming = core.NewSignal(cx, false)
	e.thumbs, e.thumbAsked = map[string]*image.RGBA{}, map[string]bool{}
	cx.Effect(v.scheduleSmooth)
	cx.Effect(v.wantNav)
	cx.Effect(v.wantDetails)
}

// commitExtras starts over for the image just committed.
func (v *viewer) commitExtras(s shown) {
	e := &v.extras
	e.seq++
	e.base, e.waiting = nil, nil
	e.smoothScale = 0
	e.smooth.Set(core.ImageSource{})
	e.player, e.frames, e.paused, e.decoding, e.pending = nil, nil, false, false, nil
	e.tickSeq++
	e.renaming.Set(false)
	p := s.pic
	e.animated = s.err == nil && (p.Format == "gif" || p.Format == "png" || p.Format == "webp") && anim.IsAnimated(p.Data)
	if s.err != nil || s.src.Empty() {
		return
	}
	cfg := v.cfg.Get()
	switch {
	case e.animated && (p.Format == "png" || cfg.AnimationSpeed != 1):
		// nsgo shows APNG as a still and plays the others at normal speed.
		v.withAnimation(func() {})
	case !e.animated && cfg.ColorManagement && p.Data != nil && p.Format != "svg":
		seq, data := e.seq, p.Data
		background(func() {
			if !needsTransform(data) {
				return
			}
			v.cx.Post(func() {
				if seq == e.seq {
					v.withBase(func(*image.RGBA) {})
				}
			})
		})
	}
}

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
	background(func() {
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
	return !e.smooth.Get().Empty() && e.smoothScale == v.currentScale()
}

// scheduleSmooth resamples the view with area averaging once it stays
// still at a scale below smoothBelow: Direct2D's linear filter aliases
// fine detail below 50%. As an effect it follows the image, the zoom and
// the window's size.
func (v *viewer) scheduleSmooth() {
	e := &v.extras
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
	time.AfterFunc(settleDelay, func() {
		v.cx.Post(func() {
			if seq != e.seq || mine != e.smoothSeq {
				return
			}
			done := func(out *image.RGBA) {
				v.cx.Post(func() {
					if seq == e.seq && out != nil && v.currentScale() == scale {
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
				background(func() { done(resample.Region(src, src.Rect, w, h)) })
				return
			}
			// Encoded bytes are decoded straight at the size shown, so a
			// large photo is never decoded in full for this.
			data, target := s.pic.Data, v.colorTarget()
			background(func() {
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

// withAnimation calls fn once the shown animation's frames are decoded,
// decoding them on a heavy worker the first time, and from then on plays
// them itself instead of nsgo.
func (v *viewer) withAnimation(fn func()) {
	e := &v.extras
	if !e.animated {
		v.flash(locales.NotAnimated(v.cx))
		return
	}
	if e.player != nil {
		fn()
		return
	}
	e.pending = append(e.pending, fn)
	if e.decoding {
		return
	}
	e.decoding = true
	seq, data := e.seq, v.img.Get().pic.Data
	background(func() {
		a, err := anim.Decode(data, cacheBytes/2)
		v.cx.Post(func() {
			if seq != e.seq {
				return
			}
			e.decoding = false
			pending := e.pending
			e.pending = nil
			if err != nil {
				if errors.Is(err, anim.ErrTooLarge) {
					v.flash(locales.AnimTooLarge(v.cx))
				}
				return
			}
			e.frames, e.player = a, anim.NewPlayer(a)
			v.showFrame()
			v.tick()
			for _, fn := range pending {
				fn()
			}
		})
	})
}

func (v *viewer) showFrame() {
	e := &v.extras
	v.slots[v.front.Get()].Set(core.ImageSource{Pixels: e.frames.Frames[e.player.Frame()].Image})
}

// tick waits for the next frame while the animation plays. The time is
// measured, not assumed, so a late timer catches up.
func (v *viewer) tick() {
	e := &v.extras
	e.tickSeq++
	if e.paused || e.player.Done() {
		return
	}
	seq, mine := e.seq, e.tickSeq
	e.tickAt = time.Now()
	time.AfterFunc(e.player.Remaining(v.cfg.Get().AnimationSpeed), func() {
		v.cx.Post(func() {
			if seq != e.seq || mine != e.tickSeq {
				return
			}
			now := time.Now()
			if e.player.Advance(now.Sub(e.tickAt), v.cfg.Get().AnimationSpeed) {
				v.showFrame()
			}
			v.tick()
		})
	})
}

// runAnimation carries out the animation actions.
func (v *viewer) runAnimation(a settings.Action) {
	e := &v.extras
	switch a {
	case settings.AnimPause:
		v.withAnimation(func() {
			e.paused = !e.paused
			if !e.paused && e.player.Done() {
				e.player.Reset()
				v.showFrame()
			}
			v.tick()
			v.flash(pick(e.paused, locales.AnimPaused(v.cx), locales.AnimPlaying(v.cx)))
		})
	case settings.AnimPrev, settings.AnimNext:
		v.withAnimation(func() {
			e.paused = true
			e.tickSeq++
			e.player.Step(pick(a == settings.AnimNext, 1, -1))
			v.showFrame()
			v.flash(locales.AnimFrame(v.cx, strconv.Itoa(e.player.Frame()+1), strconv.Itoa(len(e.frames.Frames))))
		})
	case settings.AnimFaster, settings.AnimSlower:
		speeds := settings.AnimationSpeeds
		i := max(0, slices.Index(speeds, v.cfg.Get().AnimationSpeed))
		i = min(max(i+pick(a == settings.AnimFaster, 1, -1), 0), len(speeds)-1)
		v.update(func(c *settings.Config) { c.AnimationSpeed = speeds[i] })
		v.flash(locales.AnimSpeed(v.cx, strconv.FormatFloat(speeds[i], 'f', -1, 64)))
		if e.animated {
			v.withAnimation(v.tick)
		}
	}
}

// wantNav makes the overview's picture once the overview shows for an
// image. Files get the shell's thumbnail, which Explorer has likely
// cached; archive entries are scaled from their pixels.
func (v *viewer) wantNav() {
	e := &v.extras
	if !v.navShown() || e.navSeq == e.seq {
		return
	}
	e.navSeq = e.seq
	seq, s, cat := e.seq, v.img.Get(), v.cat
	done := func(img *image.RGBA, upright bool) {
		v.cx.Post(func() {
			if seq == e.seq && img != nil {
				e.nav.Set(navThumb{src: core.ImageSource{Pixels: img}, upright: upright, seq: seq})
			}
		})
	}
	fromPixels := func() {
		v.withBase(func(src *image.RGBA) { background(func() { done(thumbs.Image(src, navSize*2), false) }) })
	}
	if cat.Archive != "" {
		fromPixels()
		return
	}
	background(func() {
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
			return core.Style{Position: core.Absolute, Right: core.Pt(12), Bottom: core.Pt(12),
				Width: core.Pt(core.Points(nw)), Height: core.Pt(core.Points(nh)), Overflow: core.Hidden,
				Background: core.RGBA(0x000000, 0.55), BorderWidth: 1, BorderColor: core.RGBA(0xffffff, 0.35)}
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

// hoverStrip shows the strip of thumbnails while the pointer is near the
// bottom of the viewport, when the strip is on.
func (v *viewer) hoverStrip(p core.Point) {
	on := v.cfg.Get().Filmstrip && v.count.Get() > 1 && float64(p.Y) > float64(v.viewport.Get().Height)-stripHeight-12
	if on != v.extras.stripOn.GetUntracked() {
		v.extras.stripOn.Set(on)
	}
}

// stripRange is the items the strip shows: as many as fit, around the
// shown one.
func (v *viewer) stripRange() (from, to int) {
	n := max(1, int(v.viewport.Get().Width)/stripCell)
	count, i := v.count.Get(), v.index.Get()
	from = max(0, min(i-n/2, count-n))
	return from, min(count, from+n)
}

func (v *viewer) filmstrip(cx *core.Scope) core.Node {
	e := &v.extras
	return core.View{TestID: "filmstrip",
		Style: core.Style{Position: core.Absolute, Left: core.Pt(0), Right: core.Pt(0), Bottom: core.Pt(0), Height: core.Pt(stripHeight),
			Direction: core.Row, JustifyContent: core.JustifyCenter, AlignItems: core.AlignCenter, Gap: 8,
			Background: core.RGBA(0x000000, 0.6)},
		Children: []core.Node{core.Dynamic(func(cx *core.Scope) core.Node {
			from, to := v.stripRange()
			cat := v.cat
			var cells []core.Node
			for j := from; j < to && cat != nil && j < len(cat.Items); j++ {
				item := cat.Items[j]
				v.wantThumb(cat, item)
				cells = append(cells, core.View{TestID: "strip-" + strconv.Itoa(j), OnPress: func(core.PressEvent) { v.show(j) },
					StyleFunc: func() core.Style {
						border := core.RGBA(0xffffff, 0.15)
						if v.index.Get() == j {
							border = core.RGB(0xffffff)
						}
						return core.Style{Width: core.Pt(stripCell - 8), Height: core.Pt(stripCell - 8), BorderWidth: 2, BorderColor: border,
							AlignItems: core.AlignCenter, JustifyContent: core.JustifyCenter, Overflow: core.Hidden}
					},
					Children: []core.Node{core.Image{ResizeMode: core.Contain, SourceFunc: func() core.ImageSource {
						e.thumbsAdded.Get()
						if t := e.thumbs[thumbKey(cat, item)]; t != nil {
							return core.ImageSource{Pixels: t}
						}
						return core.ImageSource{}
					}, Style: core.Style{Width: core.Pct(100), Height: core.Pct(100), PointerEvents: core.PointerEventsNone}}}})
			}
			return core.Group(cells)
		})}}
}

func thumbKey(cat *catalog.Catalog, item catalog.Item) string { return cat.Archive + "|" + item.Path }

// wantThumb makes item's thumbnail on a heavy worker unless it is made or
// being made. Files get the shell's; archive entries are decoded here.
func (v *viewer) wantThumb(cat *catalog.Catalog, item catalog.Item) {
	e := &v.extras
	key := thumbKey(cat, item)
	if e.thumbs[key] != nil || e.thumbAsked[key] {
		return
	}
	e.thumbAsked[key] = true
	background(func() {
		img, err := makeThumb(cat, item)
		v.cx.Post(func() {
			delete(e.thumbAsked, key)
			if err != nil || img == nil {
				return
			}
			e.thumbs[key] = img
			e.thumbOrder = append(e.thumbOrder, key)
			if len(e.thumbOrder) > thumbCache {
				delete(e.thumbs, e.thumbOrder[0])
				e.thumbOrder = e.thumbOrder[1:]
			}
			e.thumbsAdded.Update(func(n int) int { return n + 1 })
		})
	})
}

func makeThumb(cat *catalog.Catalog, item catalog.Item) (*image.RGBA, error) {
	if cat.Archive == "" {
		if img, err := thumbs.File(item.Path, thumbSize); err == nil {
			return img, nil
		}
	}
	pic, err := readPicture(cat, item)
	if err != nil {
		return nil, err
	}
	if pic.Pixels != nil {
		return thumbs.Image(pic.Pixels, thumbSize), nil
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(pic.Data))
	if err != nil {
		return nil, err
	}
	if int64(cfg.Width)*int64(cfg.Height) > 1<<28 {
		return nil, errors.New("too large for a thumbnail")
	}
	m, _, err := image.Decode(bytes.NewReader(pic.Data))
	if err != nil {
		return nil, err
	}
	rgba, ok := m.(*image.RGBA)
	if !ok {
		rgba = image.NewRGBA(m.Bounds())
		draw.Draw(rgba, rgba.Rect, m, m.Bounds().Min, draw.Src)
	}
	return thumbs.Image(rgba, thumbSize), nil
}

// wantDetails reads the shown image's details once the information
// overlay shows it.
func (v *viewer) wantDetails() {
	e := &v.extras
	if !v.info.Get() || v.img.Get().src.Empty() || e.metaSeq == e.seq {
		return
	}
	e.metaSeq = e.seq
	seq, s, cat := e.seq, v.img.Get(), v.cat
	background(func() {
		data := s.pic.Data
		if data == nil || s.pic.Format == "svg" {
			var err error
			if data, err = cat.Read(s.item); err != nil {
				return
			}
		}
		d := details{info: meta.Read(data), seq: seq}
		if livephoto.IsLIVP(s.item.Name) {
			d.live = true
		} else {
			_, d.live = livephoto.FindVideo(data)
		}
		v.cx.Post(func() {
			if seq == e.seq {
				e.meta.Set(d)
			}
		})
	})
}

// detailsText is the overlay's lines past the first: the details read so
// far and the read-ahead cache's counts.
func (v *viewer) detailsText() string {
	cx, e := v.cx, &v.extras
	var b strings.Builder
	if d := e.meta.Get(); d.seq == e.seq {
		for _, f := range d.info.Fields {
			b.WriteString("\n" + detailLabel(cx, f.Key) + "  " + f.Value)
		}
		if d.live {
			b.WriteString("\n" + locales.InfoLive(cx))
		}
		if p := d.info.Prompt; p != "" {
			if r := []rune(p); len(r) > promptShown {
				p = string(r[:promptShown]) + "…"
			}
			b.WriteString("\n" + locales.InfoPrompt(cx) + "\n" + p)
		}
	}
	st := v.stats
	b.WriteString("\n" + locales.InfoCache(cx, strconv.Itoa(st.hits), strconv.Itoa(st.reads), strconv.Itoa(st.evictions)))
	return b.String()
}

func detailLabel(cx *core.Scope, key string) string {
	switch key {
	case meta.Camera:
		return locales.InfoCamera(cx)
	case meta.Lens:
		return locales.InfoLens(cx)
	case meta.Exposure:
		return locales.InfoExposure(cx)
	case meta.Taken:
		return locales.InfoTaken(cx)
	case meta.GPS:
		return locales.InfoGPS(cx)
	case meta.Software:
		return locales.InfoSoftware(cx)
	case meta.Comment:
		return locales.InfoComment(cx)
	}
	return key
}

// edgeAction is what a click at p does with edge clicks on: the left and
// right sixths move, their top corners turn the image.
func (v *viewer) edgeAction(p core.Point) settings.Action {
	vp := v.viewport.Get()
	x, y := float64(p.X)/float64(vp.Width), float64(p.Y)/float64(vp.Height)
	corner := y < 0.15
	switch {
	case x < 1.0/6 && corner:
		return settings.RotateLeft
	case x > 5.0/6 && corner:
		return settings.RotateRight
	case x < 1.0/6:
		return settings.Previous
	case x > 5.0/6:
		return settings.Next
	}
	return settings.None
}
