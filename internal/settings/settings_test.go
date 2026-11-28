package settings

import (
	"math"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestConcurrentSavesUseIndependentFiles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	start := make(chan struct{})
	errs := make(chan error, 16)
	var workers sync.WaitGroup
	for i := range 16 {
		workers.Go(func() {
			c := Default()
			c.SlideshowSeconds = float64(i + 1)
			<-start
			errs <- Save(path, c)
		})
	}
	close(start)
	workers.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Errorf("concurrent save failed: %v", err)
		}
	}
	if _, err := Load(path); err != nil {
		t.Fatalf("saved configuration is incomplete: %v", err)
	}
}

func TestNonFiniteNumbersUseDefaults(t *testing.T) {
	for _, value := range []string{".nan", ".inf", "-.inf", "1e100"} {
		t.Run(value, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.yaml")
			if err := os.WriteFile(path, []byte("zoom_step: "+value+"\nslideshow_seconds: "+value+"\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			got, err := Load(path)
			if err != nil {
				t.Fatal(err)
			}
			if math.IsNaN(got.ZoomStep) || got.ZoomStep != Default().ZoomStep || got.SlideshowSeconds != Default().SlideshowSeconds {
				t.Fatalf("invalid numbers escaped normalization: zoom=%v slideshow=%v", got.ZoomStep, got.SlideshowSeconds)
			}
		})
	}
}

func TestMissingFileGivesDefaults(t *testing.T) {
	c, err := Load(filepath.Join(t.TempDir(), "none.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if c.Mouse[MiddleClick] != Close || c.Mouse[WheelDown] != Next {
		t.Errorf("defaults = %v", c.Mouse)
	}
	// The window starts neither topmost nor with a title bar, and shows the full title at the top-right on hover.
	if c.AlwaysOnTop || c.ShowTitleBar {
		t.Errorf("always on top %v, title bar %v", c.AlwaysOnTop, c.ShowTitleBar)
	}
	if !c.HoverTitle || c.HoverTitlePlace != TitleRight {
		t.Errorf("hover title %v at %q", c.HoverTitle, c.HoverTitlePlace)
	}
	// The viewer opens where the user left it.
	if !c.RememberWindow {
		t.Error("the window's size is not remembered by default")
	}
	// A launch reuses the open window unless the user allows several.
	if c.MultipleWindows {
		t.Error("multiple windows are allowed by default")
	}
}

func TestFileOverridesOnlyWhatItNames(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.yaml")
	data := "mouse:\n  middle_click: full_screen\n  wheel_up: bogus\nkeys:\n  next: [n]\nzoom_step: 9\nfit: window\nhover_title_place: middle\n"
	if err := os.WriteFile(p, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if c.Mouse[MiddleClick] != FullScreen {
		t.Errorf("middle = %q", c.Mouse[MiddleClick])
	}
	if c.Mouse[WheelUp] != None {
		t.Errorf("unknown action kept: %q", c.Mouse[WheelUp])
	}
	if c.Mouse[WheelDown] != Next {
		t.Errorf("unnamed gesture lost its default: %q", c.Mouse[WheelDown])
	}
	if len(c.Keys[Next]) != 1 || c.Keys[Next][0] != "n" || len(c.Keys[Previous]) == 0 {
		t.Errorf("keys = %v / %v", c.Keys[Next], c.Keys[Previous])
	}
	if c.ZoomStep != Default().ZoomStep || c.Fit != FitWindow {
		t.Errorf("zoom %v fit %v", c.ZoomStep, c.Fit)
	}
	if !c.HoverTitle || c.HoverTitlePlace != TitleRight {
		t.Errorf("hover title %v at %q", c.HoverTitle, c.HoverTitlePlace)
	}
}

func TestInvalidFileIsAnErrorWithDefaults(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.yaml")
	os.WriteFile(p, []byte("mouse: [unclosed"), 0o644)
	c, err := Load(p)
	if err == nil {
		t.Fatal("no error for an invalid file")
	}
	if c.Mouse[MiddleClick] != Close {
		t.Errorf("not defaults: %v", c.Mouse)
	}
}

func TestSaveThenLoadRoundTrips(t *testing.T) {
	p := filepath.Join(t.TempDir(), "FreeView", "config.yaml")
	c := Default()
	c.Mouse[MiddleClick] = Minimize
	c.Keys[Delete] = []string{"shift+delete"}
	c.Loop = false
	c.HoverTitle = false
	c.HoverTitlePlace = TitleLeft
	if err := Save(p, c); err != nil {
		t.Fatal(err)
	}
	got, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if got.Mouse[MiddleClick] != Minimize || got.Keys[Delete][0] != "shift+delete" || got.Loop || got.HoverTitle || got.HoverTitlePlace != TitleLeft {
		t.Errorf("round trip = %+v", got)
	}
}

func TestCloneIsIndependent(t *testing.T) {
	a := Default()
	b := a.Clone()
	b.Mouse[MiddleClick] = None
	b.Keys[Next][0] = "x"
	if a.Mouse[MiddleClick] != Close || a.Keys[Next][0] != "right" {
		t.Error("Clone shares maps or slices")
	}
}

func TestWindowsRoundTripAndDropSizelessFrames(t *testing.T) {
	p := filepath.Join(t.TempDir(), "FreeView", "windows.yaml")
	if w, err := LoadWindows(p); err != nil || len(w) != 0 {
		t.Fatalf("missing file = %v, %v", w, err)
	}
	in := Windows{"main": {X: 10, Y: 20, Width: 1200, Height: 800}, "preferences": {X: 5, Y: 5, Width: 0, Height: 600}}
	if err := SaveWindows(p, in); err != nil {
		t.Fatal(err)
	}
	got, err := LoadWindows(p)
	if err != nil {
		t.Fatal(err)
	}
	if got["main"] != in["main"] {
		t.Errorf("main = %+v", got["main"])
	}
	if _, ok := got["preferences"]; ok {
		t.Error("a frame without a width was kept")
	}
}

func TestWindowsRejectsNonFiniteFramesAndHandlesNull(t *testing.T) {
	p := filepath.Join(t.TempDir(), "windows.yaml")
	for _, data := range []string{"null\n", "main: {x: .nan, y: 0, width: 800, height: 600}\n", "main: {x: 0, y: 0, width: .inf, height: 600}\n"} {
		if err := os.WriteFile(p, []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
		got, err := LoadWindows(p)
		if err != nil || got == nil || len(got) != 0 {
			t.Fatalf("invalid window configuration %q: %#v %v", data, got, err)
		}
	}
}

// The overview and the strip start off; an animation speed not offered
// falls back to normal.
func TestEnhancementDefaults(t *testing.T) {
	c := Default()
	if c.Navigator || c.Filmstrip || !c.SmoothZoom || !c.ColorManagement || c.EdgeClicks || c.AnimationSpeed != 1 {
		t.Errorf("defaults %+v", c)
	}
	p := filepath.Join(t.TempDir(), "config.yaml")
	os.WriteFile(p, []byte("animation_speed: 3\nnavigator: true\n"), 0o644)
	got, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if got.AnimationSpeed != 1 || !got.Navigator {
		t.Errorf("loaded speed %v navigator %v", got.AnimationSpeed, got.Navigator)
	}
}
