//go:build windows

package winmouse

import (
	"syscall"
	"unsafe"
)

const (
	wmClose = 0x0010
	// dwmwaTransitionsForceDisabled turns off the window's DWM animations.
	dwmwaTransitionsForceDisabled = 3
)

var (
	procDwmSetWindowAttribute = syscall.NewLazyDLL("dwmapi.dll").NewProc("DwmSetWindowAttribute")
	msgQuiet                  = func() uint32 {
		name, _ := syscall.UTF16PtrFromString("FreeViewCloseQuietly")
		r, _, _ := procRegisterMessage.Call(uintptr(unsafe.Pointer(name)))
		return uint32(r)
	}()
)

// CloseQuietly has the window vanish at once when it next closes, as the
// app quits: Windows' close animation would show nsgo's composited
// content already gone, a blank flash. It may be called from any
// goroutine before quitting; the window's own thread takes it before the
// quit it posts. The window closing itself, by Alt+F4 or its close
// button, does the same on its own (wndProc).
func CloseQuietly() {
	if hwnd := attached.Load(); hwnd != 0 {
		procPostMessage.Call(hwnd, uintptr(msgQuiet), 0, 0)
	}
}

func disableTransitions(hwnd uintptr) {
	on := int32(1)
	procDwmSetWindowAttribute.Call(hwnd, dwmwaTransitionsForceDisabled, uintptr(unsafe.Pointer(&on)), unsafe.Sizeof(on))
}
