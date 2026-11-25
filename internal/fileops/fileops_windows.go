package fileops

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"syscall"
	"unsafe"

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

// renameNoReplace renames without MOVEFILE_REPLACE_EXISTING, so the
// check for an existing target and the rename are one atomic step. It
// also omits MOVEFILE_COPY_ALLOWED: a move across volumes fails with
// ERROR_NOT_SAME_DEVICE and MoveTo does the copy itself, deleting the
// source only after the copy is complete.
func renameNoReplace(from, to string) error {
	f, err := windows.UTF16PtrFromString(from)
	if err != nil {
		return err
	}
	t, err := windows.UTF16PtrFromString(to)
	if err != nil {
		return err
	}
	err = windows.MoveFileEx(f, t, 0)
	if errors.Is(err, windows.ERROR_ALREADY_EXISTS) || errors.Is(err, windows.ERROR_FILE_EXISTS) {
		return ErrExists
	}
	if err != nil {
		return &os.LinkError{Op: "rename", Old: from, New: to, Err: err}
	}
	return nil
}

func crossDevice(err error) bool {
	return errors.Is(err, windows.ERROR_NOT_SAME_DEVICE)
}

// OpenWith starts program with path as its single argument and does not
// wait for it. os/exec quotes the argument by the rules of
// CommandLineToArgvW, so spaces and quotes in path survive, and no
// command shell is involved that could interpret metacharacters.
func OpenWith(program, path string) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	cmd := exec.Command(program, abs)
	cmd.Dir = filepath.Dir(abs) // some editors resolve siblings from here
	if err := cmd.Start(); err != nil {
		return err
	}
	// The program outlives this call; release the handle instead of Wait.
	return cmd.Process.Release()
}

// ShowProperties opens the Explorer properties dialog of path. Call it
// off the UI thread: ShellExecuteEx can wait for shell extensions. It
// returns once the dialog is requested; the shell shows the property
// sheet on its own thread in this process, so it closes with the app.
func ShowProperties(path string) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	if _, err := os.Stat(abs); err != nil {
		return err
	}
	file, err := windows.UTF16PtrFromString(abs)
	if err != nil {
		return err
	}
	verb, err := windows.UTF16PtrFromString("properties")
	if err != nil {
		return err
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	// Property sheet handlers need STA COM. Balance both S_OK and S_FALSE
	// on this same thread, before Go returns it to the worker pool.
	if err := windows.CoInitializeEx(0, windows.COINIT_APARTMENTTHREADED|windows.COINIT_DISABLE_OLE1DDE); err != nil && err != syscall.Errno(windows.S_FALSE) {
		return err
	}
	defer windows.CoUninitialize()
	// SEE_MASK_INVOKEIDLIST routes the verb through the item's context
	// menu, where "properties" lives. This worker has no message loop,
	// so SEE_MASK_NOASYNC finishes the shell handoff before returning.
	const invokeIDList, noUI, noAsync = 0x0C, 0x400, 0x100
	info := shellExecuteInfo{Mask: invokeIDList | noUI | noAsync, Verb: verb, File: file, Show: windows.SW_SHOWNORMAL}
	info.Size = uint32(unsafe.Sizeof(info))
	if ok, _, err := shellExecuteEx.Call(uintptr(unsafe.Pointer(&info))); ok == 0 {
		return err
	}
	return nil
}
