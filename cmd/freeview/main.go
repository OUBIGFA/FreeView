// Command freeview is FreeView, a lightweight image viewer for Windows. It
// follows qView's simple, chrome-free viewing and adds Honeyview's
// customization: every mouse gesture and keyboard shortcut can run any
// action, and archives open like folders.
package main

import (
	"github.com/nsgo-dev/nsgo"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/desktop"

	_ "github.com/OUBIGFA/FreeView/cmd/freeview/locales"
	"github.com/OUBIGFA/FreeView/internal/settings"
)

func main() {
	frames = loadFrames()
	opts := []nsgo.Option{nsgo.WindowSize(1100, 760), nsgo.WindowConstraints(core.WindowConstraints{MinWidth: 320, MinHeight: 240})}
	// The viewer reports a configuration it cannot read once it shows;
	// until then the defaults decide.
	cfg := settings.Default()
	if path, err := settings.Path(); err == nil {
		cfg, _ = settings.Load(path)
	}
	if f, ok := frames.frame("main"); ok && cfg.RememberWindow {
		opts = append(opts, nsgo.WindowFrame(f))
	}
	nsgo.Run(App, opts...)
}

// App runs FreeView as a desktop app, which may open windows besides the
// main one (the preferences); closing the main window quits.
func App(app *core.Scope) {
	desktop.SetShutdownMode(app, desktop.ShutdownOnMainWindowClose)
	nsgo.SetMainView(app, Root)
}

// Root is the main window: the viewer, which owns all state.
func Root(cx *core.Scope) core.Node {
	desktop.SetMenuBar(cx, desktop.MenuBarAutoHide)
	return newViewer(cx).render(cx)
}
