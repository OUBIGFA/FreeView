//go:build windows

package thumbs

import (
	"errors"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

// noShell reports whether err means the system has no shell to make
// thumbnails, as on a server core or a CI runner without Explorer.
func noShell(err error) bool {
	var h hresult
	if !errors.As(err, &h) {
		return false
	}
	switch uint32(h) {
	case 0x80040154, // REGDB_E_CLASSNOTREG
		0x80004002, // E_NOINTERFACE
		0x8004B200: // WTS_E_FAILEDEXTRACTION
		return true
	}
	return false
}

func TestFilePNG(t *testing.T) {
	// Mostly orange, with a small blue square, 600×300.
	img := image.NewRGBA(image.Rect(0, 0, 600, 300))
	orange, blue := color.RGBA{230, 120, 20, 255}, color.RGBA{0, 0, 255, 255}
	for y := range 300 {
		for x := range 600 {
			c := orange
			if x < 40 && y < 40 {
				c = blue
			}
			img.SetRGBA(x, y, c)
		}
	}
	path := filepath.Join(t.TempDir(), "strip.png")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	const size = 96
	thumb, err := File(path, size)
	if noShell(err) {
		t.Skipf("no shell thumbnails here: %v", err)
	}
	if err != nil {
		t.Fatal(err)
	}
	b := thumb.Rect
	if b.Dx() > size || b.Dy() > size || b.Dx() < size/2 {
		t.Fatalf("thumbnail %v not within %d×%d", b.Size(), size, size)
	}
	// The aspect ratio stays 2:1, give or take rounding.
	if d := b.Dx() - 2*b.Dy(); d < -2 || d > 2 {
		t.Errorf("thumbnail %v lost the 2:1 aspect", b.Size())
	}
	// The center is the dominant orange, opaque.
	c := thumb.RGBAAt(b.Min.X+b.Dx()*3/4, b.Min.Y+b.Dy()/2)
	near := func(a, b uint8) bool { return int(a)-int(b) < 12 && int(b)-int(a) < 12 }
	if !near(c.R, orange.R) || !near(c.G, orange.G) || !near(c.B, orange.B) || c.A != 255 {
		t.Errorf("center %v, want about %v", c, orange)
	}
	if _, err := File(filepath.Join(t.TempDir(), "missing.png"), size); err == nil {
		t.Error("a missing file should fail")
	}
}
