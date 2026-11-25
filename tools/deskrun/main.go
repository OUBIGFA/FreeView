//go:build windows

// Command deskrun runs a desktop test of FreeView on a private Windows
// desktop, so the test never takes the user's screen, focus, mouse or
// keyboard. It starts the program there, posts window messages to its
// main nsgo window as a script says and saves PrintWindow captures.
//
// Usage: go run ./tools/deskrun <script> <exe> [args...]
//
// Script lines:
//
//	sleep <ms>                       wait
//	post <msg> <wparam> <lparam>     PostMessage to the main window
//	opost <msg> <wparam> <lparam>    PostMessage to the other visible nsgo window
//	responsive main|other           fail if the window cannot answer within one second
//	other true|false                assert whether another nsgo window is visible
//	mouse <msg> <wparam> <x> <y>     a mouse message at client x, y (wheel messages get screen coordinates)
//	shot <file.png>                  capture the main window's client area
//	shotall <prefix>                 capture every visible nsgo window of the process
//	list                             print the process's top-level windows, visible or not and topmost or not
//	alive [true|false]               print, optionally assert, whether the process still runs
//	mem                              print the process's working set, private bytes and CPU time so far
//	state                            print whether the window is topmost, its frame's height beyond the client area (0: no title bar) and its position
//	omouse <msg> <wparam> <x> <y>    mouse, to the other visible nsgo window (such as the preferences)
//	oshot <file.png>                 shot, of the other visible nsgo window
//	frame main|other <x> <y> <w> <h>  move and size the main window, or another visible nsgo window, in screen pixels (SetWindowPos)
//	hit <x> <y>                      print WM_NCHITTEST at client x, y (2: HTCAPTION, drags the window)
//	launch <ms> [args...]            start the exe again with args, wait ms and print whether that run still runs (false: it handed its launch to the first)
//
// Posted messages bypass the system's input state: keys arrive without
// modifiers held, so shortcuts with Ctrl or Shift cannot be tested here.
package main

import (
	"fmt"
	"image"
	"image/png"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

var (
	user32              = syscall.NewLazyDLL("user32.dll")
	gdi32               = syscall.NewLazyDLL("gdi32.dll")
	kernel              = syscall.NewLazyDLL("kernel32.dll")
	pCreateDesktop      = user32.NewProc("CreateDesktopW")
	pCloseDesktop       = user32.NewProc("CloseDesktop")
	pEnumDesktopWindows = user32.NewProc("EnumDesktopWindows")
	pGetTid             = user32.NewProc("GetWindowThreadProcessId")
	pPost               = user32.NewProc("PostMessageW")
	pIsVisible          = user32.NewProc("IsWindowVisible")
	pGetClientRect      = user32.NewProc("GetClientRect")
	pGetDC              = user32.NewProc("GetDC")
	pReleaseDC          = user32.NewProc("ReleaseDC")
	pPrintWindow        = user32.NewProc("PrintWindow")
	pCreateCompatDC     = gdi32.NewProc("CreateCompatibleDC")
	pCreateDIB          = gdi32.NewProc("CreateDIBSection")
	pSelect             = gdi32.NewProc("SelectObject")
	pDeleteDC           = gdi32.NewProc("DeleteDC")
	pDeleteObj          = gdi32.NewProc("DeleteObject")
	pCreateProcess      = kernel.NewProc("CreateProcessW")
	pTerminate          = kernel.NewProc("TerminateProcess")
	pGetClassName       = user32.NewProc("GetClassNameW")
	pClientToScreen     = user32.NewProc("ClientToScreen")
	pGetWindowRect      = user32.NewProc("GetWindowRect")
	pGetWindowLong      = user32.NewProc("GetWindowLongPtrW")
	pSend               = user32.NewProc("SendMessageW")
	pSendTimeout        = user32.NewProc("SendMessageTimeoutW")
	pSetWindowPos       = user32.NewProc("SetWindowPos")
)

type startupInfo struct {
	Cb                                              uint32
	_                                               *uint16
	Desktop                                         *uint16
	Title                                           *uint16
	X, Y, XSize, YSize, XCount, YCount, Fill, Flags uint32
	ShowWindow, _                                   uint16
	_                                               *byte
	StdIn, StdOut, StdErr                           syscall.Handle
}
type procInfo struct {
	Process, Thread syscall.Handle
	Pid, Tid        uint32
}

func main() {
	// usage: deskrun <script> <exe> [args...]; script lines: sleep ms | shot file | post msg wparam lparam | key vk
	script, _ := os.ReadFile(os.Args[1])
	name, _ := syscall.UTF16PtrFromString(fmt.Sprintf("freeview-test-%d", os.Getpid()))
	hdesk, _, err := pCreateDesktop.Call(uintptr(unsafe.Pointer(name)), 0, 0, 0, 0x10000000, 0)
	if hdesk == 0 {
		panic(err)
	}
	defer pCloseDesktop.Call(hdesk)
	var si startupInfo
	si.Cb = uint32(unsafe.Sizeof(si))
	si.Desktop = name
	cmd := `"` + os.Args[2] + `"`
	for _, a := range os.Args[3:] {
		cmd += ` "` + a + `"`
	}
	cl, _ := syscall.UTF16PtrFromString(cmd)
	var pi procInfo
	started := time.Now()
	r, _, err := pCreateProcess.Call(0, uintptr(unsafe.Pointer(cl)), 0, 0, 0, 0, 0, 0, uintptr(unsafe.Pointer(&si)), uintptr(unsafe.Pointer(&pi)))
	if r == 0 {
		panic(err)
	}
	defer pTerminate.Call(uintptr(pi.Process), 0)
	var hwnd uintptr
	// One callback for the whole wait: Go never frees callbacks.
	find := syscall.NewCallback(func(h, _ uintptr) uintptr {
		var p uint32
		pGetTid.Call(h, uintptr(unsafe.Pointer(&p)))
		v, _, _ := pIsVisible.Call(h)
		var cn [64]uint16
		pGetClassName.Call(h, uintptr(unsafe.Pointer(&cn[0])), 64)
		if p == pi.Pid && v != 0 && syscall.UTF16ToString(cn[:]) == "NsgoWindow" {
			hwnd = h
			return 0
		}
		return 1
	})
	// Polled every 5 ms, so "visible after" measures the start: FreeView
	// shows its window once the image it opens with is on screen.
	for i := 0; i < 2000 && hwnd == 0; i++ {
		time.Sleep(5 * time.Millisecond)
		pEnumDesktopWindows.Call(hdesk, find, 0)
	}
	fmt.Println("hwnd", hwnd, "visible after", time.Since(started).Milliseconds(), "ms")
	for _, line := range strings.Split(string(script), "\n") {
		// other is the process's visible nsgo window besides the main one.
		other := func() (target uintptr) {
			cb := syscall.NewCallback(func(h, _ uintptr) uintptr {
				var p uint32
				pGetTid.Call(h, uintptr(unsafe.Pointer(&p)))
				var cn [64]uint16
				pGetClassName.Call(h, uintptr(unsafe.Pointer(&cn[0])), 64)
				v, _, _ := pIsVisible.Call(h)
				if p == pi.Pid && v != 0 && h != hwnd && syscall.UTF16ToString(cn[:]) == "NsgoWindow" {
					target = h
					return 0
				}
				return 1
			})
			pEnumDesktopWindows.Call(hdesk, cb, 0)
			return target
		}
		f := strings.Fields(line)
		if len(f) == 0 {
			continue
		}
		n := func(i int) uintptr { v, _ := strconv.ParseInt(f[i], 0, 64); return uintptr(v) }
		switch f[0] {
		case "launch":
			cmd := `"` + os.Args[2] + `"`
			for _, a := range f[2:] {
				cmd += ` "` + a + `"`
			}
			cl, _ := syscall.UTF16PtrFromString(cmd)
			var pi2 procInfo
			if r, _, err := pCreateProcess.Call(0, uintptr(unsafe.Pointer(cl)), 0, 0, 0, 0, 0, 0, uintptr(unsafe.Pointer(&si)), uintptr(unsafe.Pointer(&pi2))); r == 0 {
				panic(err)
			}
			defer pTerminate.Call(uintptr(pi2.Process), 0)
			time.Sleep(time.Duration(n(1)) * time.Millisecond)
			r, _, _ := kernel.NewProc("WaitForSingleObject").Call(uintptr(pi2.Process), 0)
			fmt.Println("launch alive", r == 0x102)
		case "sleep":
			time.Sleep(time.Duration(n(1)) * time.Millisecond)
		case "post", "opost":
			target := hwnd
			if f[0] == "opost" {
				target = other()
			}
			pPost.Call(target, n(1), n(2), n(3))
		case "responsive":
			target := hwnd
			if f[1] == "other" {
				target = other()
			}
			// WM_NULL with SMTO_BLOCK|SMTO_ABORTIFHUNG gives a bounded
			// failure before PrintWindow or SetWindowPos could hang.
			var result uintptr
			ok, _, _ := pSendTimeout.Call(target, 0, 0, 0, 0x3, 1000, uintptr(unsafe.Pointer(&result)))
			if target == 0 || ok == 0 {
				panic("unresponsive window: " + f[1])
			}
			fmt.Println("responsive", f[1], true)
		case "other":
			got := other() != 0
			if got != (f[1] == "true") {
				panic(fmt.Sprintf("other = %v, want %s", got, f[1]))
			}
			fmt.Println("other", got)
		case "mem":
			// PROCESS_MEMORY_COUNTERS_EX, through K32GetProcessMemoryInfo.
			var pmc struct {
				cb                                                          uint32
				faults                                                      uint32
				peakWorkingSet, workingSet, qpPeakPaged, qpPaged            uintptr
				qpPeakNonPaged, qpNonPaged, pagefile, peakPagefile, private uintptr
			}
			pmc.cb = uint32(unsafe.Sizeof(pmc))
			kernel.NewProc("K32GetProcessMemoryInfo").Call(uintptr(pi.Process), uintptr(unsafe.Pointer(&pmc)), uintptr(pmc.cb))
			var created, exited, kernelTime, userTime syscall.Filetime
			syscall.GetProcessTimes(syscall.Handle(pi.Process), &created, &exited, &kernelTime, &userTime)
			ticks := func(t syscall.Filetime) int64 { return int64(t.HighDateTime)<<32 | int64(t.LowDateTime) }
			cpu := time.Duration(ticks(kernelTime)+ticks(userTime)) * 100
			fmt.Printf("mem working=%.1fMB peak=%.1fMB private=%.1fMB cpu=%v\n", float64(pmc.workingSet)/(1<<20),
				float64(pmc.peakWorkingSet)/(1<<20), float64(pmc.private)/(1<<20), cpu)
		case "alive":
			r, _, _ := kernel.NewProc("WaitForSingleObject").Call(uintptr(pi.Process), 0)
			fmt.Println("alive", r == 0x102)
			if len(f) > 1 && (r == 0x102) != (f[1] == "true") {
				panic("unexpected process lifetime")
			}
		case "shotall":
			k := 0
			cb := syscall.NewCallback(func(h, _ uintptr) uintptr {
				var p uint32
				pGetTid.Call(h, uintptr(unsafe.Pointer(&p)))
				var cn [64]uint16
				pGetClassName.Call(h, uintptr(unsafe.Pointer(&cn[0])), 64)
				v, _, _ := pIsVisible.Call(h)
				if p == pi.Pid && v != 0 && syscall.UTF16ToString(cn[:]) == "NsgoWindow" {
					shot(h, fmt.Sprintf("%s%d.png", f[1], k))
					k++
				}
				return 1
			})
			pEnumDesktopWindows.Call(hdesk, cb, 0)
		case "list":
			cb := syscall.NewCallback(func(h, _ uintptr) uintptr {
				var p uint32
				pGetTid.Call(h, uintptr(unsafe.Pointer(&p)))
				var cn [64]uint16
				pGetClassName.Call(h, uintptr(unsafe.Pointer(&cn[0])), 64)
				v, _, _ := pIsVisible.Call(h)
				if p == pi.Pid {
					ex, _, _ := pGetWindowLong.Call(h, ^uintptr(19)) // GWL_EXSTYLE
					fmt.Println("win", h, syscall.UTF16ToString(cn[:]), v, "topmost", ex&0x8 != 0)
				}
				return 1
			})
			pEnumDesktopWindows.Call(hdesk, cb, 0)
		case "state":
			const gwlExStyle, wsExTopmost = ^uintptr(19), 0x8 // GWL_EXSTYLE is -20
			ex, _, _ := pGetWindowLong.Call(hwnd, gwlExStyle)
			var wr, cr [4]int32
			pGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&wr)))
			pGetClientRect.Call(hwnd, uintptr(unsafe.Pointer(&cr)))
			fmt.Println("state topmost", ex&wsExTopmost != 0, "frame", (wr[3]-wr[1])-cr[3], "at", wr[0], wr[1], "size", wr[2]-wr[0], wr[3]-wr[1])
		case "frame":
			target := hwnd
			if f[1] == "other" {
				target = other()
			}
			const swpNoZOrder, swpNoActivate = 0x4, 0x10
			r, _, _ := pSetWindowPos.Call(target, 0, n(2), n(3), n(4), n(5), swpNoZOrder|swpNoActivate)
			fmt.Println("frame", f[1], target, r)
		case "hit":
			pt := [2]int32{int32(n(1)), int32(n(2))}
			pClientToScreen.Call(hwnd, uintptr(unsafe.Pointer(&pt)))
			r, _, _ := pSend.Call(hwnd, 0x84, 0, uintptr(uint32(uint16(pt[0]))|uint32(uint16(pt[1]))<<16))
			fmt.Println("hit", f[1], f[2], r)
		case "shot":
			shot(hwnd, f[1])
		case "mouse", "omouse": // mouse msg wparam x y (client coords; wheel messages get screen coords)
			target := hwnd
			if f[0] == "omouse" {
				target = other()
			}
			pt := [2]int32{int32(n(3)), int32(n(4))}
			if n(1) == 0x20A || n(1) == 0x20E {
				pClientToScreen.Call(target, uintptr(unsafe.Pointer(&pt)))
			}
			pPost.Call(target, n(1), n(2), uintptr(uint32(uint16(pt[0]))|uint32(uint16(pt[1]))<<16))
		case "oshot":
			shot(other(), f[1])
		}
	}
}

func shot(hwnd uintptr, file string) {
	var rc [4]int32
	pGetClientRect.Call(hwnd, uintptr(unsafe.Pointer(&rc)))
	w, h := rc[2], rc[3]
	sdc, _, _ := pGetDC.Call(0)
	dc, _, _ := pCreateCompatDC.Call(sdc)
	type bih struct {
		Size          uint32
		W, H          int32
		Planes, Bits  uint16
		Comp, SizeImg uint32
		X, Y          int32
		Used, Imp     uint32
	}
	bi := bih{Size: 40, W: w, H: -h, Planes: 1, Bits: 32}
	var bits *byte // the DIB's pixels, which the system allocates
	bmp, _, _ := pCreateDIB.Call(dc, uintptr(unsafe.Pointer(&bi)), 0, uintptr(unsafe.Pointer(&bits)), 0, 0)
	pSelect.Call(dc, bmp)
	r, _, _ := pPrintWindow.Call(hwnd, dc, 3) // PW_CLIENTONLY|PW_RENDERFULLCONTENT
	img := image.NewRGBA(image.Rect(0, 0, int(w), int(h)))
	if bits == nil || w <= 0 || h <= 0 {
		fmt.Println("shot", file, "failed: no window area")
		return
	}
	src := unsafe.Slice(bits, int(w*h*4))
	for i := 0; i < len(src); i += 4 {
		img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = src[i+2], src[i+1], src[i], 255
	}
	fo, _ := os.Create(file)
	png.Encode(fo, img)
	fo.Close()
	pDeleteObj.Call(bmp)
	pDeleteDC.Call(dc)
	pReleaseDC.Call(0, sdc)
	fmt.Println("shot", file, w, h, r)
}
