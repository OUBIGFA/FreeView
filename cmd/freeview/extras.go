package main

// The features off the fast path. Showing an image stays what it was:
// read the file, hand the bytes to nsgo, show it; none of this file runs
// before the image is on screen, and nothing here blocks the engine's
// goroutine. Everything heavy (decoding pixels in full, color transforms,
// resampling, thumbnails, animation frames, metadata) runs on at most
// heavyWorkers goroutines, starts only when its feature is on and needed,
// and its result is dropped when the user has moved on meanwhile. Settled
// resampling restarts its timer while browsing; queued obsolete work is canceled.

import (
	"context"
	"image"
	"time"

	"github.com/nsgo-dev/nsgo/core"

	"github.com/OUBIGFA/FreeView/internal/anim"
	"github.com/OUBIGFA/FreeView/internal/meta"
	"github.com/OUBIGFA/FreeView/internal/settings"
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
func background(ctx context.Context, fn func()) {
	go func() {
		select {
		case heavy <- struct{}{}:
		case <-ctx.Done():
			return
		}
		defer func() { <-heavy }()
		if ctx.Err() != nil {
			return
		}
		lowPriority(fn)
	}()
}

// background captures the image lifetime on the engine goroutine. Work
// waiting for a slot is discarded when the image is replaced or closed.
func (v *viewer) background(fn func()) { background(v.extras.work, fn) }

func (v *viewer) cancelExtras() {
	if v.extras.cancel != nil {
		v.extras.cancel()
	}
}

// extras is the state of the features off the fast path; the engine's
// goroutine owns it.
type extras struct {
	work         context.Context
	cancel       context.CancelFunc
	smoothCancel context.CancelFunc
	thumbWork    context.Context // thumbnail requests belong to the catalog, not one selected image
	thumbCancel  context.CancelFunc
	seq          uint64 // bumped for every image committed; results check it
	animated     bool   // the shown image is an animated GIF, APNG or WebP
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
	e.work, e.cancel = context.WithCancel(context.Background())
	e.thumbWork, e.thumbCancel = context.WithCancel(context.Background())
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
	v.cancelExtras()
	e.work, e.cancel = context.WithCancel(context.Background())
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
		v.background(func() {
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
