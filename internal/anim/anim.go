// Package anim decodes every frame of an animated GIF, APNG or WebP, so
// the viewer can pause, step and change the speed of an animation. nsgo
// plays GIF and WebP itself but offers none of these controls, and shows
// APNG as a still.
//
// Decoding all frames costs memory and time, so the viewer stays with
// nsgo's own playback until the user asks for a control (or the file is
// an APNG), and calls Decode off the UI thread.
package anim

import (
	"bytes"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"image"
	"image/draw"
	"image/gif"
	"image/png"
	"time"

	"golang.org/x/image/webp"
)

// Frame is one fully composited frame: the whole canvas, premultiplied
// as image.RGBA is, and how long it shows.
type Frame struct {
	Image *image.RGBA
	Delay time.Duration
}

// Animation is every frame of an animated image.
type Animation struct {
	Frames        []Frame
	Loops         int // times the animation plays; 0 plays it forever
	Width, Height int
}

var (
	// ErrNotAnimated is a still image: one frame or none. The caller
	// shows it as before.
	ErrNotAnimated = errors.New("anim: not an animation")
	// ErrTooLarge is an animation whose composited frames would exceed
	// the memory allowed; the caller keeps nsgo's own playback.
	ErrTooLarge = errors.New("anim: animation too large to decode")
)

var (
	pngSignature = []byte("\x89PNG\r\n\x1a\n")
	gifSignature = []byte("GIF8")
)

// IsAnimated cheaply reports whether data holds an animated GIF, APNG or
// WebP, without decoding pixels.
func IsAnimated(data []byte) bool {
	switch {
	case bytes.HasPrefix(data, gifSignature):
		return gifFrames(data, 2) > 1
	case bytes.HasPrefix(data, pngSignature):
		n, _ := apngFrames(data)
		return n > 1
	case isWebP(data):
		flags, ok := webpFlags(data)
		return ok && flags&webpAnimation != 0 && bytes.Contains(data, []byte("ANMF"))
	}
	return false
}

// Decode composites every frame of an animated GIF, APNG or WebP,
// following each format's disposal and blending rules. maxBytes bounds
// the frames' pixels (width × height × 4 × frames); a larger animation is
// ErrTooLarge, reported before the frames are composited.
func Decode(data []byte, maxBytes int) (a *Animation, err error) {
	// The decoders are fed untrusted files: a panic in one becomes an error.
	defer func() {
		if r := recover(); r != nil {
			a, err = nil, errors.New("anim: damaged animation")
		}
	}()
	switch {
	case bytes.HasPrefix(data, gifSignature):
		return decodeGIF(data, maxBytes)
	case bytes.HasPrefix(data, pngSignature):
		return decodeAPNG(data, maxBytes)
	case isWebP(data):
		return decodeWebP(data, maxBytes)
	}
	return nil, ErrNotAnimated
}

// delay applies the browsers' rule: a delay of 10 ms or less shows for
// 100 ms, as many files written for old browsers count on.
func delay(d time.Duration) time.Duration {
	if d <= 10*time.Millisecond {
		return 100 * time.Millisecond
	}
	return d
}

func checkBudget(w, h, frames, maxBytes int) error {
	if w <= 0 || h <= 0 {
		return errors.New("anim: empty canvas")
	}
	if frames < 2 {
		return ErrNotAnimated
	}
	// Divide the budget before comparing; multiplying file-supplied
	// dimensions and frame counts can overflow even a signed 64-bit int.
	if maxBytes <= 0 || int64(w) > int64(maxBytes)/4/int64(h)/int64(frames) {
		return ErrTooLarge
	}
	return nil
}

// snapshot copies the canvas as one frame.
func snapshot(canvas *image.RGBA) *image.RGBA {
	c := image.NewRGBA(canvas.Rect)
	copy(c.Pix, canvas.Pix)
	return c
}

// clearRect makes r of canvas transparent.
func clearRect(canvas *image.RGBA, r image.Rectangle) {
	draw.Draw(canvas, r, image.Transparent, image.Point{}, draw.Src)
}

// gifFrames counts a GIF's image descriptors by walking its blocks.
func gifFrames(data []byte, stopAt int) int {
	if len(data) < 13 {
		return 0
	}
	p := 13
	if data[10]&0x80 != 0 { // global color table
		p += 3 << (data[10]&7 + 1)
	}
	n := 0
	skipBlocks := func() bool {
		for p < len(data) {
			size := int(data[p])
			p += 1 + size
			if size == 0 {
				return true
			}
		}
		return false
	}
	for p < len(data) {
		switch data[p] {
		case 0x21: // extension: label, then sub-blocks
			p += 2
			if !skipBlocks() {
				return n
			}
		case 0x2c: // image descriptor
			n++
			if (stopAt > 0 && n >= stopAt) || p+10 > len(data) {
				return n
			}
			flags := data[p+9]
			p += 10
			if flags&0x80 != 0 {
				p += 3 << (flags&7 + 1)
			}
			p++ // LZW minimum code size
			if !skipBlocks() {
				return n
			}
		default: // trailer or damage
			return n
		}
	}
	return n
}

func decodeGIF(data []byte, maxBytes int) (*Animation, error) {
	config, err := gif.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	if err := checkBudget(config.Width, config.Height, gifFrames(data, 0), maxBytes); err != nil {
		return nil, err
	}
	g, err := gif.DecodeAll(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	w, h := g.Config.Width, g.Config.Height
	if err := checkBudget(w, h, len(g.Image), maxBytes); err != nil {
		return nil, err
	}
	// image/gif: 0 loops forever, -1 plays once, n plays n+1 times.
	loops := 0
	if g.LoopCount < 0 {
		loops = 1
	} else if g.LoopCount > 0 {
		loops = g.LoopCount + 1
	}
	a := &Animation{Loops: loops, Width: w, Height: h}
	canvas := image.NewRGBA(image.Rect(0, 0, w, h))
	for i, frame := range g.Image {
		var saved *image.RGBA
		disposal := byte(0)
		if i < len(g.Disposal) {
			disposal = g.Disposal[i]
		}
		if disposal == gif.DisposalPrevious {
			saved = snapshot(canvas)
		}
		draw.Draw(canvas, frame.Bounds(), frame, frame.Bounds().Min, draw.Over)
		d := 0
		if i < len(g.Delay) {
			d = g.Delay[i]
		}
		a.Frames = append(a.Frames, Frame{Image: snapshot(canvas), Delay: delay(time.Duration(d) * 10 * time.Millisecond)})
		switch disposal {
		case gif.DisposalBackground:
			// Browsers clear to transparent rather than to the background color.
			clearRect(canvas, frame.Bounds())
		case gif.DisposalPrevious:
			copy(canvas.Pix, saved.Pix)
		}
	}
	return a, nil
}

// pngChunk is one chunk of a PNG file.
type pngChunk struct {
	typ  string
	data []byte
}

// pngChunks walks a PNG's chunks, stopping at damage.
func pngChunks(data []byte) []pngChunk {
	var out []pngChunk
	for p := len(pngSignature); p+12 <= len(data); {
		n := int(binary.BigEndian.Uint32(data[p:]))
		if n < 0 || p+12+n > len(data) {
			break
		}
		c := pngChunk{typ: string(data[p+4 : p+8]), data: data[p+8 : p+8+n]}
		out = append(out, c)
		p += 12 + n
		if c.typ == "IEND" {
			break
		}
	}
	return out
}

// apngFrames is an APNG's frame count and play count from its acTL,
// which must come before the first IDAT.
func apngFrames(data []byte) (frames, plays int) {
	for _, c := range pngChunks(data) {
		switch c.typ {
		case "acTL":
			if len(c.data) >= 8 {
				return int(binary.BigEndian.Uint32(c.data)), int(binary.BigEndian.Uint32(c.data[4:]))
			}
			return 0, 0
		case "IDAT":
			return 0, 0
		}
	}
	return 0, 0
}

// apngFrame is one fcTL and the image data that follows it.
type apngFrame struct {
	w, h, x, y     int
	delay          time.Duration
	dispose, blend byte
	data           [][]byte
}

func writeChunk(b *bytes.Buffer, typ string, data []byte) {
	var n [4]byte
	binary.BigEndian.PutUint32(n[:], uint32(len(data)))
	b.Write(n[:])
	crc := crc32.NewIEEE()
	crc.Write([]byte(typ))
	crc.Write(data)
	b.WriteString(typ)
	b.Write(data)
	binary.BigEndian.PutUint32(n[:], crc.Sum32())
	b.Write(n[:])
}

func decodeAPNG(data []byte, maxBytes int) (*Animation, error) {
	chunks := pngChunks(data)
	if len(chunks) == 0 || chunks[0].typ != "IHDR" || len(chunks[0].data) != 13 {
		return nil, errors.New("anim: damaged PNG")
	}
	ihdr := chunks[0].data
	w, h := int(binary.BigEndian.Uint32(ihdr)), int(binary.BigEndian.Uint32(ihdr[4:]))
	count, plays := apngFrames(data)
	if count < 2 {
		return nil, ErrNotAnimated
	}
	// The chunks every frame's standalone PNG repeats: palette,
	// transparency and color information, all before the first IDAT.
	var shared []pngChunk
	var frames []*apngFrame
	var cur *apngFrame
	seenIDAT := false
	for _, c := range chunks[1:] {
		switch c.typ {
		case "fcTL":
			if len(c.data) < 26 {
				return nil, errors.New("anim: damaged fcTL")
			}
			d := c.data
			num, den := int(binary.BigEndian.Uint16(d[20:])), int(binary.BigEndian.Uint16(d[22:]))
			if den == 0 {
				den = 100
			}
			cur = &apngFrame{w: int(binary.BigEndian.Uint32(d[4:])), h: int(binary.BigEndian.Uint32(d[8:])),
				x: int(binary.BigEndian.Uint32(d[12:])), y: int(binary.BigEndian.Uint32(d[16:])),
				delay:   delay(time.Duration(num) * time.Second / time.Duration(den)),
				dispose: d[24], blend: d[25]}
			frames = append(frames, cur)
		case "IDAT":
			seenIDAT = true
			// The default image is a frame only when an fcTL precedes it.
			if cur != nil {
				cur.data = append(cur.data, c.data)
			}
		case "fdAT":
			if cur != nil && len(c.data) > 4 {
				cur.data = append(cur.data, c.data[4:])
			}
		case "IEND", "acTL":
		default:
			if !seenIDAT {
				shared = append(shared, c)
			}
		}
	}
	if err := checkBudget(w, h, len(frames), maxBytes); err != nil {
		return nil, err
	}
	a := &Animation{Loops: plays, Width: w, Height: h}
	canvas := image.NewRGBA(image.Rect(0, 0, w, h))
	for i, f := range frames {
		r := image.Rect(f.x, f.y, f.x+f.w, f.y+f.h)
		if f.w <= 0 || f.h <= 0 || !r.In(canvas.Rect) || len(f.data) == 0 {
			return nil, errors.New("anim: damaged APNG frame")
		}
		var b bytes.Buffer
		b.Write(pngSignature)
		hdr := append([]byte(nil), ihdr...)
		binary.BigEndian.PutUint32(hdr, uint32(f.w))
		binary.BigEndian.PutUint32(hdr[4:], uint32(f.h))
		writeChunk(&b, "IHDR", hdr)
		for _, c := range shared {
			writeChunk(&b, c.typ, c.data)
		}
		writeChunk(&b, "IDAT", bytes.Join(f.data, nil))
		writeChunk(&b, "IEND", nil)
		img, err := png.Decode(&b)
		if err != nil {
			return nil, err
		}
		dispose := f.dispose
		if i == 0 && dispose == 2 {
			dispose = 1 // the spec: "previous" on the first frame is "background"
		}
		var saved *image.RGBA
		if dispose == 2 {
			saved = snapshot(canvas)
		}
		op := draw.Over
		if f.blend == 0 {
			op = draw.Src
		}
		draw.Draw(canvas, r, img, img.Bounds().Min, op)
		a.Frames = append(a.Frames, Frame{Image: snapshot(canvas), Delay: f.delay})
		switch dispose {
		case 1:
			clearRect(canvas, r)
		case 2:
			copy(canvas.Pix, saved.Pix)
		}
	}
	return a, nil
}

const (
	webpAnimation = 0x02
	webpAlpha     = 0x10
)

func isWebP(data []byte) bool {
	return len(data) >= 12 && string(data[:4]) == "RIFF" && string(data[8:12]) == "WEBP"
}

// riffChunks walks the chunks of a RIFF body, each padded to an even size.
func riffChunks(body []byte) []pngChunk {
	var out []pngChunk
	for p := 0; p+8 <= len(body); {
		n := int(binary.LittleEndian.Uint32(body[p+4:]))
		if n < 0 || p+8+n > len(body) {
			break
		}
		out = append(out, pngChunk{typ: string(body[p : p+4]), data: body[p+8 : p+8+n]})
		p += 8 + n + n&1
	}
	return out
}

func webpFlags(data []byte) (byte, bool) {
	c := riffChunks(data[12:])
	if len(c) == 0 || c[0].typ != "VP8X" || len(c[0].data) < 10 {
		return 0, false
	}
	return c[0].data[0], true
}

func u24(b []byte) int { return int(b[0]) | int(b[1])<<8 | int(b[2])<<16 }

func putU24(b []byte, v int) { b[0], b[1], b[2] = byte(v), byte(v>>8), byte(v>>16) }

func appendRIFFChunk(b []byte, typ string, data []byte) []byte {
	b = append(b, typ...)
	b = binary.LittleEndian.AppendUint32(b, uint32(len(data)))
	b = append(b, data...)
	if len(data)&1 == 1 {
		b = append(b, 0)
	}
	return b
}

// webpStill wraps one ANMF frame's chunks (optional ALPH, then VP8 or
// VP8L) as a standalone WebP that x/image/webp decodes.
func webpStill(w, h int, frame []pngChunk) []byte {
	var body []byte
	hasAlpha := false
	for _, c := range frame {
		hasAlpha = hasAlpha || c.typ == "ALPH"
	}
	if hasAlpha {
		x := make([]byte, 10)
		x[0] = webpAlpha
		putU24(x[4:], w-1)
		putU24(x[7:], h-1)
		body = appendRIFFChunk(body, "VP8X", x)
	}
	for _, c := range frame {
		if c.typ == "ALPH" || c.typ == "VP8 " || c.typ == "VP8L" {
			body = appendRIFFChunk(body, c.typ, c.data)
		}
	}
	out := []byte("RIFF")
	out = binary.LittleEndian.AppendUint32(out, uint32(4+len(body)))
	out = append(out, "WEBP"...)
	return append(out, body...)
}

func decodeWebP(data []byte, maxBytes int) (*Animation, error) {
	chunks := riffChunks(data[12:])
	if len(chunks) == 0 || chunks[0].typ != "VP8X" || len(chunks[0].data) < 10 || chunks[0].data[0]&webpAnimation == 0 {
		return nil, ErrNotAnimated
	}
	w, h := u24(chunks[0].data[4:])+1, u24(chunks[0].data[7:])+1
	a := &Animation{Width: w, Height: h}
	var anmf [][]byte
	for _, c := range chunks[1:] {
		switch c.typ {
		case "ANIM":
			if len(c.data) >= 6 {
				a.Loops = int(binary.LittleEndian.Uint16(c.data[4:]))
			}
		case "ANMF":
			if len(c.data) >= 16 {
				anmf = append(anmf, c.data)
			}
		}
	}
	if err := checkBudget(w, h, len(anmf), maxBytes); err != nil {
		return nil, err
	}
	canvas := image.NewRGBA(image.Rect(0, 0, w, h))
	for _, f := range anmf {
		x, y := u24(f)*2, u24(f[3:])*2
		fw, fh := u24(f[6:])+1, u24(f[9:])+1
		d := time.Duration(u24(f[12:])) * time.Millisecond
		flags := f[15]
		r := image.Rect(x, y, x+fw, y+fh)
		if !r.In(canvas.Rect) {
			return nil, errors.New("anim: damaged WebP frame")
		}
		img, err := webp.Decode(bytes.NewReader(webpStill(fw, fh, riffChunks(f[16:]))))
		if err != nil {
			return nil, err
		}
		op := draw.Over
		if flags&0x02 != 0 { // do not blend
			op = draw.Src
		}
		draw.Draw(canvas, r, img, img.Bounds().Min, op)
		a.Frames = append(a.Frames, Frame{Image: snapshot(canvas), Delay: delay(d)})
		if flags&0x01 != 0 { // dispose to background, which browsers make transparent
			clearRect(canvas, r)
		}
	}
	return a, nil
}
