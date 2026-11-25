package main

import (
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/desktop"

	"github.com/OUBIGFA/FreeView/cmd/freeview/locales"
	"github.com/OUBIGFA/FreeView/internal/settings"
	"github.com/OUBIGFA/FreeView/internal/winmouse"
)

// repeating are the actions a held key repeats.
var repeating = map[settings.Action]bool{settings.Next: true, settings.Previous: true, settings.ZoomIn: true, settings.ZoomOut: true}

// bindKeys binds every shortcut of the preferences, binding them again
// whenever the preferences change: the Dynamic's scope, and with it the
// previous bindings, goes as it runs again.
func (v *viewer) keyBindings() core.Node {
	return core.Dynamic(func(cx *core.Scope) core.Node {
		for action, keys := range v.cfg.Get().Keys {
			for _, key := range keys {
				if key == "" {
					continue
				}
				cx.OnKeyWith(key, core.KeyOptions{Repeat: repeating[action]}, func() { v.run(action, nil) })
			}
		}
		return core.Group{}
	})
}

// attachMouse reports the middle and side buttons, which nsgo does not
// pass to apps on Windows, as gestures, and watches hovering at the top
// of the window.
func attachMouse(v *viewer) {
	winmouse.WatchTop(func(over bool) { v.cx.Post(func() { v.hoverTop(over) }) })
	go func() {
		err := winmouse.Attach(10*time.Second, func(b winmouse.Button) {
			g := map[winmouse.Button]settings.Gesture{winmouse.Middle: settings.MiddleClick,
				winmouse.Back: settings.BackButton, winmouse.Forward: settings.ForwardButton}[b]
			v.cx.Post(func() { at := v.pointer; v.gesture(g, &at) })
		})
		if err != nil {
			v.cx.Post(func() { v.flash(err.Error()) })
		}
	}()
}

// actionLabel names an action in the language the app shows.
func actionLabel(cx *core.Scope, a settings.Action) string {
	switch a {
	case settings.Next:
		return locales.ActionNext(cx)
	case settings.Previous:
		return locales.ActionPrevious(cx)
	case settings.First:
		return locales.ActionFirst(cx)
	case settings.Last:
		return locales.ActionLast(cx)
	case settings.ZoomIn:
		return locales.ActionZoomIn(cx)
	case settings.ZoomOut:
		return locales.ActionZoomOut(cx)
	case settings.ZoomFit:
		return locales.ActionZoomFit(cx)
	case settings.ZoomOriginal:
		return locales.ActionZoomOriginal(cx)
	case settings.RotateLeft:
		return locales.ActionRotateLeft(cx)
	case settings.RotateRight:
		return locales.ActionRotateRight(cx)
	case settings.FlipH:
		return locales.ActionFlipH(cx)
	case settings.FlipV:
		return locales.ActionFlipV(cx)
	case settings.FullScreen:
		return locales.ActionFullScreen(cx)
	case settings.Escape:
		return locales.ActionEscape(cx)
	case settings.Close:
		return locales.ActionClose(cx)
	case settings.Minimize:
		return locales.ActionMinimize(cx)
	case settings.Open:
		return locales.ActionOpen(cx)
	case settings.Reveal:
		return locales.ActionReveal(cx)
	case settings.Delete:
		return locales.ActionDelete(cx)
	case settings.CopyPath:
		return locales.ActionCopyPath(cx)
	case settings.CopyImage:
		return locales.ActionCopyImage(cx)
	case settings.Slideshow:
		return locales.ActionSlideshow(cx)
	case settings.Info:
		return locales.ActionInfo(cx)
	case settings.AlwaysOnTop:
		return locales.ActionAlwaysOnTop(cx)
	case settings.TitleBar:
		return locales.ActionTitleBar(cx)
	case settings.Menu:
		return locales.ActionMenu(cx)
	case settings.Preferences:
		return locales.ActionPreferences(cx)
	case settings.Navigator:
		return locales.ActionNavigator(cx)
	case settings.Filmstrip:
		return locales.ActionFilmstrip(cx)
	case settings.Rename:
		return locales.ActionRename(cx)
	case settings.CopyTo:
		return locales.ActionCopyTo(cx)
	case settings.MoveTo:
		return locales.ActionMoveTo(cx)
	case settings.OpenWith:
		return locales.ActionOpenWith(cx)
	case settings.Properties:
		return locales.ActionProperties(cx)
	case settings.ExportVideo:
		return locales.ActionExportVideo(cx)
	case settings.AnimPause:
		return locales.ActionAnimPause(cx)
	case settings.AnimPrev:
		return locales.ActionAnimPrev(cx)
	case settings.AnimNext:
		return locales.ActionAnimNext(cx)
	case settings.AnimFaster:
		return locales.ActionAnimFaster(cx)
	case settings.AnimSlower:
		return locales.ActionAnimSlower(cx)
	}
	return locales.ActionNone(cx)
}

// gestureLabel names a mouse gesture in the language the app shows.
func gestureLabel(cx *core.Scope, g settings.Gesture) string {
	switch g {
	case settings.WheelUp:
		return locales.GestureWheelUp(cx)
	case settings.WheelDown:
		return locales.GestureWheelDown(cx)
	case settings.CtrlWheelUp:
		return locales.GestureCtrlWheelUp(cx)
	case settings.CtrlWheelDown:
		return locales.GestureCtrlWheelDown(cx)
	case settings.ShiftWheelUp:
		return locales.GestureShiftWheelUp(cx)
	case settings.ShiftWheelDown:
		return locales.GestureShiftWheelDown(cx)
	case settings.AltWheelUp:
		return locales.GestureAltWheelUp(cx)
	case settings.AltWheelDown:
		return locales.GestureAltWheelDown(cx)
	case settings.MiddleClick:
		return locales.GestureMiddleClick(cx)
	case settings.BackButton:
		return locales.GestureBackButton(cx)
	case settings.ForwardButton:
		return locales.GestureForwardButton(cx)
	case settings.LeftDoubleClick:
		return locales.GestureLeftDoubleClick(cx)
	case settings.RightClick:
		return locales.GestureRightClick(cx)
	}
	return string(g)
}

// item is a menu item running action, showing its first shortcut. The
// shortcut is shown only: the key bindings run it before the menu would.
func (v *viewer) item(a settings.Action) desktop.MenuItem {
	shortcut := ""
	if keys := v.cfg.Get().Keys[a]; len(keys) > 0 {
		shortcut = keys[0]
	}
	return desktop.MenuItem{Label: actionLabel(v.cx, a), Shortcut: shortcut, OnSelect: func() { v.run(a, nil) }}
}

func (v *viewer) checked(a settings.Action, on bool) desktop.MenuItem {
	it := v.item(a)
	it.Checkable, it.Checked = true, on
	return it
}

var separator = desktop.MenuItem{Separator: true}

// menuItems is the context menu.
func (v *viewer) menuItems() []desktop.MenuItem {
	return []desktop.MenuItem{
		v.item(settings.Open), v.item(settings.Reveal), separator,
		v.item(settings.Next), v.item(settings.Previous), v.item(settings.First), v.item(settings.Last), separator,
		v.item(settings.ZoomIn), v.item(settings.ZoomOut), v.item(settings.ZoomFit), v.item(settings.ZoomOriginal), separator,
		v.item(settings.RotateLeft), v.item(settings.RotateRight), v.item(settings.FlipH), v.item(settings.FlipV), separator,
		v.checked(settings.FullScreen, desktop.FullScreen(v.cx)), v.checked(settings.Slideshow, v.slideshow.Get()),
		v.checked(settings.Info, v.info.Get()), v.checked(settings.AlwaysOnTop, v.cfg.Get().AlwaysOnTop),
		v.checked(settings.TitleBar, v.cfg.Get().ShowTitleBar), v.checked(settings.Navigator, v.cfg.Get().Navigator),
		v.checked(settings.Filmstrip, v.cfg.Get().Filmstrip), separator,
		v.item(settings.CopyImage), v.item(settings.CopyPath), v.item(settings.Rename), v.item(settings.CopyTo),
		v.item(settings.MoveTo), v.item(settings.OpenWith), v.item(settings.Properties), v.item(settings.Delete), separator,
		v.item(settings.Preferences), v.item(settings.Close),
	}
}

// updateMenu sets the application menu, which Alt shows, again whenever
// the shortcuts or checked states change.
func (v *viewer) updateMenu() {
	cx := v.cx
	desktop.SetApplicationMenu(cx, []desktop.MenuItem{
		{Label: locales.MenuFile(cx), Items: []desktop.MenuItem{
			v.item(settings.Open), v.item(settings.Reveal), separator,
			v.item(settings.CopyImage), v.item(settings.CopyPath), v.item(settings.Rename), v.item(settings.CopyTo),
			v.item(settings.MoveTo), v.item(settings.OpenWith), v.item(settings.Properties), v.item(settings.ExportVideo),
			v.item(settings.Delete), separator,
			v.item(settings.Preferences), separator, v.item(settings.Close),
		}},
		{Label: locales.MenuView(cx), Items: []desktop.MenuItem{
			v.item(settings.ZoomIn), v.item(settings.ZoomOut), v.item(settings.ZoomFit), v.item(settings.ZoomOriginal), separator,
			v.item(settings.RotateLeft), v.item(settings.RotateRight), v.item(settings.FlipH), v.item(settings.FlipV), separator,
			v.checked(settings.FullScreen, desktop.FullScreen(cx)), v.checked(settings.Info, v.info.Get()),
			v.checked(settings.AlwaysOnTop, v.cfg.Get().AlwaysOnTop), v.checked(settings.TitleBar, v.cfg.Get().ShowTitleBar),
			v.checked(settings.Navigator, v.cfg.Get().Navigator), v.checked(settings.Filmstrip, v.cfg.Get().Filmstrip),
		}},
		{Label: locales.MenuGo(cx), Items: []desktop.MenuItem{
			v.item(settings.Next), v.item(settings.Previous), v.item(settings.First), v.item(settings.Last), separator,
			v.checked(settings.Slideshow, v.slideshow.Get()), separator,
			v.item(settings.AnimPause), v.item(settings.AnimPrev), v.item(settings.AnimNext),
			v.item(settings.AnimFaster), v.item(settings.AnimSlower),
		}},
	})
}
