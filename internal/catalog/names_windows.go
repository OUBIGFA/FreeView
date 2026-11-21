package catalog

import (
	"path/filepath"
	"syscall"
	"unsafe"
)

// StrCmpLogicalW is the comparison Explorer sorts file names with: digits
// compare as numbers ("2.jpg" before "10.jpg") and case is ignored, so
// FreeView moves through a folder in the order Explorer shows it.
var strCmpLogicalW = syscall.NewLazyDLL("shlwapi.dll").NewProc("StrCmpLogicalW")

// compareNames compares two paths the way Explorer orders them: by folder
// first, then by file name.
func compareNames(a, b string) int {
	da, db := filepath.Dir(a), filepath.Dir(b)
	if da != db {
		if r := logical(da, db); r != 0 {
			return r
		}
	}
	return logical(filepath.Base(a), filepath.Base(b))
}

func logical(a, b string) int {
	pa, _ := syscall.UTF16PtrFromString(a)
	pb, _ := syscall.UTF16PtrFromString(b)
	r, _, _ := strCmpLogicalW.Call(uintptr(unsafe.Pointer(pa)), uintptr(unsafe.Pointer(pb)))
	return int(int32(r))
}
