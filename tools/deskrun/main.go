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
//	mouse <msg> <wparam> <x> <y>     a mouse message at client x, y (wheel messages get screen coordinates)
//	shot <file.png>                  capture the main window's client area
//	shotall <prefix>                 capture every visible nsgo window of the process
//	list                             print the process's top-level windows, visible or not and topmost or not
//	alive                            print whether the process still runs
//	state                            print whether the window is topmost, its frame's height beyond the client area (0: no title bar) and its position
//	frame main|other <x> <y> <w> <h>  move and size the main window, or another visible nsgo window, in screen pixels (SetWindowPos)
//	hit <x> <y>                      print WM_NCHITTEST at client x, y (2: HTCAPTION, drags the window)
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
	r, _, err := pCreateProcess.Call(0, uintptr(unsafe.Pointer(cl)), 0, 0, 0, 0, 0, 0, uintptr(unsafe.Pointer(&si)), uintptr(unsafe.Pointer(&pi)))
	if r == 0 {
		panic(err)
	}
	defer pTerminate.Call(uintptr(pi.Process), 0)
	var hwnd uintptr
	for i := 0; i < 100 && hwnd == 0; i++ {
		time.Sleep(100 * time.Millisecond)
		cb := syscall.NewCallback(func(h, _ uintptr) uintptr {
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
		pEnumDesktopWindows.Call(hdesk, cb, 0)
	}
	fmt.Println("hwnd", hwnd)
	for _, line := range strings.Split(string(script), "\n") {
		f := strings.Fields(line)
		if len(f) == 0 {
			continue
		}
		n := func(i int) uintptr { v, _ := strconv.ParseInt(f[i], 0, 64); return uintptr(v) }
		switch f[0] {
		case "sleep":
			time.Sleep(time.Duration(n(1)) * time.Millisecond)
		case "post":
			pPost.Call(hwnd, n(1), n(2), n(3))
		case "alive":
			r, _, _ := kernel.NewProc("WaitForSingleObject").Call(uintptr(pi.Process), 0)
			fmt.Println("alive", r == 0x102)
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
				target = 0
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
		case "mouse": // mouse msg wparam x y (client coords; wheel messages get screen coords)
			pt := [2]int32{int32(n(3)), int32(n(4))}
			if n(1) == 0x20A || n(1) == 0x20E {
				pClientToScreen.Call(hwnd, uintptr(unsafe.Pointer(&pt)))
			}
			pPost.Call(hwnd, n(1), n(2), uintptr(uint32(uint16(pt[0]))|uint32(uint16(pt[1]))<<16))
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
