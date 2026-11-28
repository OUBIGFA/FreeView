package main

import (
	"github.com/nsgo-dev/nsgo/base"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/desktop"
	"github.com/nsgo-dev/nsgo/ui"

	"github.com/OUBIGFA/FreeView/assets/icons/solar"
	"github.com/OUBIGFA/FreeView/cmd/freeview/locales"
	"github.com/OUBIGFA/FreeView/internal/settings"
)

// contextMenu uses nsgo's menu controls, including their cascading
// submenus, keyboard navigation, dismissal and screen-edge placement.
// A zero-size anchor follows the pointer so a custom mouse binding or
// keyboard shortcut opens the very same menu as a secondary click.
func (v *viewer) contextMenu(cx *core.Scope) core.Node {
	cx.SetSubtreeAppearance(core.AppearanceDark)
	return base.Menu{
		Open:         v.menuOpen.Get,
		OnOpenChange: func(open bool, _ base.Reason) { v.menuOpen.Set(open) },
		Label:        locales.ActionMenu(cx),
		StyleFunc: func() core.Style {
			at := v.menuAt.Get()
			// Reserve the full menu height where the viewport permits it;
			// nsgo still clamps and scrolls it in smaller windows.
			at.Y = min(at.Y, max(core.Points(8), v.viewport.Get().Height-672))
			return core.Style{Position: core.Absolute, Left: core.Pt(at.X), Top: core.Pt(at.Y),
				Width: core.Pt(0), Height: core.Pt(0), Visibility: core.VisibilityHidden}
		},
		Placement: core.PlaceBottom, Align: core.AlignStart, Margin: 8, Offset: 4,
		PopupStyleFunc: func() core.Style {
			return core.Style{MinWidth: core.Pt(256), Padding: core.All(8), CornerRadius: radiusPanel,
				BorderWidth: 1, BorderColor: core.RGB(0x666666), Background: core.RGB(overlayColor), Color: core.RGB(overlayTextColor)}
		},
		Children: []core.Node{core.Func(func(cx *core.Scope) core.Node {
			intent := &menuIntent{}
			cx.Cleanup(intent.cancel)
			// These snapshots are made on opening, just like the OS menu.
			// Showing a popup never rebuilds the image or its bindings.
			sub := func(id, text string, icon func() *core.SVG, items []desktop.MenuItem) core.Node {
				return core.Func(func(cx *core.Scope) core.Node {
					row := (ui.Submenu{Text: text, Icon: icon, TestID: "context-" + id,
						Back: locales.MenuBack(cx), Children: menuNodes(items)}).Render(cx).(base.Submenu)
					row.Style = menuRowStyle(row.Style)
					row.Trigger = menuTypography(row.Trigger)
					return intent.submenu(cx, row)
				})
			}
			return core.Group{
				menuChoice(v.contextItem(settings.Open, solar.FolderOpen), intent),
				menuChoice(v.contextItem(settings.Reveal, solar.Folder), intent), ui.MenuSeparator{},
				sub("browse", locales.MenuGo(cx), solar.Gallery, v.browseMenu()),
				sub("zoom", locales.MenuZoom(cx), solar.MagnifierZoomIn, v.zoomMenu()),
				sub("transform", locales.MenuTransform(cx), solar.RotateClockwise, v.transformMenu()),
				sub("animation", locales.MenuAnimation(cx), solar.ClapperboardPlay, v.animationMenu()),
				sub("files", locales.PrefsFilesGroup(cx), solar.FolderWithFiles, v.fileMenu()), ui.MenuSeparator{},
				// Frequent switches stay one click away, with their current
				// state visible without opening a submenu.
				v.contextToggle(intent, settings.FullScreen, desktop.FullScreen(v.cx)),
				v.contextToggle(intent, settings.Slideshow, v.slideshow.Get()),
				v.contextToggle(intent, settings.Info, v.info.Get()),
				v.contextToggle(intent, settings.AlwaysOnTop, v.cfg.Get().AlwaysOnTop),
				v.contextToggle(intent, settings.TitleBar, v.cfg.Get().ShowTitleBar),
				v.contextToggle(intent, settings.Navigator, v.cfg.Get().Navigator),
				v.contextToggle(intent, settings.Filmstrip, v.cfg.Get().Filmstrip), ui.MenuSeparator{},
				menuChoice(v.contextItem(settings.CopyImage, solar.Copy), intent),
				menuChoice(v.contextItem(settings.CopyPath, solar.LinkMinimalistic2), intent),
				menuChoice(v.contextItem(settings.Delete, solar.TrashBinMinimalistic), intent), ui.MenuSeparator{},
				menuChoice(v.contextItem(settings.Preferences, solar.Settings), intent),
				menuChoice(v.contextItem(settings.Close, solar.CloseCircle), intent),
			}
		})},
	}
}

// menuNodes gives the native menu's choices the ui theme. The action,
// shortcut, check mark and availability come from the same definitions.
func menuNodes(items []desktop.MenuItem) []core.Node {
	nodes := make([]core.Node, 0, len(items))
	for _, item := range items {
		if item.Separator {
			nodes = append(nodes, ui.MenuSeparator{})
			continue
		}
		node := ui.MenuItem{Text: item.Label, Shortcut: item.Shortcut, OnSelect: item.OnSelect,
			Disabled: func() bool { return item.Disabled }}
		if item.Checkable {
			node.Checked = func() bool { return item.Checked }
		}
		nodes = append(nodes, menuChoice(node, nil))
	}
	return nodes
}

// menuChoice applies the app's readable menu density and forwards check state.
// In v0.3.4 ui.MenuItem draws Checked but omits it from base.MenuItem,
// so without this bridge assistive technology sees a plain menu choice.
func menuChoice(node ui.MenuItem, intent *menuIntent) core.Node {
	return core.Func(func(cx *core.Scope) core.Node {
		row := node.Render(cx).(base.MenuItem)
		row.Checked = node.Checked
		row.Style = menuRowStyle(row.Style)
		for i, child := range row.Children {
			row.Children[i] = menuTypography(child)
		}
		if intent != nil {
			return intent.row(cx, row.Render(cx).(core.View))
		}
		return row
	})
}

func menuRowStyle(style core.Style) core.Style {
	return style.Merge(core.Style{MinHeight: core.Pt(32), Padding: core.Symmetric(4, 8), Gap: 8, CornerRadius: radiusControl})
}

// Only text metrics change; nsgo keeps icons, checks, focus and keyboard handling.
func menuTypography(node core.Node) core.Node {
	switch n := node.(type) {
	case core.Text:
		size := textBody
		if n.Style.FontSize == ui.TypeCaption.FontSize {
			size = textSmall
		}
		n.Style.FontSize, n.Style.LineHeight = size, 20
		return n
	case core.Group:
		for i, child := range n {
			n[i] = menuTypography(child)
		}
		return n
	}
	return node
}

func (v *viewer) contextToggle(intent *menuIntent, a settings.Action, checked bool) core.Node {
	node := v.contextItem(a, nil)
	node.Checked = func() bool { return checked }
	return menuChoice(node, intent)
}

func (v *viewer) contextItem(a settings.Action, icon func() *core.SVG) ui.MenuItem {
	item := v.item(a)
	node := ui.MenuItem{Text: item.Label, Icon: icon, Shortcut: item.Shortcut, TestID: "context-" + string(a),
		Disabled: func() bool { return item.Disabled }, OnSelect: item.OnSelect}
	if a == settings.Delete {
		node.Variant = ui.MenuItemDanger
	}
	return node
}
