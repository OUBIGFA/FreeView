//go:build !windows

package fileops

import (
	"errors"
	"os"
	"syscall"
)

// FreeView ships for Windows only. These stubs keep the package building
// elsewhere so shared code and tests can still be compiled.

// renameNoReplace uses link-then-unlink, which fails instead of replacing
// an existing target. It is a portable fallback, not a shipped path.
func renameNoReplace(from, to string) error {
	if err := os.Link(from, to); err != nil {
		if errors.Is(err, os.ErrExist) {
			return ErrExists
		}
		return err
	}
	return os.Remove(from)
}

func crossDevice(err error) bool { return errors.Is(err, syscall.EXDEV) }

// OpenWith is only implemented on Windows.
func OpenWith(program, path string) error { return errors.ErrUnsupported }

// ShowProperties is only implemented on Windows.
func ShowProperties(path string) error { return errors.ErrUnsupported }
