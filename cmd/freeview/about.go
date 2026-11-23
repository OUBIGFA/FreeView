package main

import (
	"bytes"
	"image"
	"image/png"
	"sync"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/ui"
	xdraw "golang.org/x/image/draw"

	"github.com/OUBIGFA/FreeView/assets/branding"
	"github.com/OUBIGFA/FreeView/cmd/freeview/locales"
)

const (
	repositoryURL = "https://github.com/OUBIGFA/FreeView"
	logoPoints    = 56
)

var launchBrowser = openBrowser

// openWebURL keeps the shell's browser handoff off both the nsgo engine
// and the Windows message thread. nsgo v0.2.0's Scope.OpenURL calls
// ShellExecuteEx on the message thread, freezing every window if it waits.
func openWebURL(cx *core.Scope, url string) {
	launch := launchBrowser
	go func() {
		err := launch(url)
		if err != nil {
			// Post also drops the reply if this settings page has closed.
			cx.Post(func() {
				cx.ShowAlert(core.AlertConfig{Title: locales.AboutOpenFailed(cx),
					Message: locales.OpenFailed(cx, url, err.Error()), Cancelable: true,
					Actions: []core.AlertAction{{Label: locales.UpdateOk(cx), Role: core.AlertDefault}}}, func(int) {})
			})
		}
	}()
}

// logoPixels is the brand image shrunk once to 128 pixels a side: the
// 1200-pixel source decoded as it is would hold 5.5 MB for a 56-point
// mark. It is made the first time the About page shows.
var logoPixels = sync.OnceValue(func() *image.RGBA {
	src, err := png.Decode(bytes.NewReader(branding.Logo))
	if err != nil {
		return nil // the embedded PNG is fixed; a test decodes it
	}
	dst := image.NewRGBA(image.Rect(0, 0, 128, 128))
	xdraw.CatmullRom.Scale(dst, dst.Rect, src, src.Bounds(), xdraw.Src, nil)
	return dst
})

// appVersion is the version nsgo builds into the app, marked when the
// build is a development one, which never updates.
func appVersion(cx *core.Scope) string {
	app := core.App()
	switch {
	case app.Version == "":
		return locales.AboutDevelopment(cx)
	case app.Development:
		return locales.AboutVersionDevelopment(cx, app.Version)
	}
	return locales.AboutVersion(cx, app.Version)
}

// aboutGroups is the About page: the app and its version, its repository,
// updates and licenses. Links open in the browser.
func aboutGroups(cx *core.Scope) []ui.SettingsGroup {
	open := func(url string) func() { return func() { openWebURL(cx, url) } }
	button := func(id, text string, press func()) core.Node {
		return ui.Button{Text: text, Variant: ui.ButtonOutline, Size: ui.SizeSmall, TestID: id, OnPress: press}
	}
	logo := core.Image{TestID: "about-logo", Source: core.ImageSource{Pixels: logoPixels()},
		Style: core.Style{Width: core.Pt(logoPoints), Height: core.Pt(logoPoints)}}
	return []ui.SettingsGroup{
		{ID: "app", Title: locales.AboutApp(cx), Items: []ui.SettingsItem{
			{ID: "app", Title: "FreeView", Description: appVersion(cx) + "\n" + locales.AboutSummary(cx), Leading: logo},
			{ID: "github", Title: "GitHub", Description: repositoryURL,
				Control: button("about-github", locales.AboutOpenRepository(cx), open(repositoryURL))},
			{ID: "feedback", Title: locales.AboutFeedback(cx), Description: locales.AboutFeedbackHint(cx),
				Control: button("about-feedback", locales.AboutFeedbackButton(cx), open(repositoryURL+"/issues"))},
		}},
		{ID: "updates", Title: locales.AboutUpdates(cx), Items: []ui.SettingsItem{
			{ID: "check-updates", Title: locales.AboutCheckUpdates(cx), Description: locales.AboutCheckUpdatesHint(cx),
				Control: button("about-check-updates", locales.AboutCheckNow(cx), func() { checkUpdates(cx, true) })},
			{ID: "releases", Title: locales.AboutReleases(cx), Description: locales.AboutReleasesHint(cx),
				Control: button("about-releases", locales.AboutOpen(cx), open(repositoryURL+"/releases"))},
		}},
		{ID: "licenses", Title: locales.AboutLicenses(cx), Items: []ui.SettingsItem{
			{ID: "license", Title: locales.AboutLicense(cx), Description: "GPL-3.0-only",
				Control: button("about-license", locales.AboutView(cx), open(repositoryURL+"/blob/main/LICENSE"))},
			{ID: "notices", Title: locales.AboutNotices(cx), Description: locales.AboutNoticesHint(cx),
				Control: button("about-notices", locales.AboutView(cx), open(repositoryURL+"/blob/main/third_party/README.md"))},
		}},
	}
}
