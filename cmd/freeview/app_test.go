package main

import (
	"archive/zip"
	"bytes"
	"image"
	"image/png"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

// TestMain hands the main thread to the platform under nsgo test -target.
func TestMain(m *testing.M) { nsgotest.Main(m) }

func pngOf(t *testing.T, w, h int) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, w, h))); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func fileURI(p string) string {
	return (&url.URL{Scheme: "file", Path: "/" + filepath.ToSlash(p)}).String()
}

// start runs the viewer with its preferences in a fresh profile.
func start(t *testing.T) *nsgotest.App {
	t.Setenv("AppData", t.TempDir())
	return nsgotest.Start(t, Root, nsgotest.Size(800, 600))
}

func waitInfo(t *testing.T, app *nsgotest.App, want string) string {
	t.Helper()
	var got string
	app.WaitUntil(5*time.Second, func() bool {
		if !app.Exists("id:info") {
			return false
		}
		got = app.Find("id:info").Text
		return strings.Contains(got, want)
	})
	if !strings.Contains(got, want) {
		t.Fatalf("info = %q, want it to hold %q", got, want)
	}
	return got
}

func TestBrowsesAFolderWithKeysAndTheWheel(t *testing.T) {
	dir := t.TempDir()
	for name, size := range map[string][2]int{"a1.png": {400, 300}, "a2.png": {1600, 1200}, "a10.png": {100, 50}} {
		if err := os.WriteFile(filepath.Join(dir, name), pngOf(t, size[0], size[1]), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	app := start(t)
	app.DropFiles("id:viewport", fileURI(filepath.Join(dir, "a2.png")))
	app.WaitFor("id:image", 5*time.Second)
	app.Key("i")
	// Large images shrink to the 800 by 600 window by default.
	waitInfo(t, app, "a2.png\n2 / 3   1600 × 1200   50%")

	app.Key("right")
	waitInfo(t, app, "a10.png\n3 / 3   100 × 50   100%")
	app.Key("right") // loops by default
	waitInfo(t, app, "a1.png\n1 / 3")

	// The wheel moves between images; Ctrl+wheel zooms by the zoom step.
	app.Wheel("id:viewport", core.Point{Y: -33}, core.Modifiers{})
	waitInfo(t, app, "a2.png\n2 / 3")
	app.Wheel("id:viewport", core.Point{Y: 33}, core.Modifiers{Control: true})
	waitInfo(t, app, "63%")
	app.Key("ctrl+1")
	waitInfo(t, app, "100%")
	app.Key("ctrl+0")
	waitInfo(t, app, "50%")
	app.Key("end")
	waitInfo(t, app, "3 / 3")
	app.Key("home")
	waitInfo(t, app, "1 / 3")
}

func TestOpensAnArchiveLikeAFolder(t *testing.T) {
	file := filepath.Join(t.TempDir(), "book.cbz")
	f, err := os.Create(file)
	if err != nil {
		t.Fatal(err)
	}
	z := zip.NewWriter(f)
	for _, n := range []string{"p10.png", "p9.png"} {
		w, _ := z.Create(n)
		w.Write(pngOf(t, 30, 40))
	}
	z.Close()
	f.Close()

	app := start(t)
	app.DropFiles("id:viewport", fileURI(file))
	app.WaitFor("id:image", 5*time.Second)
	app.Key("i")
	waitInfo(t, app, "p9.png\n1 / 2   30 × 40")
	app.Key("pagedown")
	waitInfo(t, app, "p10.png\n2 / 2")
}

func TestShowsWhyAFileCannotBeShown(t *testing.T) {
	p := filepath.Join(t.TempDir(), "broken.png")
	os.WriteFile(p, []byte("not a png"), 0o644)
	app := start(t)
	app.DropFiles("id:viewport", fileURI(p))
	v := app.WaitFor("id:error", 5*time.Second)
	if !strings.Contains(v.Text, "broken.png") {
		t.Errorf("error = %q", v.Text)
	}
}

// Without a title bar, the default, a strip at the top moves the window.
func TestDragStripWithoutTitleBar(t *testing.T) {
	app := start(t)
	app.WaitFor("id:open", 5*time.Second)
	// Its height is the hidden title bar's, 0 in the test model.
	if len(app.QueryMounted("id:drag")) == 0 {
		t.Error("no drag strip without the title bar")
	}
}

// With the title bar, the system's moves the window and the strip goes.
func TestNoDragStripWithTitleBar(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("AppData", dir)
	os.MkdirAll(filepath.Join(dir, "FreeView"), 0o755)
	os.WriteFile(filepath.Join(dir, "FreeView", "config.yaml"), []byte("show_title_bar: true\n"), 0o644)
	app := nsgotest.Start(t, Root, nsgotest.Size(800, 600))
	app.WaitFor("id:open", 5*time.Second)
	if len(app.QueryMounted("id:drag")) > 0 {
		t.Error("drag strip shown with the title bar")
	}
}

// The preferences show one category at a time beside their own sidebar,
// and every matching row while searching.
func TestPreferencesNavigateAndSearch(t *testing.T) {
	t.Setenv("AppData", t.TempDir())
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node { return preferences{v: newViewer(cx)}.Render(cx) },
		nsgotest.Size(1040, 700))
	app.WaitFor("id:settings-view-fit", 5*time.Second)
	if app.Exists("id:settings-mouse-middle_click") {
		t.Error("mouse rows shown on the view page")
	}
	app.Press("id:settings-nav-mouse")
	app.WaitFor("id:settings-mouse-middle_click", 5*time.Second)
	if app.Exists("id:settings-view-fit") {
		t.Error("view rows shown on the mouse page")
	}
	// "top" matches the view page's switch and the keyboard page's action (the tests run in English).
	app.Type("id:settings-search", "top")
	app.WaitFor("id:settings-view-always-on-top", 5*time.Second)
	app.WaitFor("id:settings-keyboard-key-always_on_top", 5*time.Second)
	if app.Exists("id:settings-view-fit") || app.Exists("id:settings-mouse-middle_click") {
		t.Error("rows that do not match shown while searching")
	}
}
