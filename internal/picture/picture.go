// Package picture prepares an image file's bytes for display: it reads the
// size and EXIF orientation without decoding the pixels, and decodes the
// formats nsgo does not (BMP and TIFF, through golang.org/x/image) into
// pixels. nsgo decodes JPEG, PNG, GIF and WebP itself, animations included.
package picture

import (
	"bytes"
	"fmt"
	"image"
	"image/draw"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"

	"github.com/rwcarlsen/goexif/exif"
	_ "golang.org/x/image/bmp"
	_ "golang.org/x/image/tiff"
	_ "golang.org/x/image/webp"
)

// Picture is an image ready to hand to nsgo: either its encoded Data or
// decoded Pixels, never both.
type Picture struct {
	Data   []byte
	Pixels *image.RGBA
	// Width and Height are the stored pixel size, before Orientation.
	Width, Height int
	Format        string
	// Orientation is the EXIF orientation, 1 to 8; 1 means upright.
	Orientation int
}

// nativeFormats are the formats nsgo decodes and animates itself.
var nativeFormats = map[string]bool{"jpeg": true, "png": true, "gif": true, "webp": true}

// Load prepares data, the bytes of an image file.
func Load(data []byte) (Picture, error) {
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return Picture{}, fmt.Errorf("unrecognized image: %w", err)
	}
	p := Picture{Width: cfg.Width, Height: cfg.Height, Format: format, Orientation: orientation(data)}
	if nativeFormats[format] {
		p.Data = data
		return p, nil
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return Picture{}, err
	}
	rgba, ok := img.(*image.RGBA)
	if !ok {
		rgba = image.NewRGBA(img.Bounds())
		draw.Draw(rgba, rgba.Rect, img, img.Bounds().Min, draw.Src)
	}
	p.Pixels = rgba
	return p, nil
}

// orientation reads the EXIF orientation tag, 1 when there is none.
func orientation(data []byte) int {
	x, err := exif.Decode(bytes.NewReader(data))
	if err != nil {
		return 1
	}
	tag, err := x.Get(exif.Orientation)
	if err != nil {
		return 1
	}
	v, err := tag.Int(0)
	if err != nil || v < 1 || v > 8 {
		return 1
	}
	return v
}

// OrientationTransform returns the rotation (clockwise degrees) and the
// horizontal mirror that display an image stored with EXIF orientation o
// upright, applied as mirror first, then rotation.
func OrientationTransform(o int) (rotate int, mirror bool) {
	switch o {
	case 2:
		return 0, true
	case 3:
		return 180, false
	case 4:
		return 180, true
	case 5:
		return 270, true
	case 6:
		return 90, false
	case 7:
		return 90, true
	case 8:
		return 270, false
	}
	return 0, false
}
