// Package livephoto finds the motion clip stored with a still photo, so
// FreeView, which plays no video, can show the still and offer to export
// the clip unchanged: the clip appended to an Android motion photo (Google
// MotionPhoto / MicroVideo, Samsung) in a JPEG or HEIC, and the clip beside
// the still in an iOS .livp, a ZIP of a HEIC or JPEG and a .mov.
//
// Nothing is decoded or re-encoded: a clip is the exact bytes of its MP4 or
// QuickTime container, found by walking its boxes. Every box size is checked
// against the data, as the files come from many vendors and may be
// truncated or corrupt.
package livephoto

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// Video is a motion clip found with a still photo.
type Video struct {
	Data []byte // the clip's container bytes, a subslice of the input where possible
	Ext  string // ".mp4" or ".mov"
}

const (
	// searchWindow is how far from the start the XMP gives the clip's
	// ftyp box is looked for: vendors miscount padding and headers.
	searchWindow = 64 << 10
	// maxTrailer is the most non-box data allowed after a clip found
	// without XMP, for vendor trailers such as Samsung's SEFH/SEFT.
	maxTrailer = 1 << 20
	// xmpScanLimit bounds the search for the XMP packet of a non-JPEG
	// file, which HEIF keeps near the start, in its meta item data.
	xmpScanLimit = 1 << 20
	// maxEntry bounds a .livp entry, against ZIP bombs.
	maxEntry = 1 << 30
)

// heifBrands are ftyp major brands of HEIF and AVIF images: a box chain
// starting with one is the photo itself, never its clip.
var heifBrands = map[string]bool{"heic": true, "heix": true, "heim": true, "heis": true, "hevc": true,
	"hevx": true, "hevm": true, "hevs": true, "mif1": true, "mif2": true, "msf1": true, "avif": true, "avis": true}

var ftypTag = []byte("ftyp")

// mp4Boxes are the top-level boxes of an MP4 or QuickTime file.
var mp4Boxes = map[string]bool{"ftyp": true, "moov": true, "mdat": true, "free": true, "skip": true, "wide": true,
	"uuid": true, "meta": true, "moof": true, "mfra": true, "udta": true, "pdin": true, "sidx": true, "styp": true}

// FindVideo looks for the clip of an Android motion photo (Google
// MotionPhoto / MicroVideo, Samsung) in a JPEG or HEIC/HEIF file's bytes.
// ok is false for an ordinary photo.
//
// The XMP's offset or length is tried first, corrected to the nearest
// valid MP4 nearby; without it, a file that is marked as a motion photo is
// searched for an MP4 that ends the file.
func FindVideo(data []byte) (Video, bool) {
	xmp := readXMP(data)
	for _, est := range xmpStarts(xmp, len(data)) {
		if v, ok := clipNear(data, est); ok {
			return v, true
		}
	}
	if !hasMotionMarker(data, xmp) {
		return Video{}, false
	}
	return clipInTail(data)
}

// hasMotionMarker reports whether the file says it is a motion photo,
// even without a usable offset: in its XMP, or by the name of the data
// block Samsung writes before the clip.
func hasMotionMarker(data, xmp []byte) bool {
	return bytes.Contains(xmp, []byte("MotionPhoto")) || bytes.Contains(xmp, []byte("MicroVideo")) ||
		bytes.Contains(data, []byte("MotionPhoto_Data"))
}

// clipNear returns the valid clip whose ftyp box starts closest to est,
// within searchWindow of it.
func clipNear(data []byte, est int) (Video, bool) {
	lo := max(est-searchWindow, 1) // offset 0 is the photo itself
	hi := min(est+searchWindow, len(data)-8)
	var best Video
	bestDist := -1
	for s := lo; s <= hi; s++ {
		k := bytes.Index(data[s+4:hi+8], ftypTag)
		if k < 0 {
			break
		}
		s += k
		if v, ok := clipAt(data, s); ok {
			if d := abs(s - est); bestDist < 0 || d < bestDist {
				best, bestDist = v, d
			}
		}
	}
	return best, bestDist >= 0
}

// clipInTail returns the first valid clip that ends the file, allowing
// a trailer of at most maxTrailer bytes. The first is taken, not the last,
// so an MP4 nested in the clip's own media data is not mistaken for it.
func clipInTail(data []byte) (Video, bool) {
	for s := 1; s+8 <= len(data); s++ {
		k := bytes.Index(data[s+4:], ftypTag)
		if k < 0 {
			break
		}
		s += k
		if v, ok := clipAt(data, s); ok && len(data)-(s+len(v.Data)) <= maxTrailer {
			return v, true
		}
	}
	return Video{}, false
}

// clipAt returns the MP4 or QuickTime clip starting at start: an ftyp box
// with a video brand, then a chain of boxes that includes moov. The chain
// ends where the data no longer looks like a box; a box that does but
// overruns the data means the clip is truncated or corrupt.
func clipAt(data []byte, start int) (Video, bool) {
	ftyp, err := boxAt(data, start)
	if err != nil || ftyp.typ != "ftyp" || ftyp.size < ftyp.hdr+8 {
		return Video{}, false
	}
	brand := data[start+ftyp.hdr : start+ftyp.hdr+4]
	if !validType(brand) || heifBrands[string(brand)] {
		return Video{}, false
	}
	end, moov := start+ftyp.size, false
	for end < len(data) {
		b, err := boxAt(data, end)
		if errors.Is(err, errNotBox) {
			break
		}
		if err != nil {
			// An MP4 box that overruns the file is a truncated clip; any
			// other header is a vendor trailer that happens to look like
			// one, and ends the clip.
			if mp4Boxes[string(data[end+4:end+8])] {
				return Video{}, false
			}
			break
		}
		moov = moov || b.typ == "moov"
		end += b.size
	}
	if !moov {
		return Video{}, false
	}
	ext := ".mp4"
	if string(brand) == "qt  " {
		ext = ".mov"
	}
	return Video{Data: data[start:end:end], Ext: ext}, true
}

// box is an ISO BMFF box header: its type, header length and total size.
type box struct {
	typ       string
	hdr, size int
}

var (
	errNotBox = errors.New("not a box")
	errBadBox = errors.New("box size out of range")
)

// boxAt reads the box header at off. It returns errNotBox when the bytes
// cannot be a box header, and errBadBox when they can but the size is
// impossible: smaller than the header or past the end of data. A size of
// 1 means a 64-bit size follows; 0 means the box runs to the end.
func boxAt(data []byte, off int) (box, error) {
	if off < 0 || len(data)-off < 8 || !validType(data[off+4:off+8]) {
		return box{}, errNotBox
	}
	rest := uint64(len(data) - off)
	size, hdr := uint64(binary.BigEndian.Uint32(data[off:])), uint64(8)
	switch size {
	case 0:
		size = rest
	case 1:
		if rest < 16 {
			return box{}, errBadBox
		}
		size, hdr = binary.BigEndian.Uint64(data[off+8:]), 16
	}
	if size < hdr || size > rest {
		return box{}, errBadBox
	}
	return box{typ: string(data[off+4 : off+8]), hdr: int(hdr), size: int(size)}, nil
}

// validType reports whether t can be a box type or brand: four printable
// ASCII characters, or © as QuickTime uses.
func validType(t []byte) bool {
	for _, c := range t {
		if (c < 0x20 || c > 0x7e) && c != 0xa9 {
			return false
		}
	}
	return len(t) == 4
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

var xmpNamespace = []byte("http://ns.adobe.com/xap/1.0/\x00")

// readXMP returns the XMP packet: a JPEG's XMP APP1 segment, or for other
// files (HEIF) the x:xmpmeta element found in the first xmpScanLimit bytes.
// It returns nil when there is none.
func readXMP(data []byte) []byte {
	if len(data) >= 2 && data[0] == 0xff && data[1] == 0xd8 {
		return jpegXMP(data)
	}
	head := data[:min(len(data), xmpScanLimit)]
	i := bytes.Index(head, []byte("<x:xmpmeta"))
	if i < 0 {
		return nil
	}
	packet := data[i:min(len(data), i+xmpScanLimit)]
	if j := bytes.Index(packet, []byte("</x:xmpmeta>")); j >= 0 {
		return packet[:j+len("</x:xmpmeta>")]
	}
	return packet
}

// jpegXMP walks a JPEG's marker segments up to the image data for the
// XMP APP1 segment.
func jpegXMP(data []byte) []byte {
	for off := 2; off+4 <= len(data); {
		if data[off] != 0xff {
			return nil
		}
		marker := data[off+1]
		switch {
		case marker == 0xff: // fill byte
			off++
			continue
		case marker == 0x01 || marker == 0xd8 || marker >= 0xd0 && marker <= 0xd7: // no length
			off += 2
			continue
		case marker == 0xda || marker == 0xd9: // image data or end: no more metadata
			return nil
		}
		n := int(binary.BigEndian.Uint16(data[off+2:]))
		if n < 2 || off+2+n > len(data) {
			return nil
		}
		seg := data[off+4 : off+2+n]
		if marker == 0xe1 && bytes.HasPrefix(seg, xmpNamespace) {
			return seg[len(xmpNamespace):]
		}
		off += 2 + n
	}
	return nil
}

// fieldRe matches an XMP property by local name, whatever its namespace
// prefix, as an attribute (name="v") or an element (<p:name>v<).
func fieldRe(name string) *regexp.Regexp {
	return regexp.MustCompile(`(?:^|[\s<])(?:[\w-]+:)?` + name + `\s*(?:=\s*"([^"]*)"|=\s*'([^']*)'|>([^<]*)<)`)
}

var (
	microVideoOffsetRe = fieldRe("MicroVideoOffset")
	semanticRe         = fieldRe("Semantic")
	lengthRe           = fieldRe("Length")
	paddingRe          = fieldRe("Padding")
	// itemRe matches the start of a Container:Item, but not of an
	// Item:Semantic-style property element.
	itemRe = regexp.MustCompile(`<[\w-]+:Item[\s/>]`)
)

// xmpStarts returns where the XMP says the clip starts, from the
// MicroVideoOffset and from the Container:Directory, in that order.
func xmpStarts(xmp []byte, size int) []int {
	var starts []int
	if n, ok := xmpNumber(microVideoOffsetRe, xmp); ok && n > 0 && n < int64(size) {
		starts = append(starts, size-int(n))
	}
	if s, ok := directoryStart(xmp, size); ok {
		starts = append(starts, s)
	}
	return starts
}

// directoryStart reads the Container:Directory: its items lie in order
// after the primary image, each followed by its Item:Padding, so the
// MotionPhoto item starts where the lengths and paddings from the end of
// the file put it.
func directoryStart(xmp []byte, size int) (int, bool) {
	type item struct {
		semantic        string
		length, padding int64
	}
	locs := itemRe.FindAllIndex(xmp, -1)
	items := make([]item, len(locs))
	motion := -1
	for i, loc := range locs {
		end := len(xmp)
		if i+1 < len(locs) {
			end = locs[i+1][0]
		}
		seg := xmp[loc[0]:end]
		it := &items[i]
		it.semantic, _ = xmpValue(semanticRe, seg)
		it.length, _ = xmpNumber(lengthRe, seg)
		it.padding, _ = xmpNumber(paddingRe, seg)
		if motion < 0 && strings.EqualFold(it.semantic, "MotionPhoto") {
			motion = i
		}
	}
	if motion < 0 || items[motion].length <= 0 {
		return 0, false
	}
	end := int64(size)
	for _, it := range items[motion+1:] {
		end -= it.length + it.padding
	}
	start := end - items[motion].padding - items[motion].length
	if start <= 0 || start >= int64(size) {
		return 0, false
	}
	return int(start), true
}

// xmpValue returns the value of the first match of the property re.
func xmpValue(re *regexp.Regexp, xmp []byte) (string, bool) {
	m := re.FindSubmatch(xmp)
	if m == nil {
		return "", false
	}
	for _, g := range m[1:] {
		if g != nil {
			return strings.TrimSpace(string(g)), true
		}
	}
	return "", false
}

// xmpNumber returns the property re as a byte count. Counts outside
// 0 to 1 TiB are refused, so sums of them cannot overflow.
func xmpNumber(re *regexp.Regexp, xmp []byte) (int64, bool) {
	v, ok := xmpValue(re, xmp)
	if !ok {
		return 0, false
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil || n < 0 || n > 1<<40 {
		return 0, false
	}
	return n, true
}

// IsLIVP reports whether name has the .livp extension.
func IsLIVP(name string) bool {
	return strings.EqualFold(filepath.Ext(name), ".livp")
}

// ErrNoStill is the error for a .livp without a HEIC or JPEG still.
var ErrNoStill = errors.New("livp: no still image")

var (
	stillExts = []string{".heic", ".heif", ".hif", ".jpg", ".jpeg"}
	clipExts  = []string{".mov", ".mp4"}
)

// OpenLIVP reads an iOS .livp (a ZIP holding a HEIC or JPEG still and a
// .mov): it returns the still's bytes with its file name (for the
// extension) and the clip. clip is zero when the archive holds no clip,
// as the still alone can still be shown.
func OpenLIVP(data []byte) (still []byte, stillName string, clip Video, err error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, "", Video{}, fmt.Errorf("livp: %w", err)
	}
	var stillFile, clipFile *zip.File
	for _, f := range zr.File {
		name := entryName(f)
		base := path.Base(name)
		// Skip folders and the resource forks macOS adds to archives.
		if f.FileInfo().IsDir() || strings.HasPrefix(name, "__MACOSX/") || strings.HasPrefix(base, "._") {
			continue
		}
		ext := strings.ToLower(path.Ext(base))
		switch {
		case stillFile == nil && slices.Contains(stillExts, ext):
			stillFile = f
		case clipFile == nil && slices.Contains(clipExts, ext):
			clipFile = f
		}
	}
	if stillFile == nil {
		return nil, "", Video{}, ErrNoStill
	}
	if still, err = entryBytes(data, stillFile); err != nil {
		return nil, "", Video{}, fmt.Errorf("livp: %s: %w", stillFile.Name, err)
	}
	stillName = path.Base(entryName(stillFile))
	if clipFile != nil {
		b, err := entryBytes(data, clipFile)
		if err != nil {
			return nil, "", Video{}, fmt.Errorf("livp: %s: %w", clipFile.Name, err)
		}
		clip = Video{Data: b, Ext: strings.ToLower(path.Ext(entryName(clipFile)))}
	}
	return still, stillName, clip, nil
}

// entryName is an entry's slash path; archives made on Windows may use
// backslashes.
func entryName(f *zip.File) string { return strings.ReplaceAll(f.Name, `\`, "/") }

// entryBytes returns an entry's bytes: a subslice of data when the entry
// is stored uncompressed and its checksum matches, as .livp entries
// usually are, else the decompressed bytes.
func entryBytes(data []byte, f *zip.File) ([]byte, error) {
	if f.UncompressedSize64 > maxEntry {
		return nil, errors.New("entry too large")
	}
	if f.Method == zip.Store && f.CompressedSize64 == f.UncompressedSize64 {
		if off, err := f.DataOffset(); err == nil && off >= 0 {
			if end := off + int64(f.CompressedSize64); end <= int64(len(data)) {
				if b := data[off:end:end]; crc32.ChecksumIEEE(b) == f.CRC32 {
					return b, nil
				}
			}
		}
	}
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	b, err := io.ReadAll(io.LimitReader(rc, maxEntry+1))
	if err != nil {
		return nil, err
	}
	if len(b) > maxEntry {
		return nil, errors.New("entry too large")
	}
	return b, nil
}
