package main

// Actions dispatch shared keyboard, mouse and menu commands.
import (
	"path/filepath"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/desktop"

	"github.com/OUBIGFA/FreeView/cmd/freeview/locales"
	"github.com/OUBIGFA/FreeView/internal/settings"
	"github.com/OUBIGFA/FreeView/internal/winmouse"
)

// step moves delta images, as next and previous do.
func (v *viewer) step(delta int) {
	if v.cat == nil || len(v.cat.Items) == 0 {
		return
	}
	j := v.cat.Step(v.index.Get(), delta, v.cfg.Get().Loop)
	if j != v.index.Get() {
		v.show(j)
	}
}

// run carries out action; anchor is where the pointer was for zooms, nil
// for the viewport's center.
func (v *viewer) run(action settings.Action, anchor *core.Point) {
	cx := v.cx
	at := v.center()
	if anchor != nil {
		at = *anchor
	}
	step := v.cfg.Get().ZoomStep
	switch action {
	case settings.Next:
		v.step(1)
	case settings.Previous:
		v.step(-1)
	case settings.First:
		v.show(0)
	case settings.Last:
		if v.cat != nil {
			v.show(len(v.cat.Items) - 1)
		}
	case settings.ZoomIn:
		v.zoomTo(v.currentScale()*step, at)
	case settings.ZoomOut:
		v.zoomTo(v.currentScale()/step, at)
	case settings.ZoomFit:
		mode := v.cfg.Get().Fit
		if mode == settings.FitOriginal {
			mode = settings.FitShrink
		}
		cx.Batch(func() { v.fit.Set(mode); v.offset.Set(core.Point{}) })
	case settings.ZoomOriginal:
		v.zoomTo(1, at)
	case settings.RotateLeft, settings.RotateRight:
		d := 90
		if action == settings.RotateLeft {
			d = 270
		}
		v.transform(func(rot int, mir bool) (int, bool) { return rot + d, mir })
	case settings.FlipH:
		// Mirroring the screen after a rotation turns the rotation back.
		v.transform(func(rot int, mir bool) (int, bool) { return 360 - rot, !mir })
	case settings.FlipV:
		v.transform(func(rot int, mir bool) (int, bool) { return 540 - rot, !mir })
	case settings.FullScreen:
		desktop.ToggleFullScreen(cx)
	case settings.Escape:
		if v.extras.renaming.Get() {
			v.extras.renaming.Set(false)
		} else if desktop.FullScreen(cx) {
			desktop.ToggleFullScreen(cx)
		} else {
			winmouse.CloseQuietly()
			cx.Quit()
		}
	case settings.Close:
		// Quit rather than close the main window: nsgo v0.2.0 on Windows
		// hides the main window of an App without ending the process.
		winmouse.CloseQuietly()
		cx.Quit()
	case settings.Minimize:
		desktop.MinimizeWindow(cx)
	case settings.Open:
		v.pick()
	case settings.Reveal:
		if p := v.currentFile(); p != "" {
			desktop.RevealFile(cx, p, func(error) {})
		}
	case settings.Delete:
		v.delete()
	case settings.CopyPath:
		if p := v.currentFile(); p != "" {
			if v.img.Get().cat.Archive != "" {
				p = filepath.Join(p, filepath.FromSlash(v.img.Get().item.Path))
			}
			cx.SetClipboard(p)
			v.flash(locales.CopiedPath(cx))
		}
	case settings.CopyImage:
		v.copyImage()
	case settings.Slideshow:
		on := !v.slideshow.Get()
		v.slideshow.Set(on)
		if on {
			v.flash(locales.SlideshowOn(cx))
		} else {
			v.flash(locales.SlideshowOff(cx))
		}
	case settings.Info:
		v.info.Set(!v.info.Get())
	case settings.AlwaysOnTop:
		v.update(func(c *settings.Config) { c.AlwaysOnTop = !c.AlwaysOnTop })
	case settings.TitleBar:
		v.update(func(c *settings.Config) { c.ShowTitleBar = !c.ShowTitleBar })
	case settings.Menu:
		v.menuAt.Set(v.pointer)
		v.menuOpen.Set(true)
	case settings.Preferences:
		openPreferences(v)
	case settings.Navigator:
		v.update(func(c *settings.Config) { c.Navigator = !c.Navigator })
	case settings.Filmstrip:
		v.update(func(c *settings.Config) { c.Filmstrip = !c.Filmstrip })
	case settings.Rename, settings.CopyTo, settings.MoveTo, settings.OpenWith, settings.Properties, settings.ExportVideo:
		v.runFile(action)
	case settings.AnimPause, settings.AnimPrev, settings.AnimNext, settings.AnimFaster, settings.AnimSlower:
		v.runAnimation(action)
	}
}
