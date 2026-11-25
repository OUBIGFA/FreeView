//go:build !windows

package colorprofile

import (
	"errors"
	"image"
)

// Display needs Windows' per-monitor color profiles; elsewhere it is
// unsupported.
func Display(hwnd uintptr) ([]byte, error) { return nil, errors.ErrUnsupported }

// Convert needs Windows' WIC color transform; elsewhere it is unsupported.
func Convert(data []byte, target []byte) (*image.RGBA, error) { return nil, errors.ErrUnsupported }

// ConvertScaled needs Windows' WIC; elsewhere it is unsupported.
func ConvertScaled(data []byte, target []byte, w, h int) (*image.RGBA, error) {
	return nil, errors.ErrUnsupported
}
