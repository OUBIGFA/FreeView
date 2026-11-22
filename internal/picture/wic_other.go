//go:build !windows

package picture

import (
	"errors"
	"image"
)

// decodeWIC needs Windows; FreeView ships for Windows only.
func decodeWIC([]byte, Codec) (*image.RGBA, int, error) {
	return nil, 0, errors.New("this format needs Windows")
}

func missingCodec(error) bool { return false }
