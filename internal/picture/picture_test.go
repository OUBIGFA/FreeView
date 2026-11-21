package picture

import (
	"bytes"
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
	p, err := Load(buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if p.Pixels != nil || !bytes.Equal(p.Data, buf.Bytes()) || p.Width != 4 || p.Height != 3 || p.Orientation != 1 {
		t.Errorf("png = %+v", p)
	}
	buf.Reset()
	gif.Encode(&buf, sample(), nil)
	if p, err := Load(buf.Bytes()); err != nil || p.Data == nil || p.Format != "gif" {
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
		p, err := Load(buf.Bytes())
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
	if _, err := Load([]byte("not an image")); err == nil {
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
