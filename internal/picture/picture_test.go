package picture

import (
	"bytes"
	"encoding/binary"
	"errors"
	"image"
	"image/color"
	"image/gif"
	"image/png"
	"testing"

	"golang.org/x/image/bmp"
	"golang.org/x/image/tiff"
)

func sample() *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, 4, 3))
	img.Set(0, 0, color.RGBA{255, 0, 0, 255})
	return img
}

func TestNativeFormatsKeepTheirBytes(t *testing.T) {
	var buf bytes.Buffer
	png.Encode(&buf, sample())
	p, err := Load(buf.Bytes(), "")
	if err != nil {
		t.Fatal(err)
	}
	if p.Pixels != nil || !bytes.Equal(p.Data, buf.Bytes()) || p.Width != 4 || p.Height != 3 || p.Orientation != 1 {
		t.Errorf("png = %+v", p)
	}
	buf.Reset()
	gif.Encode(&buf, sample(), nil)
	if p, err := Load(buf.Bytes(), ""); err != nil || p.Data == nil || p.Format != "gif" {
		t.Errorf("gif = %+v, %v", p, err)
	}
}

func TestBMPAndTIFFAreDecodedToPixels(t *testing.T) {
	for name, enc := range map[string]func(*bytes.Buffer) error{
		"bmp":  func(b *bytes.Buffer) error { return bmp.Encode(b, sample()) },
		"tiff": func(b *bytes.Buffer) error { return tiff.Encode(b, sample(), nil) },
	} {
		var buf bytes.Buffer
		if err := enc(&buf); err != nil {
			t.Fatal(err)
		}
		p, err := Load(buf.Bytes(), "")
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if p.Data != nil || p.Pixels == nil || p.Pixels.Rect.Dx() != 4 || p.Format != name {
			t.Errorf("%s = %+v", name, p)
		}
		if r, _, _, _ := p.Pixels.At(0, 0).RGBA(); r>>8 != 255 {
			t.Errorf("%s: first pixel red = %d", name, r>>8)
		}
	}
}

func TestGarbageIsAnError(t *testing.T) {
	if _, err := Load([]byte("not an image"), "x.png"); err == nil {
		t.Fatal("no error")
	}
}

func TestOrientationTransformsTurnImagesUpright(t *testing.T) {
	want := map[int][2]any{1: {0, false}, 2: {0, true}, 3: {180, false}, 4: {180, true},
		5: {270, true}, 6: {90, false}, 7: {90, true}, 8: {270, false}, 0: {0, false}}
	for o, w := range want {
		r, m := OrientationTransform(o)
		if r != w[0].(int) || m != w[1].(bool) {
			t.Errorf("orientation %d = %d, %v; want %v", o, r, m, w)
		}
	}
}

// icoOf wraps a PNG in an ICO container, as Vista and later icons hold
// their large images.
func icoOf(t *testing.T, img image.Image) []byte {
	t.Helper()
	var p bytes.Buffer
	if err := png.Encode(&p, img); err != nil {
		t.Fatal(err)
	}
	b := img.Bounds()
	var ico bytes.Buffer
	binary.Write(&ico, binary.LittleEndian, []uint16{0, 1, 1})
	ico.Write([]byte{byte(b.Dx()), byte(b.Dy()), 0, 0})
	binary.Write(&ico, binary.LittleEndian, []uint16{1, 32})
	binary.Write(&ico, binary.LittleEndian, []uint32{uint32(p.Len()), 22})
	ico.Write(p.Bytes())
	return ico.Bytes()
}

// Windows decodes ICO itself, with no extension to install.
func TestICOIsDecodedByWindows(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 16, 8))
	src.Set(1, 2, color.RGBA{0, 0, 255, 255})
	data := icoOf(t, src)
	for _, name := range []string{"app.ico", "misnamed.jpg"} {
		p, err := Load(data, name)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if p.Pixels == nil || p.Width != 16 || p.Height != 8 || p.Pixels.RGBAAt(1, 2) != (color.RGBA{0, 0, 255, 255}) {
			t.Errorf("%s = %dx%d, pixel %v", name, p.Width, p.Height, p.Pixels.RGBAAt(1, 2))
		}
	}
}

// nsgo draws an SVG from its bytes; its size in points is its size.
func TestSVGKeepsItsBytes(t *testing.T) {
	svg := []byte(`<svg xmlns="http://www.w3.org/2000/svg" width="120" height="80"><rect width="120" height="80"/></svg>`)
	p, err := Load(svg, "drawing.svg")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(p.Data, svg) || p.Pixels != nil || p.Width != 120 || p.Height != 80 || p.Format != "svg" {
		t.Errorf("svg = %+v", p)
	}
}

// A file the decoder for its extension cannot read fails, and the
// failure names the extension to install only when that is the reason.
func TestBrokenFilesOfCodecFormats(t *testing.T) {
	for name, codec := range map[string]Codec{"photo.heic": CodecHEIF, "photo.avif": CodecAV1, "photo.jxl": CodecJXL, "photo.nef": CodecRaw} {
		_, err := Load([]byte("not an image at all"), name)
		var missing MissingCodecError
		if err == nil || errors.As(err, &missing) && missing.Codec != codec {
			t.Errorf("%s: err = %v", name, err)
		}
	}
}
