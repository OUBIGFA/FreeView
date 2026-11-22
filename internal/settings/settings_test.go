package settings

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMissingFileGivesDefaults(t *testing.T) {
	c, err := Load(filepath.Join(t.TempDir(), "none.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if c.Mouse[MiddleClick] != Close || c.Mouse[WheelDown] != Next {
		t.Errorf("defaults = %v", c.Mouse)
	}
	// The window starts neither topmost nor with a title bar.
	if c.AlwaysOnTop || c.ShowTitleBar {
		t.Errorf("always on top %v, title bar %v", c.AlwaysOnTop, c.ShowTitleBar)
	}
}

func TestFileOverridesOnlyWhatItNames(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.yaml")
	data := "mouse:\n  middle_click: full_screen\n  wheel_up: bogus\nkeys:\n  next: [n]\nzoom_step: 9\nfit: window\n"
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
	c.Background = "#000000"
	if err := Save(p, c); err != nil {
		t.Fatal(err)
	}
	got, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if got.Mouse[MiddleClick] != Minimize || got.Keys[Delete][0] != "shift+delete" || got.Background != "#000000" {
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
