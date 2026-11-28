package main

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
	"golang.org/x/image/bmp"

	"github.com/OUBIGFA/FreeView/internal/catalog"
	"github.com/OUBIGFA/FreeView/internal/settings"
)

// inspectViewer keeps all state access on the engine through App.Post.
func inspectViewer(t *testing.T) (*nsgotest.App, *viewer) {
	t.Helper()
	t.Setenv("APPDATA", t.TempDir())
	var v *viewer
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		v = newViewer(cx)
		return v.render(cx)
	}, nsgotest.Size(800, 600))
	return app, v
}

func TestCopyDecodedImagePreservesPixels(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 32, 24))
	img.SetRGBA(4, 5, color.RGBA{R: 255, A: 255})
	var data bytes.Buffer
	if err := bmp.Encode(&data, img); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "copy.bmp")
	if err := os.WriteFile(path, data.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	app := start(t)
	app.DropFiles("id:viewport", fileURI(path))
	app.Key("i")
	waitInfo(t, app, "copy.bmp")
	app.Key("ctrl+c")
	app.WaitUntil(time.Second, func() bool { return len(app.ClipboardImage) > 0 })
	got, err := png.Decode(bytes.NewReader(app.ClipboardImage))
	if err != nil {
		t.Fatal(err)
	}
	if got.Bounds() != img.Bounds() || color.RGBAModel.Convert(got.At(4, 5)) != img.At(4, 5) {
		t.Fatalf("clipboard pixels changed: bounds=%v pixel=%v", got.Bounds(), got.At(4, 5))
	}
}

func TestEmptyCatalogDiscardsPendingDecode(t *testing.T) {
	dir := folder(t, map[string][2]int{"a.png": {40, 30}})
	app, v := inspectViewer(t)
	app.DropFiles("id:viewport", fileURI(filepath.Join(dir, "a.png")))
	app.WaitUntil(time.Second, func() bool { return !v.next.src.Empty() })
	app.Post(func() { v.setCatalog(&catalog.Catalog{Dir: t.TempDir()}, 0) })
	app.Settle()
	loadImages(app)
	app.Post(func() {
		if !v.img.Get().src.Empty() {
			t.Error("the old decode reappeared in an empty folder")
		}
	})
	app.Settle()
}

func TestPrefetchAndNavigationShareOneRead(t *testing.T) {
	dir := folder(t, map[string][2]int{"a.png": {40, 30}, "b.png": {40, 30}})
	cat, _, err := catalog.Open(context.Background(), dir, catalog.Order{})
	if err != nil {
		t.Fatal(err)
	}
	app, v := inspectViewer(t)
	app.Post(func() {
		v.cat = cat
		v.count.Set(2)
		v.prefetch(0) // loop's left and right neighbor are both b.png
		v.show(1)     // user requests the same image before read completion
		if v.stats.reads != 1 {
			t.Errorf("started %d reads for the same image, want 1", v.stats.reads)
		}
	})
	app.Settle()
}

func TestMoveToSameFolderKeepsImage(t *testing.T) {
	dir := folder(t, map[string][2]int{"a.png": {40, 30}, "b.png": {40, 30}})
	app := startWith(t, func(c *settings.Config) { c.TargetFolder = dir; c.ShowInfo = true })
	app.DropFiles("id:viewport", fileURI(filepath.Join(dir, "a.png")))
	waitInfo(t, app, "a.png\n1 / 2")
	app.Key("f8")
	waitMessage(t, app, "Moved to "+dir)
	if got := app.Find("id:info").Text; !strings.Contains(got, "a.png\n1 / 2") {
		t.Fatalf("moving into its own folder changed the view: %s", got)
	}
	if _, err := os.Stat(filepath.Join(dir, "a.png")); err != nil {
		t.Fatal(err)
	}
}

func TestRemovingEarlierFileKeepsCurrentImage(t *testing.T) {
	dir := folder(t, map[string][2]int{"a.png": {40, 30}, "b.png": {40, 30}, "c.png": {40, 30}})
	app, v := inspectViewer(t)
	app.DropFiles("id:viewport", fileURI(filepath.Join(dir, "c.png")))
	app.Key("i")
	waitInfo(t, app, "c.png\n3 / 3")
	// A move/delete requested earlier can finish after the user navigates.
	app.Post(func() { v.dropItem(filepath.Join(dir, "a.png")) })
	app.Settle()
	loadImages(app)
	if got := app.Find("id:info").Text; !strings.Contains(got, "c.png\n2 / 2") {
		t.Fatalf("completion of an earlier file operation changed the current image: %s", got)
	}
}

func TestDisablingSmoothZoomHidesExistingResult(t *testing.T) {
	dir := folder(t, map[string][2]int{"a.png": {1600, 1200}})
	app, v := inspectViewer(t)
	app.DropFiles("id:viewport", fileURI(filepath.Join(dir, "a.png")))
	app.Key("i")
	waitInfo(t, app, "a.png")
	app.Post(func() {
		v.extras.smoothScale = v.currentScale()
		v.extras.smooth.Set(core.ImageSource{Pixels: image.NewRGBA(image.Rect(0, 0, 2, 2))})
		v.update(func(c *settings.Config) { c.SmoothZoom = false })
		if v.smoothShown() {
			t.Error("a disabled smooth result still covers the original image")
		}
	})
	app.Settle()
}

func TestRapidSettingsChangesPersistTheLatest(t *testing.T) {
	app, v := inspectViewer(t)
	var path string
	app.Post(func() {
		path = v.cfgPath
		for i := range 30 {
			v.update(func(c *settings.Config) { c.SlideshowSeconds = float64(i + 1) })
		}
	})
	app.Settle()
	if path == "" {
		t.Fatal("settings path was not initialized")
	}
	select {
	case <-v.configSaved:
	case <-time.After(3 * time.Second):
		t.Fatal("settings writes did not finish")
	}
	got, err := settings.Load(path)
	if err != nil || got.SlideshowSeconds != 30 {
		t.Fatalf("last settings: interval=%v err=%v", got.SlideshowSeconds, err)
	}
	app.Settle()
	if app.Exists("id:message") {
		t.Fatalf("saving settings reported a failure: %s", app.Find("id:message").Text)
	}
}

func TestCanceledEnhancementsDoNotRun(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var ran atomic.Bool
	background(ctx, func() { ran.Store(true) })
	// A live marker submitted after cancellation must still get a worker.
	done := make(chan struct{})
	background(context.Background(), func() { close(done) })
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("workers are blocked")
	}
	if ran.Load() {
		t.Error("canceled enhancement still ran")
	}
}

func TestFilmstripRequestsSurviveNavigation(t *testing.T) {
	dir := folder(t, map[string][2]int{"a.png": {40, 30}, "b.png": {40, 30}})
	app, v := inspectViewer(t)
	app.DropFiles("id:viewport", fileURI(filepath.Join(dir, "a.png")))
	app.Key("i")
	waitInfo(t, app, "a.png")
	// Hold both workers so the requests remain queued across the commit.
	for range heavyWorkers {
		select {
		case heavy <- struct{}{}:
		case <-time.After(time.Second):
			t.Fatal("workers did not become idle")
		}
	}
	released := false
	defer func() {
		if !released {
			for range heavyWorkers {
				<-heavy
			}
		}
	}()
	app.Key("t")
	app.HoverAt("id:viewport", core.Point{X: 400, Y: 590})
	app.WaitFor("id:strip-1", time.Second)
	app.Press("id:strip-1")
	waitInfo(t, app, "b.png")
	for range heavyWorkers {
		<-heavy
	}
	released = true
	app.WaitUntil(3*time.Second, func() bool { return len(v.extras.thumbs) == 2 })
}
