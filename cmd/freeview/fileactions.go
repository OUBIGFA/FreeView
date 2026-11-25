package main

// The file commands: rename, copy or move to a folder, open in another
// program, the Explorer properties and saving a motion photo's clip. Each
// touches the disk on a goroutine and reports back through Post.

import (
	"errors"
	"path/filepath"
	"strings"

	"github.com/nsgo-dev/nsgo/core"

	"github.com/OUBIGFA/FreeView/cmd/freeview/locales"
	"github.com/OUBIGFA/FreeView/internal/fileops"
	"github.com/OUBIGFA/FreeView/internal/livephoto"
	"github.com/OUBIGFA/FreeView/internal/settings"
)

// fileOnDisk is the shown image's file when it is one on disk, not an
// archive entry; with ok false it says why not.
func (v *viewer) fileOnDisk() (path string, ok bool) {
	if v.cat == nil || v.index.Get() < 0 || v.img.Get().item.Path == "" {
		return "", false
	}
	if v.cat.Archive != "" {
		v.flash(locales.ArchiveNoChange(v.cx))
		return "", false
	}
	return v.img.Get().item.Path, true
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
	go func() {
		op := fileops.CopyTo
		if move {
			op = fileops.MoveTo
		}
		_, err := op(path, dir)
		cx.Post(func() {
			switch {
			case err != nil && move:
				v.flash(locales.MoveFailed(cx, name, err.Error()))
			case err != nil:
				v.flash(locales.CopyFailed(cx, name, err.Error()))
			case move:
				v.flash(locales.MovedTo(cx, dir))
				v.dropItem(path)
			default:
				v.flash(locales.CopiedTo(cx, dir))
			}
		})
	}()
}

// dropItem takes the image at path out of the catalog after it left the
// folder, showing the one that took its place.
func (v *viewer) dropItem(path string) {
	i := v.cat.Index(path)
	if i < 0 {
		return
	}
	delete(v.cache, path)
	next := v.cat.Remove(i)
	v.count.Set(len(v.cat.Items))
	if next < 0 {
		v.index.Set(-1)
		v.commit(shown{}, v.front.Get())
		v.openErr.Set(locales.FolderEmpty(v.cx, v.cat.Title()))
		return
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
	go func() {
		next, err := fileops.Rename(path, base)
		cx.Post(func() {
			if err != nil {
				v.flash(locales.RenameFailed(cx, renameError(cx, err)))
				return
			}
			i := v.cat.Index(path)
			if i < 0 {
				return
			}
			it := &v.cat.Items[i]
			it.Path, it.Name = next, filepath.Base(next)
			if pic, ok := v.cache[path]; ok {
				delete(v.cache, path)
				v.cache[next] = pic
				for k, p := range v.cacheOrder {
					if p == path {
						v.cacheOrder[k] = next
					}
				}
			}
			if s := v.img.Get(); s.item.Path == path {
				s.item = *it
				v.img.Set(s)
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
	path := v.img.Get().item.Path
	stem := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	// The field keeps Esc from the viewer's bindings, as a text input
	// edits with it, so it is bound here, where it fires first.
	cx.OnKeyWith("escape", core.KeyOptions{When: core.KeyAlways}, func() { v.extras.renaming.Set(false) })
	return core.View{TestID: "rename",
		Style: core.Style{Position: core.Absolute, Left: core.Pt(0), Right: core.Pt(0), Bottom: core.Pt(60), AlignItems: core.AlignCenter},
		Children: []core.Node{core.View{
			Style: core.Style{Background: core.RGBA(0x000000, 0.75), Padding: core.Symmetric(10, 14), CornerRadius: 8, Gap: 6, Width: core.Pt(420)},
			Children: []core.Node{
				core.Text{Content: locales.RenameHint(cx, filepath.Ext(path)), Style: core.Style{Color: core.RGB(0xcfcfcf), FontSize: 12, LineHeight: 17}},
				core.Input{TestID: "rename-input", DefaultValue: stem, AutoFocus: true, OnSubmit: v.rename,
					Style: core.Style{Color: core.RGB(0xffffff), FontSize: 14, Height: core.Pt(28), Padding: core.Symmetric(0, 8),
						Background: core.RGBA(0xffffff, 0.1), CornerRadius: 4}},
			}}}}
}

// exportVideo saves the clip of the shown motion photo, unchanged, where
// the user says.
func (v *viewer) exportVideo() {
	cx := v.cx
	if v.cat == nil || v.index.Get() < 0 {
		return
	}
	cat, item := v.cat, v.img.Get().item
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
