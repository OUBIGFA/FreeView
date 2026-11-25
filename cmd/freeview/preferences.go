package main

import (
	"strconv"
	"strings"

	"github.com/nsgo-dev/nsgo/base"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/desktop"
	"github.com/nsgo-dev/nsgo/ui"

	"github.com/OUBIGFA/FreeView/assets/icons/solar"
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
		opts := desktop.WindowOptions{Title: locales.PrefsTitle(v.cx), Width: 1040, Height: 700, Key: "preferences"}
		if f, ok := frames.frame("preferences"); ok {
			opts.Frame = f
		}
		desktop.OpenWindow(v.cx, opts, func(cx *core.Scope) core.Node {
			frames.remember(cx, "preferences", func(err error) { v.flash(locales.PrefsSaveFailed(v.cx, err.Error())) })
			return preferences{v: v}.Render(cx)
		})
	})
}

const (
	prefsSidebarWidth = 216 // as ui.Settings' own sidebar
	prefsPageMargin   = 40  // beside the page, on both sides
	prefsPageWidth    = 760 // the page's widest, so rows stay readable
)

type preferences struct{ v *viewer }

// Render draws the sidebar and the search field itself and leaves the
// rows of the shown page to ui.Settings: ui.Settings' own page fills its
// pane edge to edge, and has no way to keep side margins.
func (p preferences) Render(cx *core.Scope) core.Node {
	v := p.v
	colors := ui.ColorsOf(cx)
	// The preferences stay in front of a topmost viewer: they float with
	// it, and leave the top with it.
	onTop := core.NewMemo(cx, func() bool { return v.cfg.Get().AlwaysOnTop })
	cx.Effect(func() {
		desktop.SetWindowLevel(cx, pick(onTop.Get(), desktop.WindowLevelFloating, desktop.WindowLevelNormal))
	})
	// resets rebuilds the pages after Restore defaults, so the shortcut
	// fields show the defaults rather than what was typed.
	resets := core.NewSignal(cx, 0)
	reset := ui.Button{Text: locales.PrefsReset(cx), Variant: ui.ButtonOutline, Size: ui.SizeSmall, TestID: "reset",
		OnPress: func() { v.setConfig(settings.Default()); resets.Update(func(n int) int { return n + 1 }) }}
	category := core.NewSignal(cx, "view")
	query := core.NewSignal(cx, "")
	categories := func(cx *core.Scope) []ui.SettingsCategory {
		return []ui.SettingsCategory{
			{ID: "view", Title: locales.PrefsView(cx), Groups: p.viewGroups(cx, reset)},
			{ID: "mouse", Title: locales.PrefsMouse(cx), Groups: []ui.SettingsGroup{{ID: "mouse", Title: locales.PrefsMouseGroup(cx), Items: p.mouseItems(cx)}}},
			{ID: "keyboard", Title: locales.PrefsKeyboard(cx), Groups: []ui.SettingsGroup{{ID: "keys", Title: locales.PrefsKeyboardGroup(cx), Items: p.keyItems(cx)}}},
			{ID: "about", Title: locales.PrefsAbout(cx), Groups: aboutGroups(cx)},
		}
	}
	var nav []ui.SidebarItem
	for _, c := range categories(cx) {
		nav = append(nav, ui.SidebarItem{ID: c.ID, Text: c.Title, TestID: "settings-nav-" + c.ID})
	}
	title := ui.Text(locales.PrefsTitle(cx), 13, ui.Bold, colors.Text)
	title.Role = core.RoleHeader
	sidebar := core.View{TestID: "settings-sidebar", Style: core.Style{Width: core.Pt(prefsSidebarWidth), FlexShrink: 0},
		StyleFunc: func() core.Style { return core.Style{Background: ui.Alpha(colors.Text(), 0.035)} },
		Children: []core.Node{
			core.View{Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, MinHeight: core.Pt(56), Padding: core.Symmetric(12, 16)},
				Children: []core.Node{title}},
			core.View{Style: core.Style{Padding: core.Edges{Right: 12, Bottom: 16, Left: 12}}, Children: []core.Node{searchField(cx, query)}},
			core.View{Style: ui.FillRest, Children: []core.Node{ui.Sidebar{
				Sections: []ui.SidebarSection{{Items: nav}}, Selected: category.Get,
				OnSelect: func(id string) { category.Set(id); query.Set("") }}}},
		}}
	// The page: the chosen category, or every row matching the search.
	page := core.Dynamic(func(cx *core.Scope) core.Node {
		resets.Get()
		all := categories(cx)
		shown, heading := all[:1], all[0].Title
		for i, c := range all {
			if c.ID == category.Get() {
				shown, heading = all[i:i+1], c.Title
			}
		}
		if q := strings.TrimSpace(query.Get()); q != "" {
			shown, heading = matchingSettings(all, q), locales.PrefsResults(cx)
		}
		return ui.Settings{Title: heading, EmptyText: locales.PrefsEmpty(cx), Categories: shown, HideSidebar: true, HideSearch: true}
	})
	return core.View{
		StyleFunc: func() core.Style {
			return core.Style{FlexGrow: 1, FlexShrink: 1, Direction: core.Row, Background: cx.Palette().Background, Color: colors.Text()}
		},
		Children: []core.Node{
			sidebar,
			core.View{Style: core.Style{Width: core.Pt(1), FlexShrink: 0}, StyleFunc: ui.Filled(colors.Border)},
			core.View{Style: ui.FillRest.Merge(core.Style{AlignItems: core.AlignCenter, Padding: core.Symmetric(0, prefsPageMargin)}),
				Children: []core.Node{core.View{Style: ui.FillRest.Merge(core.Style{Width: core.Pct(100), MaxWidth: core.Pt(prefsPageWidth)}),
					Children: []core.Node{page}}}},
		},
	}
}

// searchField is the search through every row, drawn as ui.Settings
// draws its own.
func searchField(cx *core.Scope, query *core.Signal[string]) core.Node {
	colors := ui.ColorsOf(cx)
	return core.View{StyleFunc: func() core.Style {
		return ui.FinOf(cx).Field().Over(core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 6, Height: core.Pt(32),
			Padding: core.Symmetric(0, 10), CornerRadius: 8, BorderCurve: core.BorderCurveContinuous,
			BorderWidth: 1, BorderColor: colors.FieldBorder(), Background: colors.Field()})
	}, Children: []core.Node{
		ui.Icon(solar.Magnifier, 14, colors.Faint),
		base.Input{Value: query.Get, OnChange: query.Set, Placeholder: locales.PrefsSearch(cx), Label: locales.PrefsSearch(cx), TestID: "settings-search",
			Style: core.Style{FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(0), FontSize: 13},
			StyleFunc: func() core.Style {
				return core.Style{Color: colors.Text(), PlaceholderColor: colors.Faint(), SelectionColor: colors.Accent()}
			}},
	}}
}

// matchingSettings keeps the rows of categories that match query, in
// their categories and groups.
func matchingSettings(categories []ui.SettingsCategory, query string) []ui.SettingsCategory {
	var out []ui.SettingsCategory
	for _, r := range ui.SearchSettings(categories, query) {
		c, g := categories[r.Category], categories[r.Category].Groups[r.Group]
		if n := len(out); n == 0 || out[n-1].ID != c.ID {
			c.Groups = nil
			out = append(out, c)
		}
		last := &out[len(out)-1]
		if n := len(last.Groups); n == 0 || last.Groups[n-1].ID != g.ID {
			g.Items = nil
			last.Groups = append(last.Groups, g)
		}
		group := &last.Groups[len(last.Groups)-1]
		group.Items = append(group.Items, categories[r.Category].Groups[r.Group].Items[r.Item])
	}
	return out
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
	var speeds []ui.Option[float64]
	for _, s := range settings.AnimationSpeeds {
		speeds = append(speeds, ui.Option[float64]{Value: s, Label: "× " + strconv.FormatFloat(s, 'f', -1, 64)})
	}
	places := []ui.Option[settings.TitlePlace]{
		{Value: settings.TitleLeft, Label: locales.PrefsTitlePlaceLeft(cx)},
		{Value: settings.TitleCenter, Label: locales.PrefsTitlePlaceCenter(cx)},
		{Value: settings.TitleRight, Label: locales.PrefsTitlePlaceRight(cx)},
	}
	return []ui.SettingsGroup{
		{ID: "display", Title: locales.PrefsViewGroup(cx), Actions: []core.Node{reset}, Items: []ui.SettingsItem{
			{ID: "fit", Title: locales.PrefsFit(cx), Description: locales.PrefsFitHint(cx), Control: choice(fits, func(c settings.Config) settings.Fit { return c.Fit },
				func(c *settings.Config, x settings.Fit) { c.Fit = x }, v, "fit")},
			{ID: "zoom-step", Title: locales.PrefsZoomStep(cx), Control: choice(steps, func(c settings.Config) float64 { return c.ZoomStep },
				func(c *settings.Config, x float64) { c.ZoomStep = x }, v, "zoom-step")},
			{ID: "show-info", Title: locales.PrefsShowInfo(cx), Control: toggle(func(c settings.Config) bool { return c.ShowInfo },
				func(c *settings.Config, on bool) { c.ShowInfo = on }, v, "show-info")},
			{ID: "always-on-top", Title: locales.PrefsAlwaysOnTop(cx), Control: toggle(func(c settings.Config) bool { return c.AlwaysOnTop },
				func(c *settings.Config, on bool) { c.AlwaysOnTop = on }, v, "always-on-top")},
			{ID: "title-bar", Title: locales.PrefsShowTitleBar(cx), Control: toggle(func(c settings.Config) bool { return c.ShowTitleBar },
				func(c *settings.Config, on bool) { c.ShowTitleBar = on }, v, "title-bar")},
			{ID: "hover-title", Title: locales.PrefsHoverTitle(cx), Description: locales.PrefsHoverTitleHint(cx),
				Control: toggle(func(c settings.Config) bool { return c.HoverTitle },
					func(c *settings.Config, on bool) { c.HoverTitle = on }, v, "hover-title")},
			{ID: "hover-title-place", Title: locales.PrefsHoverTitlePlace(cx),
				Control: choice(places, func(c settings.Config) settings.TitlePlace { return c.HoverTitlePlace },
					func(c *settings.Config, x settings.TitlePlace) { c.HoverTitlePlace = x }, v, "hover-title-place")},
			{ID: "remember-window", Title: locales.PrefsRememberWindow(cx), Control: toggle(func(c settings.Config) bool { return c.RememberWindow },
				func(c *settings.Config, on bool) { c.RememberWindow = on }, v, "remember-window")},
			{ID: "open-maximized", Title: locales.PrefsOpenMaximized(cx), Control: toggle(func(c settings.Config) bool { return c.OpenMaximized },
				func(c *settings.Config, on bool) { c.OpenMaximized = on }, v, "open-maximized")},
			{ID: "multiple-windows", Title: locales.PrefsMultipleWindows(cx), Description: locales.PrefsMultipleWindowsHint(cx),
				Control: toggle(func(c settings.Config) bool { return c.MultipleWindows },
					func(c *settings.Config, on bool) { c.MultipleWindows = on }, v, "multiple-windows")},
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
			{ID: "edge-clicks", Title: locales.PrefsEdgeClicks(cx), Description: locales.PrefsEdgeClicksHint(cx),
				Control: toggle(func(c settings.Config) bool { return c.EdgeClicks },
					func(c *settings.Config, on bool) { c.EdgeClicks = on }, v, "edge-clicks")},
		}},
		{ID: "enhance", Title: locales.PrefsEnhanceGroup(cx), Items: []ui.SettingsItem{
			{ID: "navigator", Title: locales.PrefsNavigator(cx), Description: locales.PrefsNavigatorHint(cx),
				Control: toggle(func(c settings.Config) bool { return c.Navigator },
					func(c *settings.Config, on bool) { c.Navigator = on }, v, "navigator")},
			{ID: "filmstrip", Title: locales.PrefsFilmstrip(cx), Description: locales.PrefsFilmstripHint(cx),
				Control: toggle(func(c settings.Config) bool { return c.Filmstrip },
					func(c *settings.Config, on bool) { c.Filmstrip = on }, v, "filmstrip")},
			{ID: "smooth-zoom", Title: locales.PrefsSmoothZoom(cx), Description: locales.PrefsSmoothZoomHint(cx),
				Control: toggle(func(c settings.Config) bool { return c.SmoothZoom },
					func(c *settings.Config, on bool) { c.SmoothZoom = on }, v, "smooth-zoom")},
			{ID: "color-management", Title: locales.PrefsColorManagement(cx), Description: locales.PrefsColorManagementHint(cx),
				Control: toggle(func(c settings.Config) bool { return c.ColorManagement },
					func(c *settings.Config, on bool) { c.ColorManagement = on }, v, "color-management")},
			{ID: "animation-speed", Title: locales.PrefsAnimationSpeed(cx), Control: choice(speeds,
				func(c settings.Config) float64 { return c.AnimationSpeed }, func(c *settings.Config, x float64) { c.AnimationSpeed = x }, v, "animation-speed")},
		}},
		{ID: "files", Title: locales.PrefsFilesGroup(cx), Items: []ui.SettingsItem{
			{ID: "external-editor", Title: locales.PrefsExternalEditor(cx), Description: locales.PrefsExternalEditorHint(cx),
				Control: pathSetting(cx, v, "external-editor", func(c settings.Config) string { return c.ExternalEditor },
					func(c *settings.Config, p string) { c.ExternalEditor = p }, v.pickEditor)},
			{ID: "target-folder", Title: locales.PrefsTargetFolder(cx), Description: locales.PrefsTargetFolderHint(cx),
				Control: pathSetting(cx, v, "target-folder", func(c settings.Config) string { return c.TargetFolder },
					func(c *settings.Config, p string) { c.TargetFolder = p }, func(done func(string)) {
						v.cx.PickDirectories(core.PickDirectoriesOptions{Title: locales.PickFolder(v.cx)}, func(dirs []core.Directory, err error) {
							if err == nil && len(dirs) > 0 && dirs[0].Path != "" {
								done(dirs[0].Path)
							}
						})
					})},
		}},
	}
}

// pathSetting shows a program or folder preference with buttons to
// choose it and to clear it.
func pathSetting(cx *core.Scope, v *viewer, id string, get func(settings.Config) string, set func(*settings.Config, string), choose func(done func(string))) core.Node {
	return ui.HStack{Gap: 8, Children: []core.Node{
		core.Text{TestID: id + "-path", ContentFunc: func() string {
			if p := get(v.cfg.Get()); p != "" {
				return p
			}
			return locales.PrefsNotSet(cx)
		}, StyleFunc: func() core.Style {
			return ui.TypeCaption.Merge(core.Style{Color: ui.ColorsOf(cx).Muted(), MaxWidth: core.Pt(260)})
		}},
		ui.Button{TestID: id + "-choose", Size: ui.SizeSmall, Variant: ui.ButtonOutline, Text: locales.PrefsChoose(cx),
			OnPress: func() { choose(func(p string) { v.update(func(c *settings.Config) { set(c, p) }) }) }},
		ui.Button{TestID: id + "-clear", Size: ui.SizeSmall, Variant: ui.ButtonOutline, Text: locales.PrefsClear(cx),
			OnPress: func() { v.update(func(c *settings.Config) { set(c, "") }) }},
	}}
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
