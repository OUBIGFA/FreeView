//go:build windows

package picture

// Windows Imaging Component (WIC) decodes the formats Windows has codecs
// for: ICO always, and HEIF, AVIF, JPEG XL and camera RAW once their
// extensions from the Microsoft Store are installed. FreeView ships no
// decoder of its own for them, so its size stays the same; the price is
// that a format whose extension is missing cannot be shown, which
// MissingCodecError says.
//
// The interfaces are called through their vtables, with the slot numbers
// of wincodec.h and objidl.h noted beside each call.

import (
	"errors"
	"fmt"
	"image"
	"runtime"
	"syscall"
	"unsafe"
)

var (
	ole32            = syscall.NewLazyDLL("ole32.dll")
	coInitializeEx   = ole32.NewProc("CoInitializeEx")
	coUninitialize   = ole32.NewProc("CoUninitialize")
	coCreateInstance = ole32.NewProc("CoCreateInstance")
	propVariantClear = ole32.NewProc("PropVariantClear")
)

type guid struct {
	a    uint32
	b, c uint16
	d    [8]byte
}

var (
	clsidWICImagingFactory = guid{0xcacaf262, 0x9370, 0x4615, [8]byte{0xa1, 0x3b, 0x9f, 0x55, 0x39, 0xda, 0x4c, 0x0a}}
	iidWICImagingFactory   = guid{0xec5ec8a9, 0xc395, 0x4314, [8]byte{0x9c, 0x77, 0x54, 0xd7, 0xa9, 0x35, 0xff, 0x70}}
	// GUID_WICPixelFormat32bppPRGBA: premultiplied RGBA, as image.RGBA is.
	pixelFormatPRGBA = guid{0x3cc4a650, 0xa527, 0x4d37, [8]byte{0xa9, 0x16, 0x31, 0x42, 0xc7, 0xeb, 0xed, 0xba}}
	// GUID_ContainerFormatRaw, the Raw Image Extension's container. Most
	// RAW files are TIFF inside, so left to choose, WIC may pick its TIFF
	// decoder and show only the small embedded preview.
	containerFormatRaw = guid{0xfe99ce60, 0xf19c, 0x433c, [8]byte{0xa3, 0xae, 0x00, 0xac, 0xef, 0xa9, 0xca, 0x21}}
	// GUID_ContainerFormatHeif and GUID_ContainerFormatJpegXL.
	containerFormatHeif = guid{0xe1e62521, 0x6787, 0x405b, [8]byte{0xa3, 0x39, 0x50, 0x07, 0x15, 0xb5, 0x76, 0x3f}}
	containerFormatJXL  = guid{0xfec14e3f, 0x427a, 0x4736, [8]byte{0xaa, 0xe6, 0x27, 0xed, 0x84, 0xf6, 0x93, 0x22}}
)

// containers are the decoders asked for by name for the formats whose
// codec is an extension: asked so, WIC reports a codec it lacks as
// missing, apart from a file no decoder recognizes.
var containers = map[Codec]*guid{CodecHEIF: &containerFormatHeif, CodecAV1: &containerFormatHeif,
	CodecJXL: &containerFormatJXL, CodecRaw: &containerFormatRaw}

const (
	coinitMultithreaded      = 0x0
	clsctxInprocServer       = 0x1
	rpcEChangedMode          = 0x80010106
	wincodecComponentMissing = 0x88982f50 // WINCODEC_ERR_COMPONENTNOTFOUND
	// WINCODEC_ERR_COMPONENTINITIALIZEFAILURE: Windows 11 registers its JPEG
	// XL decoder before the Store extension that implements it is installed.
	wincodecComponentInitFailure = 0x88982f8b
	vtUI2                        = 18
	// maxPixels refuses images whose pixels would take over 1 GiB.
	maxPixels = 1 << 28
)

// hresult is a failed COM call.
type hresult uint32

func (h hresult) Error() string { return fmt.Sprintf("WIC error 0x%08X", uint32(h)) }

// com is a COM interface pointer.
type com unsafe.Pointer

// call calls method slot of o with args after o itself.
func call(o com, slot int, args ...uintptr) error {
	vtbl := *(*unsafe.Pointer)(o)
	fn := *(*uintptr)(unsafe.Add(vtbl, slot*int(unsafe.Sizeof(uintptr(0)))))
	hr, _, _ := syscall.SyscallN(fn, append([]uintptr{uintptr(o)}, args...)...)
	if int32(hr) < 0 {
		return hresult(hr)
	}
	return nil
}

func release(o com) {
	if o != nil {
		call(o, 2) // IUnknown::Release
	}
}

func ptr[T any](p *T) uintptr { return uintptr(unsafe.Pointer(p)) }

// decodeWIC decodes the first frame of data with WIC into pixels, with
// the orientation its metadata gives (1 without one). codec, when set,
// names the decoder to use rather than the one WIC picks.
func decodeWIC(data []byte, codec Codec) (img *image.RGBA, orient int, err error) {
	container, raw := containers[codec], codec == CodecRaw
	if len(data) == 0 {
		return nil, 0, errors.New("empty file")
	}
	// COM is set up per thread: keep this goroutine on one.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	switch hr, _, _ := coInitializeEx.Call(0, coinitMultithreaded); {
	case hr == 0 || hr == 1: // S_OK, or S_FALSE when the thread had it
		defer coUninitialize.Call()
	case uint32(hr) != rpcEChangedMode: // a single-threaded thread still works
		return nil, 0, hresult(hr)
	}
	var factory, stream, decoder, frame, preview, converter com
	defer func() {
		for _, o := range []com{converter, preview, frame, decoder, stream, factory} {
			release(o)
		}
	}()
	if hr, _, _ := coCreateInstance.Call(ptr(&clsidWICImagingFactory), 0, clsctxInprocServer,
		ptr(&iidWICImagingFactory), ptr(&factory)); int32(hr) < 0 {
		return nil, 0, hresult(hr)
	}
	// The stream reads data in place, so it must stay where it is.
	var pin runtime.Pinner
	pin.Pin(&data[0])
	defer pin.Unpin()
	if err := call(factory, 14, ptr(&stream)); err != nil { // IWICImagingFactory::CreateStream
		return nil, 0, err
	}
	if err := call(stream, 16, uintptr(unsafe.Pointer(&data[0])), uintptr(len(data))); err != nil { // IWICStream::InitializeFromMemory
		return nil, 0, err
	}
	if container != nil {
		if err := call(factory, 7, ptr(container), 0, ptr(&decoder)); err != nil { // CreateDecoder
			return nil, 0, err
		}
		if err := call(decoder, 4, uintptr(stream), 0); err != nil { // IWICBitmapDecoder::Initialize, on demand
			return nil, 0, err
		}
	} else if err := call(factory, 4, uintptr(stream), 0, 0, ptr(&decoder)); err != nil { // CreateDecoderFromStream
		return nil, 0, err
	}
	if err := call(decoder, 13, 0, ptr(&frame)); err != nil { // IWICBitmapDecoder::GetFrame
		return nil, 0, err
	}
	orient = orientationOf(frame)
	source := frame
	if raw && fullPreview(decoder, frame, &preview) {
		source = preview
	}
	if err := call(factory, 10, ptr(&converter)); err != nil { // CreateFormatConverter
		return nil, 0, err
	}
	// IWICFormatConverter::Initialize: no dither, no palette, alpha threshold 0.
	if err := call(converter, 8, uintptr(source), ptr(&pixelFormatPRGBA), 0, 0, 0, 0); err != nil {
		return nil, 0, err
	}
	var w, h uint32
	if err := call(converter, 3, ptr(&w), ptr(&h)); err != nil { // GetSize
		return nil, 0, err
	}
	if w == 0 || h == 0 || uint64(w)*uint64(h) > maxPixels {
		return nil, 0, fmt.Errorf("unsupported size %d × %d", w, h)
	}
	img = image.NewRGBA(image.Rect(0, 0, int(w), int(h)))
	if err := call(converter, 7, 0, uintptr(img.Stride), uintptr(len(img.Pix)), ptr(&img.Pix[0])); err != nil { // CopyPixels
		return nil, 0, err
	}
	return img, orient, nil
}

// fullPreview sets preview to a RAW file's embedded preview when it is
// as large as the image, as most cameras write it. The camera rendered it
// already, so it shows in a tenth of the time developing the sensor data
// takes (1.1 to 1.9 s for 24 to 30 MP samples), as Honeyview shows RAW.
func fullPreview(decoder, frame com, preview *com) bool {
	if call(decoder, 9, ptr(preview)) != nil { // IWICBitmapDecoder::GetPreview
		return false
	}
	var fw, fh, pw, ph uint32
	if call(frame, 3, ptr(&fw), ptr(&fh)) != nil || call(*preview, 3, ptr(&pw), ptr(&ph)) != nil ||
		max(pw, ph) < max(fw, fh)*9/10 {
		release(*preview)
		*preview = nil
		return false
	}
	return true
}

// orientationOf reads System.Photo.Orientation from frame's metadata, 1
// when it has none.
func orientationOf(frame com) int {
	var reader com
	if call(frame, 8, ptr(&reader)) != nil { // IWICBitmapFrameDecode::GetMetadataQueryReader
		return 1
	}
	defer release(reader)
	name, _ := syscall.UTF16PtrFromString("System.Photo.Orientation")
	var pv struct {
		vt       uint16
		reserved [3]uint16
		val      [2]uint64
	}
	if call(reader, 5, ptr(name), ptr(&pv)) != nil { // IWICMetadataQueryReader::GetMetadataByName
		return 1
	}
	defer propVariantClear.Call(ptr(&pv))
	if v := int(uint16(pv.val[0])); pv.vt == vtUI2 && v >= 1 && v <= 8 {
		return v
	}
	return 1
}

// missingCodec reports whether err says no installed codec reads the file.
func missingCodec(err error) bool {
	var h hresult
	return errors.As(err, &h) && (uint32(h) == wincodecComponentMissing || uint32(h) == wincodecComponentInitFailure)
}
