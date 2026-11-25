package main

import (
	"runtime"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	kernel32             = windows.NewLazySystemDLL("kernel32.dll")
	globalMemoryStatusEx = kernel32.NewProc("GlobalMemoryStatusEx")
	setThreadPriority    = kernel32.NewProc("SetThreadPriority")
)

// threadPriorityBelowNormal and threadPriorityNormal are SetThreadPriority's
// THREAD_PRIORITY_BELOW_NORMAL and THREAD_PRIORITY_NORMAL.
const threadPriorityBelowNormal, threadPriorityNormal = -1, 0

// lowPriority runs fn on its own OS thread at below-normal priority, so
// Windows runs the window's thread and nsgo's decoding of the shown image
// first whenever both want a core.
func lowPriority(fn func()) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	thread, _ := windows.GetCurrentThread()
	below := threadPriorityBelowNormal
	setThreadPriority.Call(uintptr(thread), uintptr(below))
	// The thread goes back to Go's pool afterwards, at normal priority.
	defer setThreadPriority.Call(uintptr(thread), threadPriorityNormal)
	fn()
}

// physicalMemory is the machine's installed memory in bytes, 0 when
// Windows does not say.
func physicalMemory() uint64 {
	var m struct { // MEMORYSTATUSEX
		length, load                       uint32
		totalPhys, availPhys               uint64
		totalPage, availPage               uint64
		totalVirtual, availVirtual, extVir uint64
	}
	m.length = uint32(unsafe.Sizeof(m))
	if ok, _, _ := globalMemoryStatusEx.Call(uintptr(unsafe.Pointer(&m))); ok == 0 {
		return 0
	}
	return m.totalPhys
}
