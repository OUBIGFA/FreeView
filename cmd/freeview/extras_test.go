package main

import (
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/color"
	"image/gif"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"

	"github.com/OUBIGFA/FreeView/internal/settings"
)

// folder writes PNGs of the given sizes into a new folder.
func folder(t *testing.T, sizes map[string][2]int) string {
	dir := t.TempDir()
	for name, size := range sizes {
		if err := os.WriteFile(filepath.Join(dir, name), pngOf(t, size[0], size[1]), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// startWith runs the viewer with the preferences change makes to the
// defaults.
func startWith(t *testing.T, change func(*settings.Config)) *nsgotest.App {
	appData := t.TempDir()
	t.Setenv("APPDATA", appData)
	c := settings.Default()
	change(&c)
	if err := settings.Save(filepath.Join(appData, "FreeView", "config.yaml"), c); err != nil {
		t.Fatal(err)
	}
	return nsgotest.Start(t, Root, nsgotest.Size(800, 600))
}

func waitMessage(t *testing.T, app *nsgotest.App, want string) {
	t.Helper()
	var got string
	app.WaitUntil(5*time.Second, func() bool {
		loadImages(app)
		if app.Exists("id:message") {
			got = app.Find("id:message").Text
		}
		return got == want
	})
	if got != want {
		t.Fatalf("message %q, want %q", got, want)
	}
}

// The overview and the strip of thumbnails show only once turned on.
func TestOverviewAndStripAreOptIn(t *testing.T) {
	dir := folder(t, map[string][2]int{"c1.png": {1600, 1200}, "c2.png": {300, 200}, "c3.png": {300, 200}})
	app := start(t)
	app.DropFiles("id:viewport", fileURI(filepath.Join(dir, "c1.png")))
	app.Key("i")
	waitInfo(t, app, "c1.png\n1 / 3")
	app.Key("ctrl+1") // larger than the window now
	waitInfo(t, app, "100%")
	app.HoverAt("id:viewport", core.Point{X: 400, Y: 590})
	app.Settle()
	if app.Exists("id:navigator") || app.Exists("id:filmstrip") {
		t.Fatal("the overview or the strip shows while turned off")
	}

	app.Key("n")
	app.WaitFor("id:navigator", 5*time.Second)
	app.Key("ctrl+0") // fits again: nothing to overview
	app.WaitForGone("id:navigator", 5*time.Second)

	app.Key("t")
	app.HoverAt("id:viewport", core.Point{X: 400, Y: 300})
	app.HoverAt("id:viewport", core.Point{X: 400, Y: 590})
	app.WaitFor("id:strip-2", 5*time.Second)
	app.Press("id:strip-2")
	waitInfo(t, app, "c3.png\n3 / 3")
	app.HoverAt("id:viewport", core.Point{X: 400, Y: 100})
	app.WaitForGone("id:filmstrip", 5*time.Second)
}

// F2 renames the shown image in place, keeping its extension.
func TestRenameKeepsThePlace(t *testing.T) {
	dir := folder(t, map[string][2]int{"d1.png": {40, 30}, "d2.png": {40, 30}})
	app := start(t)
	app.DropFiles("id:viewport", fileURI(filepath.Join(dir, "d2.png")))
	app.Key("i")
	waitInfo(t, app, "d2.png\n2 / 2")
	app.Key("f2")
	app.WaitFor("id:rename-input", 5*time.Second)
	app.Clear("id:rename-input")
	app.Type("id:rename-input", "z9")
	app.Submit("id:rename-input")
	waitInfo(t, app, "z9.png\n2 / 2")
	if _, err := os.Stat(filepath.Join(dir, "z9.png")); err != nil {
		t.Fatal(err)
	}
	if app.Exists("id:rename-input") {
		t.Error("the name field stays")
	}
	// Esc leaves a rename without closing the window.
	app.Key("f2")
	app.WaitFor("id:rename-input", 5*time.Second)
	app.Key("escape")
	app.WaitForGone("id:rename-input", 5*time.Second)
}

// With edge clicks on, a click at the right edge shows the next image.
func TestEdgeClicks(t *testing.T) {
	dir := folder(t, map[string][2]int{"e1.png": {40, 30}, "e2.png": {40, 30}})
	app := startWith(t, func(c *settings.Config) { c.EdgeClicks = true; c.ShowInfo = true })
	app.DropFiles("id:viewport", fileURI(filepath.Join(dir, "e1.png")))
	waitInfo(t, app, "e1.png\n1 / 2")
	app.PressAt("id:viewport", core.Point{X: 790, Y: 300})
	waitInfo(t, app, "e2.png\n2 / 2")
	app.PressAt("id:viewport", core.Point{X: 10, Y: 300})
	waitInfo(t, app, "e1.png\n1 / 2")
}

// The information overlay shows the prompt an AI generator wrote.
func TestInfoShowsThePrompt(t *testing.T) {
	dir := t.TempDir()
	data := pngOf(t, 40, 30)
	body := []byte("parameters\x00a red fox\nSteps: 20")
	chunk := binary.BigEndian.AppendUint32(nil, uint32(len(body)))
	chunk = append(append(chunk, "tEXt"...), body...)
	chunk = binary.BigEndian.AppendUint32(chunk, crc32.ChecksumIEEE(append([]byte("tEXt"), body...)))
	data = append(append(append([]byte(nil), data[:len(data)-12]...), chunk...), data[len(data)-12:]...)
	if err := os.WriteFile(filepath.Join(dir, "f.png"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	app := start(t)
	app.DropFiles("id:viewport", fileURI(filepath.Join(dir, "f.png")))
	app.Key("i")
	waitInfo(t, app, "AI prompt\na red fox\nSteps: 20")
}

// The animation keys step through a GIF's frames, and say when an image
// is no animation.
func TestAnimationSteps(t *testing.T) {
	dir := t.TempDir()
	pal := color.Palette{color.Black, color.White}
	frame := func(c uint8) *image.Paletted {
		p := image.NewPaletted(image.Rect(0, 0, 8, 8), pal)
		for i := range p.Pix {
			p.Pix[i] = c
		}
		return p
	}
	var b bytes.Buffer
	if err := gif.EncodeAll(&b, &gif.GIF{Image: []*image.Paletted{frame(0), frame(1), frame(0)}, Delay: []int{10, 10, 10}}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "g1.gif"), b.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "g2.png"), pngOf(t, 8, 8), 0o644); err != nil {
		t.Fatal(err)
	}
	app := start(t)
	app.DropFiles("id:viewport", fileURI(filepath.Join(dir, "g1.gif")))
	app.Key("i")
	waitInfo(t, app, "g1.gif")
	app.Key("l")
	waitMessage(t, app, "Frame 2 of 3")
	app.Key("j")
	app.Key("j")
	waitMessage(t, app, "Frame 3 of 3")
	app.Key("right")
	waitInfo(t, app, "g2.png")
	app.Key("k")
	waitMessage(t, app, "This image is not an animation")
}
