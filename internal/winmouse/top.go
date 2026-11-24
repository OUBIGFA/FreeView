//go:build windows

package winmouse

import "sync/atomic"

const (
	wmNCMouseMove  = 0x00A0
	wmNCMouseLeave = 0x02A2
	wmMouseMove    = 0x0200
	htCaption      = 2
	htTop          = 12
	htTopLeft      = 13
	htTopRight     = 14
)

// topTracker tracks whether the pointer rests on the top non-client area
// that drags or resizes the window from the top edge.
type topTracker struct {
	over bool
}

func (t *topTracker) nc(code uintptr) (over, changed bool) {
	over = code == htCaption || code == htTop || code == htTopLeft || code == htTopRight
	changed = over != t.over
	t.over = over
	return over, changed
}

func (t *topTracker) leave() bool {
	if !t.over {
		return false
	}
	t.over = false
	return true
}

var (
	topWatch atomic.Pointer[func(bool)]
	top      topTracker // window thread only
)

// WatchTop reports on the window's thread when the mouse enters or leaves
// the top of the window that moves it (hidden title bar's drag strip or
// top resize edge); fn must hand work to the engine via core.Scope.Post.
// It may be called before or after Attach.
func WatchTop(fn func(over bool)) {
	if fn == nil {
		topWatch.Store(nil)
		return
	}
	topWatch.Store(&fn)
}

func notifyTop(over bool) {
	if p := topWatch.Load(); p != nil && *p != nil {
		(*p)(over)
	}
}
