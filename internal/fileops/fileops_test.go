package fileops

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestValidateName(t *testing.T) {
	tests := []struct {
		name string
		want error
	}{
		{"photo", nil},
		{"假期 照片 2026", nil},
		{"a.b", nil},
		{".hidden", nil},
		{"CONSOLE", nil},
		{"COM0", nil},
		{"LPT10", nil},
		{"x-CON", nil},
		{strings.Repeat("图", 255), nil},
		{"", ErrEmptyName},
		{"   ", ErrEmptyName},
		{"..", ErrEmptyName},
		{" . ", ErrEmptyName},
		{"a<b", ErrInvalidChars},
		{"a>b", ErrInvalidChars},
		{"a:b", ErrInvalidChars},
		{`a"b`, ErrInvalidChars},
		{"a/b", ErrInvalidChars},
		{`a\b`, ErrInvalidChars},
		{"a|b", ErrInvalidChars},
		{"a?b", ErrInvalidChars},
		{"a*b", ErrInvalidChars},
		{"a\tb", ErrInvalidChars},
		{"a\x00b", ErrInvalidChars},
		{"a\x1fb", ErrInvalidChars},
		{"a\xffb", ErrInvalidChars},
		{"CON", ErrReservedName},
		{"con", ErrReservedName},
		{"Prn", ErrReservedName},
		{"aux", ErrReservedName},
		{"NUL", ErrReservedName},
		{"com1", ErrReservedName},
		{"COM9", ErrReservedName},
		{"lpt5", ErrReservedName},
		{"COM¹", ErrReservedName},
		{"lpt³", ErrReservedName},
		{"nul.txt", ErrReservedName},
		{"CON.tar.gz", ErrReservedName},
		{"con .x", ErrReservedName},
		{"name.", ErrTrailingDot},
		{"name ", ErrTrailingDot},
		{"CON.", ErrTrailingDot},
		{strings.Repeat("a", 256), ErrNameTooLong},
		// U+1F600 takes two UTF-16 units, so 128 of them exceed 255.
		{strings.Repeat("\U0001F600", 128), ErrNameTooLong},
	}
	for _, tt := range tests {
		if got := ValidateName(tt.name); !errors.Is(got, tt.want) || (tt.want == nil && got != nil) {
			t.Errorf("ValidateName(%q) = %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestRename(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "old.jpg")
	writeFile(t, src, "img")
	got, err := Rename(src, "新名字")
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(dir, "新名字.jpg"); got != want {
		t.Fatalf("Rename = %q, want %q", got, want)
	}
	if readFile(t, got) != "img" {
		t.Error("content changed")
	}
	if _, err := os.Stat(src); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("source still exists: %v", err)
	}
	if _, err := Rename(got, "bad?"); !errors.Is(err, ErrInvalidChars) {
		t.Errorf("invalid name: %v", err)
	}
	// The extension is kept, so "nul" + ".jpg" is still a device name.
	if _, err := Rename(got, "nul"); !errors.Is(err, ErrReservedName) {
		t.Errorf("reserved name: %v", err)
	}
}

func TestRenameExists(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "a.png")
	other := filepath.Join(dir, "b.png")
	writeFile(t, src, "a")
	writeFile(t, other, "b")
	if _, err := Rename(src, "b"); !errors.Is(err, ErrExists) {
		t.Fatalf("Rename onto other file = %v, want ErrExists", err)
	}
	// The same name differing only in case is another file, too.
	if _, err := Rename(src, "B"); !errors.Is(err, ErrExists) {
		t.Fatalf("Rename onto other file by case = %v, want ErrExists", err)
	}
	if readFile(t, src) != "a" || readFile(t, other) != "b" {
		t.Error("files changed after failed rename")
	}
}

func TestRenameCaseOnly(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "photo.jpg")
	writeFile(t, src, "x")
	got, err := Rename(src, "Photo")
	if err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "Photo.jpg" {
		t.Fatalf("dir = %v, want only Photo.jpg", entries)
	}
	if got != filepath.Join(dir, "Photo.jpg") {
		t.Errorf("Rename = %q", got)
	}
	// Renaming to the current name is a successful no-op.
	if same, err := Rename(got, "Photo"); err != nil || same != got {
		t.Errorf("same-name Rename = %q, %v", same, err)
	}
}

func TestFreeName(t *testing.T) {
	dir := t.TempDir()
	for i, want := range []string{"pic.webp", "pic (2).webp", "pic (3).webp", "pic (4).webp"} {
		got := FreeName(dir, "pic.webp")
		if got != filepath.Join(dir, want) {
			t.Fatalf("step %d: FreeName = %q, want %q", i, got, want)
		}
		writeFile(t, got, "")
	}
	writeFile(t, filepath.Join(dir, "noext"), "")
	if got := FreeName(dir, "noext"); got != filepath.Join(dir, "noext (2)") {
		t.Errorf("FreeName without extension = %q", got)
	}
	writeFile(t, filepath.Join(dir, ".cfg"), "")
	if got := FreeName(dir, ".cfg"); got != filepath.Join(dir, ".cfg (2)") {
		t.Errorf("FreeName for dot file = %q", got)
	}
}

func TestCopyTo(t *testing.T) {
	srcDir, dstDir := t.TempDir(), t.TempDir()
	src := filepath.Join(srcDir, "shot.gif")
	writeFile(t, src, "GIF89a data")
	mtime := time.Date(2020, 3, 4, 5, 6, 7, 0, time.UTC)
	if err := os.Chtimes(src, mtime, mtime); err != nil {
		t.Fatal(err)
	}
	existing := filepath.Join(dstDir, "shot.gif")
	writeFile(t, existing, "keep me")

	got, err := CopyTo(src, dstDir)
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(dstDir, "shot (2).gif"); got != want {
		t.Fatalf("CopyTo = %q, want %q", got, want)
	}
	if readFile(t, got) != "GIF89a data" {
		t.Error("copy content differs")
	}
	if readFile(t, existing) != "keep me" {
		t.Error("existing file was overwritten")
	}
	if readFile(t, src) != "GIF89a data" {
		t.Error("source changed")
	}
	info, err := os.Stat(got)
	if err != nil {
		t.Fatal(err)
	}
	if !info.ModTime().Equal(mtime) {
		t.Errorf("mtime = %v, want %v", info.ModTime(), mtime)
	}
	entries, _ := os.ReadDir(dstDir)
	if len(entries) != 2 {
		t.Errorf("dst has %d entries, want 2 (temp file left?)", len(entries))
	}
	if _, err := CopyTo(filepath.Join(srcDir, "missing.gif"), dstDir); err == nil {
		t.Error("CopyTo of missing file succeeded")
	}
}

func TestMoveToSameVolume(t *testing.T) {
	root := t.TempDir()
	srcDir, dstDir := filepath.Join(root, "src"), filepath.Join(root, "dst")
	for _, d := range []string{srcDir, dstDir} {
		if err := os.Mkdir(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	src := filepath.Join(srcDir, "a.jpg")
	writeFile(t, src, "moved")
	writeFile(t, filepath.Join(dstDir, "a.jpg"), "other")

	got, err := MoveTo(src, dstDir)
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(dstDir, "a (2).jpg"); got != want {
		t.Fatalf("MoveTo = %q, want %q", got, want)
	}
	if readFile(t, got) != "moved" || readFile(t, filepath.Join(dstDir, "a.jpg")) != "other" {
		t.Error("unexpected content after move")
	}
	if _, err := os.Stat(src); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("source still exists: %v", err)
	}
	// Moving into the folder it is already in leaves it untouched.
	if same, err := MoveTo(got, dstDir); err != nil || same != got {
		t.Errorf("MoveTo own folder = %q, %v", same, err)
	}
}

// OpenWith is not run against a real program: any GUI or console child
// could show a window. Only the failure path is checked.
func TestOpenWithMissingProgram(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows only")
	}
	file := filepath.Join(t.TempDir(), "a.jpg")
	writeFile(t, file, "")
	if err := OpenWith(filepath.Join(t.TempDir(), "missing.exe"), file); err == nil {
		t.Error("OpenWith of missing program succeeded")
	}
}
