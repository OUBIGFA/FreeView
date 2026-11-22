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

// remember saves the frame of cx's window under key whenever the user
// moves or resizes it, half a second after the last change so a drag
// writes the file once. Maximized and in full screen nsgo reports the
// frame the window had before, which is the one to open with.
func (s *frameStore) remember(cx *core.Scope, key string, failed func(error)) {
	if s == nil || s.path == "" {
		return
	}
	frame := core.NewMemo(cx, func() core.WindowFrame { return desktop.Frame(cx) })
	var seq uint64
	cx.Effect(func() {
		f := frame.Get()
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
