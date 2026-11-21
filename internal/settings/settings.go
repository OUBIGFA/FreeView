// Package settings holds FreeView's user configuration: which action each
// mouse gesture and keyboard shortcut runs, and the viewer's preferences.
// It is stored as YAML so users can also edit it by hand.
package settings

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"

	"go.yaml.in/yaml/v3"
)

// Action names something the viewer can do. The string is stored in the
// configuration file, so existing values must stay stable.
type Action string

const (
	None         Action = "none"
	Next         Action = "next"
	Previous     Action = "previous"
	First        Action = "first"
	Last         Action = "last"
	ZoomIn       Action = "zoom_in"
	ZoomOut      Action = "zoom_out"
	ZoomFit      Action = "zoom_fit"
	ZoomOriginal Action = "zoom_original"
	RotateLeft   Action = "rotate_left"
	RotateRight  Action = "rotate_right"
	FlipH        Action = "flip_horizontal"
	FlipV        Action = "flip_vertical"
	FullScreen   Action = "full_screen"
	Escape       Action = "escape" // leaves full screen, else closes the window (Honeyview's Esc)
	Close        Action = "close"
	Minimize     Action = "minimize"
	Open         Action = "open"
	Reveal       Action = "reveal"
	Delete       Action = "delete"
	CopyPath     Action = "copy_path"
	CopyImage    Action = "copy_image"
	Slideshow    Action = "slideshow"
	Info         Action = "info"
	Menu         Action = "menu"
	Preferences  Action = "preferences"
)

// Actions lists every action in the order the preferences window shows them.
var Actions = []Action{Next, Previous, First, Last, ZoomIn, ZoomOut, ZoomFit, ZoomOriginal,
	RotateLeft, RotateRight, FlipH, FlipV, FullScreen, Escape, Close, Minimize, Open, Reveal,
	Delete, CopyPath, CopyImage, Slideshow, Info, Menu, Preferences}

// Gesture is a mouse input that can be bound to an action.
type Gesture string

const (
	WheelUp         Gesture = "wheel_up"
	WheelDown       Gesture = "wheel_down"
	CtrlWheelUp     Gesture = "ctrl_wheel_up"
	CtrlWheelDown   Gesture = "ctrl_wheel_down"
	ShiftWheelUp    Gesture = "shift_wheel_up"
	ShiftWheelDown  Gesture = "shift_wheel_down"
	AltWheelUp      Gesture = "alt_wheel_up"
	AltWheelDown    Gesture = "alt_wheel_down"
	MiddleClick     Gesture = "middle_click"
	BackButton      Gesture = "back_button"    // XBUTTON1
	ForwardButton   Gesture = "forward_button" // XBUTTON2
	LeftDoubleClick Gesture = "left_double_click"
	RightClick      Gesture = "right_click"
)

// Gestures lists every gesture in display order.
var Gestures = []Gesture{WheelUp, WheelDown, CtrlWheelUp, CtrlWheelDown, ShiftWheelUp, ShiftWheelDown,
	AltWheelUp, AltWheelDown, MiddleClick, BackButton, ForwardButton, LeftDoubleClick, RightClick}

// Fit says how an image is scaled when it is shown.
type Fit string

const (
	FitShrink   Fit = "shrink"   // shrink large images to the window, keep small ones at 100%
	FitWindow   Fit = "window"   // scale every image to fill the window
	FitOriginal Fit = "original" // always 100%
)

// Sort orders the images of a folder or archive.
type Sort string

const (
	SortName     Sort = "name" // Explorer's natural order
	SortModified Sort = "modified"
	SortSize     Sort = "size"
)

// Config is the whole configuration file.
type Config struct {
	Fit              Fit                 `yaml:"fit"`
	Sort             Sort                `yaml:"sort"`
	SortDescending   bool                `yaml:"sort_descending"`
	Loop             bool                `yaml:"loop"`
	SlideshowSeconds float64             `yaml:"slideshow_seconds"`
	ZoomStep         float64             `yaml:"zoom_step"`  // multiplier per zoom step, such as 1.25
	Background       string              `yaml:"background"` // #rrggbb
	ShowInfo         bool                `yaml:"show_info"`
	ConfirmDelete    bool                `yaml:"confirm_delete"`
	Mouse            map[Gesture]Action  `yaml:"mouse"`
	Keys             map[Action][]string `yaml:"keys"`
}

// Default returns the configuration a new user starts with. Its mouse
// bindings follow Honeyview's: the wheel moves between images, Ctrl+wheel
// zooms, the middle button closes and the side buttons move back and forth.
func Default() Config {
	return Config{
		Fit: FitShrink, Sort: SortName, Loop: true, SlideshowSeconds: 3, ZoomStep: 1.25,
		Background: "#1e1e1e", ShowInfo: false, ConfirmDelete: true,
		Mouse: map[Gesture]Action{
			WheelUp: Previous, WheelDown: Next, CtrlWheelUp: ZoomIn, CtrlWheelDown: ZoomOut,
			ShiftWheelUp: RotateLeft, ShiftWheelDown: RotateRight, AltWheelUp: First, AltWheelDown: Last,
			MiddleClick: Close, BackButton: Previous, ForwardButton: Next,
			LeftDoubleClick: FullScreen, RightClick: Menu,
		},
		Keys: map[Action][]string{
			Next:         {"right", "pagedown", "space"},
			Previous:     {"left", "pageup", "backspace"},
			First:        {"home"},
			Last:         {"end"},
			ZoomIn:       {"=", "ctrl+="},
			ZoomOut:      {"-", "ctrl+-"},
			ZoomFit:      {"/", "ctrl+0"},
			ZoomOriginal: {"*", "ctrl+1"},
			RotateLeft:   {"ctrl+l"},
			RotateRight:  {"ctrl+r"},
			FlipH:        {"ctrl+shift+f"},
			FlipV:        {"ctrl+f"},
			FullScreen:   {"enter", "f11"},
			Escape:       {"escape"},
			Close:        {"ctrl+w"},
			Minimize:     {"ctrl+m"},
			Open:         {"ctrl+o"},
			Reveal:       {"ctrl+e"},
			Delete:       {"delete"},
			CopyPath:     {"ctrl+shift+c"},
			CopyImage:    {"ctrl+c"},
			Slideshow:    {"s"},
			Info:         {"i", "tab"},
			Menu:         {},
			Preferences:  {"p", "ctrl+p"},
		},
	}
}

// Path is where the configuration lives: %APPDATA%\FreeView\config.yaml.
func Path() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "FreeView", "config.yaml"), nil
}

// Load reads the file at path over the defaults: a missing file, key or
// gesture keeps its default, so new options reach existing users. A file
// that cannot be parsed is an error rather than a silent reset.
func Load(path string) (Config, error) {
	c := Default()
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return c, nil
	}
	if err != nil {
		return c, err
	}
	// yaml.v3 decodes into the existing maps key by key, so gestures and
	// actions the file leaves out keep their defaults.
	if err := yaml.Unmarshal(data, &c); err != nil {
		return Default(), err
	}
	return c.normalized(), nil
}

func mergeMap[K comparable, V any](base, over map[K]V) map[K]V {
	out := make(map[K]V, len(base)+len(over))
	for k, v := range base {
		out[k] = v
	}
	for k, v := range over {
		out[k] = v
	}
	return out
}

// normalized replaces values the viewer cannot use with defaults.
func (c Config) normalized() Config {
	d := Default()
	if !slices.Contains([]Fit{FitShrink, FitWindow, FitOriginal}, c.Fit) {
		c.Fit = d.Fit
	}
	if !slices.Contains([]Sort{SortName, SortModified, SortSize}, c.Sort) {
		c.Sort = d.Sort
	}
	if c.SlideshowSeconds < 0.5 {
		c.SlideshowSeconds = d.SlideshowSeconds
	}
	if c.ZoomStep <= 1 || c.ZoomStep > 4 {
		c.ZoomStep = d.ZoomStep
	}
	if _, ok := ParseColor(c.Background); !ok {
		c.Background = d.Background
	}
	for g, a := range c.Mouse {
		if !slices.Contains(Gestures, g) {
			delete(c.Mouse, g)
		} else if a != None && !slices.Contains(Actions, a) {
			c.Mouse[g] = None
		}
	}
	for a := range c.Keys {
		if !slices.Contains(Actions, a) {
			delete(c.Keys, a)
		}
	}
	return c
}

// Save writes c to path, through a temporary file so a crash never leaves
// a half-written configuration.
func Save(path string, c Config) error {
	data, err := yaml.Marshal(c)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Clone returns a copy of c whose maps can be changed without touching c.
func (c Config) Clone() Config {
	c.Mouse = mergeMap(c.Mouse, nil)
	keys := make(map[Action][]string, len(c.Keys))
	for a, k := range c.Keys {
		keys[a] = slices.Clone(k)
	}
	c.Keys = keys
	return c
}

// ParseColor reads a #rrggbb color.
func ParseColor(s string) (uint32, bool) {
	if len(s) != 7 || s[0] != '#' {
		return 0, false
	}
	v, err := strconv.ParseUint(s[1:], 16, 32)
	return uint32(v), err == nil
}
