// Package colorprofile color-manages images whose embedded ICC profile is
// not sRGB. nsgo draws JPEG, PNG and WebP through Direct2D without color
// management, so a Display P3 or Adobe RGB photo would look washed out or
// oversaturated. Following the project rule to reuse system abilities, the
// conversion itself is done by Windows' own WIC color transform
// (IWICColorTransform) rather than a bundled CMM such as lcms2; this file
// holds the platform-independent parts: finding the embedded profile and
// recognizing sRGB so the common case skips conversion entirely.
package colorprofile

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"io"
	"math"
	"strings"
	"unicode/utf16"
)

// maxProfile caps a decompressed PNG profile; real profiles are a few
// KiB to a few MiB, so anything larger is treated as hostile.
const maxProfile = 64 << 20

// Embedded returns the ICC profile embedded in a JPEG (APP2 ICC_PROFILE
// segments, reassembled by sequence number), PNG (iCCP, zlib), WebP (VP8X
// ICCP chunk) or TIFF (tag 34675), or nil when there is none or it cannot
// be read. The result never aliases data.
func Embedded(data []byte) []byte {
	switch {
	case len(data) >= 4 && data[0] == 0xFF && data[1] == 0xD8:
		return jpegProfile(data)
	case bytes.HasPrefix(data, []byte("\x89PNG\r\n\x1a\n")):
		return pngProfile(data)
	case len(data) >= 12 && string(data[:4]) == "RIFF" && string(data[8:12]) == "WEBP":
		return webpProfile(data)
	case len(data) >= 8 && (bytes.HasPrefix(data, []byte("II*\x00")) || bytes.HasPrefix(data, []byte("MM\x00*"))):
		return tiffProfile(data)
	}
	return nil
}

// jpegProfile walks the marker segments up to the start of scan. A profile
// over 64 KiB is split across several APP2 segments, each tagged with its
// 1-based sequence number and the total count; writers need not emit them
// in order, so they are collected first and joined only when complete.
func jpegProfile(data []byte) []byte {
	const sig = "ICC_PROFILE\x00"
	var chunks [256][]byte
	total := 0
scan:
	for i := 2; i+4 <= len(data); {
		if data[i] != 0xFF {
			break // lost sync: give up rather than guess
		}
		m := data[i+1]
		switch {
		case m == 0xFF: // fill byte
			i++
			continue
		case m == 0x01 || m >= 0xD0 && m <= 0xD8: // markers without a length
			i += 2
			continue
		case m == 0xD9 || m == 0xDA: // EOI or SOS: no APP segments follow
			break scan
		}
		l := int(binary.BigEndian.Uint16(data[i+2:]))
		if l < 2 || i+2+l > len(data) {
			break
		}
		seg := data[i+4 : i+2+l]
		if m == 0xE2 && len(seg) >= len(sig)+2 && string(seg[:len(sig)]) == sig {
			seq, n := int(seg[len(sig)]), int(seg[len(sig)+1])
			if seq >= 1 && n >= 1 && seq <= n {
				chunks[seq] = seg[len(sig)+2:]
				total = n
			}
		}
		i += 2 + l
	}
	if total == 0 {
		return nil
	}
	var out []byte
	for s := 1; s <= total; s++ {
		if chunks[s] == nil {
			return nil // a missing piece would hand the CMM a corrupt profile
		}
		out = append(out, chunks[s]...)
	}
	return out
}

// pngProfile reads the iCCP chunk, which must precede IDAT: a profile
// name, a NUL, compression method 0 (zlib) and the compressed profile.
func pngProfile(data []byte) []byte {
	for i := 8; i+12 <= len(data); {
		n := int64(binary.BigEndian.Uint32(data[i:]))
		typ := string(data[i+4 : i+8])
		if n > int64(len(data)-i-12) || typ == "IDAT" || typ == "IEND" {
			return nil
		}
		if typ == "iCCP" {
			body := data[i+8 : i+8+int(n)]
			nul := bytes.IndexByte(body, 0)
			if nul < 1 || nul > 79 || nul+2 > len(body) || body[nul+1] != 0 {
				return nil
			}
			r, err := zlib.NewReader(bytes.NewReader(body[nul+2:]))
			if err != nil {
				return nil
			}
			p, err := io.ReadAll(io.LimitReader(r, maxProfile+1))
			if err != nil || len(p) > maxProfile || len(p) == 0 {
				return nil
			}
			return p
		}
		i += 12 + int(n)
	}
	return nil
}

// webpProfile finds the ICCP chunk of an extended (VP8X) WebP file.
func webpProfile(data []byte) []byte {
	for i := 12; i+8 <= len(data); {
		n := int64(binary.LittleEndian.Uint32(data[i+4:]))
		if n > int64(len(data)-i-8) {
			return nil
		}
		if string(data[i:i+4]) == "ICCP" {
			return bytes.Clone(data[i+8 : i+8+int(n)])
		}
		i += 8 + int(n) + int(n&1) // chunks are padded to even sizes
	}
	return nil
}

// tiffProfile reads tag 34675 (InterColorProfile) from the first IFD.
func tiffProfile(data []byte) []byte {
	var bo binary.ByteOrder = binary.LittleEndian
	if data[0] == 'M' {
		bo = binary.BigEndian
	}
	size := int64(len(data))
	ifd := int64(bo.Uint32(data[4:]))
	if ifd+2 > size {
		return nil
	}
	count := int64(bo.Uint16(data[ifd:]))
	for e := ifd + 2; e+12 <= size && e < ifd+2+count*12; e += 12 {
		if bo.Uint16(data[e:]) != 34675 {
			continue
		}
		typ, n := bo.Uint16(data[e+2:]), int64(bo.Uint32(data[e+4:]))
		if (typ != 7 && typ != 1) || n <= 4 { // UNDEFINED or BYTE; a profile never fits inline
			return nil
		}
		off := int64(bo.Uint32(data[e+8:]))
		if off+n > size {
			return nil
		}
		return bytes.Clone(data[off : off+n])
	}
	return nil
}

// sRGB's colorants adapted to the D50 PCS, as sRGB ICC profiles record
// them (IEC 61966-2.1 primaries, Bradford-adapted).
var srgbColorants = [3][3]float64{
	{0.4361, 0.2225, 0.0139}, // rXYZ
	{0.3851, 0.7169, 0.0971}, // gXYZ
	{0.1431, 0.0606, 0.7141}, // bXYZ
}

// colorantTolerance absorbs the rounding differences between vendors'
// sRGB profiles; the nearest common wide gamut (Display P3, red X≈0.515)
// is far outside it.
const colorantTolerance = 0.01

// IsSRGB reports whether profile is (equivalent to) sRGB, so the common
// case skips any conversion. It accepts a description naming sRGB or
// IEC 61966-2.1, or rXYZ/gXYZ/bXYZ colorants matching sRGB's within a
// small tolerance. A malformed profile cannot be handed to a CMM, so it
// counts as sRGB too: true means "leave the pixels alone". A well-formed
// profile that is not RGB (gray, CMYK) or has no matrix colorants (LUT
// based) reports false, since WIC can still convert it.
func IsSRGB(profile []byte) bool {
	p, ok := parse(profile)
	if !ok {
		return true
	}
	if d, ok := p.tag("desc"); ok {
		text, ok := descText(d)
		if !ok {
			return true
		}
		if strings.Contains(text, "sRGB") || strings.Contains(strings.ReplaceAll(text, " ", ""), "IEC61966-2.1") {
			return true
		}
	} else if p.bad {
		return true
	}
	if string(p.data[16:20]) != "RGB " {
		return false
	}
	for i, sig := range []string{"rXYZ", "gXYZ", "bXYZ"} {
		d, ok := p.tag(sig)
		if !ok {
			return p.bad // malformed: leave alone; merely absent: LUT profile, convert
		}
		xyz, ok := readXYZ(d)
		if !ok {
			return true
		}
		for c := range 3 {
			if math.Abs(xyz[c]-srgbColorants[i][c]) > colorantTolerance {
				return false
			}
		}
	}
	return true
}

// icc is a profile whose header and tag table have been bounds-checked.
type icc struct {
	data []byte
	bad  bool // set when a tag entry points outside the profile
}

func parse(b []byte) (icc, bool) {
	if len(b) < 132 {
		return icc{}, false
	}
	size := int64(binary.BigEndian.Uint32(b))
	if size < 132 || size > int64(len(b)) || string(b[36:40]) != "acsp" {
		return icc{}, false
	}
	b = b[:size]
	n := int64(binary.BigEndian.Uint32(b[128:]))
	if 132+n*12 > size {
		return icc{}, false
	}
	return icc{data: b}, true
}

// tag returns the data of the tag with signature sig. An entry pointing
// outside the profile marks it bad and is not returned.
func (p *icc) tag(sig string) ([]byte, bool) {
	n := int(binary.BigEndian.Uint32(p.data[128:]))
	for i := range n {
		e := p.data[132+i*12:]
		if string(e[:4]) != sig {
			continue
		}
		off, size := int64(binary.BigEndian.Uint32(e[4:])), int64(binary.BigEndian.Uint32(e[8:]))
		if off+size > int64(len(p.data)) || size < 8 {
			p.bad = true
			return nil, false
		}
		return p.data[off : off+size], true
	}
	return nil, false
}

// descText reads a v2 textDescriptionType ('desc', its ASCII part) or a v4
// multiLocalizedUnicodeType ('mluc', every record joined), the two types
// the description tag uses. Another type has no text to match but is not
// malformed.
func descText(d []byte) (string, bool) {
	switch string(d[:4]) {
	case "desc":
		if len(d) < 12 {
			return "", false
		}
		n := int64(binary.BigEndian.Uint32(d[8:]))
		if 12+n > int64(len(d)) {
			return "", false
		}
		return strings.TrimRight(string(d[12:12+n]), "\x00"), true
	case "mluc":
		if len(d) < 16 {
			return "", false
		}
		n, recSize := int64(binary.BigEndian.Uint32(d[8:])), int64(binary.BigEndian.Uint32(d[12:]))
		if recSize < 12 || 16+n*recSize > int64(len(d)) {
			return "", false
		}
		var sb strings.Builder
		for i := range n {
			r := d[16+i*recSize:]
			l, off := int64(binary.BigEndian.Uint32(r[4:])), int64(binary.BigEndian.Uint32(r[8:]))
			if off+l > int64(len(d)) || l%2 != 0 {
				return "", false
			}
			u := make([]uint16, l/2)
			for j := range u {
				u[j] = binary.BigEndian.Uint16(d[off+int64(j)*2:])
			}
			sb.WriteString(string(utf16.Decode(u)))
			sb.WriteByte('\n')
		}
		return sb.String(), true
	}
	return "", true
}

// readXYZ reads the first value of an XYZType tag (three s15Fixed16).
func readXYZ(d []byte) ([3]float64, bool) {
	var v [3]float64
	if len(d) < 20 || string(d[:4]) != "XYZ " {
		return v, false
	}
	for i := range v {
		v[i] = float64(int32(binary.BigEndian.Uint32(d[8+i*4:]))) / 65536
	}
	return v, true
}
