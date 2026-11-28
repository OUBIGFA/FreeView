package settings

import (
	"errors"
	"io/fs"
	"math"
	"os"
	"path/filepath"

	"go.yaml.in/yaml/v3"
)

// Frame is where a window was and how large, in points from the main
// screen's top left, as nsgo's desktop.Frame reports it: the outer size,
// title bar and borders included.
type Frame struct {
	X      float64 `yaml:"x"`
	Y      float64 `yaml:"y"`
	Width  float64 `yaml:"width"`
	Height float64 `yaml:"height"`
	// Maximized says the window was maximized when last left; X, Y, Width
	// and Height are then its frame from before, which restoring it uses.
	Maximized bool `yaml:"maximized,omitempty"`
}

// Windows remembers each window's last frame by its key, such as "main"
// and "preferences". It lives in a file of its own, so Restore defaults
// in the preferences leaves the windows where the user put them.
type Windows map[string]Frame

// WindowsPath is where the frames live: %APPDATA%\FreeView\windows.yaml.
func WindowsPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "FreeView", "windows.yaml"), nil
}

// LoadWindows reads the frames at path; a missing file remembers none.
// Frames without a size are dropped, so the window opens at its default.
func LoadWindows(path string) (Windows, error) {
	w := Windows{}
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return w, nil
	}
	if err != nil {
		return w, err
	}
	if err := yaml.Unmarshal(data, &w); err != nil {
		return Windows{}, err
	}
	if w == nil {
		return Windows{}, nil
	}
	for k, f := range w {
		finite := func(n float64) bool { return !math.IsNaN(n) && !math.IsInf(n, 0) }
		if !finite(f.X) || !finite(f.Y) || !finite(f.Width) || !finite(f.Height) || f.Width <= 0 || f.Height <= 0 {
			delete(w, k)
		}
	}
	return w, nil
}

// SaveWindows writes w to path.
func SaveWindows(path string, w Windows) error { return writeYAML(path, w) }
