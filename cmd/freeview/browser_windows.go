package main

import (
	"runtime"
	"syscall"
	"unsafe"

	"github.com/nsgo-dev/nsgo/core"
	"golang.org/x/sys/windows"
)

var shellExecuteEx = windows.NewLazySystemDLL("shell32.dll").NewProc("ShellExecuteExW")

// shellExecuteInfo is the Windows SHELLEXECUTEINFOW layout.
type shellExecuteInfo struct {
	Size, Mask                  uint32
	Window                      windows.Handle
	Verb, File, Parameters, Dir *uint16
	Show                        int32
	Instance, IDList            uintptr
	Class                       *uint16
	ClassKey                    windows.Handle
	HotKey                      uint32
	Icon, Process               windows.Handle
}

// openBrowser uses the default browser without a command shell. It must
// run in a worker: ShellExecuteEx can wait for shell extensions or DDE.
func openBrowser(url string) error {
	if err := core.CheckURL(url); err != nil {
		return err
	}
	file, err := windows.UTF16PtrFromString(url)
	if err != nil {
		return err
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	// Shell extensions may need STA COM. Balance both S_OK and S_FALSE
	// on this same thread, before Go returns it to the worker pool.
	if err := windows.CoInitializeEx(0, windows.COINIT_APARTMENTTHREADED|windows.COINIT_DISABLE_OLE1DDE); err != nil && err != syscall.Errno(windows.S_FALSE) {
		return err
	}
	defer windows.CoUninitialize()
	const noUI, noAsync = 0x400, 0x100 // SEE_MASK_FLAG_NO_UI, SEE_MASK_NOASYNC
	// This worker has no message loop, so finish the shell handoff here.
	// No owner HWND lets settings close while the browser is starting.
	info := shellExecuteInfo{Mask: noUI | noAsync, File: file, Show: windows.SW_SHOWNORMAL}
	info.Size = uint32(unsafe.Sizeof(info))
	if ok, _, err := shellExecuteEx.Call(uintptr(unsafe.Pointer(&info))); ok == 0 {
		return err
	}
	return nil
}
