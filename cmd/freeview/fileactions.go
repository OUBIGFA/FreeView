package main

// The file commands: rename, copy or move to a folder, open in another
// program, the Explorer properties and saving a motion photo's clip. Each
// touches the disk on a goroutine and reports back through Post.

import (
	"bytes"
	"context"
	"errors"
	"image/png"
	"path/filepath"
	"strings"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/desktop"
	"github.com/nsgo-dev/nsgo/ui"

	"github.com/OUBIGFA/FreeView/cmd/freeview/locales"
	"github.com/OUBIGFA/FreeView/internal/catalog"
	"github.com/OUBIGFA/FreeView/internal/fileops"
	"github.com/OUBIGFA/FreeView/internal/livephoto"
	"github.com/OUBIGFA/FreeView/internal/settings"
)

// fileOnDisk is the shown image's file when it is one on disk, not an
// archive entry; with ok false it says why not.
func (v *viewer) fileOnDisk() (path string, ok bool) {
	s := v.img.Get()
	if s.cat == nil || s.item.Path == "" {
		return "", false
	}
	if s.cat.Archive != "" {
		v.flash(locales.ArchiveNoChange(v.cx))
		return "", false
	}
	return s.item.Path, true
}

// runFile carries out the file actions.
func (v *viewer) runFile(a settings.Action) {
	switch a {
	case settings.Rename:
		if _, ok := v.fileOnDisk(); ok {
			v.extras.renaming.Set(true)
		}
	case settings.CopyTo, settings.MoveTo:
		path, ok := v.fileOnDisk()
		if !ok {
			return
		}
		v.withFolder(func(dir string) { v.copyOrMove(path, dir, a == settings.MoveTo) })
	case settings.OpenWith:
		if p := v.currentFile(); p != "" {
			v.withEditor(func(editor string) {
				go func() {
					if err := fileops.OpenWith(editor, p); err != nil {
						v.cx.Post(func() { v.flash(locales.OpenWithFailed(v.cx, err.Error())) })
					}
				}()
			})
		}
	case settings.Properties:
		if p := v.currentFile(); p != "" {
			go func() {
				if err := fileops.ShowProperties(p); err != nil {
					v.cx.Post(func() { v.flash(locales.PropertiesFailed(v.cx, err.Error())) })
				}
			}()
		}
	case settings.ExportVideo:
		v.exportVideo()
	}
}

// withFolder calls fn with the folder Copy to and Move to use: the one
// the preferences name, else one the user picks each time.
func (v *viewer) withFolder(fn func(dir string)) {
	if dir := v.cfg.Get().TargetFolder; dir != "" {
		fn(dir)
		return
	}
	v.cx.PickDirectories(core.PickDirectoriesOptions{Title: locales.PickFolder(v.cx), Start: v.cat.Dir},
		func(dirs []core.Directory, err error) {
			if err == nil && len(dirs) > 0 && dirs[0].Path != "" {
				fn(dirs[0].Path)
			}
		})
}

// withEditor calls fn with the program Open with runs, asking for one
// the first time and keeping it in the preferences.
func (v *viewer) withEditor(fn func(program string)) {
	if p := v.cfg.Get().ExternalEditor; p != "" {
		fn(p)
		return
	}
	v.pickEditor(func(p string) {
		v.update(func(c *settings.Config) { c.ExternalEditor = p })
		fn(p)
	})
}

func (v *viewer) pickEditor(fn func(program string)) {
	v.cx.PickFiles(core.PickFilesOptions{Types: []string{".exe"}, Title: locales.PickEditor(v.cx)},
		func(files []core.File, err error) {
			if err == nil && len(files) > 0 && files[0].Path != "" {
				fn(files[0].Path)
			}
		})
}

func (v *viewer) copyOrMove(path, dir string, move bool) {
	cx, name := v.cx, filepath.Base(path)
	cat := v.img.Get().cat
	go func() {
		op := fileops.CopyTo
		if move {
			op = fileops.MoveTo
		}
		target, err := op(path, dir)
		cx.Post(func() {
			switch {
			case err != nil && move:
				v.flash(locales.MoveFailed(cx, name, err.Error()))
			case err != nil:
				v.flash(locales.CopyFailed(cx, name, err.Error()))
			case move:
				v.flash(locales.MovedTo(cx, dir))
				if target != path && v.cat == cat {
					v.dropItem(path)
				}
			default:
				v.flash(locales.CopiedTo(cx, dir))
			}
		})
	}()
}

// dropItem takes the image at path out of the catalog after it left the
// folder, showing the one that took its place.
func (v *viewer) dropItem(path string) {
	if v.cat == nil {
		return
	}
	i := v.cat.Index(path)
	if i < 0 {
		return
	}
	v.forget(path)
	current := v.img.Get().item.Path
	selected := ""
	if j := v.index.Get(); j >= 0 && j < len(v.cat.Items) {
		selected = v.cat.Items[j].Path
	}
	next := v.cat.Remove(i)
	v.count.Set(len(v.cat.Items))
	if next < 0 {
		v.loadSeq++
		v.index.Set(-1)
		v.commit(shown{}, v.front.Get())
		v.openErr.Set(locales.FolderEmpty(v.cx, v.cat.Title()))
		return
	}
	if selected != path {
		if j := v.cat.Index(selected); j >= 0 {
			next = j
		}
		if selected == current {
			v.index.Set(next)
			return
		}
	}
	v.show(next)
}

// rename renames the shown image to base, keeping its extension, and
// keeps its place in the catalog.
func (v *viewer) rename(base string) {
	cx := v.cx
	v.extras.renaming.Set(false)
	path, ok := v.fileOnDisk()
	stem := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	if !ok || base == stem {
		return
	}
	cat := v.img.Get().cat
	go func() {
		next, err := fileops.Rename(path, base)
		cx.Post(func() {
			if err != nil {
				v.flash(locales.RenameFailed(cx, renameError(cx, err)))
				return
			}
			if v.cat != cat {
				return
			}
			i := v.cat.Index(path)
			if i < 0 {
				return
			}
			it := &v.cat.Items[i]
			it.Path, it.Name = next, filepath.Base(next)
			if pic, ok := v.cache[path]; ok {
				v.forget(path)
				v.cache[next] = pic
				v.cacheOrder = append(v.cacheOrder, next)
			}
			if s := v.img.Get(); s.item.Path == path {
				s.item = *it
				v.img.Set(s)
			}
			if v.next.item.Path == path {
				v.next.item = *it
			}
		})
	}()
}

func renameError(cx *core.Scope, err error) string {
	switch {
	case errors.Is(err, fileops.ErrEmptyName):
		return locales.NameEmpty(cx)
	case errors.Is(err, fileops.ErrInvalidChars):
		return locales.NameInvalid(cx)
	case errors.Is(err, fileops.ErrReservedName):
		return locales.NameReserved(cx)
	case errors.Is(err, fileops.ErrExists):
		return locales.NameExists(cx)
	}
	return err.Error()
}

// renameBox is the field the new name is typed in, over the image.
func (v *viewer) renameBox(cx *core.Scope) core.Node {
	cx.SetSubtreeAppearance(core.AppearanceDark)
	path := v.img.Get().item.Path
	stem := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	draft := core.NewSignal(cx, stem)
	// The field keeps Esc from the viewer's bindings, as a text input
	// edits with it, so it is bound here, where it fires first.
	cx.OnKeyWith("escape", core.KeyOptions{When: core.KeyAlways}, func() { v.extras.renaming.Set(false) })
	return core.View{TestID: "rename",
		StyleFunc: func() core.Style {
			return core.Style{Position: core.Absolute, Left: core.Pt(16), Right: core.Pt(16), Bottom: core.Pt(v.bottomOverlayInset()), AlignItems: core.AlignCenter}
		},
		Children: []core.Node{core.View{
			Style: core.Style{Background: core.RGBA(overlayColor, 0.98), BorderWidth: 1, BorderColor: core.RGB(0x666666),
				CornerRadius: 16, BorderCurve: core.BorderCurveContinuous, Padding: core.All(24), Gap: 12, Width: core.Pct(100), MaxWidth: core.Pt(460)},
			Children: []core.Node{
				ui.Text(locales.ActionRename(cx), textTitle, ui.Semibold, ui.ColorsOf(cx).Text),
				core.Text{Content: locales.RenameHint(cx, filepath.Ext(path)), Style: core.Style{Color: core.RGB(overlayMutedColor), FontSize: textSmall, LineHeight: 20}},
				ui.Input{TestID: "rename-input", Label: locales.ActionRename(cx), Value: draft.Get, OnChange: draft.Set, AutoFocus: true, OnSubmit: v.rename,
					Style: core.Style{FontSize: textBody, Height: core.Pt(controlHeight), CornerRadius: radiusControl}},
				core.View{Style: core.Style{Direction: core.Row, JustifyContent: core.JustifyEnd, Gap: 8}, Children: []core.Node{
					ui.Button{Text: locales.Cancel(cx), Variant: ui.ButtonGhost, Style: buttonStyle(), TextStyle: core.Style{FontSize: textBody}, TestID: "rename-cancel", OnPress: func() { v.extras.renaming.Set(false) }},
					ui.Button{Text: locales.UpdateOk(cx), Variant: ui.ButtonPrimary, Style: buttonStyle(), TextStyle: core.Style{FontSize: textBody}, TestID: "rename-confirm", OnPress: func() { v.rename(draft.Get()) }},
				}},
			}}}}
}

// exportVideo saves the clip of the shown motion photo, unchanged, where
// the user says.
func (v *viewer) exportVideo() {
	cx := v.cx
	if v.cat == nil || v.index.Get() < 0 {
		return
	}
	s := v.img.Get()
	cat, item := s.cat, s.item
	if cat == nil {
		return
	}
	go func() {
		data, err := cat.Read(item)
		var clip livephoto.Video
		found := false
		if err == nil {
			if livephoto.IsLIVP(item.Name) {
				_, _, clip, err = livephoto.OpenLIVP(data)
				found = err == nil && len(clip.Data) > 0
			} else {
				clip, found = livephoto.FindVideo(data)
			}
		}
		cx.Post(func() {
			if !found {
				v.flash(locales.NoMotion(cx))
				return
			}
			name := strings.TrimSuffix(item.Name, filepath.Ext(item.Name)) + clip.Ext
			cx.SaveFile(core.SaveOptions{Name: name, Data: clip.Data, Start: cat.Dir}, func(saved bool, path string, err error) {
				switch {
				case err != nil:
					v.flash(locales.VideoSaveFailed(cx, err.Error()))
				case saved:
					v.flash(locales.VideoSaved(cx, filepath.Base(path)))
				}
			})
		})
	}()
}

// currentFile is the file on disk the shown image is in: the image or its
// archive; "" with nothing shown.
func (v *viewer) currentFile() string {
	s := v.img.Get()
	if s.cat == nil || s.item.Path == "" {
		return ""
	}
	if s.cat.Archive != "" {
		return s.cat.Archive
	}
	return s.item.Path
}

func (v *viewer) pick() {
	types := append(catalog.ImageExtensions(), catalog.ArchiveExtensions()...)
	start := ""
	if v.cat != nil {
		start = v.cat.Dir
	}
	v.cx.PickFiles(core.PickFilesOptions{Types: types, TypesName: locales.PickImages(v.cx), Start: start},
		func(files []core.File, err error) {
			if err == nil && len(files) > 0 && files[0].Path != "" {
				v.open(files[0].Path)
			}
		})
}

func (v *viewer) delete() {
	cx := v.cx
	s := v.img.Get()
	if s.cat == nil || s.item.Path == "" {
		return
	}
	if s.cat.Archive != "" {
		v.flash(locales.ArchiveReadOnly(cx))
		return
	}
	item, cat := s.item, s.cat
	trash := func() {
		desktop.TrashFile(cx, item.Path, func(_ string, err error) {
			if err != nil {
				v.flash(locales.DeleteFailed(cx, item.Name, err.Error()))
				return
			}
			if v.cat == cat {
				v.dropItem(item.Path)
			}
		})
	}
	if !v.cfg.Get().ConfirmDelete {
		trash()
		return
	}
	cx.ShowAlert(core.AlertConfig{
		Title: locales.DeleteTitle(cx), Message: locales.DeleteMessage(cx, item.Name), Cancelable: true,
		Actions: []core.AlertAction{{Label: locales.DeleteConfirm(cx), Role: core.AlertDestructive}, {Label: locales.Cancel(cx), Role: core.AlertCancel}},
	}, func(i int) {
		if i == 0 {
			trash()
		}
	})
}

func (v *viewer) copyImage() {
	p := v.img.Get().pic
	if p.Pixels != nil {
		// Encoding a large decoded image can take hundreds of milliseconds.
		// A user-requested copy retains its source even if the view changes.
		background(context.Background(), func() {
			var buf bytes.Buffer
			if err := png.Encode(&buf, p.Pixels); err != nil {
				return
			}
			v.cx.Post(func() { v.cx.SetClipboardImage(buf.Bytes()); v.flash(locales.CopiedImage(v.cx)) })
		})
		return
	}
	if len(p.Data) == 0 {
		return
	}
	v.cx.SetClipboardImage(p.Data)
	v.flash(locales.CopiedImage(v.cx))
}
