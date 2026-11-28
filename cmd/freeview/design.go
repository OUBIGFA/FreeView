package main

import (
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/ui"
)

// Shared values implement docs/ui-design.md. Keep app-owned surfaces here;
// nsgo still owns input, selection, focus and platform window behavior.
const (
	textBody          core.Points = 14
	textSmall         core.Points = 13
	textTitle         core.Points = 16
	textHeading       core.Points = 24
	controlHeight     core.Points = 36
	radiusControl     core.Points = 8
	radiusPanel       core.Points = 12
	viewportColor                 = 0x1e1e1e
	overlayColor                  = 0x282828
	overlayTextColor              = 0xf0f0f0
	overlayMutedColor             = 0xb0b0b0
)

// grayTheme covers both appearances explicitly: muted labels must remain
// readable, and a light selected control needs dark text in dark mode.
func grayTheme() *core.Theme {
	t := core.DefaultTheme().Clone()
	set := func(p *core.Palette, page, surface, text, muted, border, accent, onAccent uint32) {
		p.Background, p.Surface = core.RGB(page), core.RGB(surface)
		p.Text, p.TextMuted, p.Border = core.RGB(text), core.RGB(muted), core.RGB(border)
		p.Accent, p.AccentText = core.RGB(accent), core.RGB(onAccent)
		p.AccentHover, p.AccentPressed = ui.Mix(p.Accent, p.Text, 0.16), ui.Mix(p.Accent, p.Text, 0.3)
		p.SurfaceHover, p.SurfacePressed = core.RGBA(text, 0.08), core.RGBA(text, 0.14)
		p.Selection, p.Focus, p.Scrollbar = core.RGBA(accent, 0.2), core.RGB(muted), core.RGBA(text, 0.4)
		for name, value := range map[string]core.Color{
			"sidebar": core.RGB(surface), "inset": core.RGB(surface), "raised": core.RGB(page),
			"field": core.RGB(page), "fieldBorder": core.RGB(border), "line": core.RGBA(text, 0.16),
			"faint": core.RGB(muted), "accentFill": core.RGB(accent), "light": core.RGB(accent),
			"onLight": core.RGB(onAccent), "track": core.RGB(border), "knob": core.RGB(page),
		} {
			p.SetColor(name, value)
		}
	}
	set(&t.Light, 0xffffff, 0xf4f4f4, 0x181818, 0x6a6a6a, 0xb8b8b8, 0x303030, 0xffffff)
	set(&t.Dark, 0x202020, 0x282828, 0xf0f0f0, 0xb0b0b0, 0x666666, 0xd8d8d8, 0x181818)
	return t
}

func fieldStyle(width core.Points) core.Style {
	return core.Style{Width: core.Pt(width), Height: core.Pt(controlHeight), FontSize: textBody, LineHeight: 20, CornerRadius: radiusControl}
}

func buttonStyle() core.Style {
	return core.Style{Height: core.Pt(controlHeight), CornerRadius: radiusControl}
}

// Floating content stays legible over both bright and dark photographs.
func overlayStyle() core.Style {
	return core.Style{Color: core.RGB(overlayTextColor), Background: core.RGBA(overlayColor, 0.96),
		Padding: core.Symmetric(12, 16), CornerRadius: radiusPanel, FontSize: textBody, LineHeight: 20}
}

// bottomOverlayInset keeps transient panels and the navigator above the
// thumbnail strip, rather than covering its interactive thumbnails.
func (v *viewer) bottomOverlayInset() core.Points {
	if v.cfg.Get().Filmstrip && v.extras.stripOn.Get() && v.cat != nil {
		return stripHeight + 16
	}
	return 16
}
