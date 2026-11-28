// Command freeview is FreeView, a lightweight image viewer for Windows. It
// follows qView's simple, chrome-free viewing and adds Honeyview's
// customization: every mouse gesture and keyboard shortcut can run any
// action, and archives open like folders.
package main

import (
	"os"
	"runtime/debug"
	"strconv"
	"time"

	"github.com/nsgo-dev/nsgo"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/desktop"

	_ "github.com/OUBIGFA/FreeView/cmd/freeview/locales"
	"github.com/OUBIGFA/FreeView/internal/settings"
)

func main() {
	// Collect at half the default heap growth: decoding a photo makes
	// garbage several times its pixels, and at the default the heap grew
	// about 100 MB further while browsing 24 MP JPEGs, at no measurable
	// saving of CPU time.
	debug.SetGCPercent(50)
	frames = loadFrames()
	// The viewer reports a configuration it cannot read once it shows;
	// until then the defaults decide.
	if path, err := settings.Path(); err == nil {
		startCfg, _ = settings.Load(path)
	}
	// nsgo hands the regular files named on the command line to
	// OnOpenFiles, as the system's Open With does.
	for _, arg := range os.Args[1:] {
		if fi, err := os.Stat(arg); err == nil && fi.Mode().IsRegular() {
			launching = true
		}
	}
	opts := []nsgo.Option{nsgo.WindowSize(1100, 760)}
	// A key no other run shares gives this run its own single-instance
	// lock: later launches neither reach this window nor hand theirs here.
	if startCfg.MultipleWindows {
		opts = append(opts, nsgo.InstanceKey(strconv.Itoa(os.Getpid())))
	}
	nsgo.Run(App, opts...)
}

// startCfg is the configuration as FreeView starts, which decides the
// single-instance lock and the main window's frame.
var startCfg = settings.Default()

// App runs FreeView as a desktop app, which may open windows besides the
// main one (the preferences); closing the main window quits. The main
// window opens hidden and shows once ready (viewer.reveal): shown at
// once, it would first show its title bar and no image.
func App(app *core.Scope) {
	app.SetTheme(grayTheme())
	desktop.SetShutdownMode(app, desktop.ShutdownOnMainWindowClose)
	opts := desktop.WindowOptions{Hidden: true, Constraints: core.WindowConstraints{MinWidth: 320, MinHeight: 240}}
	if f, ok := frames.frame("main"); ok && startCfg.RememberWindow {
		opts.Frame = f
	}
	desktop.SetMainWindow(app, desktop.OpenWindow(app, opts, Root))
	// Later than the first image, which should not wait for the network.
	time.AfterFunc(10*time.Second, func() { app.Post(func() { checkDaily(app) }) })
}

// Root is the main window: the viewer, which owns all state.
func Root(cx *core.Scope) core.Node {
	desktop.SetMenuBar(cx, desktop.MenuBarAutoHide)
	return newViewer(cx).render(cx)
}
