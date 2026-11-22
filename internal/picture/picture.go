// Package picture prepares an image file's bytes for display: it reads the
// size and EXIF orientation without decoding the pixels, and decodes the
// formats nsgo does not into pixels: BMP and TIFF through golang.org/x/image,
// and ICO, HEIF, AVIF, JPEG XL and camera RAW through Windows' own codecs
// (WIC), with gen2brain/heic and gen2brain/jpegxl decoding HEIF and JPEG XL
// where Windows lacks their extensions. nsgo decodes JPEG, PNG, GIF, WebP
// and SVG itself, animations included.
package picture

import (
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"image"
	"image/draw"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"maps"
	"math"
	"path/filepath"
	"slices"
	"strings"

	"github.com/gen2brain/heic"
	"github.com/gen2brain/jpegxl"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/rwcarlsen/goexif/exif"
	_ "golang.org/x/image/bmp"
	_ "golang.org/x/image/tiff"
	_ "golang.org/x/image/webp"
)

// Picture is an image ready to hand to nsgo: either its encoded Data or
// decoded Pixels, never both.
type Picture struct {
	Data   []byte
	Pixels *image.RGBA
	// Width and Height are the stored pixel size, before Orientation.
	Width, Height int
	Format        string
	// Orientation is the EXIF orientation, 1 to 8; 1 means upright.
	Orientation int
}

// nativeFormats are the formats nsgo decodes and animates itself.
var nativeFormats = map[string]bool{"jpeg": true, "png": true, "gif": true, "webp": true}

// Codec names the Windows extension, from the Microsoft Store, that a
// format needs before Windows can decode it.
type Codec string

const (
	CodecHEIF Codec = "heif" // HEIF Image Extensions, with HEVC Video Extensions
	CodecAV1  Codec = "av1"  // AV1 Video Extension
	CodecJXL  Codec = "jxl"  // JPEG XL Image Extension
	CodecRaw  Codec = "raw"  // Raw Image Extension
)

// MissingCodecError says Windows has no codec for the file: its
// extension Codec is not installed. HEIF and JPEG XL never report it, as
// their fallback decoders take over.
type MissingCodecError struct{ Codec Codec }

func (e MissingCodecError) Error() string {
	return "Windows has no codec for this format: " + string(e.Codec)
}

// RawExtensions are the camera RAW formats, which Windows' Raw Image
// Extension (LibRaw) decodes.
var RawExtensions = []string{".dng", ".cr2", ".cr3", ".crw", ".nef", ".nrw", ".arw", ".srf", ".sr2", ".orf",
	".rw2", ".raf", ".pef", ".srw", ".x3f", ".3fr", ".erf", ".kdc", ".mrw", ".mos", ".iiq", ".rwl"}

// wicExtensions are the other formats only WIC decodes, with the codec
// each needs; ICO needs none.
var wicExtensions = map[string]Codec{".ico": "", ".heic": CodecHEIF, ".heif": CodecHEIF, ".hif": CodecHEIF,
	".avif": CodecAV1, ".jxl": CodecJXL}

// Extensions are the formats Load decodes beyond the ones Go and nsgo
// recognize by content: the WIC formats and SVG.
func Extensions() []string {
	exts := slices.Sorted(maps.Keys(wicExtensions))
	return append(append(exts, ".svg", ".svgz"), RawExtensions...)
}

// Load prepares data, the bytes of the image file name; name's extension
// chooses the decoder only for formats that cannot be told by content.
func Load(data []byte, name string) (Picture, error) {
	ext := strings.ToLower(filepath.Ext(name))
	if slices.Contains(RawExtensions, ext) {
		return loadWIC(data, CodecRaw, ext[1:])
	}
	if codec, ok := wicExtensions[ext]; ok {
		return loadWIC(data, codec, ext[1:])
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		if p, svgErr := loadSVG(data); svgErr == nil {
			return p, nil
		}
		// A HEIC or ICO saved under another extension still opens.
		if p, wicErr := loadWIC(data, "", "wic"); wicErr == nil {
			return p, nil
		}
		return Picture{}, fmt.Errorf("unrecognized image: %w", err)
	}
	// The fallback decoders register their formats with package image:
	// Windows still decodes a HEIC or JPEG XL found by content first.
	if codec, ok := map[string]Codec{"heic": CodecHEIF, "jxl": CodecJXL}[format]; ok {
		return loadWIC(data, codec, format)
	}
	p := Picture{Width: cfg.Width, Height: cfg.Height, Format: format, Orientation: orientation(data)}
	if nativeFormats[format] {
		p.Data = data
		return p, nil
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return Picture{}, err
	}
	p.Pixels = rgbaOf(img)
	return p, nil
}

func rgbaOf(img image.Image) *image.RGBA {
	if rgba, ok := img.(*image.RGBA); ok {
		return rgba
	}
	rgba := image.NewRGBA(img.Bounds())
	draw.Draw(rgba, rgba.Rect, img, img.Bounds().Min, draw.Src)
	return rgba
}

// fallbacks decode the formats whose Windows extensions users most often
// lack: HEIF needs the paid HEVC Video Extensions, and JPEG XL Windows 11
// 24H2 with an extension few install. They run in WebAssembly (wazero),
// slower than Windows' codecs and with more memory (a 1280 × 854 HEIC:
// 222 ms against 39 ms), so Windows decodes first whenever it can. Both apply
// the image's own rotation. Built with the nodynamic tag (nsgo.yaml),
// they never load a libheif or libjxl DLL from the search path.
var fallbacks = map[Codec]func(io.Reader) (image.Image, error){CodecHEIF: heic.Decode, CodecJXL: jpegxl.Decode}

// loadSVG keeps an SVG's bytes for nsgo, which draws it, with its size
// in points as its pixel size.
func loadSVG(data []byte) (Picture, error) {
	doc := data
	if bytes.HasPrefix(data, []byte{0x1f, 0x8b}) { // .svgz
		zr, err := gzip.NewReader(bytes.NewReader(data))
		if err != nil {
			return Picture{}, err
		}
		if doc, err = io.ReadAll(io.LimitReader(zr, 64<<20)); err != nil {
			return Picture{}, err
		}
	}
	s, err := core.ParseSVG(doc)
	if err != nil {
		return Picture{}, err
	}
	w, h := s.Size()
	if w <= 0 || h <= 0 {
		return Picture{}, errors.New("an SVG without a size")
	}
	return Picture{Data: data, Width: int(math.Ceil(float64(w))), Height: int(math.Ceil(float64(h))), Format: "svg", Orientation: 1}, nil
}

// loadWIC decodes data with Windows' codecs. A missing codec is reported
// as the extension to install. The pixels come upright: Windows' HEIF,
// AVIF and JPEG XL decoders apply the image's own rotation, and the
// orientation WIC reports is applied for the formats that leave it, as
// RAW does.
func loadWIC(data []byte, codec Codec, format string) (Picture, error) {
	img, orient, err := decodeWIC(data, codec)
	if err != nil {
		missing := missingCodec(err) && codec != ""
		if fallback := fallbacks[codec]; fallback != nil {
			// Also when the extension is there but fails, as HEIF does
			// without the HEVC extension it decodes with.
			m, fbErr := fallback(bytes.NewReader(data))
			if fbErr == nil {
				img := rgbaOf(m)
				return Picture{Pixels: img, Width: img.Rect.Dx(), Height: img.Rect.Dy(), Format: format, Orientation: 1}, nil
			}
			if missing {
				return Picture{}, fbErr
			}
			return Picture{}, err
		}
		if missing {
			return Picture{}, MissingCodecError{codec}
		}
		return Picture{}, err
	}
	p := Picture{Pixels: img, Width: img.Rect.Dx(), Height: img.Rect.Dy(), Format: format, Orientation: 1}
	if codec == CodecRaw {
		p.Orientation = orient
	}
	return p, nil
}

// orientation reads the EXIF orientation tag, 1 when there is none.
func orientation(data []byte) int {
	x, err := exif.Decode(bytes.NewReader(data))
	if err != nil {
		return 1
	}
	tag, err := x.Get(exif.Orientation)
	if err != nil {
		return 1
	}
	v, err := tag.Int(0)
	if err != nil || v < 1 || v > 8 {
		return 1
	}
	return v
}

// OrientationTransform returns the rotation (clockwise degrees) and the
// horizontal mirror that display an image stored with EXIF orientation o
// upright, applied as mirror first, then rotation.
func OrientationTransform(o int) (rotate int, mirror bool) {
	switch o {
	case 2:
		return 0, true
	case 3:
		return 180, false
	case 4:
		return 180, true
	case 5:
		return 270, true
	case 6:
		return 90, false
	case 7:
		return 90, true
	case 8:
		return 270, false
	}
	return 0, false
}
