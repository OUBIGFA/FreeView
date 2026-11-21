//go:build windows

// Package winmouse reports the middle and side mouse buttons of FreeView's
// main window, which nsgo v0.2.0 does not pass to apps on Windows: it
// drops WM_MBUTTON* and turns XBUTTON1 into a back press. The package
// subclasses the window (SetWindowLongPtrW with GWLP_WNDPROC), reports
// clicks of those buttons and lets every other message through unchanged.
//
// This is a stopgap until nsgo reports these buttons itself; see
// docs/roadmap.md.
package winmouse

import (
	"errors"
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
	original                uintptr
	report                  func(Button)
	middleDown              bool
	wndProcCallback         = syscall.NewCallback(wndProc)
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
			return nil
		}
		if time.Now().After(deadline) {
			return errors.New("winmouse: no window to attach to")
		}
		time.Sleep(20 * time.Millisecond)
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
