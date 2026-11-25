package colorprofile

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// windowsSRGB is the sRGB profile Windows ships.
var windowsSRGB = filepath.Join(os.Getenv("SystemRoot"), "System32", "spool", "drivers", "color", "sRGB Color Space Profile.icm")

// Display P3's colorants adapted to D50, as Apple's Display P3 profile
// records them.
var p3Colorants = [3][3]float64{
	{0.5151, 0.2412, -0.0011},
	{0.2920, 0.6922, 0.0419},
	{0.1571, 0.0666, 0.7841},
}

// s15 encodes v as an ICC s15Fixed16Number.
func s15(v float64) uint32 {
	if v < 0 {
		return uint32(int32(v*65536 - 0.5))
	}
	return uint32(int32(v*65536 + 0.5))
}

// synthProfile builds a minimal valid ICC v2 display profile: header,
// desc, cprt, wtpt, rXYZ/gXYZ/bXYZ and a pure gamma for every channel.
func synthProfile(desc string, colorants [3][3]float64, gamma float64) []byte {
	be := binary.BigEndian
	xyz := func(v [3]float64) []byte {
		b := []byte("XYZ \x00\x00\x00\x00")
		for _, c := range v {
			b = be.AppendUint32(b, s15(c))
		}
		return b
	}
	textDesc := be.AppendUint32([]byte("desc\x00\x00\x00\x00"), uint32(len(desc)+1))
	textDesc = append(textDesc, desc...)
	textDesc = append(textDesc, 0)
	textDesc = append(textDesc, make([]byte, 4+4+2+1+67)...) // empty Unicode and ScriptCode parts
	curv := be.AppendUint16([]byte("curv\x00\x00\x00\x00\x00\x00\x00\x01"), uint16(gamma*256+0.5))
	tags := []struct {
		sig  string
		data []byte
	}{
		{"desc", textDesc},
		{"cprt", []byte("text\x00\x00\x00\x00No copyright\x00")},
		{"wtpt", xyz([3]float64{0.9642, 1, 0.8249})},
		{"rXYZ", xyz(colorants[0])},
		{"gXYZ", xyz(colorants[1])},
		{"bXYZ", xyz(colorants[2])},
		{"rTRC", curv},
		{"gTRC", curv},
		{"bTRC", curv},
	}
	header := make([]byte, 128)
	be.PutUint32(header[8:], 0x02100000)
	copy(header[12:], "mntrRGB XYZ ")
	be.PutUint16(header[24:], 2026)
	be.PutUint16(header[26:], 1)
	be.PutUint16(header[28:], 1)
	copy(header[36:], "acsp")
	be.PutUint32(header[68:], s15(0.9642))
	be.PutUint32(header[72:], s15(1))
	be.PutUint32(header[76:], s15(0.8249))
	table := be.AppendUint32(nil, uint32(len(tags)))
	var body []byte
	off := 128 + 4 + 12*len(tags)
	for _, t := range tags {
		table = append(table, t.sig...)
		table = be.AppendUint32(table, uint32(off+len(body)))
		table = be.AppendUint32(table, uint32(len(t.data)))
		body = append(body, t.data...)
		for len(body)%4 != 0 {
			body = append(body, 0)
		}
	}
	out := append(append(header, table...), body...)
	be.PutUint32(out, uint32(len(out)))
	return out
}

// pngWith encodes img as PNG and inserts an iCCP chunk with profile
// right after IHDR, where the specification places it.
func pngWith(t testing.TB, img image.Image, profile []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := (&png.Encoder{CompressionLevel: png.BestSpeed}).Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	var z bytes.Buffer
	zw := zlib.NewWriter(&z)
	zw.Write(profile)
	zw.Close()
	chunk := append([]byte("iCCPICC\x00\x00"), z.Bytes()...)
	full := binary.BigEndian.AppendUint32(nil, uint32(len(chunk)-4))
	full = append(full, chunk...)
	full = binary.BigEndian.AppendUint32(full, crc32.ChecksumIEEE(chunk))
	b := buf.Bytes()
	const ihdrEnd = 8 + 25
	return append(append(append([]byte{}, b[:ihdrEnd]...), full...), b[ihdrEnd:]...)
}

func TestEmbeddedJPEGSplit(t *testing.T) {
	profile := synthProfile("Synthetic P3", p3Colorants, 2.2)
	half := len(profile) / 2
	app2 := func(seq byte, part []byte) []byte {
		seg := append([]byte("ICC_PROFILE\x00"), seq, 2)
		seg = append(seg, part...)
		return append([]byte{0xFF, 0xE2, byte((len(seg) + 2) >> 8), byte(len(seg) + 2)}, seg...)
	}
	data := []byte{0xFF, 0xD8, 0xFF, 0xE0, 0, 4, 'J', 'F'} // SOI, a short APP0
	data = append(data, app2(2, profile[half:])...)        // out of order on purpose
	data = append(data, app2(1, profile[:half])...)
	data = append(data, 0xFF, 0xDA, 0, 2, 0xFF, 0xE2, 0, 2, 0xFF, 0xD9) // SOS ends the search
	if got := Embedded(data); !bytes.Equal(got, profile) {
		t.Fatalf("reassembled %d bytes, want %d", len(got), len(profile))
	}
	// A missing piece must not yield a truncated profile.
	if got := Embedded(append([]byte{0xFF, 0xD8}, app2(1, profile[:half])...)); got != nil {
		t.Fatalf("incomplete profile returned %d bytes", len(got))
	}
}

func TestEmbeddedPNG(t *testing.T) {
	profile := synthProfile("Synthetic P3", p3Colorants, 2.2)
	data := pngWith(t, image.NewRGBA(image.Rect(0, 0, 2, 2)), profile)
	if got := Embedded(data); !bytes.Equal(got, profile) {
		t.Fatalf("got %d bytes, want %d", len(got), len(profile))
	}
	if _, err := png.Decode(bytes.NewReader(data)); err != nil {
		t.Fatalf("test PNG invalid: %v", err)
	}
	var plain bytes.Buffer
	png.Encode(&plain, image.NewRGBA(image.Rect(0, 0, 1, 1)))
	if got := Embedded(plain.Bytes()); got != nil {
		t.Fatalf("untagged PNG returned a profile")
	}
}

func TestEmbeddedWebP(t *testing.T) {
	profile := synthProfile("Synthetic P3", p3Colorants, 2.2)
	chunk := func(fourcc string, body []byte) []byte {
		b := binary.LittleEndian.AppendUint32([]byte(fourcc), uint32(len(body)))
		b = append(b, body...)
		if len(body)%2 == 1 {
			b = append(b, 0)
		}
		return b
	}
	body := append([]byte("WEBP"), chunk("VP8X", make([]byte, 10))...)
	body = append(body, chunk("ICCP", profile)...)
	data := binary.LittleEndian.AppendUint32([]byte("RIFF"), uint32(len(body)))
	data = append(data, body...)
	if got := Embedded(data); !bytes.Equal(got, profile) {
		t.Fatalf("got %d bytes, want %d", len(got), len(profile))
	}
}

func TestIsSRGB(t *testing.T) {
	if b, err := os.ReadFile(windowsSRGB); err == nil {
		if !IsSRGB(b) {
			t.Errorf("Windows sRGB profile not recognized")
		}
	} else {
		t.Logf("skipping the Windows sRGB profile: %v", err)
	}
	if IsSRGB(synthProfile("Synthetic P3", p3Colorants, 2.2)) {
		t.Errorf("Display P3 colorants taken for sRGB")
	}
	// Colorants alone identify sRGB under another name.
	if !IsSRGB(synthProfile("Monitor default", srgbColorants, 2.2)) {
		t.Errorf("sRGB colorants not recognized")
	}
	if !IsSRGB(synthProfile("sRGB IEC61966-2.1", p3Colorants, 2.2)) {
		t.Errorf("sRGB description not recognized")
	}
	// Malformed input is left alone.
	for _, b := range [][]byte{nil, []byte("short"), make([]byte, 200)} {
		if !IsSRGB(b) {
			t.Errorf("malformed profile of %d bytes counted as convertible", len(b))
		}
	}
}

func TestIsSRGBMluc(t *testing.T) {
	p := synthProfile("Synthetic P3", p3Colorants, 2.2)
	// Point the desc entry (the first tag) at a v4 mluc naming sRGB.
	name := []uint16{'s', 'R', 'G', 'B', ' ', 'v', '4'}
	mluc := binary.BigEndian.AppendUint32([]byte("mluc\x00\x00\x00\x00"), 1)
	mluc = binary.BigEndian.AppendUint32(mluc, 12)
	mluc = append(mluc, "enUS"...)
	mluc = binary.BigEndian.AppendUint32(mluc, uint32(len(name)*2))
	mluc = binary.BigEndian.AppendUint32(mluc, 28)
	for _, c := range name {
		mluc = binary.BigEndian.AppendUint16(mluc, c)
	}
	off := len(p)
	p = append(p, mluc...)
	binary.BigEndian.PutUint32(p, uint32(len(p)))
	binary.BigEndian.PutUint32(p[132+4:], uint32(off))
	binary.BigEndian.PutUint32(p[132+8:], uint32(len(mluc)))
	if !IsSRGB(p) {
		t.Errorf("mluc description naming sRGB not recognized")
	}
}

func needWindows(t testing.TB) {
	if runtime.GOOS != "windows" {
		t.Skip("WIC is Windows only")
	}
}

func TestConvertP3Red(t *testing.T) {
	needWindows(t)
	img := image.NewNRGBA(image.Rect(0, 0, 4, 1))
	img.Set(0, 0, color.NRGBA{255, 0, 0, 255})
	img.Set(1, 0, color.NRGBA{255, 255, 255, 255})
	img.Set(2, 0, color.NRGBA{0, 0, 0, 128})
	// A muted P3 red inside the sRGB gamut must come out more saturated;
	// pure red alone would pass even if the transform did nothing.
	img.Set(3, 0, color.NRGBA{180, 90, 90, 255})
	data := pngWith(t, img, synthProfile("Synthetic P3", p3Colorants, 2.2))
	out, err := Convert(data, nil)
	if err != nil {
		t.Fatal(err)
	}
	red := out.RGBAAt(0, 0)
	t.Logf("P3 red -> sRGB %v", red)
	if red.R < 250 || red.G > 40 || red.B > 40 || red.A != 255 {
		t.Errorf("P3 red became %v, want saturated sRGB red", red)
	}
	if white := out.RGBAAt(1, 0); white.R < 250 || white.G < 250 || white.B < 250 {
		t.Errorf("white became %v", white)
	}
	if a := out.RGBAAt(2, 0); a.A < 127 || a.A > 129 || a.R > 1 {
		t.Errorf("translucent black became %v, alpha not kept", a)
	}
	muted := out.RGBAAt(3, 0)
	t.Logf("P3 (180,90,90) -> sRGB %v", muted)
	if int(muted.R)-int(muted.G) < 90+10 {
		t.Errorf("muted P3 red became %v, not converted", muted)
	}
}

func TestConvertSRGBIdentity(t *testing.T) {
	needWindows(t)
	profile, err := os.ReadFile(windowsSRGB)
	if err != nil {
		t.Skipf("no Windows sRGB profile: %v", err)
	}
	img := image.NewNRGBA(image.Rect(0, 0, 16, 16))
	for i := range img.Pix {
		img.Pix[i] = byte(i * 37)
	}
	for i := 3; i < len(img.Pix); i += 4 {
		img.Pix[i] = 255
	}
	out, err := Convert(pngWith(t, img, profile), nil)
	if err != nil {
		t.Fatal(err)
	}
	for i := range img.Pix {
		if d := int(img.Pix[i]) - int(out.Pix[i]); d < -1 || d > 1 {
			t.Fatalf("byte %d: %d became %d", i, img.Pix[i], out.Pix[i])
		}
	}
}

// ConvertScaled shrinks while it converts: one-pixel stripes average to
// gray, and the halves of the image keep their colors.
func TestConvertScaled(t *testing.T) {
	needWindows(t)
	img := image.NewNRGBA(image.Rect(0, 0, 16, 8))
	for y := range 8 {
		for x := range 16 {
			c := color.NRGBA{0, 0, 255, 255} // the right half blue
			if x < 8 {
				c = color.NRGBA{uint8(255 * (x % 2)), uint8(255 * (x % 2)), uint8(255 * (x % 2)), 255}
			}
			img.Set(x, y, c)
		}
	}
	var plain bytes.Buffer
	if err := png.Encode(&plain, img); err != nil {
		t.Fatal(err)
	}
	out, err := ConvertScaled(plain.Bytes(), nil, 4, 2)
	if err != nil {
		t.Fatal(err)
	}
	if out.Rect.Dx() != 4 || out.Rect.Dy() != 2 {
		t.Fatalf("size %v", out.Rect)
	}
	if g := out.RGBAAt(0, 0); g.R < 110 || g.R > 145 || g.A != 255 {
		t.Errorf("stripes became %v, want mid gray", g)
	}
	if b := out.RGBAAt(3, 1); b.B < 250 || b.R > 5 {
		t.Errorf("blue became %v", b)
	}
	if _, err := ConvertScaled(plain.Bytes(), nil, 0, 2); err == nil {
		t.Error("an empty size converted")
	}
}

func TestDisplay(t *testing.T) {
	p, err := Display(0)
	if runtime.GOOS != "windows" {
		if !errors.Is(err, errors.ErrUnsupported) {
			t.Fatalf("got %v, want ErrUnsupported", err)
		}
		return
	}
	t.Logf("display profile: %d bytes, err %v, sRGB %v", len(p), err, p == nil || IsSRGB(p))
}

// BenchmarkConvert6000x4000 converts a 24-megapixel Display P3 PNG, the
// size of a typical camera JPEG.
func BenchmarkConvert6000x4000(b *testing.B) {
	needWindows(b)
	img := image.NewNRGBA(image.Rect(0, 0, 6000, 4000))
	for y := range 4000 {
		row := img.Pix[y*img.Stride:]
		for x := range 6000 {
			row[x*4], row[x*4+1], row[x*4+2], row[x*4+3] = byte(x), byte(y), byte(x+y), 255
		}
	}
	data := pngWith(b, img, synthProfile("Synthetic P3", p3Colorants, 2.2))
	for b.Loop() {
		if _, err := Convert(data, nil); err != nil {
			b.Fatal(err)
		}
	}
}
