//go:build windows

package thumbs

// The shell's IShellItemImageFactory gives the same thumbnail Explorer
// shows: from its cache when it has one, from whatever thumbnail handler or
// codec is installed otherwise. Its bitmap is converted here to
// premultiplied RGBA. The interface is called through its vtable, with the
// slot numbers of shobjidl_core.h noted beside each call.

import (
	"errors"
	"fmt"
	"image"
	"runtime"
	"syscall"
	"unsafe"
)

var (
	ole32          = syscall.NewLazyDLL("ole32.dll")
	coInitializeEx = ole32.NewProc("CoInitializeEx")
	coUninitialize = ole32.NewProc("CoUninitialize")

	shell32                     = syscall.NewLazyDLL("shell32.dll")
	shCreateItemFromParsingName = shell32.NewProc("SHCreateItemFromParsingName")

	gdi32              = syscall.NewLazyDLL("gdi32.dll")
	getObject          = gdi32.NewProc("GetObjectW")
	getDIBits          = gdi32.NewProc("GetDIBits")
	createCompatibleDC = gdi32.NewProc("CreateCompatibleDC")
	deleteDC           = gdi32.NewProc("DeleteDC")
	deleteObject       = gdi32.NewProc("DeleteObject")
)

type guid struct {
	a    uint32
	b, c uint16
	d    [8]byte
}

var iidShellItemImageFactory = guid{0xbcc18b79, 0xba16, 0x442f, [8]byte{0x80, 0xc4, 0x8a, 0x59, 0xc3, 0x0c, 0x46, 0x3b}}

const (
	// COINIT_APARTMENTTHREADED | COINIT_DISABLE_OLE1DDE: shell handlers
	// expect a single-threaded apartment.
	coinitApartment = 0x2 | 0x4
	rpcEChangedMode = 0x80010106
	// SIIGBF_RESIZETOFIT | SIIGBF_BIGGERSIZEOK: keep the aspect ratio, and
	// accept a larger cached thumbnail rather than have the shell scale one
	// to the exact size; it is scaled down here with a better filter.
	siigbfFlags  = 0x0 | 0x1
	biRGB        = 0
	dibRGBColors = 0
	// maxSize bounds the requested size, and four times it the bitmap the
	// shell may hand back, so a thumbnail stays small.
	maxSize = 4096
)

// hresult is a failed shell call.
type hresult uint32

func (h hresult) Error() string { return fmt.Sprintf("shell error 0x%08X", uint32(h)) }

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

func ptr[T any](p *T) uintptr { return uintptr(unsafe.Pointer(p)) }

// File returns a thumbnail of the image file at path no larger than
// size×size pixels, keeping its aspect ratio, through the Windows shell:
// this reuses Explorer's thumbnail cache and every codec the system has.
// The shell's thumbnail, which may be larger, is scaled down with
// resample.Region. Call it off the UI thread: it initializes COM on the
// calling thread, and a thumbnail not yet cached takes a full decode.
func File(path string, size int) (*image.RGBA, error) {
	if size < 1 || size > maxSize {
		return nil, fmt.Errorf("thumbnail size %d out of range", size)
	}
	name, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	// COM is set up per thread: keep this goroutine on one.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	switch hr, _, _ := coInitializeEx.Call(0, coinitApartment); {
	case hr == 0 || hr == 1: // S_OK, or S_FALSE when the thread had it
		defer coUninitialize.Call()
	case uint32(hr) != rpcEChangedMode: // a multithreaded thread still works
		return nil, hresult(hr)
	}
	var factory com
	if hr, _, _ := shCreateItemFromParsingName.Call(uintptr(unsafe.Pointer(name)), 0,
		ptr(&iidShellItemImageFactory), ptr(&factory)); int32(hr) < 0 {
		return nil, hresult(hr)
	}
	defer call(factory, 2) // IUnknown::Release
	// GetImage takes a SIZE by value: on 64-bit Windows its two int32s
	// travel together in one register, on 32-bit as two stack words.
	var args []uintptr
	if unsafe.Sizeof(uintptr(0)) == 8 {
		args = []uintptr{uintptr(uint64(uint32(size)) | uint64(uint32(size))<<32)}
	} else {
		args = []uintptr{uintptr(size), uintptr(size)}
	}
	var hbm uintptr
	if err := call(factory, 3, append(args, siigbfFlags, ptr(&hbm))...); err != nil { // IShellItemImageFactory::GetImage
		return nil, err
	}
	defer deleteObject.Call(hbm)
	img, err := pixels(hbm)
	if err != nil {
		return nil, err
	}
	return Image(img, size), nil
}

// pixels copies the bitmap hbm into premultiplied RGBA.
func pixels(hbm uintptr) (*image.RGBA, error) {
	var bm struct { // BITMAP
		typ, width, height, widthBytes int32
		planes, bitsPixel              uint16
		bits                           uintptr
	}
	if n, _, _ := getObject.Call(hbm, unsafe.Sizeof(bm), ptr(&bm)); n == 0 || bm.width <= 0 || bm.height == 0 {
		return nil, errors.New("unreadable thumbnail bitmap")
	}
	w, h := int(bm.width), int(max(bm.height, -bm.height))
	if w > 4*maxSize || h > 4*maxSize {
		return nil, fmt.Errorf("thumbnail bitmap too large: %d × %d", w, h)
	}
	dc, _, _ := createCompatibleDC.Call(0)
	if dc == 0 {
		return nil, errors.New("no device context for the thumbnail")
	}
	defer deleteDC.Call(dc)
	// Asking for 32-bit rows top-down (a negative height) has GetDIBits
	// convert from whatever layout the bitmap has, bottom-up included.
	var bi struct { // BITMAPINFO
		size                   uint32
		width, height          int32
		planes, bitCount       uint16
		compression, sizeImage uint32
		xppm, yppm             int32
		clrUsed, clrImportant  uint32
		colors                 [4]uint32 // room for the masks GetDIBits may write
	}
	bi.size = 40 // sizeof(BITMAPINFOHEADER)
	bi.width, bi.height, bi.planes, bi.bitCount, bi.compression = int32(w), -int32(h), 1, 32, biRGB
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	if n, _, _ := getDIBits.Call(dc, hbm, 0, uintptr(h), ptr(&img.Pix[0]), ptr(&bi), dibRGBColors); int(n) != h {
		return nil, errors.New("thumbnail bitmap copy failed")
	}
	// The shell's bitmap is premultiplied BGRA, as AlphaBlend wants, but
	// opaque images often come with alpha all zero: those are opaque.
	opaque := true
	for i := 3; i < len(img.Pix); i += 4 {
		if img.Pix[i] != 0 {
			opaque = false
			break
		}
	}
	for i := 0; i < len(img.Pix); i += 4 {
		p := img.Pix[i : i+4 : i+4]
		p[0], p[2] = p[2], p[0]
		if opaque {
			p[3] = 255
		} else {
			// Clamped so the pixels stay valid premultiplied color should
			// a thumbnail handler write straight alpha.
			p[0], p[1], p[2] = min(p[0], p[3]), min(p[1], p[3]), min(p[2], p[3])
		}
	}
	return img, nil
}
