package meta

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/png"
	"testing"
)

func pngWith(t *testing.T, chunks ...[]byte) []byte {
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewGray(image.Rect(0, 0, 1, 1))); err != nil {
		t.Fatal(err)
	}
	data := b.Bytes()
	// The text goes before IEND, the last 12 bytes.
	return append(append(append([]byte(nil), data[:len(data)-12]...), bytes.Join(chunks, nil)...), data[len(data)-12:]...)
}

func chunk(typ string, body []byte) []byte {
	out := binary.BigEndian.AppendUint32(nil, uint32(len(body)))
	out = append(append(out, typ...), body...)
	return binary.BigEndian.AppendUint32(out, crc32.ChecksumIEEE(append([]byte(typ), body...)))
}

func deflate(s string) []byte {
	var b bytes.Buffer
	z := zlib.NewWriter(&b)
	z.Write([]byte(s))
	z.Close()
	return b.Bytes()
}

func TestPNGPrompts(t *testing.T) {
	for name, c := range map[string]struct {
		chunks [][]byte
		want   string
	}{
		"webui tEXt":            {[][]byte{chunk("tEXt", []byte("parameters\x00a cat, caf\xe9\nSteps: 20"))}, "a cat, café\nSteps: 20"},
		"comfy zTXt":            {[][]byte{chunk("zTXt", append([]byte("prompt\x00\x00"), deflate(`{"3":{}}`)...))}, `{"3":{}}`},
		"iTXt compressed":       {[][]byte{chunk("iTXt", append([]byte("Description\x00\x01\x00zh\x00\x00"), deflate("一只猫")...))}, "一只猫"},
		"iTXt plain wins order": {[][]byte{chunk("tEXt", []byte("Comment\x00second")), chunk("iTXt", []byte("parameters\x00\x00\x00\x00\x00first"))}, "first"},
		"no prompt":             {[][]byte{chunk("tEXt", []byte("Author\x00someone"))}, ""},
	} {
		if got := Read(pngWith(t, c.chunks...)).Prompt; got != c.want {
			t.Errorf("%s: %q, want %q", name, got, c.want)
		}
	}
	// Damaged chunks are skipped without a panic.
	data := pngWith(t, chunk("zTXt", []byte("prompt\x00\x00notzlib")))
	if got := Read(data).Prompt; got != "" {
		t.Errorf("damaged zTXt read as %q", got)
	}
	for n := range len(data) {
		Read(data[:n])
	}
}

func TestUserComment(t *testing.T) {
	be := []byte("UNICODE\x00")
	for _, r := range "hi" {
		be = append(be, 0, byte(r))
	}
	le := []byte("UNICODE\x00h\x00i\x00")
	for name, c := range map[string]struct {
		v    []byte
		want string
	}{"ascii": {[]byte("ASCII\x00\x00\x00steps 20\x00"), "steps 20"}, "utf16 be": {be, "hi"}, "utf16 le": {le, "hi"},
		"jis": {[]byte("JIS\x00\x00\x00\x00\x00xx"), ""}, "short": {[]byte("AS"), ""}} {
		if got := userComment(c.v); got != c.want {
			t.Errorf("%s: %q, want %q", name, got, c.want)
		}
	}
}

// A JPEG without EXIF has no fields and no prompt.
func TestNoEXIF(t *testing.T) {
	if info := Read([]byte{0xff, 0xd8, 0xff, 0xd9}); len(info.Fields) != 0 || info.Prompt != "" {
		t.Errorf("%+v", info)
	}
}
