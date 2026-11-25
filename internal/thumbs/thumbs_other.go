//go:build !windows

package thumbs

import (
	"errors"
	"image"
)

// File needs the Windows shell; elsewhere it reports errors.ErrUnsupported.
func File(path string, size int) (*image.RGBA, error) {
	return nil, errors.ErrUnsupported
}
