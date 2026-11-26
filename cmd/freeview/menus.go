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
			return core.Style{Position: core.Absolute, Left: core.Pt(at.X), Top: core.Pt(at.Y),
				Width: core.Pt(0), Height: core.Pt(0), Visibility: core.VisibilityHidden}
		},
		Placement: core.PlaceBottom, Align: core.AlignStart, Margin: 8, Offset: 2,
		PopupStyleFunc: func() core.Style {
			return ui.FinOf(cx).Menu().Over(core.Style{MinWidth: core.Pt(232), Padding: core.All(6),
				CornerRadius: 10, BorderWidth: 1, Color: ui.ColorsOf(cx).Text()})
		},
		Children: []core.Node{core.Func(func(cx *core.Scope) core.Node {
			// These snapshots are made on opening, just like the OS menu.
			// Showing a popup never rebuilds the image or its bindings.
			sub := func(id, text string, icon func() *core.SVG, items []desktop.MenuItem) core.Node {
				return ui.Submenu{Text: text, Icon: icon, TestID: "context-" + id,
					Back: locales.MenuBack(cx), Children: menuNodes(items)}
			}
			return core.Group{
				v.contextItem(settings.Open, solar.FolderOpen),
				v.contextItem(settings.Reveal, solar.Folder), ui.MenuSeparator{},
				sub("browse", locales.MenuGo(cx), solar.Gallery, v.browseMenu()),
				sub("zoom", locales.MenuZoom(cx), solar.MagnifierZoomIn, v.zoomMenu()),
				sub("transform", locales.MenuTransform(cx), solar.RotateClockwise, v.transformMenu()),
				sub("animation", locales.MenuAnimation(cx), solar.ClapperboardPlay, v.animationMenu()),
				sub("files", locales.PrefsFilesGroup(cx), solar.FolderWithFiles, v.fileMenu()), ui.MenuSeparator{},
				// Frequent switches stay one click away, with their current
				// state visible without opening a submenu.
				v.contextToggle(settings.FullScreen, desktop.FullScreen(v.cx)),
				v.contextToggle(settings.Slideshow, v.slideshow.Get()),
				v.contextToggle(settings.Info, v.info.Get()),
				v.contextToggle(settings.AlwaysOnTop, v.cfg.Get().AlwaysOnTop),
				v.contextToggle(settings.TitleBar, v.cfg.Get().ShowTitleBar),
				v.contextToggle(settings.Navigator, v.cfg.Get().Navigator),
				v.contextToggle(settings.Filmstrip, v.cfg.Get().Filmstrip), ui.MenuSeparator{},
				v.contextItem(settings.CopyImage, solar.Copy),
				v.contextItem(settings.CopyPath, solar.LinkMinimalistic2),
				v.contextItem(settings.Delete, solar.TrashBinMinimalistic), ui.MenuSeparator{},
				v.contextItem(settings.Preferences, solar.Settings),
				v.contextItem(settings.Close, solar.CloseCircle),
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
		nodes = append(nodes, menuChoice(node))
	}
	return nodes
}

// menuChoice preserves nsgo's appearance while forwarding the check state.
// In v0.3.4 ui.MenuItem draws Checked but omits it from base.MenuItem,
// so without this bridge assistive technology sees a plain menu choice.
func menuChoice(node ui.MenuItem) core.Node {
	if node.Checked == nil {
		return node
	}
	return core.Func(func(cx *core.Scope) core.Node {
		row := node.Render(cx).(base.MenuItem)
		row.Checked = node.Checked
		return row
	})
}

func (v *viewer) contextToggle(a settings.Action, checked bool) core.Node {
	node := v.contextItem(a, nil)
	node.Checked = func() bool { return checked }
	return menuChoice(node)
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
