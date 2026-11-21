package main

import (
	"strconv"
	"strings"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/desktop"
	"github.com/nsgo-dev/nsgo/ui"

	"github.com/OUBIGFA/FreeView/cmd/freeview/locales"
	"github.com/OUBIGFA/FreeView/internal/settings"
)

// setConfig applies new preferences at once and saves them in the
// background. A change of order lists the catalog again at the same image.
func (v *viewer) setConfig(next settings.Config) {
	prev := v.cfg.Get()
	v.cfg.Set(next)
	if path := v.cfgPath; path != "" {
		snapshot := next.Clone()
		go func() {
			if err := settings.Save(path, snapshot); err != nil {
				v.cx.Post(func() { v.flash(locales.PrefsSaveFailed(v.cx, err.Error())) })
			}
		}()
	}
	if next.ShowInfo != prev.ShowInfo {
		v.info.Set(next.ShowInfo)
	}
	if next.Fit != prev.Fit && v.fit.Get() != "" {
		v.fit.Set(next.Fit)
	}
	if (next.Sort != prev.Sort || next.SortDescending != prev.SortDescending) && v.cat != nil && v.index.Get() >= 0 {
		target := v.cat.Archive
		if target == "" {
			target = v.img.Get().item.Path
		}
		v.open(target)
	}
}

// update changes one part of the preferences.
func (v *viewer) update(fn func(c *settings.Config)) {
	c := v.cfg.Get().Clone()
	fn(&c)
	v.setConfig(c)
}

// openPreferences shows the preferences window, or brings it forward.
// It is opened through Post so the viewer's root owns it: opened straight
// from a key binding, it would belong to the bindings' scope, which goes
// as soon as a preference changes, closing the window.
func openPreferences(v *viewer) {
	v.cx.Post(func() {
		desktop.OpenWindow(v.cx, desktop.WindowOptions{Title: locales.PrefsTitle(v.cx), Width: 820, Height: 600, Key: "preferences"},
			func(cx *core.Scope) core.Node { return preferences{v: v}.Render(cx) })
	})
}

type preferences struct{ v *viewer }

func (p preferences) Render(cx *core.Scope) core.Node {
	v := p.v
	// resets rebuilds the pages after Restore defaults, so the shortcut
	// fields show the defaults rather than what was typed.
	resets := core.NewSignal(cx, 0)
	reset := ui.Button{Text: locales.PrefsReset(cx), Variant: ui.ButtonOutline, Size: ui.SizeSmall, TestID: "reset",
		OnPress: func() { v.setConfig(settings.Default()); resets.Update(func(n int) int { return n + 1 }) }}
	category := core.NewSignal(cx, "")
	return core.View{
		StyleFunc: func() core.Style {
			return core.Style{FlexGrow: 1, FlexShrink: 1, Background: cx.Palette().Background, Color: ui.ColorsOf(cx).Text()}
		},
		Children: []core.Node{core.Dynamic(func(cx *core.Scope) core.Node {
			resets.Get()
			return ui.Settings{
				Title: locales.PrefsTitle(cx), SearchLabel: locales.PrefsSearch(cx), SearchPlaceholder: locales.PrefsSearch(cx),
				ResultsTitle: locales.PrefsResults(cx), EmptyText: locales.PrefsEmpty(cx), Category: category,
				Categories: []ui.SettingsCategory{
					{ID: "view", Title: locales.PrefsView(cx), Groups: p.viewGroups(cx, reset)},
					{ID: "mouse", Title: locales.PrefsMouse(cx), Groups: []ui.SettingsGroup{{ID: "mouse", Title: locales.PrefsMouseGroup(cx), Items: p.mouseItems(cx)}}},
					{ID: "keyboard", Title: locales.PrefsKeyboard(cx), Groups: []ui.SettingsGroup{{ID: "keys", Title: locales.PrefsKeyboardGroup(cx), Items: p.keyItems(cx)}}},
				},
			}
		})},
	}
}

// choice is a select bound to one preference.
func choice[T comparable](options []ui.Option[T], get func(settings.Config) T, set func(*settings.Config, T), v *viewer, id string) core.Node {
	return ui.Select[T]{Options: options, TestID: id, Value: func() T { return get(v.cfg.Get()) },
		OnChange: func(x T) { v.update(func(c *settings.Config) { set(c, x) }) }}
}

func toggle(get func(settings.Config) bool, set func(*settings.Config, bool), v *viewer, id string) core.Node {
	return ui.Switch{TestID: id, Checked: func() bool { return get(v.cfg.Get()) },
		OnChange: func(on bool) { v.update(func(c *settings.Config) { set(c, on) }) }}
}

func (p preferences) viewGroups(cx *core.Scope, reset core.Node) []ui.SettingsGroup {
	v := p.v
	fits := []ui.Option[settings.Fit]{{Value: settings.FitShrink, Label: locales.PrefsFitShrink(cx)},
		{Value: settings.FitWindow, Label: locales.PrefsFitWindow(cx)}, {Value: settings.FitOriginal, Label: locales.PrefsFitOriginal(cx)}}
	var colors []ui.Option[string]
	for _, c := range []string{"#1e1e1e", "#000000", "#3c3c3c", "#808080", "#f0f0f0", "#ffffff"} {
		colors = append(colors, ui.Option[string]{Value: c, Label: c})
	}
	var steps []ui.Option[float64]
	for _, s := range []float64{1.1, 1.25, 1.5, 2} {
		steps = append(steps, ui.Option[float64]{Value: s, Label: "× " + strconv.FormatFloat(s, 'f', -1, 64)})
	}
	sorts := []ui.Option[settings.Sort]{{Value: settings.SortName, Label: locales.PrefsSortName(cx)},
		{Value: settings.SortModified, Label: locales.PrefsSortModified(cx)}, {Value: settings.SortSize, Label: locales.PrefsSortSize(cx)}}
	var intervals []ui.Option[float64]
	for _, s := range []float64{1, 2, 3, 5, 10, 20} {
		intervals = append(intervals, ui.Option[float64]{Value: s, Label: strconv.FormatFloat(s, 'f', -1, 64)})
	}
	return []ui.SettingsGroup{
		{ID: "display", Title: locales.PrefsViewGroup(cx), Actions: []core.Node{reset}, Items: []ui.SettingsItem{
			{ID: "fit", Title: locales.PrefsFit(cx), Control: choice(fits, func(c settings.Config) settings.Fit { return c.Fit },
				func(c *settings.Config, x settings.Fit) { c.Fit = x }, v, "fit")},
			{ID: "background", Title: locales.PrefsBackground(cx), Control: choice(colors, func(c settings.Config) string { return c.Background },
				func(c *settings.Config, x string) { c.Background = x }, v, "background")},
			{ID: "zoom-step", Title: locales.PrefsZoomStep(cx), Control: choice(steps, func(c settings.Config) float64 { return c.ZoomStep },
				func(c *settings.Config, x float64) { c.ZoomStep = x }, v, "zoom-step")},
			{ID: "show-info", Title: locales.PrefsShowInfo(cx), Control: toggle(func(c settings.Config) bool { return c.ShowInfo },
				func(c *settings.Config, on bool) { c.ShowInfo = on }, v, "show-info")},
		}},
		{ID: "browse", Title: locales.PrefsBrowseGroup(cx), Items: []ui.SettingsItem{
			{ID: "sort", Title: locales.PrefsSort(cx), Control: choice(sorts, func(c settings.Config) settings.Sort { return c.Sort },
				func(c *settings.Config, x settings.Sort) { c.Sort = x }, v, "sort")},
			{ID: "descending", Title: locales.PrefsSortDescending(cx), Control: toggle(func(c settings.Config) bool { return c.SortDescending },
				func(c *settings.Config, on bool) { c.SortDescending = on }, v, "descending")},
			{ID: "loop", Title: locales.PrefsLoop(cx), Control: toggle(func(c settings.Config) bool { return c.Loop },
				func(c *settings.Config, on bool) { c.Loop = on }, v, "loop")},
			{ID: "slideshow", Title: locales.PrefsSlideshow(cx), Control: choice(intervals, func(c settings.Config) float64 { return c.SlideshowSeconds },
				func(c *settings.Config, x float64) { c.SlideshowSeconds = x }, v, "slideshow")},
			{ID: "confirm-delete", Title: locales.PrefsConfirmDelete(cx), Control: toggle(func(c settings.Config) bool { return c.ConfirmDelete },
				func(c *settings.Config, on bool) { c.ConfirmDelete = on }, v, "confirm-delete")},
		}},
	}
}

func (p preferences) mouseItems(cx *core.Scope) []ui.SettingsItem {
	v := p.v
	options := []ui.Option[settings.Action]{{Value: settings.None, Label: actionLabel(cx, settings.None)}}
	for _, a := range settings.Actions {
		options = append(options, ui.Option[settings.Action]{Value: a, Label: actionLabel(cx, a)})
	}
	var items []ui.SettingsItem
	for _, g := range settings.Gestures {
		items = append(items, ui.SettingsItem{ID: string(g), Title: gestureLabel(cx, g),
			Control: ui.Select[settings.Action]{Options: options, TestID: "mouse-" + string(g), Searchable: true, Style: core.Style{Width: core.Pt(240)},
				Value: func() settings.Action {
					if a := v.cfg.Get().Mouse[g]; a != "" {
						return a
					}
					return settings.None
				},
				OnChange: func(a settings.Action) { v.update(func(c *settings.Config) { c.Mouse[g] = a }) }}})
	}
	return items
}

// parseKeys reads a comma-separated list of shortcuts.
func parseKeys(text string) []string {
	var keys []string
	for _, k := range strings.Split(text, ",") {
		if k = strings.ToLower(strings.TrimSpace(k)); k != "" {
			keys = append(keys, k)
		}
	}
	return keys
}

func (p preferences) keyItems(cx *core.Scope) []ui.SettingsItem {
	v := p.v
	items := []ui.SettingsItem{{ID: "keys-hint", Content: core.Text{Content: locales.PrefsKeysHint(cx),
		StyleFunc: func() core.Style {
			return ui.TypeCaption.Merge(core.Style{Color: ui.ColorsOf(cx).Muted(), Padding: core.Symmetric(4, 0)})
		}}}}
	for _, a := range settings.Actions {
		items = append(items, ui.SettingsItem{ID: "key-" + string(a), Title: actionLabel(cx, a), Keywords: v.cfg.Get().Keys[a],
			Control: shortcutField{v: v, action: a}})
	}
	return items
}

// shortcutField edits an action's shortcuts as text, and records the next
// key pressed while its recorder has the focus.
type shortcutField struct {
	v      *viewer
	action settings.Action
}

func (f shortcutField) Render(cx *core.Scope) core.Node {
	v, a := f.v, f.action
	draft := core.NewSignal(cx, strings.Join(v.cfg.Get().Keys[a], ", "))
	recording := core.NewSignal(cx, false)
	recorder := &core.Ref{}
	commit := func(text string) {
		draft.Set(text)
		v.update(func(c *settings.Config) { c.Keys[a] = parseKeys(text) })
	}
	return ui.HStack{Gap: 8, Children: []core.Node{
		ui.Input{TestID: "keys-" + string(a), Value: draft.Get, OnChange: commit, Label: actionLabel(cx, a),
			Style: core.Style{Width: core.Pt(220)}},
		core.View{
			Ref: recorder, Focusable: func() bool { return true }, TestID: "record-" + string(a),
			OnBlur: func() { recording.Set(false) },
			OnKeyDown: func(e core.KeyEvent) bool {
				if !recording.Get() {
					return false
				}
				s := e.Shortcut()
				if s == "" {
					return true // a modifier alone; wait for the key
				}
				keys := append(parseKeys(draft.Get()), s)
				commit(strings.Join(keys, ", "))
				recording.Set(false)
				return true
			},
			Children: []core.Node{ui.Button{Size: ui.SizeSmall, Variant: ui.ButtonOutline, OnPress: func() {
				recording.Set(true)
				recorder.Focus()
			}, Text: locales.PrefsRecord(cx)}},
		},
		core.Show{When: recording.Get, Then: func(cx *core.Scope) core.Node {
			return core.Text{Content: locales.PrefsRecording(cx), StyleFunc: func() core.Style {
				return ui.TypeCaption.Merge(core.Style{Color: ui.ColorsOf(cx).Muted()})
			}}
		}},
	}}
}
