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
		// Chain writes in event order. A mutex inside the goroutine alone
		// would serialize them in scheduler order and could save an old value last.
		previous := v.configSaved
		done := make(chan struct{})
		v.configSaved = done
		go func() {
			defer close(done)
			if previous != nil {
				<-previous
			}
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
	if next.SmoothZoom != prev.SmoothZoom {
		v.extras.smoothScale = 0
		v.extras.smooth.Set(core.ImageSource{})
	}
	if next.ColorManagement != prev.ColorManagement && !v.extras.animated {
		// Discard pixels converted under the previous color policy.
		s := v.img.Get()
		v.slots[v.front.Get()].Set(s.src)
		v.commitExtras(s)
		v.scheduleSmooth()
	}
	if next.AnimationSpeed != prev.AnimationSpeed && v.extras.animated {
		v.withAnimation(v.tick)
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

// Post makes the viewer's root own this window, rather than a key binding
// or menu item's short-lived scope. Changing a setting must not close it.
func openPreferences(v *viewer) {
	v.cx.Post(func() {
		opts := desktop.WindowOptions{Title: locales.PrefsTitle(v.cx), Width: 1040, Height: 700,
			Constraints: core.WindowConstraints{MinWidth: 820, MinHeight: 560}, Key: "preferences"}
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
	prefsSidebarWidth = 216
	prefsPageMargin   = 32
	prefsPageWidth    = 760
)

type preferences struct{ v *viewer }

// Render uses one scroll view for the page, spanning the entire right
// pane. Rows use nsgo's form controls; ui.Settings would add a second
// scroll view and its own page padding here.
func (p preferences) Render(cx *core.Scope) core.Node {
	v, colors := p.v, ui.ColorsOf(cx)
	onTop := core.NewMemo(cx, func() bool { return v.cfg.Get().AlwaysOnTop })
	cx.Effect(func() {
		desktop.SetWindowLevel(cx, pick(onTop.Get(), desktop.WindowLevelFloating, desktop.WindowLevelNormal))
	})
	category, query := core.NewSignal(cx, "general"), core.NewSignal(cx, "")
	resets := core.NewSignal(cx, 0)
	reset := ui.Button{Text: locales.PrefsReset(cx), Variant: ui.ButtonOutline, TestID: "reset",
		Style: core.Style{Width: core.Pct(100), Height: core.Pt(34)}, OnPress: func() {
			cx.ShowAlert(core.AlertConfig{Title: locales.PrefsResetTitle(cx), Message: locales.PrefsResetHint(cx), Cancelable: true,
				Actions: []core.AlertAction{{Label: locales.PrefsReset(cx), Role: core.AlertDestructive}, {Label: locales.Cancel(cx), Role: core.AlertCancel}}}, func(i int) {
				if i == 0 {
					v.setConfig(settings.Default())
					resets.Update(func(n int) int { return n + 1 })
				}
			})
		}}
	// Metadata is stable while a setting changes. Build fresh row models
	// only for navigation, search or reset, so inputs keep focus and scroll.
	catalog := core.Untracked(cx, func() []ui.SettingsCategory { return p.categories(cx) })
	var nav []ui.SidebarItem
	for _, c := range catalog {
		nav = append(nav, ui.SidebarItem{ID: c.ID, Text: c.Title, Icon: c.Icon, TestID: "settings-nav-" + c.ID})
	}
	current := func() ui.SettingsCategory {
		for _, c := range catalog {
			if c.ID == category.Get() {
				return c
			}
		}
		return catalog[0]
	}
	headingText := ui.Textf(func() string {
		if strings.TrimSpace(query.Get()) != "" {
			return locales.PrefsResults(cx)
		}
		return current().Title
	}, 22, ui.Medium, colors.Text)
	headingText.Role, headingText.MaxLines = core.RoleHeader, 1
	heading := prefsColumn(core.View{Style: core.Style{Gap: 6, Padding: core.Edges{Top: 26, Bottom: 22}}, Children: []core.Node{
		headingText,
		core.Text{ContentFunc: func() string {
			if strings.TrimSpace(query.Get()) != "" {
				return locales.PrefsSearchHint(cx)
			}
			return current().Description
		}, StyleFunc: func() core.Style { return core.Style{FontSize: 13, LineHeight: 20, Color: colors.Muted()} }},
	}})
	heading.Style.FlexShrink = 0
	page := core.Dynamic(func(cx *core.Scope) core.Node {
		resets.Get()
		id, q := category.Get(), strings.TrimSpace(query.Get())
		all := core.Untracked(cx, func() []ui.SettingsCategory { return p.categories(cx) })
		shown := all[:1]
		for i, c := range all {
			if c.ID == id {
				shown = all[i : i+1]
				break
			}
		}
		if q != "" {
			shown = matchingSettings(all, q)
		}
		return prefsRows(cx, shown, q != "")
	})
	scroll := &core.Ref{}
	cx.Effect(func() {
		category.Get()
		query.Get()
		scroll.ScrollTo(core.Point{}, false)
	})
	return core.View{StyleFunc: func() core.Style {
		return core.Style{FlexGrow: 1, FlexShrink: 1, Direction: core.Row, Background: cx.Palette().Background, Color: colors.Text()}
	}, Children: []core.Node{
		core.View{TestID: "settings-sidebar", Style: core.Style{Width: core.Pt(prefsSidebarWidth), FlexShrink: 0},
			StyleFunc: func() core.Style { return core.Style{Background: ui.Alpha(colors.Text(), 0.035)} }, Children: []core.Node{
				core.View{Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 10, Height: core.Pt(56), Padding: core.Symmetric(12, 18)},
					Children: []core.Node{ui.Icon(solar.Gallery, 20, colors.Muted), ui.Text("FreeView", 16, ui.Medium, colors.Text)}},
				core.View{Style: core.Style{Padding: core.Edges{Right: 12, Bottom: 16, Left: 12}}, Children: []core.Node{searchField(cx, query)}},
				core.View{Style: ui.FillRest, Children: []core.Node{ui.Sidebar{Sections: []ui.SidebarSection{{Items: nav}}, Selected: category.Get,
					OnSelect: func(id string) { category.Set(id); query.Set("") }}}},
				core.View{Style: core.Style{Padding: core.Edges{Top: 12, Right: 12, Bottom: 16, Left: 12}}, Children: []core.Node{reset}},
			}},
		core.View{Style: core.Style{Width: core.Pt(1), FlexShrink: 0}, StyleFunc: ui.Filled(colors.Border)},
		core.View{Style: ui.FillRest, Children: []core.Node{heading,
			core.Scroll{Ref: scroll, TestID: "settings-page", Style: ui.FillRest, Children: []core.Node{prefsColumn(page)}},
		}},
	}}
}

func prefsColumn(child core.Node) core.View {
	return core.View{Style: core.Style{AlignItems: core.AlignCenter, Padding: core.Symmetric(0, prefsPageMargin)},
		Children: []core.Node{core.View{Style: core.Style{Width: core.Pct(100), MaxWidth: core.Pt(prefsPageWidth)}, Children: []core.Node{child}}}}
}

// prefsRows keeps related controls on one native form surface, with
// readable row heights. Wide controls are below their label and detail.
func prefsRows(cx *core.Scope, categories []ui.SettingsCategory, searching bool) core.Node {
	colors := ui.ColorsOf(cx)
	var sections []core.Node
	for _, c := range categories {
		if searching {
			title := ui.Text(c.Title, 16, ui.Medium, colors.Text)
			title.Role = core.RoleHeader
			sections = append(sections, title)
		}
		for _, g := range c.Groups {
			var rows []core.Node
			for _, item := range g.Items {
				id := "settings-" + c.ID + "-" + item.ID
				switch {
				case item.Content != nil:
					rows = append(rows, core.View{TestID: id, Style: core.Style{Padding: core.All(14)}, Children: []core.Node{item.Content}})
				case item.Stacked:
					rows = append(rows, core.View{TestID: id, Style: core.Style{Padding: core.All(14), Gap: 8}, Children: []core.Node{
						ui.Text(item.Title, 13, ui.Medium, colors.Text),
						core.Text{Content: item.Description, StyleFunc: func() core.Style { return core.Style{FontSize: 12.5, LineHeight: 19, Color: colors.Muted()} }},
						item.Control,
					}})
				default:
					row := ui.FormRow{TestID: id, Title: item.Title, Detail: item.Description, Leading: item.Leading,
						LeadingWidth: logoPoints, MinHeight: 56, DetailStyle: core.Style{FontSize: 12.5, LineHeight: 19}, Control: item.Control}
					if item.Leading != nil {
						row.MinHeight, row.TitleStyle = 88, core.Style{FontSize: 16, FontWeight: ui.Medium}
					}
					rows = append(rows, row)
				}
			}
			sections = append(sections, ui.FormSection{Title: g.Title, Children: rows})
		}
	}
	if len(sections) == 0 {
		sections = append(sections, ui.Text(locales.PrefsEmpty(cx), 14, ui.Regular, colors.Muted))
	}
	return core.View{Style: core.Style{Gap: 22, Padding: core.Edges{Bottom: 28}}, Children: sections}
}

func searchField(cx *core.Scope, query *core.Signal[string]) core.Node {
	colors := ui.ColorsOf(cx)
	return core.View{StyleFunc: func() core.Style {
		return ui.FinOf(cx).Field().Over(core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 8, Height: core.Pt(32),
			Padding: core.Symmetric(0, 10), CornerRadius: 8, BorderWidth: 1, BorderColor: colors.FieldBorder(), Background: colors.Field()})
	}, Children: []core.Node{
		ui.Icon(solar.Magnifier, 14, colors.Muted),
		base.Input{Value: query.Get, OnChange: query.Set, Placeholder: locales.PrefsSearch(cx), Label: locales.PrefsSearch(cx), TestID: "settings-search",
			Style: core.Style{FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(0), FontSize: 13},
			StyleFunc: func() core.Style {
				return core.Style{Color: colors.Text(), PlaceholderColor: colors.Muted(), SelectionColor: cx.Palette().Selection}
			}},
	}}
}

// matchingSettings preserves category and group context for global search.
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

func choice[T comparable](options []ui.Option[T], get func(settings.Config) T, set func(*settings.Config, T), v *viewer, id string) core.Node {
	return ui.Select[T]{Options: options, TestID: id, Style: core.Style{Width: core.Pt(200)}, Value: func() T { return get(v.cfg.Get()) },
		OnChange: func(x T) { v.update(func(c *settings.Config) { set(c, x) }) }}
}

func toggle(get func(settings.Config) bool, set func(*settings.Config, bool), v *viewer, id string) core.Node {
	return ui.Switch{TestID: id, Checked: func() bool { return get(v.cfg.Get()) },
		OnChange: func(on bool) { v.update(func(c *settings.Config) { set(c, on) }) }}
}

// categories is the single model for sidebar, search and page content.
// Config field bindings stay the same when controls move between pages.
func (p preferences) categories(cx *core.Scope) []ui.SettingsCategory {
	v := p.v
	fits := []ui.Option[settings.Fit]{{Value: settings.FitShrink, Label: locales.PrefsFitShrink(cx)},
		{Value: settings.FitWindow, Label: locales.PrefsFitWindow(cx)}, {Value: settings.FitOriginal, Label: locales.PrefsFitOriginal(cx)}}
	var steps, intervals, speeds []ui.Option[float64]
	for _, s := range []float64{1.1, 1.25, 1.5, 2} {
		steps = append(steps, ui.Option[float64]{Value: s, Label: "× " + strconv.FormatFloat(s, 'f', -1, 64)})
	}
	for _, s := range []float64{1, 2, 3, 5, 10, 20} {
		intervals = append(intervals, ui.Option[float64]{Value: s, Label: strconv.FormatFloat(s, 'f', -1, 64)})
	}
	for _, s := range settings.AnimationSpeeds {
		speeds = append(speeds, ui.Option[float64]{Value: s, Label: "× " + strconv.FormatFloat(s, 'f', -1, 64)})
	}
	places := []ui.Option[settings.TitlePlace]{{Value: settings.TitleLeft, Label: locales.PrefsTitlePlaceLeft(cx)},
		{Value: settings.TitleCenter, Label: locales.PrefsTitlePlaceCenter(cx)}, {Value: settings.TitleRight, Label: locales.PrefsTitlePlaceRight(cx)}}
	sorts := []ui.Option[settings.Sort]{{Value: settings.SortName, Label: locales.PrefsSortName(cx)},
		{Value: settings.SortModified, Label: locales.PrefsSortModified(cx)}, {Value: settings.SortSize, Label: locales.PrefsSortSize(cx)}}
	return []ui.SettingsCategory{
		{ID: "general", Title: locales.PrefsGeneral(cx), Description: locales.PrefsGeneralHint(cx), Icon: solar.Settings, Groups: []ui.SettingsGroup{
			{ID: "window", Title: locales.PrefsGeneralGroup(cx), Items: []ui.SettingsItem{
				{ID: "always-on-top", Title: locales.PrefsAlwaysOnTop(cx), Control: toggle(func(c settings.Config) bool { return c.AlwaysOnTop }, func(c *settings.Config, on bool) { c.AlwaysOnTop = on }, v, "always-on-top")},
				{ID: "title-bar", Title: locales.PrefsShowTitleBar(cx), Control: toggle(func(c settings.Config) bool { return c.ShowTitleBar }, func(c *settings.Config, on bool) { c.ShowTitleBar = on }, v, "title-bar")},
				{ID: "hover-title", Title: locales.PrefsHoverTitle(cx), Description: locales.PrefsHoverTitleHint(cx), Control: toggle(func(c settings.Config) bool { return c.HoverTitle }, func(c *settings.Config, on bool) { c.HoverTitle = on }, v, "hover-title")},
				{ID: "hover-title-place", Title: locales.PrefsHoverTitlePlace(cx), Control: choice(places, func(c settings.Config) settings.TitlePlace { return c.HoverTitlePlace }, func(c *settings.Config, x settings.TitlePlace) { c.HoverTitlePlace = x }, v, "hover-title-place")},
			}},
			{ID: "startup", Title: locales.PrefsStartup(cx), Items: []ui.SettingsItem{
				{ID: "remember-window", Title: locales.PrefsRememberWindow(cx), Control: toggle(func(c settings.Config) bool { return c.RememberWindow }, func(c *settings.Config, on bool) { c.RememberWindow = on }, v, "remember-window")},
				{ID: "open-maximized", Title: locales.PrefsOpenMaximized(cx), Control: toggle(func(c settings.Config) bool { return c.OpenMaximized }, func(c *settings.Config, on bool) { c.OpenMaximized = on }, v, "open-maximized")},
				{ID: "multiple-windows", Title: locales.PrefsMultipleWindows(cx), Description: locales.PrefsMultipleWindowsHint(cx), Control: toggle(func(c settings.Config) bool { return c.MultipleWindows }, func(c *settings.Config, on bool) { c.MultipleWindows = on }, v, "multiple-windows")},
			}},
		}},
		{ID: "display", Title: locales.PrefsDisplay(cx), Description: locales.PrefsDisplayHint(cx), Icon: solar.Gallery, Groups: []ui.SettingsGroup{
			{ID: "image", Title: locales.PrefsDisplayGroup(cx), Items: []ui.SettingsItem{
				{ID: "fit", Title: locales.PrefsFit(cx), Description: locales.PrefsFitHint(cx), Control: choice(fits, func(c settings.Config) settings.Fit { return c.Fit }, func(c *settings.Config, x settings.Fit) { c.Fit = x }, v, "fit")},
				{ID: "zoom-step", Title: locales.PrefsZoomStep(cx), Control: choice(steps, func(c settings.Config) float64 { return c.ZoomStep }, func(c *settings.Config, x float64) { c.ZoomStep = x }, v, "zoom-step")},
				{ID: "smooth-zoom", Title: locales.PrefsSmoothZoom(cx), Description: locales.PrefsSmoothZoomHint(cx), Control: toggle(func(c settings.Config) bool { return c.SmoothZoom }, func(c *settings.Config, on bool) { c.SmoothZoom = on }, v, "smooth-zoom")},
				{ID: "color-management", Title: locales.PrefsColorManagement(cx), Description: locales.PrefsColorManagementHint(cx), Control: toggle(func(c settings.Config) bool { return c.ColorManagement }, func(c *settings.Config, on bool) { c.ColorManagement = on }, v, "color-management")},
			}},
			{ID: "overlays", Title: locales.PrefsOverlays(cx), Items: []ui.SettingsItem{
				{ID: "show-info", Title: locales.PrefsShowInfo(cx), Control: toggle(func(c settings.Config) bool { return c.ShowInfo }, func(c *settings.Config, on bool) { c.ShowInfo = on }, v, "show-info")},
				{ID: "navigator", Title: locales.PrefsNavigator(cx), Description: locales.PrefsNavigatorHint(cx), Control: toggle(func(c settings.Config) bool { return c.Navigator }, func(c *settings.Config, on bool) { c.Navigator = on }, v, "navigator")},
			}},
		}},
		{ID: "browse", Title: locales.PrefsBrowse(cx), Description: locales.PrefsBrowseHint(cx), Icon: solar.PostsCarouselHorizontal, Groups: []ui.SettingsGroup{
			{ID: "navigation", Title: locales.PrefsBrowseGroup(cx), Items: []ui.SettingsItem{
				{ID: "sort", Title: locales.PrefsSort(cx), Control: choice(sorts, func(c settings.Config) settings.Sort { return c.Sort }, func(c *settings.Config, x settings.Sort) { c.Sort = x }, v, "sort")},
				{ID: "descending", Title: locales.PrefsSortDescending(cx), Control: toggle(func(c settings.Config) bool { return c.SortDescending }, func(c *settings.Config, on bool) { c.SortDescending = on }, v, "descending")},
				{ID: "loop", Title: locales.PrefsLoop(cx), Control: toggle(func(c settings.Config) bool { return c.Loop }, func(c *settings.Config, on bool) { c.Loop = on }, v, "loop")},
				{ID: "filmstrip", Title: locales.PrefsFilmstrip(cx), Description: locales.PrefsFilmstripHint(cx), Control: toggle(func(c settings.Config) bool { return c.Filmstrip }, func(c *settings.Config, on bool) { c.Filmstrip = on }, v, "filmstrip")},
				{ID: "edge-clicks", Title: locales.PrefsEdgeClicks(cx), Description: locales.PrefsEdgeClicksHint(cx), Control: toggle(func(c settings.Config) bool { return c.EdgeClicks }, func(c *settings.Config, on bool) { c.EdgeClicks = on }, v, "edge-clicks")},
			}},
			{ID: "playback", Title: locales.PrefsKeyboardAnimation(cx), Items: []ui.SettingsItem{
				{ID: "slideshow", Title: locales.PrefsSlideshow(cx), Control: choice(intervals, func(c settings.Config) float64 { return c.SlideshowSeconds }, func(c *settings.Config, x float64) { c.SlideshowSeconds = x }, v, "slideshow")},
				{ID: "animation-speed", Title: locales.PrefsAnimationSpeed(cx), Control: choice(speeds, func(c settings.Config) float64 { return c.AnimationSpeed }, func(c *settings.Config, x float64) { c.AnimationSpeed = x }, v, "animation-speed")},
			}},
		}},
		{ID: "files", Title: locales.PrefsFiles(cx), Description: locales.PrefsFilesHint(cx), Icon: solar.FolderWithFiles, Groups: []ui.SettingsGroup{
			{ID: "delete", Title: locales.PrefsFilesGroup(cx), Items: []ui.SettingsItem{
				{ID: "confirm-delete", Title: locales.PrefsConfirmDelete(cx), Control: toggle(func(c settings.Config) bool { return c.ConfirmDelete }, func(c *settings.Config, on bool) { c.ConfirmDelete = on }, v, "confirm-delete")},
			}},
			{ID: "paths", Title: locales.PrefsTools(cx), Items: []ui.SettingsItem{
				{ID: "external-editor", Title: locales.PrefsExternalEditor(cx), Description: locales.PrefsExternalEditorHint(cx), Stacked: true,
					Control: pathSetting(cx, v, "external-editor", func(c settings.Config) string { return c.ExternalEditor }, func(c *settings.Config, path string) { c.ExternalEditor = path }, v.pickEditor)},
				{ID: "target-folder", Title: locales.PrefsTargetFolder(cx), Description: locales.PrefsTargetFolderHint(cx), Stacked: true,
					Control: pathSetting(cx, v, "target-folder", func(c settings.Config) string { return c.TargetFolder }, func(c *settings.Config, path string) { c.TargetFolder = path }, func(done func(string)) {
						v.cx.PickDirectories(core.PickDirectoriesOptions{Title: locales.PickFolder(v.cx)}, func(dirs []core.Directory, err error) {
							if err == nil && len(dirs) > 0 && dirs[0].Path != "" {
								done(dirs[0].Path)
							}
						})
					})},
			}},
		}},
		{ID: "mouse", Title: locales.PrefsMouse(cx), Description: locales.PrefsMouseHint(cx), Icon: solar.Mouse, Groups: p.mouseGroups(cx)},
		{ID: "keyboard", Title: locales.PrefsKeyboard(cx), Description: locales.PrefsKeyboardHint(cx), Icon: solar.Keyboard, Groups: p.keyboardGroups(cx)},
		{ID: "about", Title: locales.PrefsAbout(cx), Description: locales.PrefsAboutHint(cx), Icon: solar.InfoCircle, Groups: aboutGroups(cx)},
	}
}

// Long paths get a whole row and one-line truncation; buttons keep their
// own row so neither the title nor the path can squeeze them out.
func pathSetting(cx *core.Scope, v *viewer, id string, get func(settings.Config) string, set func(*settings.Config, string), choose func(done func(string))) core.Node {
	return core.View{Style: core.Style{Gap: 10, MinWidth: core.Pt(0)}, Children: []core.Node{
		core.Text{TestID: id + "-path", MaxLines: 1, ContentFunc: func() string {
			if path := get(v.cfg.Get()); path != "" {
				return path
			}
			return locales.PrefsNotSet(cx)
		}, StyleFunc: func() core.Style {
			return core.Style{FontSize: 13, LineHeight: 20, Color: ui.ColorsOf(cx).Muted(), MinWidth: core.Pt(0), FlexShrink: 1}
		}},
		ui.HStack{Gap: 8, Children: []core.Node{
			ui.Button{TestID: id + "-choose", Variant: ui.ButtonOutline, Text: locales.PrefsChoose(cx), OnPress: func() { choose(func(path string) { v.update(func(c *settings.Config) { set(c, path) }) }) }},
			ui.Button{TestID: id + "-clear", Variant: ui.ButtonGhost, Text: locales.PrefsClear(cx), Disabled: func() bool { return get(v.cfg.Get()) == "" }, OnPress: func() { v.update(func(c *settings.Config) { set(c, "") }) }},
		}},
	}}
}

func (p preferences) mouseGroups(cx *core.Scope) []ui.SettingsGroup {
	v := p.v
	options := []ui.Option[settings.Action]{{Value: settings.None, Label: actionLabel(cx, settings.None)}}
	for _, a := range settings.Actions {
		options = append(options, ui.Option[settings.Action]{Value: a, Label: actionLabel(cx, a)})
	}
	groups := []struct {
		id, title string
		gestures  []settings.Gesture
	}{
		{"buttons", locales.PrefsMouseButtons(cx), []settings.Gesture{settings.MiddleClick, settings.BackButton, settings.ForwardButton, settings.LeftDoubleClick, settings.RightClick}},
		{"wheel", locales.PrefsMouseWheel(cx), []settings.Gesture{settings.WheelUp, settings.WheelDown}},
		{"modified", locales.PrefsMouseModifiedWheel(cx), []settings.Gesture{settings.CtrlWheelUp, settings.CtrlWheelDown, settings.ShiftWheelUp, settings.ShiftWheelDown, settings.AltWheelUp, settings.AltWheelDown}},
	}
	var out []ui.SettingsGroup
	for _, g := range groups {
		group := ui.SettingsGroup{ID: g.id, Title: g.title}
		for _, gesture := range g.gestures {
			group.Items = append(group.Items, ui.SettingsItem{ID: string(gesture), Title: gestureLabel(cx, gesture),
				Control: ui.Select[settings.Action]{Options: options, TestID: "mouse-" + string(gesture), Searchable: true, Style: core.Style{Width: core.Pt(230)},
					Value: func() settings.Action {
						if a := v.cfg.Get().Mouse[gesture]; a != "" {
							return a
						}
						return settings.None
					},
					OnChange: func(a settings.Action) { v.update(func(c *settings.Config) { c.Mouse[gesture] = a }) }}})
		}
		out = append(out, group)
	}
	return out
}

func (p preferences) keyboardGroups(cx *core.Scope) []ui.SettingsGroup {
	cfg := core.Untracked(cx, p.v.cfg.Get)
	groups := []struct {
		id, title string
		actions   []settings.Action
	}{
		{"navigation", locales.PrefsKeyboardNavigation(cx), []settings.Action{settings.Previous, settings.Next, settings.First, settings.Last}},
		{"image", locales.PrefsKeyboardView(cx), []settings.Action{settings.ZoomIn, settings.ZoomOut, settings.ZoomFit, settings.ZoomOriginal, settings.RotateLeft, settings.RotateRight, settings.FlipH, settings.FlipV, settings.Info, settings.Navigator, settings.Filmstrip}},
		{"window", locales.PrefsKeyboardWindow(cx), []settings.Action{settings.FullScreen, settings.Escape, settings.Close, settings.Minimize, settings.AlwaysOnTop, settings.TitleBar, settings.Menu, settings.Preferences}},
		{"files", locales.PrefsKeyboardFiles(cx), []settings.Action{settings.Open, settings.Reveal, settings.CopyImage, settings.CopyPath, settings.Rename, settings.CopyTo, settings.MoveTo, settings.OpenWith, settings.Properties, settings.ExportVideo, settings.Delete}},
		{"animation", locales.PrefsKeyboardAnimation(cx), []settings.Action{settings.Slideshow, settings.AnimPause, settings.AnimPrev, settings.AnimNext, settings.AnimSlower, settings.AnimFaster}},
	}
	out := []ui.SettingsGroup{{ID: "hint", Items: []ui.SettingsItem{{ID: "keys-hint", Content: core.Text{Content: locales.PrefsKeysHint(cx),
		StyleFunc: func() core.Style { return core.Style{FontSize: 13, LineHeight: 20, Color: ui.ColorsOf(cx).Muted()} }}}}}}
	for _, g := range groups {
		group := ui.SettingsGroup{ID: g.id, Title: g.title}
		for _, action := range g.actions {
			group.Items = append(group.Items, ui.SettingsItem{ID: "key-" + string(action), Title: actionLabel(cx, action), Keywords: cfg.Keys[action], Control: shortcutField{v: p.v, action: action}})
		}
		out = append(out, group)
	}
	return out
}

func parseKeys(text string) []string {
	var keys []string
	for _, key := range strings.Split(text, ",") {
		if key = strings.ToLower(strings.TrimSpace(key)); key != "" {
			keys = append(keys, key)
		}
	}
	return keys
}

// shortcutField records into its own focused scope; replacing the button's
// label during recording keeps the field's width and neighbouring rows still.
type shortcutField struct {
	v      *viewer
	action settings.Action
}

func (f shortcutField) Render(cx *core.Scope) core.Node {
	v, a := f.v, f.action
	draft := core.NewSignal(cx, strings.Join(core.Untracked(cx, v.cfg.Get).Keys[a], ", "))
	recording := core.NewSignal(cx, false)
	recorder := &core.Ref{}
	commit := func(text string) { draft.Set(text); v.update(func(c *settings.Config) { c.Keys[a] = parseKeys(text) }) }
	return ui.HStack{Gap: 8, Children: []core.Node{
		ui.Input{TestID: "keys-" + string(a), Value: draft.Get, OnChange: commit, Label: actionLabel(cx, a), Style: core.Style{Width: core.Pt(200)}},
		core.View{Ref: recorder, Focusable: func() bool { return true }, TestID: "record-" + string(a), OnBlur: func() { recording.Set(false) },
			OnKeyDown: func(e core.KeyEvent) bool {
				if !recording.Get() {
					return false
				}
				key := e.Shortcut()
				if key == "" {
					return true
				} // wait for a key beyond the modifier itself
				commit(strings.Join(append(parseKeys(draft.Get()), key), ", "))
				recording.Set(false)
				return true
			}, Children: []core.Node{ui.Button{Variant: ui.ButtonOutline, Style: core.Style{Width: core.Pt(100)}, TextFunc: func() string {
				if recording.Get() {
					return locales.PrefsRecording(cx)
				}
				return locales.PrefsRecord(cx)
			}, OnPress: func() { recording.Set(true); recorder.Focus() }}},
		},
	}}
}
