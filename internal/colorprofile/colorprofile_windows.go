//go:build windows

package colorprofile

// The conversion runs through Windows Imaging Component: the decoder
// reports the frame's color contexts, IWICColorTransform maps the pixels
// from that profile to the target with the system color engine, and a
// format converter hands back premultiplied RGBA as image.RGBA holds it.
// The interfaces are called through their vtables, with the slot numbers
// of wincodec.h noted beside each call, in the same style as the picture
// package; the helpers are repeated here so the two packages stay apart.

import (
	"errors"
	"fmt"
	"image"
	"os"
	"runtime"
	"syscall"
	"unsafe"
)

var (
	ole32            = syscall.NewLazyDLL("ole32.dll")
	coInitializeEx   = ole32.NewProc("CoInitializeEx")
	coUninitialize   = ole32.NewProc("CoUninitialize")
	coCreateInstance = ole32.NewProc("CoCreateInstance")

	user32            = syscall.NewLazyDLL("user32.dll")
	monitorFromWindow = user32.NewProc("MonitorFromWindow")
	getMonitorInfoW   = user32.NewProc("GetMonitorInfoW")

	gdi32          = syscall.NewLazyDLL("gdi32.dll")
	createDCW      = gdi32.NewProc("CreateDCW")
	deleteDC       = gdi32.NewProc("DeleteDC")
	getICMProfileW = gdi32.NewProc("GetICMProfileW")
)

type guid struct {
	a    uint32
	b, c uint16
	d    [8]byte
}

var (
	clsidWICImagingFactory = guid{0xcacaf262, 0x9370, 0x4615, [8]byte{0xa1, 0x3b, 0x9f, 0x55, 0x39, 0xda, 0x4c, 0x0a}}
	iidWICImagingFactory   = guid{0xec5ec8a9, 0xc395, 0x4314, [8]byte{0x9c, 0x77, 0x54, 0xd7, 0xa9, 0x35, 0xff, 0x70}}
	// GUID_WICPixelFormat32bppBGRA: straight alpha, which the color
	// transform needs; on premultiplied input it would shift the color of
	// translucent pixels.
	pixelFormatBGRA = guid{0x6fddc324, 0x4e03, 0x4bfe, [8]byte{0xb1, 0x85, 0x3d, 0x77, 0x76, 0x8d, 0xc9, 0x0f}}
	// GUID_WICPixelFormat32bppPRGBA: premultiplied RGBA, as image.RGBA is.
	pixelFormatPRGBA = guid{0x3cc4a650, 0xa527, 0x4d37, [8]byte{0xa9, 0x16, 0x31, 0x42, 0xc7, 0xeb, 0xed, 0xba}}
)

const (
	coinitMultithreaded = 0x0
	clsctxInprocServer  = 0x1
	rpcEChangedMode     = 0x80010106
	// maxPixels refuses images whose pixels would take over 1 GiB.
	maxPixels = 1 << 28
	// exifSRGB is the EXIF ColorSpace value for sRGB.
	exifSRGB = 1
	// WICColorContextProfile, as IWICColorContext::GetType reports an ICC
	// profile rather than an EXIF color space.
	colorContextProfile = 1

	// WICBitmapInterpolationModeFant averages the pixels each output
	// pixel covers, right for shrinking.
	interpolationFant = 3

	monitorDefaultToNearest = 2
	errorInsufficientBuffer = 122
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

// Display returns the ICC profile of the monitor showing the window hwnd,
// read from the file Windows has associated with that display, or nil
// when none is configured; callers then target sRGB. hwnd 0 picks the
// primary monitor.
func Display(hwnd uintptr) ([]byte, error) {
	mon, _, _ := monitorFromWindow.Call(hwnd, monitorDefaultToNearest)
	if mon == 0 {
		return nil, errors.New("no monitor for window")
	}
	// MONITORINFOEXW.
	var info struct {
		size          uint32
		monitor, work [4]int32
		flags         uint32
		device        [32]uint16
	}
	info.size = uint32(unsafe.Sizeof(info))
	if ok, _, err := getMonitorInfoW.Call(mon, ptr(&info)); ok == 0 {
		return nil, fmt.Errorf("GetMonitorInfoW: %w", err)
	}
	driver, _ := syscall.UTF16PtrFromString("DISPLAY")
	dc, _, err := createDCW.Call(ptr(driver), ptr(&info.device[0]), 0, 0)
	if dc == 0 {
		return nil, fmt.Errorf("CreateDCW: %w", err)
	}
	defer deleteDC.Call(dc)
	buf := make([]uint16, 260)
	for {
		n := uint32(len(buf))
		ok, _, err := getICMProfileW.Call(dc, ptr(&n), ptr(&buf[0]))
		if ok != 0 {
			break
		}
		if errno, _ := err.(syscall.Errno); errno == errorInsufficientBuffer && int(n) > len(buf) {
			buf = make([]uint16, n)
			continue
		}
		return nil, nil // no profile associated with this display
	}
	path := syscall.UTF16ToString(buf)
	if path == "" {
		return nil, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil // a stale association counts as none
		}
		return nil, err
	}
	return data, nil
}

// Convert decodes data's first frame with WIC and converts it from its
// embedded profile to target (an ICC profile, or nil for sRGB), returning
// premultiplied pixels with alpha kept. A frame without a profile is
// taken to be sRGB. Orientation is not applied; callers do that as they
// do for other decoded pixels.
func Convert(data []byte, target []byte) (*image.RGBA, error) { return convert(data, target, 0, 0) }

// ConvertScaled is Convert scaled to w × h pixels on the way, with WIC's
// Fant interpolation, which averages the pixels each output pixel covers.
// The scaler sits on the decoder, so a JPEG decodes at a reduced size
// through its own DCT scaling where it can, and a large photo never
// exists in memory at full size: this is the viewer's smooth view of an
// image shrunk to fit.
func ConvertScaled(data []byte, target []byte, w, h int) (*image.RGBA, error) {
	if w <= 0 || h <= 0 {
		return nil, fmt.Errorf("unsupported size %d × %d", w, h)
	}
	return convert(data, target, w, h)
}

func convert(data []byte, target []byte, outW, outH int) (*image.RGBA, error) {
	if len(data) == 0 {
		return nil, errors.New("empty file")
	}
	// COM is set up per thread: keep this goroutine on one.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	switch hr, _, _ := coInitializeEx.Call(0, coinitMultithreaded); {
	case hr == 0 || hr == 1: // S_OK, or S_FALSE when the thread had it
		defer coUninitialize.Call()
	case uint32(hr) != rpcEChangedMode: // a single-threaded thread still works
		return nil, hresult(hr)
	}
	var factory, stream, decoder, frame, scaler, straight, srcCtx, dstCtx, transform, converter com
	defer func() {
		for _, o := range []com{converter, transform, dstCtx, srcCtx, straight, scaler, frame, decoder, stream, factory} {
			release(o)
		}
	}()
	if hr, _, _ := coCreateInstance.Call(ptr(&clsidWICImagingFactory), 0, clsctxInprocServer,
		ptr(&iidWICImagingFactory), ptr(&factory)); int32(hr) < 0 {
		return nil, hresult(hr)
	}
	// The stream reads data in place, so it must stay where it is; the
	// profiles are pinned too so no buffer moves while WIC holds it.
	var pin runtime.Pinner
	defer pin.Unpin()
	pin.Pin(&data[0])
	if err := call(factory, 14, ptr(&stream)); err != nil { // IWICImagingFactory::CreateStream
		return nil, err
	}
	if err := call(stream, 16, uintptr(unsafe.Pointer(&data[0])), uintptr(len(data))); err != nil { // IWICStream::InitializeFromMemory
		return nil, err
	}
	if err := call(factory, 4, uintptr(stream), 0, 0, ptr(&decoder)); err != nil { // CreateDecoderFromStream
		return nil, err
	}
	if err := call(decoder, 13, 0, ptr(&frame)); err != nil { // IWICBitmapDecoder::GetFrame
		return nil, err
	}
	var w, h uint32
	if err := call(frame, 3, ptr(&w), ptr(&h)); err != nil { // IWICBitmapSource::GetSize
		return nil, err
	}
	if w == 0 || h == 0 || uint64(w)*uint64(h) > maxPixels {
		return nil, fmt.Errorf("unsupported size %d × %d", w, h)
	}
	var err error
	if srcCtx, err = sourceContext(factory, frame, data, &pin); err != nil {
		return nil, err
	}
	if dstCtx, err = newContext(factory, target, &pin); err != nil {
		return nil, err
	}
	// Straight 8-bit BGRA in and out of the transform: every WIC decoder
	// converts to it, the system engine transforms it, and alpha passes
	// through untouched.
	// The source of the conversion: the frame, or its scaler.
	source := frame
	if outW > 0 {
		if err := call(factory, 11, ptr(&scaler)); err != nil { // CreateBitmapScaler
			return nil, err
		}
		if err := call(scaler, 8, uintptr(frame), uintptr(outW), uintptr(outH), interpolationFant); err != nil { // IWICBitmapScaler::Initialize
			return nil, err
		}
		source, w, h = scaler, uint32(outW), uint32(outH)
	}
	if err := call(factory, 10, ptr(&straight)); err != nil { // CreateFormatConverter
		return nil, err
	}
	if err := call(straight, 8, uintptr(source), ptr(&pixelFormatBGRA), 0, 0, 0, 0); err != nil { // IWICFormatConverter::Initialize
		return nil, err
	}
	if err := call(factory, 16, ptr(&transform)); err != nil { // CreateColorTransformer
		return nil, err
	}
	if err := call(transform, 8, uintptr(straight), uintptr(srcCtx), uintptr(dstCtx), ptr(&pixelFormatBGRA)); err != nil { // IWICColorTransform::Initialize
		return nil, fmt.Errorf("color transform: %w", err)
	}
	if err := call(factory, 10, ptr(&converter)); err != nil { // CreateFormatConverter
		return nil, err
	}
	if err := call(converter, 8, uintptr(transform), ptr(&pixelFormatPRGBA), 0, 0, 0, 0); err != nil { // IWICFormatConverter::Initialize
		return nil, err
	}
	img := image.NewRGBA(image.Rect(0, 0, int(w), int(h)))
	if err := call(converter, 7, 0, uintptr(img.Stride), uintptr(len(img.Pix)), ptr(&img.Pix[0])); err != nil { // CopyPixels
		return nil, err
	}
	return img, nil
}

// newContext makes a color context from an ICC profile, or sRGB when
// profile is nil.
func newContext(factory com, profile []byte, pin *runtime.Pinner) (com, error) {
	var ctx com
	if err := call(factory, 15, ptr(&ctx)); err != nil { // CreateColorContext
		return nil, err
	}
	var err error
	switch {
	case profile == nil:
		err = call(ctx, 5, exifSRGB) // IWICColorContext::InitializeFromExifColorSpace
	case len(profile) == 0:
		err = errors.New("empty profile")
	default:
		pin.Pin(&profile[0])
		err = call(ctx, 4, ptr(&profile[0]), uintptr(len(profile))) // IWICColorContext::InitializeFromMemory
	}
	if err != nil {
		release(ctx)
		return nil, err
	}
	return ctx, nil
}

// newBlank makes an uninitialized color context for a decoder to fill.
func newBlank(factory com) (com, error) {
	var ctx com
	if err := call(factory, 15, ptr(&ctx)); err != nil { // CreateColorContext
		return nil, err
	}
	return ctx, nil
}

// sourceContext returns the frame's ICC color context, preferring one the
// decoder reports. Not every decoder reports the profile of every format,
// so failing that it parses one from data, and failing that it takes the
// frame to be sRGB, as browsers show untagged images.
func sourceContext(factory, frame com, data []byte, pin *runtime.Pinner) (com, error) {
	var n uint32
	if call(frame, 9, 0, 0, ptr(&n)) == nil && n > 0 && n <= 16 { // IWICBitmapFrameDecode::GetColorContexts, count only
		ctxs := make([]com, n)
		defer func() {
			for _, c := range ctxs {
				release(c)
			}
		}()
		for i := range ctxs {
			c, err := newBlank(factory)
			if err != nil {
				return nil, err
			}
			ctxs[i] = c
		}
		var got uint32
		if call(frame, 9, uintptr(n), uintptr(unsafe.Pointer(&ctxs[0])), ptr(&got)) == nil {
			for i := range ctxs[:min(got, n)] {
				var typ uint32
				if call(ctxs[i], 6, ptr(&typ)) == nil && typ == colorContextProfile { // IWICColorContext::GetType
					c := ctxs[i]
					ctxs[i] = nil // handed to the caller, not released here
					return c, nil
				}
			}
		}
	}
	return newContext(factory, Embedded(data), pin)
}
