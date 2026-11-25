package main

import (
	"sync"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/desktop"

	"github.com/OUBIGFA/FreeView/internal/settings"
)

// frames remembers the windows' frames across runs; nil, as in tests,
// remembers nothing.
var frames *frameStore

type frameStore struct {
	path    string
	mu      sync.Mutex // guards saved, which saves read off the engine
	saved   settings.Windows
	loadErr error
}

func loadFrames() *frameStore {
	s := &frameStore{saved: settings.Windows{}}
	path, err := settings.WindowsPath()
	if err != nil {
		s.loadErr = err
		return s
	}
	s.path = path
	s.saved, s.loadErr = settings.LoadWindows(path)
	return s
}

// frame returns the frame remembered for key, for nsgo to open the
// window with.
func (s *frameStore) frame(key string) (core.WindowFrame, bool) {
	if s == nil {
		return core.WindowFrame{}, false
	}
	s.mu.Lock()
	f, ok := s.saved[key]
	s.mu.Unlock()
	return core.WindowFrame{X: core.Points(f.X), Y: core.Points(f.Y), Move: true,
		Width: core.Points(f.Width), Height: core.Points(f.Height)}, ok
}

// maximized reports whether the window saved under key was maximized when
// last left, so it opens maximized again.
func (s *frameStore) maximized(key string) bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.saved[key].Maximized
}

// remember saves the frame of cx's window under key whenever the user
// moves or resizes it, half a second after the last change so a drag
// writes the file once. Maximized or in full screen the window keeps the
// frame saved before: nsgo on Windows reports the maximized or
// full-screen rectangle there (GetWindowRect), and opening with it put
// the window over the whole screen, as if maximized. Being maximized is
// saved as a flag beside that frame instead, so the window opens
// maximized and restores to where it was.
func (s *frameStore) remember(cx *core.Scope, key string, failed func(error)) {
	if s == nil || s.path == "" {
		return
	}
	frame := core.NewMemo(cx, func() core.WindowFrame { return desktop.Frame(cx) })
	whole := core.NewMemo(cx, func() bool { return desktop.Maximized(cx) || desktop.FullScreen(cx) })
	var seq uint64
	cx.Effect(func() {
		f := frame.Get()
		if whole.Get() {
			// The frame can change a moment before the state does, so a
			// save already waiting may hold the whole screen: drop it.
			seq++
			if desktop.Maximized(cx) && !desktop.FullScreen(cx) {
				s.mu.Lock()
				saved, ok := s.saved[key]
				s.mu.Unlock()
				if ok && !saved.Maximized {
					saved.Maximized = true
					go s.save(key, saved, func(err error) { cx.Post(func() { failed(err) }) })
				}
			}
			return
		}
		next := settings.Frame{X: float64(f.X), Y: float64(f.Y), Width: float64(f.Width), Height: float64(f.Height)}
		s.mu.Lock()
		same := s.saved[key] == next
		s.mu.Unlock()
		if same || next.Width <= 0 || next.Height <= 0 {
			return
		}
		seq++
		mine := seq
		time.AfterFunc(500*time.Millisecond, func() {
			cx.Post(func() {
				if mine == seq {
					go s.save(key, next, func(err error) { cx.Post(func() { failed(err) }) })
				}
			})
		})
	})
}

func (s *frameStore) save(key string, f settings.Frame, failed func(error)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.saved[key] = f
	if err := settings.SaveWindows(s.path, s.saved); err != nil {
		failed(err)
	}
}
