//go:build windows

package winmouse

import (
	"sync"
	"sync/atomic"
	"syscall"
	"unsafe"
)

// Hand is the hand cursor FreeView shows over a draggable image. Windows
// has no open or closed hand of its own (IDC_HAND points a finger), so
// the two are drawn here, as qView's Qt draws its own.
type Hand int32

const (
	NoHand     Hand = iota // nsgo's cursor
	OpenHand               // over an image that can be dragged
	ClosedHand             // while dragging it
)

const (
	wmSetCursor = 0x0020
	htClient    = 1
	smCxCursor  = 13
	idcArrow    = 32512
)

var (
	gdi32                  = syscall.NewLazyDLL("gdi32.dll")
	procCreateDIBSection   = gdi32.NewProc("CreateDIBSection")
	procCreateBitmap       = gdi32.NewProc("CreateBitmap")
	procDeleteObject       = gdi32.NewProc("DeleteObject")
	procCreateIconIndirect = user32.NewProc("CreateIconIndirect")
	procSetCursor          = user32.NewProc("SetCursor")
	procLoadCursor         = user32.NewProc("LoadCursorW")
	procGetCursorPos       = user32.NewProc("GetCursorPos")
	procWindowFromPoint    = user32.NewProc("WindowFromPoint")
	procGetSystemMetrics   = user32.NewProc("GetSystemMetrics")
	hand                   atomic.Int32
	handOnce               sync.Once
	handCursors            [3]uintptr
)

// The hands, 16 by 16: X is the outline, . the fill, a space shows
// through. The hot spot is the center.
var handArt = [3][16]string{
	OpenHand: {
		"       XX       ",
		"   XX X..XXX    ",
		"  X..XX..X..X   ",
		"  X..XX..X..X X ",
		"   X..X..X..XX.X",
		"   X..X..X..X..X",
		" XX X.......X..X",
		"X..XX..........X",
		"X...X.........X ",
		" X............X ",
		"  X...........X ",
		"  X..........X  ",
		"   X.........X  ",
		"    X.......X   ",
		"     X......X   ",
		"     X......X   ",
	},
	ClosedHand: {
		"",
		"",
		"",
		"",
		"    XX XX XX    ",
		"   X..X..X..XX  ",
		"   X........X.X ",
		"    X.........X ",
		"   XX.........X ",
		"  X...........X ",
		"  X...........X ",
		"  X..........X  ",
		"   X.........X  ",
		"    X.......X   ",
		"     X......X   ",
		"     X......X   ",
	},
}

// SetHand chooses the hand shown over the window's client area, from
// any goroutine. A change shows at once, even during a drag, when
// Windows sends no WM_SETCURSOR.
func SetHand(h Hand) {
	if Hand(hand.Swap(int32(h))) == h {
		return
	}
	if hwnd := attached.Load(); hwnd != 0 {
		procPostMessage.Call(hwnd, uintptr(msgCursor), 0, 0)
	}
}

// handCursor returns the cursor to show for the current hand, 0 for
// nsgo's own.
func handCursor() uintptr {
	h := Hand(hand.Load())
	if h == NoHand {
		return 0
	}
	handOnce.Do(func() {
		handCursors[OpenHand] = makeCursor(handArt[OpenHand])
		handCursors[ClosedHand] = makeCursor(handArt[ClosedHand])
	})
	return handCursors[h]
}

// showHand sets the cursor now while the pointer is over hwnd: the hand,
// or the arrow as it leaves the image; nsgo's own takes over again on
// the next move.
func showHand(hwnd uintptr) {
	var p struct{ X, Y int32 }
	procGetCursorPos.Call(uintptr(unsafe.Pointer(&p)))
	if w, _, _ := procWindowFromPoint.Call(uintptr(uint32(p.X)) | uintptr(uint32(p.Y))<<32); w != hwnd {
		return
	}
	c := handCursor()
	if c == 0 {
		c, _, _ = procLoadCursor.Call(0, idcArrow)
	}
	procSetCursor.Call(c)
}

// makeCursor builds a 32-bit cursor from art, scaled to the system's
// cursor size.
func makeCursor(art [16]string) uintptr {
	k, _, _ := procGetSystemMetrics.Call(smCxCursor)
	k /= 16
	if k < 1 {
		k = 1
	}
	n := int(16 * k)
	bi := struct {
		Size                   uint32
		Width, Height          int32
		Planes, BitCount       uint16
		Compression, SizeImage uint32
		XPels, YPels           int32
		ClrUsed, ClrImportant  uint32
	}{Size: 40, Width: int32(n), Height: -int32(n), Planes: 1, BitCount: 32}
	var bits unsafe.Pointer
	color, _, _ := procCreateDIBSection.Call(0, uintptr(unsafe.Pointer(&bi)), 0, uintptr(unsafe.Pointer(&bits)), 0, 0)
	if color == 0 || bits == nil {
		return 0
	}
	px := unsafe.Slice((*uint32)(bits), n*n)
	for y := 0; y < n; y++ {
		row := art[y/int(k)]
		for x := 0; x < n; x++ {
			c := byte(' ')
			if i := x / int(k); i < len(row) {
				c = row[i]
			}
			switch c {
			case 'X':
				px[y*n+x] = 0xFF000000
			case '.':
				px[y*n+x] = 0xFFFFFFFF
			}
		}
	}
	// An all-zero mask: the color bitmap's alpha decides what shows.
	mask := make([]byte, (n+15)/16*2*n)
	maskBmp, _, _ := procCreateBitmap.Call(uintptr(n), uintptr(n), 1, 1, uintptr(unsafe.Pointer(&mask[0])))
	info := struct {
		Icon               int32
		HotspotX, HotspotY uint32
		Mask, Color        uintptr
	}{HotspotX: uint32(n / 2), HotspotY: uint32(n / 2), Mask: maskBmp, Color: color}
	c, _, _ := procCreateIconIndirect.Call(uintptr(unsafe.Pointer(&info)))
	procDeleteObject.Call(maskBmp)
	procDeleteObject.Call(color)
	return c
}
