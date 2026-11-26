package main

import (
	"testing"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"

	"github.com/OUBIGFA/FreeView/internal/settings"
)

// Every existing preference remains reachable in a focused category,
// including the quality controls that previously shared the View page.
func TestPreferencesCategoriesKeepAllControls(t *testing.T) {
	for _, dark := range []bool{false, true} {
		name := "light"
		if dark {
			name = "dark"
		}
		t.Run(name, func(t *testing.T) {
			t.Setenv("APPDATA", t.TempDir())
			app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
				cx.SetTheme(grayTheme())
				return preferences{v: newViewer(cx)}.Render(cx)
			}, nsgotest.Size(820, 560), nsgotest.Environment(core.Environment{OS: core.OSWindows, Idiom: core.IdiomDesktop, Locale: "zh-Hans", Dark: dark}))
			for _, page := range []struct {
				id       string
				controls []string
			}{
				{"general", []string{"always-on-top", "title-bar", "hover-title", "hover-title-place", "remember-window", "open-maximized", "multiple-windows"}},
				{"display", []string{"fit", "zoom-step", "show-info", "navigator", "smooth-zoom", "color-management"}},
				{"browse", []string{"sort", "descending", "loop", "slideshow", "edge-clicks", "filmstrip", "animation-speed"}},
				{"files", []string{"confirm-delete", "external-editor", "target-folder"}},
			} {
				app.Press("id:settings-nav-" + page.id)
				for _, id := range page.controls {
					if len(app.QueryMounted("id:settings-"+page.id+"-"+id)) != 1 {
						t.Errorf("%s: expected one row for %s", page.id, id)
					}
				}
				pane := app.Find("id:settings-page")
				if pane.Content.Width > pane.Frame.Size.Width+1 {
					t.Errorf("%s overflows horizontally: %v in %v", page.id, pane.Content.Width, pane.Frame.Size.Width)
				}
			}
			app.Press("id:settings-nav-mouse")
			for _, g := range settings.Gestures {
				if len(app.QueryMounted("id:mouse-"+string(g))) != 1 {
					t.Errorf("missing gesture %s", g)
				}
			}
			app.Press("id:settings-nav-keyboard")
			for _, a := range settings.Actions {
				if len(app.QueryMounted("id:keys-"+string(a))) != 1 {
					t.Errorf("missing shortcut %s", a)
				}
			}
		})
	}
}

// Reset is deliberate and refreshes edited shortcut drafts as well as
// saved values. Cancelling the confirmation preserves the user's work.
func TestPreferencesResetRequiresConfirmation(t *testing.T) {
	t.Setenv("APPDATA", t.TempDir())
	var v *viewer
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		v = newViewer(cx)
		return preferences{v: v}.Render(cx)
	}, nsgotest.Size(1040, 700), nsgotest.PendingAlerts())
	app.Press("id:settings-nav-keyboard")
	app.Clear("id:keys-next")
	app.Type("id:keys-next", "ctrl+shift+n")
	app.Press("id:reset")
	app.WaitUntil(time.Second, func() bool { return len(app.Alerts()) == 1 })
	app.Answer(1)
	if got := app.Find("id:keys-next").Text; got != "ctrl+shift+n" {
		t.Fatalf("cancel changed shortcut to %q", got)
	}
	app.Press("id:reset")
	app.Answer(0)
	if got := app.Find("id:keys-next").Text; got != "right, pagedown, space" {
		t.Fatalf("reset did not refresh draft: %q", got)
	}
	app.Post(func() {
		if got := v.cfg.Get().Keys[settings.Next][0]; got != "right" {
			t.Errorf("reset did not update config: %q", got)
		}
	})
}
