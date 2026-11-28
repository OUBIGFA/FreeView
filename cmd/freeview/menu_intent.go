package main

import (
	"time"

	"github.com/nsgo-dev/nsgo/base"
	"github.com/nsgo-dev/nsgo/core"
)

// nsgo v0.3.4 provides menu semantics but switches siblings immediately on
// hover. This adapter adds pointer intent without copying its menu engine.
// Its public Render shapes are checked by the menu interaction tests.
type menuIntent struct {
	stop        func() bool
	point, apex core.Point
	panel       core.Bounds
	active      string
}

func (m *menuIntent) cancel() {
	if m.stop != nil {
		m.stop()
		m.stop = nil
	}
}

func (m *menuIntent) enter(cx *core.Scope, event core.MouseEvent, activate func(core.MouseEvent)) {
	m.cancel()
	m.point = event.PagePosition
	delay := 160 * time.Millisecond
	if m.active != "" {
		delay = 240 * time.Millisecond
	}
	// A bounded grace period permits a diagonal crossing but still lets a
	// deliberate pause on a neighboring item change the submenu.
	var apply func()
	remaining := 600 * time.Millisecond
	apply = func() {
		if m.active != "" && menuTriangle(m.point, m.apex, m.panel) && remaining > 0 {
			remaining -= 40 * time.Millisecond
			m.stop = cx.AfterFunc(40*time.Millisecond, apply)
			return
		}
		m.stop = nil
		m.apex = event.PagePosition
		activate(event)
	}
	m.stop = cx.AfterFunc(delay, apply)
}

func (m *menuIntent) row(cx *core.Scope, row core.View) core.View {
	enter, press, leave := row.OnMouseEnter, row.OnPress, row.OnMouseLeave
	row.OnMouseEnter = func(e core.MouseEvent) { m.enter(cx, e, enter) }
	row.OnMouseMove = func(e core.MouseEvent) {
		m.point = e.PagePosition
		if m.active == row.TestID {
			m.apex = e.PagePosition
		}
	}
	row.OnMouseLeave = func(e core.MouseEvent) {
		m.cancel()
		if leave != nil {
			leave(e)
		}
	}
	row.OnPress = func(e core.PressEvent) {
		m.cancel()
		if press != nil {
			press(e)
		}
	}
	return row
}

func (m *menuIntent) submenu(cx *core.Scope, submenu base.Submenu) core.Node {
	nodes := submenu.Render(cx).(core.Group)
	row := nodes[0].(core.View)
	id := row.TestID
	nodes[0] = m.row(cx, row)
	show := nodes[1].(core.Show)
	build := show.Then
	show.Then = func(cx *core.Scope) core.Node {
		parts := build(cx).(core.Group)
		panel := parts[0].(core.Overlay)
		position := panel.OnPosition
		panel.OnPosition = func(p core.OverlayPosition) {
			m.panel, m.active = p.Bounds, id
			if position != nil {
				position(p)
			}
		}
		panel.OnMouseEnter = func(core.MouseEvent) { m.cancel() }
		cx.Cleanup(func() {
			if m.active == id {
				m.active = ""
				m.cancel()
			}
		})
		parts[0] = panel
		return parts
	}
	nodes[1] = show
	return nodes
}

// menuTriangle works for a popup on either side of its parent. The two
// corners are the near edge of the actual, screen-clamped popup.
func menuTriangle(p, apex core.Point, panel core.Bounds) bool {
	if panel.Size.Width <= 0 || panel.Size.Height <= 0 {
		return false
	}
	x := panel.Origin.X
	if x < apex.X {
		x += panel.Size.Width
	}
	a := core.Point{X: x, Y: panel.Origin.Y}
	b := core.Point{X: x, Y: panel.Origin.Y + panel.Size.Height}
	cross := func(p, a, b core.Point) core.Points { return (p.X-b.X)*(a.Y-b.Y) - (a.X-b.X)*(p.Y-b.Y) }
	d1, d2, d3 := cross(p, apex, a), cross(p, a, b), cross(p, b, apex)
	return !((d1 < 0 || d2 < 0 || d3 < 0) && (d1 > 0 || d2 > 0 || d3 > 0))
}
