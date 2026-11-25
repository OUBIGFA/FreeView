// Package fileops implements the file commands of the viewer: renaming,
// copying and moving the current image, opening it with another program
// and showing its Explorer properties.
//
// The rules follow Windows file naming. Operations never overwrite an
// existing file: they either fail with ErrExists or pick a free name.
package fileops

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

// Errors returned by ValidateName and Rename. They are sentinels so the
// UI can map each to a localized message with errors.Is.
var (
	ErrEmptyName    = errors.New("fileops: name is empty")
	ErrInvalidChars = errors.New("fileops: name contains invalid characters")
	ErrReservedName = errors.New("fileops: name is reserved by Windows")
	ErrTrailingDot  = errors.New("fileops: name ends with a space or dot")
	ErrNameTooLong  = errors.New("fileops: name is too long")
	ErrExists       = errors.New("fileops: target already exists")
)

// maxNameUnits is the NTFS path component limit in UTF-16 code units.
const maxNameUnits = 255

// freeNameTries bounds the search for a free name so a directory where
// every probe fails (for example an unreadable one) cannot loop forever.
const freeNameTries = 10000

// reserved holds the device names Win32 maps regardless of extension.
// The superscript digits are also treated as COM/LPT ports by Win32.
var reserved = func() map[string]bool {
	m := map[string]bool{"CON": true, "PRN": true, "AUX": true, "NUL": true, "CONIN$": true, "CONOUT$": true}
	for _, d := range []string{"1", "2", "3", "4", "5", "6", "7", "8", "9", "¹", "²", "³"} {
		m["COM"+d] = true
		m["LPT"+d] = true
	}
	return m
}()

// ValidateName checks a new base name (without extension) for a file in
// Windows. It returns one of the exported sentinel errors, or nil.
func ValidateName(base string) error {
	if strings.Trim(base, " .") == "" {
		return ErrEmptyName
	}
	if !utf8.ValidString(base) {
		return ErrInvalidChars
	}
	for _, r := range base {
		if r < 0x20 || strings.ContainsRune(`<>:"/\|?*`, r) {
			return ErrInvalidChars
		}
	}
	if last := base[len(base)-1]; last == ' ' || last == '.' {
		return ErrTrailingDot
	}
	if isReserved(base) {
		return ErrReservedName
	}
	if nameUnits(base) > maxNameUnits {
		return ErrNameTooLong
	}
	return nil
}

// isReserved reports whether Win32 would open a device for name. It
// ignores everything from the first dot and the spaces before it, so
// "nul.txt" and "CON .jpg" are devices too.
func isReserved(name string) bool {
	stem, _, _ := strings.Cut(name, ".")
	return reserved[strings.ToUpper(strings.TrimRight(stem, " "))]
}

func nameUnits(s string) int { return len(utf16.Encode([]rune(s))) }

// Rename renames path's base name keeping its extension and returns the
// new path. It fails with ErrExists when the target is another file; a
// rename that only changes the letter case of the same file is allowed.
func Rename(path, newBase string) (string, error) {
	if err := ValidateName(newBase); err != nil {
		return "", err
	}
	name := newBase + filepath.Ext(path)
	if nameUnits(name) > maxNameUnits {
		return "", ErrNameTooLong
	}
	src, err := os.Lstat(path)
	if err != nil {
		return "", err
	}
	target := filepath.Join(filepath.Dir(path), name)
	if target == path {
		return path, nil
	}
	// On a case-insensitive volume a case-only change resolves to the
	// source itself; that is a rename, not a collision.
	if dst, err := os.Lstat(target); err == nil {
		if !os.SameFile(src, dst) || !strings.EqualFold(filepath.Base(path), name) {
			return "", ErrExists
		}
	}
	if err := renameNoReplace(path, target); err != nil {
		return "", err
	}
	return target, nil
}

// FreeName returns a path in dir for name that does not exist yet: name,
// then "stem (2).ext", "stem (3).ext", and so on. Another process can
// still take the name before it is used, so callers create the file
// without replacing and retry on ErrExists.
func FreeName(dir, name string) string {
	ext := filepath.Ext(name)
	stem := strings.TrimSuffix(name, ext)
	if stem == "" { // ".hidden" is all stem, not all extension
		stem, ext = name, ""
	}
	candidate := filepath.Join(dir, name)
	for i := 2; i < freeNameTries; i++ {
		if _, err := os.Lstat(candidate); errors.Is(err, os.ErrNotExist) {
			return candidate
		}
		candidate = filepath.Join(dir, fmt.Sprintf("%s (%d)%s", stem, i, ext))
	}
	return candidate
}

// CopyTo copies the file at path into dir under FreeName, never
// overwriting, and preserves its modification time. It returns the new
// path. Data goes to a temporary file in dir that is renamed into place,
// so a failed copy never leaves a partial file under the final name.
func CopyTo(path, dir string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("fileops: %s is not a regular file", path)
	}
	tmp, err := os.CreateTemp(dir, ".freeview-*.tmp")
	if err != nil {
		return "", err
	}
	tmpName := tmp.Name()
	placed := false
	defer func() {
		if !placed {
			tmp.Close() // may already be closed; the error is irrelevant
			os.Remove(tmpName)
		}
	}()
	if err := copyContent(tmp, path); err != nil {
		return "", err
	}
	if err := tmp.Close(); err != nil {
		return "", err
	}
	if err := os.Chtimes(tmpName, info.ModTime(), info.ModTime()); err != nil {
		return "", err
	}
	// Mode last: a read-only temp file would reject the time change.
	if err := os.Chmod(tmpName, info.Mode().Perm()); err != nil {
		return "", err
	}
	target, err := placeFree(tmpName, dir, filepath.Base(path))
	if err != nil {
		return "", err
	}
	placed = true
	return target, nil
}

func copyContent(dst *os.File, path string) error {
	src, err := os.Open(path)
	if err != nil {
		return err
	}
	defer src.Close()
	if _, err := io.Copy(dst, src); err != nil {
		return err
	}
	return dst.Sync()
}

// placeFree renames from into dir under the first free variant of name,
// retrying when another process takes the chosen name in between.
func placeFree(from, dir, name string) (string, error) {
	for range 100 {
		target := FreeName(dir, name)
		err := renameNoReplace(from, target)
		if err == nil {
			return target, nil
		}
		if !errors.Is(err, ErrExists) {
			return "", err
		}
	}
	return "", ErrExists
}

// MoveTo moves the file at path into dir under FreeName and returns the
// new path. On the same volume it is a rename. Across volumes it copies
// first and deletes the source only after the copy succeeded.
func MoveTo(path, dir string) (string, error) {
	src, err := os.Lstat(path)
	if err != nil {
		return "", err
	}
	// Moving into its own folder is a no-op, not a "(2)" duplicate.
	if a, err := os.Stat(filepath.Dir(path)); err == nil {
		if b, err := os.Stat(dir); err == nil && os.SameFile(a, b) {
			return path, nil
		}
	}
	target, err := placeFree(path, dir, src.Name())
	if err == nil || !crossDevice(err) {
		return target, err
	}
	target, err = CopyTo(path, dir)
	if err != nil {
		return "", err
	}
	if err := os.Remove(path); err != nil {
		// Keep exactly one copy: the source could not be deleted (for
		// example it is locked), so undo the copy and report the failure.
		os.Remove(target)
		return "", err
	}
	return target, nil
}
