//go:build windows

// Package winmouse reports the middle and side mouse buttons of FreeView's
// main window, which nsgo v0.2.0 does not pass to apps on Windows: it
// drops WM_MBUTTON* and turns XBUTTON1 into a back press. The package
// subclasses the window (SetWindowLongPtrW with GWLP_WNDPROC), reports
// clicks of those buttons and lets every other message through unchanged.
// The same subclass starts Windows' own window move for DragWindow and
// shows the hand cursors of SetHand.
//
// The buttons are a stopgap until nsgo reports them itself; see
// docs/roadmap.md.
package winmouse

import (
	"errors"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"
)

// Button is a mouse button nsgo does not report.
type Button int

const (
	Middle  Button = iota + 1
	Back           // XBUTTON1
	Forward        // XBUTTON2
)

const (
	wmMButtonDown = 0x0207
	wmMButtonUp   = 0x0208
	wmXButtonDown = 0x020B
	wmXButtonUp   = 0x020C
	gwlpWndProc   = ^uintptr(3) // -4
	xButton1      = 1
	wmSysCommand  = 0x0112
	scDragMove    = 0xF012 // SC_MOVE | HTCAPTION: move with the mouse, as a title bar drag does
)

var (
	user32                  = syscall.NewLazyDLL("user32.dll")
	kernel32                = syscall.NewLazyDLL("kernel32.dll")
	procEnumWindows         = user32.NewProc("EnumWindows")
	procGetWindowThreadPID  = user32.NewProc("GetWindowThreadProcessId")
	procGetClassName        = user32.NewProc("GetClassNameW")
	procIsWindowVisible     = user32.NewProc("IsWindowVisible")
	procSetWindowLongPtr    = user32.NewProc("SetWindowLongPtrW")
	procCallWindowProc      = user32.NewProc("CallWindowProcW")
	procGetCurrentProcessID = kernel32.NewProc("GetCurrentProcessId")
	procPostMessage         = user32.NewProc("PostMessageW")
	procReleaseCapture      = user32.NewProc("ReleaseCapture")
	procDefWindowProc       = user32.NewProc("DefWindowProcW")
	procRegisterMessage     = user32.NewProc("RegisterWindowMessageW")
	attached                atomic.Uintptr // the subclassed window
	// msgDrag asks the window's own thread to start the move: only the
	// thread holding the mouse capture can release it.
	msgDrag = func() uint32 {
		name, _ := syscall.UTF16PtrFromString("FreeViewDragWindow")
		r, _, _ := procRegisterMessage.Call(uintptr(unsafe.Pointer(name)))
		return uint32(r)
	}()
	msgCursor = func() uint32 {
		name, _ := syscall.UTF16PtrFromString("FreeViewCursor")
		r, _, _ := procRegisterMessage.Call(uintptr(unsafe.Pointer(name)))
		return uint32(r)
	}()
	original        uintptr
	report          func(Button)
	middleDown      bool
	wndProcCallback = syscall.NewCallback(wndProc)
)

// Attach waits for the process's first visible nsgo window and reports
// clicks of the middle and side buttons over it to fn, on the window's
// thread; fn must hand the work to the engine (core.Scope.Post). It
// gives up after timeout.
func Attach(timeout time.Duration, fn func(Button)) error {
	if original != 0 {
		return errors.New("winmouse: already attached")
	}
	deadline := time.Now().Add(timeout)
	for {
		if hwnd := findWindow(); hwnd != 0 {
			report = fn
			prev, _, err := procSetWindowLongPtr.Call(hwnd, gwlpWndProc, wndProcCallback)
			if prev == 0 {
				return err
			}
			original = prev
			attached.Store(hwnd)
			return nil
		}
		if time.Now().After(deadline) {
			return errors.New("winmouse: no window to attach to")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// DragWindow moves the window with the mouse while the left button stays
// down, through Windows' move loop: snapping to screen edges works as on a
// title bar. It may be called from any goroutine; before Attach it does
// nothing.
func DragWindow() {
	if hwnd := attached.Load(); hwnd != 0 {
		procPostMessage.Call(hwnd, uintptr(msgDrag), 0, 0)
	}
}

func findWindow() uintptr {
	pid, _, _ := procGetCurrentProcessID.Call()
	var found uintptr
	cb := syscall.NewCallback(func(hwnd, _ uintptr) uintptr {
		var owner uint32
		procGetWindowThreadPID.Call(hwnd, uintptr(unsafe.Pointer(&owner)))
		if uintptr(owner) != pid {
			return 1
		}
		if v, _, _ := procIsWindowVisible.Call(hwnd); v == 0 {
			return 1
		}
		var name [32]uint16
		procGetClassName.Call(hwnd, uintptr(unsafe.Pointer(&name[0])), uintptr(len(name)))
		if syscall.UTF16ToString(name[:]) != "NsgoWindow" {
			return 1
		}
		found = hwnd
		return 0
	})
	procEnumWindows.Call(cb, 0)
	return found
}

func wndProc(hwnd uintptr, msg uint32, wParam, lParam uintptr) uintptr {
	switch msg {
	case msgDrag:
		// DefWindowProc rather than nsgo's procedure runs the move loop,
		// which returns once the button is released.
		procReleaseCapture.Call()
		procDefWindowProc.Call(hwnd, wmSysCommand, scDragMove, 0)
		return 0
	case msgCursor:
		showHand(hwnd)
		return 0
	case wmSetCursor:
		if lParam&0xffff == htClient {
			if c := handCursor(); c != 0 {
				procSetCursor.Call(c)
				return 1
			}
		}
	case wmMButtonDown:
		middleDown = true
	case wmMButtonUp:
		if middleDown {
			middleDown = false
			report(Middle)
		}
	case wmXButtonDown:
		// Swallowed with its release: passing them on would make
		// DefWindowProc send WM_APPCOMMAND, which nsgo treats as Escape.
		return 1
	case wmXButtonUp:
		if wParam>>16&0xffff == xButton1 {
			report(Back)
		} else {
			report(Forward)
		}
		return 1
	}
	r, _, _ := procCallWindowProc.Call(original, hwnd, uintptr(msg), wParam, lParam)
	return r
}
