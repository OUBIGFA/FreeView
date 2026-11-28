package main

// Animation decoding and playback share the lifetime of the displayed image.
import (
	"errors"
	"slices"
	"strconv"
	"time"

	"github.com/nsgo-dev/nsgo/core"

	"github.com/OUBIGFA/FreeView/cmd/freeview/locales"
	"github.com/OUBIGFA/FreeView/internal/anim"
	"github.com/OUBIGFA/FreeView/internal/settings"
)

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
	v.background(func() {
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
	}
}
