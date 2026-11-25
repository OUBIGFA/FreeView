package anim

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"image"
	"image/color"
	"image/color/palette"
	"image/gif"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var (
	red   = color.RGBA{255, 0, 0, 255}
	blue  = color.RGBA{0, 0, 255, 255}
	clear = color.RGBA{}
)

func at(t *testing.T, img *image.RGBA, x, y int, want color.RGBA) {
	t.Helper()
	if got := img.RGBAAt(x, y); got != want {
		t.Errorf("pixel %d,%d = %v, want %v", x, y, got, want)
	}
}

func paletted(r image.Rectangle, c color.Color) *image.Paletted {
	p := image.NewPaletted(r, append(color.Palette{color.Transparent}, palette.Plan9[:255]...))
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			p.Set(x, y, c)
		}
	}
	return p
}

// A GIF of three frames: a red full frame, a blue 2×2 square disposed to
// the background, then a transparent frame that shows what is beneath.
func TestGIFDisposal(t *testing.T) {
	g := &gif.GIF{
		Image:     []*image.Paletted{paletted(image.Rect(0, 0, 4, 4), red), paletted(image.Rect(0, 0, 2, 2), blue), paletted(image.Rect(2, 2, 4, 4), color.Transparent)},
		Delay:     []int{0, 5, 20},
		Disposal:  []byte{gif.DisposalNone, gif.DisposalBackground, gif.DisposalNone},
		LoopCount: 2,
	}
	var b bytes.Buffer
	if err := gif.EncodeAll(&b, g); err != nil {
		t.Fatal(err)
	}
	if !IsAnimated(b.Bytes()) {
		t.Fatal("not seen as animated")
	}
	a, err := Decode(b.Bytes(), 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	if len(a.Frames) != 3 || a.Loops != 3 || a.Width != 4 {
		t.Fatalf("frames %d loops %d width %d", len(a.Frames), a.Loops, a.Width)
	}
	at(t, a.Frames[1].Image, 0, 0, blue)
	at(t, a.Frames[1].Image, 3, 3, red)
	at(t, a.Frames[2].Image, 0, 0, clear) // the blue square went to the background
	at(t, a.Frames[2].Image, 3, 3, red)   // the transparent frame kept what was there
	if a.Frames[0].Delay != 100*time.Millisecond || a.Frames[2].Delay != 200*time.Millisecond {
		t.Errorf("delays %v %v", a.Frames[0].Delay, a.Frames[2].Delay)
	}
	if _, err := Decode(b.Bytes(), 100); err != ErrTooLarge {
		t.Errorf("budget: %v", err)
	}
	var still bytes.Buffer
	gif.Encode(&still, paletted(image.Rect(0, 0, 2, 2), red), nil)
	if IsAnimated(still.Bytes()) {
		t.Error("a still GIF seen as animated")
	}
	if _, err := Decode(still.Bytes(), 1<<20); err != ErrNotAnimated {
		t.Errorf("still GIF: %v", err)
	}
}

func chunk(typ string, data []byte) []byte {
	var b bytes.Buffer
	writeChunk(&b, typ, data)
	return b.Bytes()
}

func fcTL(seq, w, h, x, y int, num, den uint16, dispose, blend byte) []byte {
	d := make([]byte, 26)
	for i, v := range []int{seq, w, h, x, y} {
		binary.BigEndian.PutUint32(d[i*4:], uint32(v))
	}
	binary.BigEndian.PutUint16(d[20:], num)
	binary.BigEndian.PutUint16(d[22:], den)
	d[24], d[25] = dispose, blend
	return chunk("fcTL", d)
}

// idat returns the IHDR and IDAT data of a w×h 8-bit RGBA image of
// color c, written by hand so every frame shares one color type.
func idat(t *testing.T, w, h int, c color.NRGBA) (ihdr, data []byte) {
	ihdr = binary.BigEndian.AppendUint32(binary.BigEndian.AppendUint32(nil, uint32(w)), uint32(h))
	ihdr = append(ihdr, 8, 6, 0, 0, 0)
	var raw bytes.Buffer
	z := zlib.NewWriter(&raw)
	for range h {
		z.Write([]byte{0}) // filter none
		for range w {
			z.Write([]byte{c.R, c.G, c.B, c.A})
		}
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	return ihdr, raw.Bytes()
}

// An APNG: the default image is a red frame; then a blue 2×2 square at
// 1,1 blended over and disposed to the previous canvas; then a
// half-transparent frame that replaces (blend source) its square.
func TestAPNG(t *testing.T) {
	ihdr, red4 := idat(t, 4, 4, color.NRGBA(red))
	_, blue2 := idat(t, 2, 2, color.NRGBA(blue))
	_, half := idat(t, 1, 1, color.NRGBA{0, 255, 0, 128})
	acTL := make([]byte, 8)
	binary.BigEndian.PutUint32(acTL, 3)
	fd := func(seq int, data []byte) []byte {
		return chunk("fdAT", append(binary.BigEndian.AppendUint32(nil, uint32(seq)), data...))
	}
	file := bytes.Join([][]byte{pngSignature, chunk("IHDR", ihdr), chunk("acTL", acTL),
		fcTL(0, 4, 4, 0, 0, 1, 10, 0, 0), chunk("IDAT", red4),
		fcTL(1, 2, 2, 1, 1, 0, 0, 2, 1), fd(2, blue2),
		fcTL(3, 1, 1, 0, 0, 3, 0, 0, 0), fd(4, half),
		chunk("IEND", nil)}, nil)
	if !IsAnimated(file) {
		t.Fatal("not seen as animated")
	}
	a, err := Decode(file, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	if len(a.Frames) != 3 || a.Loops != 0 {
		t.Fatalf("frames %d loops %d", len(a.Frames), a.Loops)
	}
	at(t, a.Frames[0].Image, 2, 2, red)
	at(t, a.Frames[1].Image, 1, 1, blue)
	at(t, a.Frames[1].Image, 0, 0, red)
	at(t, a.Frames[2].Image, 1, 1, red) // disposed to the previous canvas
	if got := a.Frames[2].Image.RGBAAt(0, 0); got.R != 0 || got.A != 128 {
		t.Errorf("blend source kept the red beneath: %v", got)
	}
	if a.Frames[0].Delay != 100*time.Millisecond || a.Frames[2].Delay != 30*time.Millisecond {
		t.Errorf("delays %v %v", a.Frames[0].Delay, a.Frames[2].Delay)
	}
	var still bytes.Buffer
	png.Encode(&still, image.NewRGBA(image.Rect(0, 0, 2, 2)))
	if IsAnimated(still.Bytes()) {
		t.Error("a still PNG seen as animated")
	}
	for n := 0; n < len(file); n += 7 {
		Decode(file[:n], 1<<20) // cut-off files must not panic
	}
}

// losslessWebP reads a lossless WebP still of golang.org/x/image's test
// data, from the module cache; the test skips without it.
func losslessWebP(t *testing.T) []byte {
	out, err := exec.Command("go", "list", "-m", "-f", "{{.Dir}}", "golang.org/x/image").Output()
	if err != nil {
		t.Skip("no go command:", err)
	}
	data, err := os.ReadFile(filepath.Join(strings.TrimSpace(string(out)), "testdata", "gopher-doc.1bpp.lossless.webp"))
	if err != nil {
		t.Skip("no x/image test data:", err)
	}
	return data
}

// An animated WebP of the same lossless frame twice, the second offset
// and not blended, with a loop count.
func TestAnimatedWebP(t *testing.T) {
	still := losslessWebP(t)
	var vp8l []byte
	for _, c := range riffChunks(still[12:]) {
		if c.typ == "VP8L" {
			vp8l = c.data
		}
	}
	bits := binary.LittleEndian.Uint32(vp8l[1:])
	w, h := int(bits&0x3fff)+1, int(bits>>14&0x3fff)+1
	x := make([]byte, 10)
	x[0] = webpAnimation
	putU24(x[4:], w+2-1)
	putU24(x[7:], h-1)
	anim := []byte{0, 0, 0, 0, 3, 0}
	frame := func(xoff, ms int, flags byte) []byte {
		f := make([]byte, 16)
		putU24(f, xoff/2)
		putU24(f[6:], w-1)
		putU24(f[9:], h-1)
		putU24(f[12:], ms)
		f[15] = flags
		return appendRIFFChunk(f, "VP8L", vp8l)
	}
	var body []byte
	body = appendRIFFChunk(body, "VP8X", x)
	body = appendRIFFChunk(body, "ANIM", anim)
	body = appendRIFFChunk(body, "ANMF", frame(0, 40, 0x01))
	body = appendRIFFChunk(body, "ANMF", frame(2, 0, 0x02))
	file := append(binary.LittleEndian.AppendUint32([]byte("RIFF"), uint32(4+len(body))), append([]byte("WEBP"), body...)...)
	if !IsAnimated(file) || IsAnimated(still) {
		t.Fatal("animation not told from a still")
	}
	a, err := Decode(file, 1<<26)
	if err != nil {
		t.Fatal(err)
	}
	if len(a.Frames) != 2 || a.Loops != 3 || a.Width != w+2 || a.Frames[0].Delay != 40*time.Millisecond || a.Frames[1].Delay != 100*time.Millisecond {
		t.Fatalf("frames %d loops %d width %d delays %v", len(a.Frames), a.Loops, a.Width, a.Frames[0].Delay)
	}
	// The first frame was disposed, so the second's leftmost columns are clear.
	at(t, a.Frames[1].Image, 0, h/2, clear)
}

func TestPlayerKeepsTimeExact(t *testing.T) {
	a := &Animation{Frames: make([]Frame, 10)}
	for i := range a.Frames {
		a.Frames[i].Delay = 20 * time.Millisecond
	}
	p := NewPlayer(a)
	for range 3 {
		p.Advance(33333*time.Microsecond, 1)
	}
	// 99.999 ms over 20 ms frames is frame 4, not frame 4 minus the
	// rounding of each tick.
	if p.Frame() != 4 {
		t.Errorf("frame %d", p.Frame())
	}
	p.Advance(40*time.Millisecond, 0.5)
	if p.Frame() != 5 {
		t.Errorf("half speed: frame %d", p.Frame())
	}
	p.Step(-7)
	if p.Frame() != 8 {
		t.Errorf("step wrap: frame %d", p.Frame())
	}
}

func TestPlayerStopsAfterLoops(t *testing.T) {
	a := &Animation{Loops: 2, Frames: []Frame{{Delay: 100 * time.Millisecond}, {Delay: 100 * time.Millisecond}}}
	p := NewPlayer(a)
	p.Advance(time.Second, 1)
	if !p.Done() || p.Frame() != 1 {
		t.Errorf("done %v frame %d", p.Done(), p.Frame())
	}
	if p.Advance(time.Second, 1) {
		t.Error("advanced after the end")
	}
	p.Reset()
	if p.Done() || p.Frame() != 0 {
		t.Error("reset")
	}
}
