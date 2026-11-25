package picture

// Decoders for the formats Windows has no codec for and Go's standard
// library does not read: Photoshop (PSD/PSB), QOI, TGA, Radiance HDR and
// OpenEXR. Each is a small pure-Go module, so FreeView needs no DLL. QOI,
// PSD and HDR register with package image and are found by content; TGA
// has no magic number, so only its extension routes a file to it, and
// EXR is found by its extension or its magic number.

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"math"

	"github.com/blezek/tga"
	"github.com/mdouchement/hdr"
	"github.com/mdouchement/hdr/codec/rgbe"
	"github.com/mrjoshuak/go-openexr/exr"
	"github.com/oov/psd"
	_ "github.com/xfmoulet/qoi" // registers "qoi" with package image
)

// maxPixels refuses images whose 8-bit RGBA pixels would take over 1 GiB.
const maxPixels = 1 << 28

// checkSize refuses an empty or oversized image before its pixels are
// allocated, so a damaged or hostile header cannot exhaust memory.
func checkSize(w, h int) error {
	if w <= 0 || h <= 0 || int64(w)*int64(h) > maxPixels {
		return fmt.Errorf("unsupported size %d × %d", w, h)
	}
	return nil
}

// tgaExtensions are the names TGA files go by. TGA has no magic number,
// so its decoder is never asked to guess at other files.
var tgaExtensions = []string{".tga", ".icb", ".vda", ".vst"}

// decoderExtensions are the extensions of the formats in this file.
var decoderExtensions = append([]string{".psd", ".psb", ".qoi", ".hdr", ".exr"}, tgaExtensions...)

// exrMagic opens every OpenEXR file.
var exrMagic = []byte{0x76, 0x2f, 0x31, 0x01}

// contentDecoders take over formats package image recognizes by content
// whose decoding needs more than image.Decode: PSD checks that the file
// holds a real composite, and HDR is tone-mapped.
var contentDecoders = map[string]func([]byte) (Picture, error){"psd": loadPSD, "psb": loadPSD, "hdr": loadHDR, "qoi": loadQOI}

// qoiEnd closes every complete QOI file.
var qoiEnd = []byte{0, 0, 0, 0, 0, 0, 0, 1}

// loadQOI decodes a QOI file. The decoder stops quietly where the data
// runs out, leaving the rest transparent, so a cut-off file is told by
// its missing end marker instead.
func loadQOI(data []byte) (Picture, error) {
	if !bytes.HasSuffix(data, qoiEnd) {
		return Picture{}, errors.New("qoi: the file is cut off")
	}
	return decodeGeneric(data, "qoi")
}

// safeDecode runs decode, turning a panic in a third-party decoder on a
// damaged file into an error.
func safeDecode[T any](decode func() (T, error)) (v T, err error) {
	defer func() {
		if r := recover(); r != nil {
			var zero T
			v, err = zero, fmt.Errorf("damaged image: %v", r)
		}
	}()
	return decode()
}

// decodeGeneric decodes data with the decoder package image picks,
// checking the header's size before any pixels are allocated.
func decodeGeneric(data []byte, format string) (Picture, error) {
	cfg, err := safeDecode(func() (image.Config, error) {
		c, _, err := image.DecodeConfig(bytes.NewReader(data))
		return c, err
	})
	if err != nil {
		return Picture{}, err
	}
	if err := checkSize(cfg.Width, cfg.Height); err != nil {
		return Picture{}, err
	}
	img, err := safeDecode(func() (*image.RGBA, error) {
		m, _, err := image.Decode(bytes.NewReader(data))
		if err != nil {
			return nil, err
		}
		return rgbaOf(m), nil
	})
	if err != nil {
		return Picture{}, err
	}
	return pixelsPicture(img, format), nil
}

func pixelsPicture(img *image.RGBA, format string) Picture {
	return Picture{Pixels: img, Width: img.Rect.Dx(), Height: img.Rect.Dy(), Format: format, Orientation: 1}
}

// psdVersionInfo is image resource 1057 (0x0421), whose fifth byte says
// whether the file holds a real merged image.
const psdVersionInfo = 0x0421

// loadPSD decodes a PSD or PSB file's merged composite, skipping the
// layers' own pixels. Photoshop leaves the composite blank (white) when
// "Maximize Compatibility" is off; resource 1057 says so, and that is
// reported rather than showing a white page.
func loadPSD(data []byte) (Picture, error) {
	opts := &psd.DecodeOptions{SkipLayerImage: true, ConfigLoaded: func(cfg psd.Config) error {
		if err := checkSize(cfg.Rect.Dx(), cfg.Rect.Dy()); err != nil {
			return err
		}
		if r, ok := cfg.Res[psdVersionInfo]; ok && len(r.Data) >= 5 && r.Data[4] == 0 {
			return errors.New("the PSD file has no merged image: save it with Maximize Compatibility")
		}
		return nil
	}}
	doc, err := safeDecode(func() (*psd.PSD, error) {
		doc, _, err := psd.Decode(bytes.NewReader(data), opts)
		return doc, err
	})
	if err != nil {
		return Picture{}, err
	}
	format := "psd"
	if doc.Config.PSB() {
		format = "psb"
	}
	img, err := safeDecode(func() (*image.RGBA, error) { return rgbaOf(doc.Picker), nil })
	if err != nil {
		return Picture{}, err
	}
	return pixelsPicture(img, format), nil
}

// loadTGA decodes a TGA file, which only its extension identifies.
func loadTGA(data []byte) (Picture, error) {
	cfg, err := safeDecode(func() (image.Config, error) { return tga.DecodeConfig(bytes.NewReader(data)) })
	if err != nil {
		return Picture{}, err
	}
	if err := checkSize(cfg.Width, cfg.Height); err != nil {
		return Picture{}, err
	}
	img, err := safeDecode(func() (*image.RGBA, error) {
		m, err := tga.Decode(bytes.NewReader(data))
		if err != nil {
			return nil, err
		}
		return rgbaOf(m), nil
	})
	if err != nil {
		return Picture{}, err
	}
	return pixelsPicture(img, "tga"), nil
}

// loadHDR decodes a Radiance RGBE (.hdr) file and tone-maps it.
func loadHDR(data []byte) (Picture, error) {
	cfg, err := safeDecode(func() (image.Config, error) { return rgbe.DecodeConfig(bytes.NewReader(data)) })
	if err != nil {
		return Picture{}, err
	}
	if err := checkSize(cfg.Width, cfg.Height); err != nil {
		return Picture{}, err
	}
	img, err := safeDecode(func() (*image.RGBA, error) {
		m, err := rgbe.Decode(bytes.NewReader(data))
		if err != nil {
			return nil, err
		}
		h, ok := m.(hdr.Image)
		if !ok {
			return nil, errors.New("hdr: unexpected image type")
		}
		b := h.Bounds()
		w, ht := b.Dx(), b.Dy()
		pix := make([]float32, 0, w*ht*4)
		rgb, isRGB := h.(*hdr.RGB)
		for y := b.Min.Y; y < b.Max.Y; y++ {
			for x := b.Min.X; x < b.Max.X; x++ {
				var r, g, bl float64
				if isRGB {
					c := rgb.RGBAt(x, y)
					r, g, bl = c.R, c.G, c.B
				} else {
					r, g, bl, _ = h.HDRAt(x, y).HDRRGBA()
				}
				pix = append(pix, float32(r), float32(g), float32(bl), 1)
			}
		}
		return toneMap(pix, w, ht), nil
	})
	if err != nil {
		return Picture{}, err
	}
	return pixelsPicture(img, "hdr"), nil
}

// loadEXR decodes the first part of an OpenEXR file's R, G, B and A
// channels and tone-maps them. A file without RGB channels (luminance
// only, or deep data) decodes black or fails.
func loadEXR(data []byte) (Picture, error) {
	img, err := safeDecode(func() (*image.RGBA, error) {
		f, err := exr.OpenReader(bytes.NewReader(data), int64(len(data)))
		if err != nil {
			return nil, err
		}
		defer f.Close()
		in, err := exr.NewRGBAInputFile(f)
		if err != nil {
			return nil, err
		}
		if err := checkSize(in.Width(), in.Height()); err != nil {
			return nil, err
		}
		m, err := in.ReadRGBA()
		if err != nil {
			return nil, err
		}
		// EXR stores colour premultiplied by alpha; the tone curve is
		// applied to the straight colour and alpha put back afterwards.
		for i := 0; i+3 < len(m.Pix); i += 4 {
			if a := m.Pix[i+3]; a > 0 && a < 1 {
				m.Pix[i] /= a
				m.Pix[i+1] /= a
				m.Pix[i+2] /= a
			}
		}
		return toneMap(m.Pix, m.Rect.Dx(), m.Rect.Dy()), nil
	})
	if err != nil {
		return Picture{}, err
	}
	return pixelsPicture(img, "exr"), nil
}

// toneMap turns linear, scene-referred RGBA samples (straight alpha, w × h
// pixels, 4 floats each) into premultiplied 8-bit pixels for display. It
// uses the global operator of Reinhard et al., "Photographic Tone
// Reproduction for Digital Images" (2002): the image is scaled so that its
// log-average luminance lands on the middle-grey key 0.18, whatever the
// file's absolute exposure, then each luminance L becomes L / (1 + L),
// which compresses highlights smoothly rather than clipping them. The
// three channels are scaled alike so hues keep their ratios; a channel
// pushed past 1 by a saturated colour is clipped. A plain 2.2 gamma
// approximates the sRGB curve. NaN, negative and infinite samples, which
// renders do produce, count as black or as the brightest value.
func toneMap(pix []float32, w, h int) *image.RGBA {
	const key, delta = 0.18, 1e-6
	clean := func(v float32) float64 {
		switch {
		case v != v || v < 0:
			return 0
		case math.IsInf(float64(v), 1):
			return math.MaxFloat32
		}
		return float64(v)
	}
	lum := func(i int) float64 {
		return 0.2126*clean(pix[i]) + 0.7152*clean(pix[i+1]) + 0.0722*clean(pix[i+2])
	}
	n := w * h
	var sum float64
	for i := 0; i < n*4; i += 4 {
		sum += math.Log(delta + lum(i))
	}
	scale := key / math.Exp(sum/float64(max(n, 1)))
	// The gamma curve as a table over the mapped [0, 1] range.
	var gamma [4096]uint8
	for i := range gamma {
		gamma[i] = uint8(math.Round(255 * math.Pow(float64(i)/4095, 1/2.2)))
	}
	encode := func(v float64) uint32 { return uint32(gamma[int(min(v, 1)*4095+0.5)]) }
	out := image.NewRGBA(image.Rect(0, 0, w, h))
	for i := 0; i < n*4; i += 4 {
		a8 := uint32(math.Round(255 * min(clean(pix[i+3]), 1)))
		out.Pix[i+3] = uint8(a8)
		l := lum(i)
		if l <= 0 || a8 == 0 {
			continue
		}
		lm := scale * l
		f := lm / (1 + lm) / l
		for c := range 3 {
			out.Pix[i+c] = uint8((encode(clean(pix[i+c])*f)*a8 + 127) / 255)
		}
	}
	return out
}
