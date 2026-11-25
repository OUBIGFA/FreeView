// Package meta reads the details the information panel shows beyond the
// size: the camera settings in a photo's EXIF, and the prompt an AI image
// generator wrote into its PNG text chunks or EXIF user comment. It only
// reads headers and text, never pixels, so the viewer runs it when the
// panel opens rather than for every image shown.
package meta

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"io"
	"math"
	"strconv"
	"strings"
	"unicode/utf16"

	"github.com/rwcarlsen/goexif/exif"
)

// Field is one line of details: a stable key the UI names in its own
// language, and the value as the file has it.
type Field struct {
	Key, Value string
}

// The keys of Fields.
const (
	Camera   = "camera"
	Lens     = "lens"
	Exposure = "exposure" // aperture, shutter, ISO and focal length on one line
	Taken    = "taken"
	GPS      = "gps"
	Software = "software"
	Comment  = "comment" // an EXIF user comment that is not a generator's
)

// Info is what Read found.
type Info struct {
	Fields []Field
	// Prompt is a generator's text: Stable Diffusion WebUI's
	// "parameters", ComfyUI's "prompt" JSON, NovelAI's "Description" and
	// "Comment", or WebUI's parameters in an EXIF user comment. "" when
	// none.
	Prompt string
}

// maxText caps one decompressed text chunk, so a hostile zTXt cannot
// exhaust memory; real prompts and workflows are well under it.
const maxText = 4 << 20

// Read collects the details of an image file's bytes.
func Read(data []byte) Info {
	var info Info
	if bytes.HasPrefix(data, []byte("\x89PNG\r\n\x1a\n")) {
		info.Prompt = pngPrompt(pngTexts(data))
	}
	x, err := exif.Decode(bytes.NewReader(data))
	if err != nil {
		return info
	}
	str := func(name exif.FieldName) string {
		t, err := x.Get(name)
		if err != nil {
			return ""
		}
		s, err := t.StringVal()
		if err != nil {
			return ""
		}
		return strings.TrimSpace(strings.TrimRight(s, "\x00"))
	}
	add := func(key, value string) {
		if value != "" {
			info.Fields = append(info.Fields, Field{key, value})
		}
	}
	// The model often repeats the make ("Canon" / "Canon EOS R5").
	camera := str(exif.Model)
	if mk := str(exif.Make); mk != "" && !strings.HasPrefix(strings.ToLower(camera), strings.ToLower(strings.Fields(mk)[0])) {
		camera = strings.TrimSpace(mk + " " + camera)
	}
	add(Camera, camera)
	add(Lens, str(exif.LensModel))
	add(Exposure, exposure(x))
	if t, err := x.DateTime(); err == nil {
		add(Taken, t.Format("2006-01-02 15:04:05"))
	}
	if lat, long, err := x.LatLong(); err == nil && !math.IsNaN(lat) && (lat != 0 || long != 0) {
		add(GPS, strconv.FormatFloat(lat, 'f', 6, 64)+", "+strconv.FormatFloat(long, 'f', 6, 64))
	}
	add(Software, str(exif.Software))
	// WebUI writes its parameters, which always list the sampling steps,
	// into the user comment of a JPEG or WebP; any other comment is a
	// plain one.
	if t, err := x.Get(exif.UserComment); err == nil {
		if c := userComment(t.Val); info.Prompt == "" && strings.Contains(c, "Steps: ") {
			info.Prompt = c
		} else {
			add(Comment, c)
		}
	}
	return info
}

// exposure writes aperture, shutter time, ISO and focal length as
// photographers read them: "f/2.8  1/250 s  ISO 400  50 mm".
func exposure(x *exif.Exif) string {
	rat := func(name exif.FieldName) (float64, bool) {
		t, err := x.Get(name)
		if err != nil {
			return 0, false
		}
		n, d, err := t.Rat2(0)
		if err != nil || d == 0 {
			return 0, false
		}
		return float64(n) / float64(d), true
	}
	num := func(f float64) string { return strconv.FormatFloat(f, 'f', -1, 64) }
	var parts []string
	if f, ok := rat(exif.FNumber); ok && f > 0 {
		parts = append(parts, "f/"+num(math.Round(f*10)/10))
	}
	if s, ok := rat(exif.ExposureTime); ok && s > 0 {
		if s < 1 {
			parts = append(parts, "1/"+num(math.Round(1/s))+" s")
		} else {
			parts = append(parts, num(math.Round(s*10)/10)+" s")
		}
	}
	if t, err := x.Get(exif.ISOSpeedRatings); err == nil {
		if iso, err := t.Int(0); err == nil && iso > 0 {
			parts = append(parts, "ISO "+strconv.Itoa(iso))
		}
	}
	if f, ok := rat(exif.FocalLength); ok && f > 0 {
		parts = append(parts, num(math.Round(f*10)/10)+" mm")
	}
	return strings.Join(parts, "  ")
}

// userComment decodes an EXIF UserComment: an 8-byte character code
// ("ASCII", "UNICODE" for UTF-16, which WebUI writes big-endian) then the
// text.
func userComment(v []byte) string {
	if len(v) < 8 {
		return ""
	}
	code, body := string(bytes.TrimRight(v[:8], "\x00 ")), v[8:]
	var s string
	switch code {
	case "UNICODE":
		order := binary.ByteOrder(binary.BigEndian)
		// A little-endian comment shows as ASCII letters at odd bytes.
		if len(body) >= 2 && body[0] != 0 && body[1] == 0 {
			order = binary.LittleEndian
		}
		u := make([]uint16, len(body)/2)
		for i := range u {
			u[i] = order.Uint16(body[i*2:])
		}
		s = string(utf16.Decode(u))
	case "ASCII", "":
		s = string(body)
	default:
		return ""
	}
	return strings.TrimSpace(strings.TrimRight(s, "\x00"))
}

// pngTexts reads a PNG's tEXt, zTXt and iTXt chunks into keyword → text,
// following the PNG specification: tEXt is Latin-1, zTXt Latin-1 behind
// zlib, iTXt UTF-8, compressed when its flag says.
func pngTexts(data []byte) map[string]string {
	texts := map[string]string{}
	for p := 8; p+12 <= len(data); {
		n := int(binary.BigEndian.Uint32(data[p:]))
		if n < 0 || p+12+n > len(data) {
			break
		}
		typ, body := string(data[p+4:p+8]), data[p+8:p+8+n]
		p += 12 + n
		if typ == "IEND" {
			break
		}
		key, rest, ok := bytes.Cut(body, []byte{0})
		if !ok {
			continue
		}
		switch typ {
		case "tEXt":
			texts[string(key)] = latin1(rest)
		case "zTXt":
			if len(rest) > 0 {
				if t, ok := inflate(rest[1:]); ok {
					texts[string(key)] = latin1(t)
				}
			}
		case "iTXt":
			if len(rest) < 2 {
				continue
			}
			compressed := rest[0] == 1
			// Language tag and translated keyword, each ended by a 0.
			_, rest, ok = bytes.Cut(rest[2:], []byte{0})
			if !ok {
				continue
			}
			_, text, ok := bytes.Cut(rest, []byte{0})
			if !ok {
				continue
			}
			if compressed {
				if text, ok = inflate(text); !ok {
					continue
				}
			}
			texts[string(key)] = string(text)
		}
	}
	return texts
}

func inflate(b []byte) ([]byte, bool) {
	z, err := zlib.NewReader(bytes.NewReader(b))
	if err != nil {
		return nil, false
	}
	out, err := io.ReadAll(io.LimitReader(z, maxText))
	return out, err == nil
}

func latin1(b []byte) string {
	r := make([]rune, len(b))
	for i, c := range b {
		r[i] = rune(c)
	}
	return string(r)
}

// pngPrompt picks the generator text from a PNG's texts, the
// generators' own keys first.
func pngPrompt(texts map[string]string) string {
	for _, key := range []string{"parameters", "prompt", "Description", "Comment", "workflow"} {
		if t := strings.TrimSpace(texts[key]); t != "" {
			return t
		}
	}
	return ""
}
