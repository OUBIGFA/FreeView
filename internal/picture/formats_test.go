package picture

// Fixtures for the pure-Go decoders, produced in the test with the
// libraries' own encoders, or built by hand for PSD, which has none.

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/blezek/tga"
	"github.com/mdouchement/hdr"
	"github.com/mdouchement/hdr/codec/rgbe"
	"github.com/mdouchement/hdr/hdrcolor"
	"github.com/mrjoshuak/go-openexr/exr"
	"github.com/xfmoulet/qoi"
)

// checkPixels fails unless p is decoded pixels of w × h in format with
// want at (x, y), allowing tol per channel.
func checkPixels(t *testing.T, name string, p Picture, w, h int, format string, x, y int, want color.RGBA, tol int) {
	t.Helper()
	if p.Data != nil || p.Pixels == nil || p.Width != w || p.Height != h || p.Pixels.Rect.Dx() != w ||
		p.Pixels.Rect.Dy() != h || p.Format != format || p.Orientation != 1 {
		t.Fatalf("%s = %dx%d %q orientation %d, data %v", name, p.Width, p.Height, p.Format, p.Orientation, p.Data != nil)
	}
	got := p.Pixels.RGBAAt(x, y)
	d := func(a, b uint8) bool { return int(a)-int(b) > tol || int(b)-int(a) > tol }
	if d(got.R, want.R) || d(got.G, want.G) || d(got.B, want.B) || d(got.A, want.A) {
		t.Errorf("%s: pixel (%d, %d) = %v, want %v", name, x, y, got, want)
	}
}

// translucent is 5 × 3 with a half-transparent red pixel at (1, 2) and
// opaque blue elsewhere.
func translucent() *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, 5, 3))
	for i := 0; i < len(img.Pix); i += 4 {
		copy(img.Pix[i:], []byte{0, 0, 255, 255})
	}
	img.SetNRGBA(1, 2, color.NRGBA{255, 0, 0, 128})
	return img
}

// Pixels come premultiplied, as image.RGBA is: half-transparent red is
// 128, 0, 0, 128.
var halfRed = color.RGBA{128, 0, 0, 128}

// qoiFile writes img as QOI by hand, every pixel a QOI_OP_RGBA chunk:
// xfmoulet/qoi's encoder writes premultiplied values where QOI stores
// straight alpha, so it cannot make a translucent fixture.
func qoiFile(img *image.NRGBA) []byte {
	var b bytes.Buffer
	b.WriteString("qoif")
	binary.Write(&b, binary.BigEndian, []uint32{uint32(img.Rect.Dx()), uint32(img.Rect.Dy())})
	b.Write([]byte{4, 0}) // RGBA, sRGB
	for i := 0; i < len(img.Pix); i += 4 {
		b.WriteByte(0xff)
		b.Write(img.Pix[i : i+4])
	}
	b.Write(qoiEnd)
	return b.Bytes()
}

func TestQOIDecodes(t *testing.T) {
	// The library's encoder still round-trips opaque images.
	var enc bytes.Buffer
	opaque := translucent()
	opaque.SetNRGBA(1, 2, color.NRGBA{255, 0, 0, 255})
	if err := qoi.Encode(&enc, opaque); err != nil {
		t.Fatal(err)
	}
	p, err := Load(enc.Bytes(), "a.qoi")
	if err != nil {
		t.Fatal(err)
	}
	checkPixels(t, "encoded", p, 5, 3, "qoi", 1, 2, color.RGBA{255, 0, 0, 255}, 0)
	for _, name := range []string{"a.qoi", "misnamed.png"} {
		p, err := Load(qoiFile(translucent()), name)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		checkPixels(t, name, p, 5, 3, "qoi", 1, 2, halfRed, 1)
		checkPixels(t, name, p, 5, 3, "qoi", 0, 0, color.RGBA{0, 0, 255, 255}, 0)
	}
}

func TestTGARoundTrip(t *testing.T) {
	var buf bytes.Buffer
	if err := tga.Encode(&buf, translucent()); err != nil {
		t.Fatal(err)
	}
	for _, ext := range tgaExtensions {
		p, err := Load(buf.Bytes(), "a"+strings.ToUpper(ext))
		if err != nil {
			t.Fatalf("%s: %v", ext, err)
		}
		checkPixels(t, ext, p, 5, 3, "tga", 1, 2, halfRed, 1)
		checkPixels(t, ext, p, 5, 3, "tga", 4, 0, color.RGBA{0, 0, 255, 255}, 0)
	}
	// TGA has no magic: under another name it is not guessed at.
	if _, err := Load(buf.Bytes(), "a.png"); err == nil {
		t.Error("a TGA named .png decoded")
	}
}

// A TGA header claiming 65535 × 65535 is refused before its pixels are
// allocated.
func TestHugeTGAIsRefused(t *testing.T) {
	h := make([]byte, 18+26) // header and a (blank) footer
	h[2] = 2                 // true colour
	binary.LittleEndian.PutUint16(h[12:], 65535)
	binary.LittleEndian.PutUint16(h[14:], 65535)
	h[16] = 32
	if _, err := Load(h, "huge.tga"); err == nil || !strings.Contains(err.Error(), "size") {
		t.Errorf("err = %v", err)
	}
}

// psdFile builds an 8-bit RGB PSD of w × h with raw (uncompressed) image
// data, every pixel c. merged, when not nil, adds resource 1057 with its
// hasRealMergedData flag.
func psdFile(w, h int, c color.RGBA, merged *bool) []byte {
	var b bytes.Buffer
	be := func(v any) { binary.Write(&b, binary.BigEndian, v) }
	b.WriteString("8BPS")
	be(uint16(1))
	b.Write(make([]byte, 6))
	be(uint16(3)) // channels
	be(uint32(h))
	be(uint32(w))
	be(uint16(8)) // depth
	be(uint16(3)) // RGB
	be(uint32(0)) // colour mode data
	var res bytes.Buffer
	if merged != nil {
		flag := byte(0)
		if *merged {
			flag = 1
		}
		res.WriteString("8BIM")
		binary.Write(&res, binary.BigEndian, uint16(psdVersionInfo))
		res.Write([]byte{0, 0}) // empty name, padded
		data := []byte{0, 0, 0, 1, flag}
		binary.Write(&res, binary.BigEndian, uint32(len(data)))
		res.Write(data)
		res.WriteByte(0) // pad to even
	}
	be(uint32(res.Len()))
	b.Write(res.Bytes())
	be(uint32(0)) // layer and mask information
	be(uint16(0)) // raw
	for _, v := range []byte{c.R, c.G, c.B} {
		b.Write(bytes.Repeat([]byte{v}, w*h))
	}
	return b.Bytes()
}

func TestPSDComposite(t *testing.T) {
	yes, no := true, false
	want := color.RGBA{10, 200, 30, 255}
	for _, merged := range []*bool{nil, &yes} {
		p, err := Load(psdFile(6, 4, want, merged), "art.psd")
		if err != nil {
			t.Fatal(err)
		}
		checkPixels(t, "psd", p, 6, 4, "psd", 5, 3, want, 0)
	}
	if _, err := Load(psdFile(6, 4, want, &no), "art.psd"); err == nil || !strings.Contains(err.Error(), "merged") {
		t.Errorf("a PSD without merged data: err = %v", err)
	}
}

func hdrFile(t *testing.T, w, h int, at func(x, y int) float64) []byte {
	t.Helper()
	m := hdr.NewRGB(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			v := at(x, y)
			m.SetRGB(x, y, hdrcolor.RGB{R: v, G: v, B: v})
		}
	}
	var buf bytes.Buffer
	if err := rgbe.Encode(&buf, m); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// A uniform grey maps to the key: 0.18 / 1.18 = 0.153, 108 after gamma
// 2.2, at whatever exposure the file was made.
func TestHDRIsToneMapped(t *testing.T) {
	for _, v := range []float64{0.05, 4, 300} {
		p, err := Load(hdrFile(t, 8, 2, func(int, int) float64 { return v }), "sky.hdr")
		if err != nil {
			t.Fatal(err)
		}
		checkPixels(t, "hdr", p, 8, 2, "hdr", 7, 1, color.RGBA{108, 108, 108, 255}, 2)
	}
	p, err := Load(hdrFile(t, 2, 1, func(x, _ int) float64 { return float64(1 + 99*x) }), "misnamed.jpg")
	if err != nil {
		t.Fatal(err)
	}
	if dark, light := p.Pixels.RGBAAt(0, 0).R, p.Pixels.RGBAAt(1, 0).R; dark >= light || light == 255 {
		t.Errorf("dark %d, light %d: highlights should compress, not clip", dark, light)
	}
}

func exrFile(t *testing.T, w, h int, r, g, b, a float32) []byte {
	t.Helper()
	m := exr.NewRGBAImage(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			m.SetRGBA(x, y, r*a, g*a, b*a, a)
		}
	}
	path := filepath.Join(t.TempDir(), "a.exr")
	if err := exr.EncodeFile(path, m); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestEXRIsToneMapped(t *testing.T) {
	data := exrFile(t, 7, 5, 2, 2, 2, 1)
	for _, name := range []string{"render.exr", "misnamed.png"} {
		p, err := Load(data, name)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		checkPixels(t, name, p, 7, 5, "exr", 6, 4, color.RGBA{108, 108, 108, 255}, 2)
	}
	// Half-transparent grey stays the key colour, premultiplied.
	p, err := Load(exrFile(t, 3, 3, 2, 2, 2, 0.5), "glass.exr")
	if err != nil {
		t.Fatal(err)
	}
	checkPixels(t, "exr alpha", p, 3, 3, "exr", 1, 1, color.RGBA{54, 54, 54, 128}, 2)
}

// Damaged files fail with an error, without a panic.
func TestDamagedFilesOfDecoderFormats(t *testing.T) {
	var tg bytes.Buffer
	tga.Encode(&tg, translucent())
	// The TGA encoder appends an extension area the image does not need;
	// cut the file inside its pixels, which end at 18 + 5 × 3 × 4 bytes.
	tgaPixels := tg.Bytes()[:18+60]
	files := map[string][]byte{
		"a.qoi": qoiFile(translucent()), "a.tga": tgaPixels, "a.psd": psdFile(6, 4, color.RGBA{1, 2, 3, 255}, nil),
		"a.hdr": hdrFile(t, 8, 8, func(x, y int) float64 { return float64(x + y) }),
		"a.exr": exrFile(t, 8, 8, 1, 0.5, 0.25, 1),
	}
	for name, data := range files {
		for _, n := range []int{0, 3, 17, len(data) / 2, len(data) - 1} {
			if _, err := Load(data[:n], name); err == nil {
				t.Errorf("%s cut to %d bytes: no error", name, n)
			}
		}
		flipped := bytes.Clone(data)
		for i := 18; i < len(flipped); i += 7 {
			flipped[i] ^= 0xa5
		}
		Load(flipped, name) // may decode noise, must not panic
		if _, err := Load([]byte("not an image at all"), name); err == nil {
			t.Errorf("%s: garbage decoded", name)
		}
	}
}

func TestExtensionsListNewFormats(t *testing.T) {
	exts := Extensions()
	for _, e := range []string{".dds", ".jxr", ".wdp", ".hdp", ".psd", ".psb", ".qoi", ".tga", ".icb", ".vda", ".vst", ".hdr", ".exr"} {
		if !strings.Contains(strings.Join(exts, " ")+" ", e+" ") {
			t.Errorf("Extensions() lacks %s", e)
		}
	}
}
