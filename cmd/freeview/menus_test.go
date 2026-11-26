package main

import (
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"

	"github.com/OUBIGFA/FreeView/internal/settings"
)

// Empty windows still offer Open and Settings, but image commands must
// not pretend to work. Escape dismisses the popup rather than the app.
func TestContextMenuOnAnEmptyWindow(t *testing.T) {
	app := start(t)
	app.RightClick("id:viewport")
	app.WaitFor("id:context-copy_image", time.Second)
	for _, id := range []string{"copy_image", "copy_path", "delete"} {
		if !app.Find("id:context-" + id).Props.Disabled {
			t.Errorf("%s enabled without an image", id)
		}
	}
	if app.Find("id:context-open").Props.Disabled || app.Find("id:context-preferences").Props.Disabled {
		t.Fatal("Open or Settings disabled on an empty window")
	}
	for _, id := range []string{"full_screen", "slideshow", "info", "always_on_top", "title_bar", "navigator", "filmstrip"} {
		if !app.Exists("id:context-" + id) {
			t.Errorf("frequent switch %s is not directly visible", id)
		}
	}
	app.Press("id:context-zoom")
	app.WaitFor("label:Zoom in", time.Second)
	if !app.Find("label:Zoom in").Props.Disabled {
		t.Error("zoom enabled without an image")
	}
	app.Key("escape") // the first Escape leaves the submenu
	app.WaitForGone("label:Zoom in", time.Second)
	app.Key("escape")
	app.WaitForGone("id:context-open", time.Second)
	if app.Quits != 0 {
		t.Fatal("Escape quit the viewer behind its menu")
	}
	app.RightClick("id:viewport")
	app.WaitFor("id:context-open", time.Second)
	app.PressAt("id:viewport", core.Point{X: 20, Y: 40})
	app.WaitForGone("id:context-open", time.Second)
}

// The menu consumes navigation keys. Closing it restores the viewer's
// own bindings, and selecting its items still runs the real commands.
func TestContextMenuNavigationAndSelection(t *testing.T) {
	dir := folder(t, map[string][2]int{"menu1.png": {1600, 1200}, "menu2.png": {40, 30}})
	t.Setenv("APPDATA", t.TempDir())
	var v *viewer
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		v = newViewer(cx)
		return v.render(cx)
	}, nsgotest.Size(800, 600))
	path := filepath.Join(dir, "menu1.png")
	app.DropFiles("id:viewport", fileURI(path))
	app.Key("i")
	waitInfo(t, app, "menu1.png\n1 / 2")
	app.RightClick("id:viewport")
	app.WaitFor("id:context-open", time.Second)
	app.Key("right")
	app.Key("home")
	app.Key("end")
	app.Post(func() {
		if v.index.Get() != 0 {
			t.Error("menu keys navigated the image behind it")
		}
	})
	app.Key("escape")
	app.WaitForGone("id:context-open", time.Second)
	app.RightClick("id:viewport")
	app.Press("id:context-copy_path")
	app.WaitForGone("id:context-open", time.Second)
	if app.Clipboard != path {
		t.Fatalf("copied path %q, want %q", app.Clipboard, path)
	}
	app.RightClick("id:viewport")
	app.WaitFor("id:context-info", time.Second)
	if !app.Find("id:context-info").Props.Checked {
		t.Error("menu did not reflect the information overlay's state")
	}
	app.Press("id:context-info")
	app.WaitForGone("id:context-open", time.Second)
	app.WaitForGone("id:info", time.Second)
	app.Key("i")
	app.Key("right")
	waitInfo(t, app, "menu2.png\n2 / 2")
	var titles []string
	for _, item := range app.Menu {
		titles = append(titles, item.Label)
	}
	if !slices.Equal(titles, []string{"File", "Image", "Go", "Animation", "Display"}) {
		t.Errorf("menu bar categories = %v", titles)
	}
}

// Common toggles remain directly selectable and report their updated
// state on every opening, including both directions of each switch.
func TestContextMenuDirectSwitches(t *testing.T) {
	dir := folder(t, map[string][2]int{"switch1.png": {40, 30}, "switch2.png": {40, 30}})
	app := start(t)
	app.DropFiles("id:viewport", fileURI(filepath.Join(dir, "switch1.png")))
	app.WaitUntil(time.Second, func() bool {
		loadImages(app)
		return len(app.Menu) > 0 && !app.Menu[2].Items[0].Disabled
	})
	for _, action := range []settings.Action{settings.Slideshow, settings.Info,
		settings.AlwaysOnTop, settings.TitleBar, settings.Navigator, settings.Filmstrip} {
		id := "id:context-" + string(action)
		app.RightClick("id:viewport")
		app.WaitFor(id, time.Second)
		before := app.Find(id).Props.Checked
		app.Press(id)
		app.WaitForGone("id:context-open", time.Second)
		app.RightClick("id:viewport")
		if got := app.Find(id).Props.Checked; got == before {
			t.Errorf("%s did not toggle", action)
		}
		app.Press(id)
		app.RightClick("id:viewport")
		if got := app.Find(id).Props.Checked; got != before {
			t.Errorf("%s did not toggle back", action)
		}
		app.Key("escape")
	}
	// Full screen belongs to the OS. The headless driver records requests;
	// report the resulting environment as the real window does.
	app.RightClick("id:viewport")
	app.Press("id:context-full_screen")
	if !slices.ContainsFunc(app.Window, func(r nsgotest.WindowRequest) bool { return r.Action == nsgotest.WindowToggleFullScreen }) {
		t.Fatal("full-screen switch did not request a window change")
	}
	app.SetEnvironment(core.Environment{FullScreen: true})
	app.RightClick("id:viewport")
	if !app.Find("id:context-full_screen").Props.Checked {
		t.Error("full-screen switch did not reflect the window state")
	}
	app.Key("escape")
}

// A user's custom binding to Show menu uses the same themed popup; a
// right click rebound to another command keeps that command instead.
func TestContextMenuRespectsCustomBindings(t *testing.T) {
	app := startWith(t, func(c *settings.Config) {
		c.Keys[settings.Menu] = []string{"m"}
		c.Mouse[settings.RightClick] = settings.Info
	})
	app.Key("m")
	app.WaitFor("id:context-open", time.Second)
	app.Key("escape")
	app.WaitForGone("id:context-open", time.Second)
	app.RightClick("id:viewport")
	app.Settle()
	if app.Exists("id:context-open") {
		t.Fatal("right click ignored its custom binding")
	}
}
