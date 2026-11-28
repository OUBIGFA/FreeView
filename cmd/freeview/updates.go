package main

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"

	"github.com/nsgo-dev/nsgo/core"

	"github.com/OUBIGFA/FreeView/cmd/freeview/locales"
	"github.com/OUBIGFA/FreeView/internal/settings"
	"github.com/OUBIGFA/FreeView/internal/update"
)

var (
	updateClient update.Client
	// working is set from the start of a check until its answer is dealt
	// with, and through a download, so only one runs at a time.
	working atomic.Bool
)

// canUpdate is false for a development build, which has no release version.
func canUpdate() bool {
	app := core.App()
	return app.Version != "" && !app.Development
}

func updateDir() (dir, checked string) {
	if cache, err := os.UserCacheDir(); err == nil {
		dir = filepath.Join(cache, "FreeView", "updates")
	}
	if path, err := settings.Path(); err == nil {
		checked = filepath.Join(filepath.Dir(path), "update-check")
	}
	return dir, checked
}

// checkDaily checks in the background when the last check is a day old,
// and says something only when a newer version is out.
func checkDaily(cx *core.Scope) {
	if !canUpdate() {
		return
	}
	_, checked := updateDir()
	// Another window may currently download or run the installer. Never
	// remove the shared update directory from a timed UI callback.
	if checked == "" || !update.Due(checked, time.Now()) {
		return
	}
	checkUpdates(cx, false)
}

// checkUpdates asks GitHub for a newer version. A manual check also says
// that FreeView is up to date or that the check failed.
func checkUpdates(cx *core.Scope, manual bool) {
	if !canUpdate() {
		if manual {
			cx.ShowAlert(core.AlertConfig{Title: locales.AppName(cx), Message: locales.AboutDevelopment(cx), Cancelable: true,
				Actions: []core.AlertAction{{Label: locales.UpdateOk(cx), Role: core.AlertDefault}}}, func(int) {})
		}
		return
	}
	if !working.CompareAndSwap(false, true) {
		return
	}
	version := core.App().Version
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		rel, found, err := updateClient.Latest(ctx, version)
		cx.Post(func() {
			working.Store(false)
			if err != nil {
				log.Printf("update: %v", err)
				if manual {
					updateFailed(cx, err)
				}
				return
			}
			if _, checked := updateDir(); checked != "" {
				update.Mark(checked, time.Now())
			}
			switch {
			case found:
				offerUpdate(cx, rel)
			case manual:
				cx.ShowAlert(core.AlertConfig{Title: locales.UpdateUpToDateTitle(cx), Message: locales.UpdateUpToDateMessage(cx, version),
					Cancelable: true, Actions: []core.AlertAction{{Label: locales.UpdateOk(cx), Role: core.AlertDefault}}}, func(int) {})
			}
		})
	}()
}

func updateFailed(cx *core.Scope, err error) {
	cx.ShowAlert(core.AlertConfig{Title: locales.UpdateFailedTitle(cx), Message: locales.UpdateFailedMessage(cx, err.Error()),
		Cancelable: true, Actions: []core.AlertAction{{Label: locales.UpdateOk(cx), Role: core.AlertDefault}}}, func(int) {})
}

// offerUpdate asks whether to install rel. The installer quits FreeView,
// installs and opens the new version, so nothing follows a good launch.
func offerUpdate(cx *core.Scope, rel update.Release) {
	cx.ShowAlert(core.AlertConfig{
		Title: locales.UpdateTitle(cx, rel.Version), Message: locales.UpdateMessage(cx), Cancelable: true,
		Actions: []core.AlertAction{{Label: locales.UpdateInstall(cx), Role: core.AlertDefault}, {Label: locales.UpdateLater(cx), Role: core.AlertCancel}},
	}, func(i int) {
		if i != 0 || !working.CompareAndSwap(false, true) {
			return
		}
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
			defer cancel()
			dir, _ := updateDir()
			path, err := updateClient.Download(ctx, rel, dir)
			if err == nil {
				err = update.Launch(path)
			}
			cx.Post(func() {
				if err != nil {
					working.Store(false)
					log.Printf("update: %v", err)
					updateFailed(cx, err)
				}
			})
		}()
	})
}
