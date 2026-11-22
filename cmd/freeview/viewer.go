package main

import (
	"bytes"
	"context"
	"errors"
	"image/png"
	"math"
	"path/filepath"
	"runtime/debug"
	"strconv"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/desktop"
	"github.com/nsgo-dev/nsgo/ui"

	"github.com/OUBIGFA/FreeView/cmd/freeview/locales"
	"github.com/OUBIGFA/FreeView/internal/catalog"
	"github.com/OUBIGFA/FreeView/internal/picture"
	"github.com/OUBIGFA/FreeView/internal/settings"
	"github.com/OUBIGFA/FreeView/internal/winmouse"
)

const (
	minScale = 0.01
	maxScale = 64
	// cacheEntries and cacheBytes bound the images kept read ahead: the
	// shown one and one on either side, and the one before them, which
	// the other image view also holds decoded. Decoded pixels (BMP, TIFF
	// and the WIC formats) take far more than file bytes: a 24 MP RAW is
	// 96 MB of them.
	cacheEntries = 4
	cacheBytes   = 256 << 20
	// touchpadStep is how far a touchpad scrolls for one wheel gesture.
	touchpadStep = 120
	// revealWait is the longest the main window stays hidden waiting for
	// the image the app launched with.
	revealWait = time.Second
)

// launching is whether the command line names a file to open, as nsgo
// hands to OnOpenFiles: the main window then waits for it to show.
var launching bool

// shown is the image on screen, or the error loading it.
type shown struct {
	item catalog.Item
	pic  picture.Picture
	src  core.ImageSource
	err  error
}

// viewer owns FreeView's state. Its fields are used on the engine's
// goroutine only; loads run on goroutines and come back through Post.
type viewer struct {
	cx      *core.Scope
	cfgPath string
	cfg     *core.Signal[settings.Config]

	cat   *catalog.Catalog
	index *core.Signal[int]
	count *core.Signal[int]
	img   *core.Signal[shown]
	// The two image views take turns: the one at front shows img and the
	// other decodes next, which replaces img once it loaded. nsgo blanks
	// an image view while it decodes a new source, so one view alone would
	// flash the background between images.
	slots [2]*core.Signal[core.ImageSource]
	front *core.Signal[int]
	next  shown // what the other view decodes; next.src is empty when none
	// openErr is what failed opening a folder or archive, "" otherwise.
	openErr *core.Signal[string]

	// fit, while not "", rescales the image to the viewport as it
	// changes; zooming by hand clears it and sets scale.
	fit    *core.Signal[settings.Fit]
	scale  *core.Signal[float64] // device pixels per image pixel
	offset *core.Signal[core.Point]
	rotate *core.Signal[int] // clockwise degrees, a multiple of 90
	mirror *core.Signal[bool]

	viewport  *core.Signal[core.Size]
	info      *core.Signal[bool]
	slideshow *core.Signal[bool]
	message   *core.Signal[string]

	pointer    core.Point // last pointer position in the viewport
	downAt     core.Point // where the left button last went down
	panStart   core.Point
	movingWin  bool // the current drag moves the window, not the image
	panning    bool // the current drag moves the image
	touchpad   core.Points
	loadSeq    uint64
	messageSeq uint64
	slideSeq   uint64
	trimSeq    uint64
	revealed   bool
	cache      map[string]picture.Picture
	cacheOrder []string
}

func newViewer(cx *core.Scope) *viewer {
	v := &viewer{cx: cx, cache: map[string]picture.Picture{}}
	cfg := settings.Default()
	var loadErr error
	if path, err := settings.Path(); err == nil {
		v.cfgPath = path
		cfg, loadErr = settings.Load(path)
	}
	// Configs and images hold maps and slices: every Set is a change.
	v.cfg = core.NewSignalFunc(cx, cfg, never[settings.Config])
	v.index = core.NewSignal(cx, -1)
	v.count = core.NewSignal(cx, 0)
	v.img = core.NewSignalFunc(cx, shown{}, never[shown])
	for k := range v.slots {
		v.slots[k] = core.NewSignalFunc(cx, core.ImageSource{}, never[core.ImageSource])
	}
	v.front = core.NewSignal(cx, 0)
	v.openErr = core.NewSignal(cx, "")
	v.fit = core.NewSignal(cx, cfg.Fit)
	v.scale = core.NewSignal(cx, 1.0)
	v.offset = core.NewSignal(cx, core.Point{})
	v.rotate = core.NewSignal(cx, 0)
	v.mirror = core.NewSignal(cx, false)
	v.viewport = core.NewSignal(cx, core.Size{})
	v.info = core.NewSignal(cx, cfg.ShowInfo)
	v.slideshow = core.NewSignal(cx, false)
	v.message = core.NewSignal(cx, "")
	if loadErr != nil {
		v.flash(locales.PrefsLoadFailed(cx, loadErr.Error()))
	}
	if frames != nil && frames.loadErr != nil {
		v.flash(locales.PrefsLoadFailed(cx, frames.loadErr.Error()))
	}
	frames.remember(cx, "main", func(err error) { v.flash(locales.PrefsSaveFailed(cx, err.Error())) })
	desktop.OnOpenFiles(cx, func(paths []string) {
		if len(paths) > 0 {
			v.open(paths[0])
		}
	})
	attachMouse(v)
	cx.Effect(v.updateTitle)
	cx.Effect(v.updateMenu)
	cx.Effect(v.scheduleSlide)
	cx.Effect(v.updateHand)
	v.applyWindow()
	if launching {
		time.AfterFunc(revealWait, func() { cx.Post(v.reveal) })
	} else {
		v.reveal()
	}
	return v
}

// reveal shows the main window, which opens hidden so it first shows
// with its title bar settled and the image the app launched with, if
// any. It posts the show so the batch showing the image goes first.
func (v *viewer) reveal() {
	if v.revealed {
		return
	}
	v.revealed = true
	v.cx.Post(func() {
		// Maximized while still hidden, so it never shows at its frame first.
		if v.cfg.Get().OpenMaximized && !desktop.Maximized(v.cx) {
			desktop.ToggleMaximize(v.cx)
		}
		desktop.CurrentWindow(v.cx).Focus()
	})
}

// applyWindow keeps the window topmost and its title bar as the
// preferences say. Memos limit the calls to real changes, so saving any
// other preference does not reframe the window.
func (v *viewer) applyWindow() {
	cx := v.cx
	onTop := core.NewMemo(cx, func() bool { return v.cfg.Get().AlwaysOnTop })
	titleBar := core.NewMemo(cx, func() bool { return v.cfg.Get().ShowTitleBar })
	cx.Effect(func() {
		desktop.SetWindowLevel(cx, pick(onTop.Get(), desktop.WindowLevelFloating, desktop.WindowLevelNormal))
	})
	cx.Effect(func() {
		show := titleBar.Get()
		desktop.SetTitleBar(cx, pick(show, desktop.TitleBarDefault, desktop.TitleBarHidden), 0)
		// Without a title bar the window has no caption buttons either, as
		// Honeyview's borderless window; Esc, the middle button, Alt+F4 or
		// the system menu of the drag strip close it.
		desktop.SetWindowControls(cx, desktop.WindowControls{Hidden: !show})
	})
}

func never[T any](T, T) bool { return false }

// open shows target: an image in its folder, a folder or an archive.
func (v *viewer) open(target string) {
	cfg := v.cfg.Get()
	order := catalog.Order{By: string(cfg.Sort), Descending: cfg.SortDescending}
	go func() {
		cat, i, err := catalog.Open(context.Background(), target, order)
		v.cx.Post(func() {
			if err != nil {
				v.openErr.Set(locales.OpenFailed(v.cx, filepath.Base(target), err.Error()))
				v.reveal()
				return
			}
			v.setCatalog(cat, i)
		})
	}()
}

func (v *viewer) setCatalog(cat *catalog.Catalog, i int) {
	v.cat = cat
	v.cache, v.cacheOrder = map[string]picture.Picture{}, nil
	v.cx.Batch(func() {
		v.count.Set(len(cat.Items))
		v.openErr.Set("")
		if len(cat.Items) == 0 {
			v.openErr.Set(locales.FolderEmpty(v.cx, cat.Title()))
			v.index.Set(-1)
			v.commit(shown{}, v.front.Get())
			return
		}
		if cat.Archive != "" {
			desktop.AddRecentFile(v.cx, cat.Archive)
		}
		v.show(i)
	})
}

// show loads the item at i and shows it once it is ready; a later show
// supersedes a load still running.
func (v *viewer) show(i int) {
	if v.cat == nil || i < 0 || i >= len(v.cat.Items) {
		return
	}
	v.index.Set(i)
	item := v.cat.Items[i]
	v.loadSeq++
	seq := v.loadSeq
	if pic, ok := v.cache[item.Path]; ok {
		v.present(item, pic, nil)
		v.prefetch(i)
		return
	}
	cat := v.cat
	go func() {
		pic, err := readPicture(cat, item)
		v.cx.Post(func() {
			if seq != v.loadSeq {
				if err == nil && cat == v.cat {
					v.remember(item.Path, pic)
				}
				return
			}
			if err == nil {
				v.remember(item.Path, pic)
			}
			v.present(item, pic, err)
			v.prefetch(i)
		})
	}()
}

func readPicture(cat *catalog.Catalog, item catalog.Item) (picture.Picture, error) {
	data, err := cat.Read(item)
	if err != nil {
		return picture.Picture{}, err
	}
	return picture.Load(data, item.Name)
}

// prefetch reads the images on either side of i ahead of time, so moving
// to them skips the disk or archive.
func (v *viewer) prefetch(i int) {
	cfg := v.cfg.Get()
	cat := v.cat
	for _, d := range []int{1, -1} {
		j := cat.Step(i, d, cfg.Loop)
		if j < 0 || j == i {
			continue
		}
		item := cat.Items[j]
		if _, ok := v.cache[item.Path]; ok {
			continue
		}
		go func() {
			pic, err := readPicture(cat, item)
			if err != nil {
				return
			}
			v.cx.Post(func() {
				if cat == v.cat {
					v.remember(item.Path, pic)
				}
			})
		}()
	}
}

// remember keeps pic in the read-ahead cache, dropping the oldest entries
// past its bounds.
func (v *viewer) remember(path string, pic picture.Picture) {
	if _, ok := v.cache[path]; ok {
		return
	}
	v.cache[path] = pic
	v.cacheOrder = append(v.cacheOrder, path)
	total := 0
	for _, p := range v.cache {
		total += pictureBytes(p)
	}
	for len(v.cacheOrder) > 1 && (len(v.cacheOrder) > cacheEntries || total > cacheBytes) {
		old := v.cacheOrder[0]
		v.cacheOrder = v.cacheOrder[1:]
		total -= pictureBytes(v.cache[old])
		delete(v.cache, old)
	}
}

func pictureBytes(p picture.Picture) int {
	if p.Pixels != nil {
		return len(p.Pixels.Pix)
	}
	return len(p.Data)
}

// present puts a read image on screen: at once when there is nothing to
// decode or a view holds it decoded, else once the back view decoded it
// (loaded), the current image staying until then.
func (v *viewer) present(item catalog.Item, pic picture.Picture, err error) {
	s := shown{item: item, pic: pic, err: err, src: core.ImageSource{Data: pic.Data, Pixels: pic.Pixels}}
	front := v.front.Get()
	back := 1 - front
	switch {
	case err == nil && pic.Pixels == nil && sameData(s.src, v.slots[front].Get()):
		v.commit(s, front)
	case err != nil || pic.Pixels != nil || sameData(s.src, v.slots[back].Get()):
		// nsgo shows pixels without a blank, as an error shows nothing.
		v.commit(s, back)
	default:
		v.next = s
		v.slots[back].Set(s.src)
	}
}

// sameData reports whether a and b hold the same encoded bytes, which an
// image view then keeps decoded.
func sameData(a, b core.ImageSource) bool {
	return len(a.Data) > 0 && len(a.Data) == len(b.Data) && &a.Data[0] == &b.Data[0]
}

// loaded follows image view k decoding its source, or failing to: when
// it is the back view's next image, that image replaces the shown one.
func (v *viewer) loaded(k int, err error) {
	if k == v.front.Get() || v.next.src.Empty() {
		return
	}
	s := v.next
	if s.err == nil {
		s.err = err
	}
	v.commit(s, k)
}

// commit shows s in image view k with a fresh view: upright as its EXIF
// says and fitted as the preferences say.
func (v *viewer) commit(s shown, k int) {
	v.next = shown{}
	rot, mir := picture.OrientationTransform(s.pic.Orientation)
	v.cx.Batch(func() {
		v.slots[k].Set(s.src)
		v.front.Set(k)
		v.img.Set(s)
		v.rotate.Set(rot)
		v.mirror.Set(mir)
		v.offset.Set(core.Point{})
		v.fit.Set(v.cfg.Get().Fit)
		v.scale.Set(1)
	})
	v.reveal()
	v.trimMemory()
}

// trimMemory hands the memory decoding left behind back to Windows once
// browsing pauses for a second. Decoding a photo leaves garbage several
// times its pixels, which the Go runtime would otherwise keep reserved
// for the next decode: measured with 24 MP JPEGs, after browsing ten of
// them the process kept about 1 GB.
func (v *viewer) trimMemory() {
	v.trimSeq++
	seq := v.trimSeq
	time.AfterFunc(time.Second, func() {
		v.cx.Post(func() {
			if seq == v.trimSeq {
				go debug.FreeOSMemory()
			}
		})
	})
}

// displaySize is the image's size in device pixels as shown, after its
// rotation, at scale 1.
func (v *viewer) displaySize() (w, h float64) {
	p := v.img.Get().pic
	w, h = float64(p.Width), float64(p.Height)
	if v.rotate.Get()%180 != 0 {
		w, h = h, w
	}
	return w, h
}

// ratio is the screen's device pixels per point.
func (v *viewer) ratio() float64 {
	if r := float64(v.cx.Env().PixelRatio()); r > 0 {
		return r
	}
	return 1
}

// fitScale is the scale that fits the image in the viewport under mode.
func (v *viewer) fitScale(mode settings.Fit) float64 {
	w, h := v.displaySize()
	vp := v.viewport.Get()
	if w == 0 || h == 0 || vp.Width == 0 || vp.Height == 0 {
		return 1
	}
	r := v.ratio()
	s := math.Min(float64(vp.Width)*r/w, float64(vp.Height)*r/h)
	switch mode {
	case settings.FitShrink:
		return math.Min(s, 1)
	case settings.FitOriginal:
		return 1
	}
	return s
}

// currentScale is the scale on screen.
func (v *viewer) currentScale() float64 {
	if mode := v.fit.Get(); mode != "" {
		return v.fitScale(mode)
	}
	return v.scale.Get()
}

// clampOffset keeps the image's center where the image covers the
// viewport: centered along an axis it fits in, never pulled past its
// edges along one it overflows.
func (v *viewer) clampOffset(o core.Point, scale float64) core.Point {
	w, h := v.displaySize()
	r := v.ratio()
	vp := v.viewport.Get()
	limit := func(o core.Points, size float64, view core.Points) core.Points {
		over := (size*scale/r - float64(view)) / 2
		if over <= 0 {
			return 0
		}
		return core.Points(math.Max(-over, math.Min(over, float64(o))))
	}
	return core.Point{X: limit(o.X, w, vp.Width), Y: limit(o.Y, h, vp.Height)}
}

// zoomTo sets the scale, keeping the image point under anchor (viewport
// coordinates) in place.
func (v *viewer) zoomTo(scale float64, anchor core.Point) {
	if v.img.Get().pic.Width == 0 {
		return
	}
	old := v.currentScale()
	scale = math.Max(minScale, math.Min(maxScale, scale))
	vp := v.viewport.Get()
	// The anchor relative to the viewport's center, and the image's
	// center relative to it, scale together.
	ax, ay := float64(anchor.X-vp.Width/2), float64(anchor.Y-vp.Height/2)
	o := v.clampOffset(v.offset.Get(), old)
	k := scale / old
	next := core.Point{X: core.Points(ax - (ax-float64(o.X))*k), Y: core.Points(ay - (ay-float64(o.Y))*k)}
	v.cx.Batch(func() {
		v.fit.Set("")
		v.scale.Set(scale)
		v.offset.Set(v.clampOffset(next, scale))
	})
}

func (v *viewer) center() core.Point {
	vp := v.viewport.Get()
	return core.Point{X: vp.Width / 2, Y: vp.Height / 2}
}

// step moves delta images, as next and previous do.
func (v *viewer) step(delta int) {
	if v.cat == nil || len(v.cat.Items) == 0 {
		return
	}
	j := v.cat.Step(v.index.Get(), delta, v.cfg.Get().Loop)
	if j != v.index.Get() {
		v.show(j)
	}
}

// run carries out action; anchor is where the pointer was for zooms, nil
// for the viewport's center.
func (v *viewer) run(action settings.Action, anchor *core.Point) {
	cx := v.cx
	at := v.center()
	if anchor != nil {
		at = *anchor
	}
	step := v.cfg.Get().ZoomStep
	switch action {
	case settings.Next:
		v.step(1)
	case settings.Previous:
		v.step(-1)
	case settings.First:
		v.show(0)
	case settings.Last:
		if v.cat != nil {
			v.show(len(v.cat.Items) - 1)
		}
	case settings.ZoomIn:
		v.zoomTo(v.currentScale()*step, at)
	case settings.ZoomOut:
		v.zoomTo(v.currentScale()/step, at)
	case settings.ZoomFit:
		mode := v.cfg.Get().Fit
		if mode == settings.FitOriginal {
			mode = settings.FitShrink
		}
		cx.Batch(func() { v.fit.Set(mode); v.offset.Set(core.Point{}) })
	case settings.ZoomOriginal:
		v.zoomTo(1, at)
	case settings.RotateLeft, settings.RotateRight:
		d := 90
		if action == settings.RotateLeft {
			d = 270
		}
		v.transform(func(rot int, mir bool) (int, bool) { return rot + d, mir })
	case settings.FlipH:
		// Mirroring the screen after a rotation turns the rotation back.
		v.transform(func(rot int, mir bool) (int, bool) { return 360 - rot, !mir })
	case settings.FlipV:
		v.transform(func(rot int, mir bool) (int, bool) { return 540 - rot, !mir })
	case settings.FullScreen:
		desktop.ToggleFullScreen(cx)
	case settings.Escape:
		if desktop.FullScreen(cx) {
			desktop.ToggleFullScreen(cx)
		} else {
			winmouse.CloseQuietly()
			cx.Quit()
		}
	case settings.Close:
		// Quit rather than close the main window: nsgo v0.2.0 on Windows
		// hides the main window of an App without ending the process.
		winmouse.CloseQuietly()
		cx.Quit()
	case settings.Minimize:
		desktop.MinimizeWindow(cx)
	case settings.Open:
		v.pick()
	case settings.Reveal:
		if p := v.currentFile(); p != "" {
			desktop.RevealFile(cx, p, func(error) {})
		}
	case settings.Delete:
		v.delete()
	case settings.CopyPath:
		if p := v.currentFile(); p != "" {
			if v.cat.Archive != "" {
				p = filepath.Join(p, filepath.FromSlash(v.img.Get().item.Path))
			}
			cx.SetClipboard(p)
			v.flash(locales.CopiedPath(cx))
		}
	case settings.CopyImage:
		v.copyImage()
	case settings.Slideshow:
		on := !v.slideshow.Get()
		v.slideshow.Set(on)
		if on {
			v.flash(locales.SlideshowOn(cx))
		} else {
			v.flash(locales.SlideshowOff(cx))
		}
	case settings.Info:
		v.info.Set(!v.info.Get())
	case settings.AlwaysOnTop:
		v.update(func(c *settings.Config) { c.AlwaysOnTop = !c.AlwaysOnTop })
	case settings.TitleBar:
		v.update(func(c *settings.Config) { c.ShowTitleBar = !c.ShowTitleBar })
	case settings.Menu:
		desktop.PopupMenu(cx, v.menuItems(), v.pointer, nil)
	case settings.Preferences:
		openPreferences(v)
	}
}

// transform changes the rotation and mirroring, keeping the fit.
func (v *viewer) transform(fn func(rot int, mir bool) (int, bool)) {
	rot, mir := fn(v.rotate.Get(), v.mirror.Get())
	v.cx.Batch(func() {
		v.rotate.Set(((rot % 360) + 360) % 360)
		v.mirror.Set(mir)
		v.offset.Set(v.clampOffset(v.offset.Get(), v.currentScale()))
	})
}

// currentFile is the file on disk the shown image is in: the image or its
// archive; "" with nothing shown.
func (v *viewer) currentFile() string {
	if v.cat == nil || v.index.Get() < 0 {
		return ""
	}
	if v.cat.Archive != "" {
		return v.cat.Archive
	}
	return v.img.Get().item.Path
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
	if v.cat == nil || v.index.Get() < 0 {
		return
	}
	if v.cat.Archive != "" {
		v.flash(locales.ArchiveReadOnly(cx))
		return
	}
	item := v.img.Get().item
	trash := func() {
		desktop.TrashFile(cx, item.Path, func(_ string, err error) {
			if err != nil {
				v.flash(locales.DeleteFailed(cx, item.Name, err.Error()))
				return
			}
			i := v.cat.Index(item.Path)
			if i < 0 {
				return
			}
			delete(v.cache, item.Path)
			next := v.cat.Remove(i)
			v.count.Set(len(v.cat.Items))
			if next < 0 {
				v.index.Set(-1)
				v.commit(shown{}, v.front.Get())
				v.openErr.Set(locales.FolderEmpty(cx, v.cat.Title()))
				return
			}
			v.show(next)
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
	data := p.Data
	if p.Pixels != nil {
		var buf bytes.Buffer
		if png.Encode(&buf, p.Pixels) != nil {
			return
		}
		data = buf.Bytes()
	}
	if len(data) == 0 {
		return
	}
	v.cx.SetClipboardImage(data)
	v.flash(locales.CopiedImage(v.cx))
}

// flash shows text over the image for a moment.
func (v *viewer) flash(text string) {
	v.messageSeq++
	seq := v.messageSeq
	v.message.Set(text)
	time.AfterFunc(1800*time.Millisecond, func() {
		v.cx.Post(func() {
			if seq == v.messageSeq {
				v.message.Set("")
			}
		})
	})
}

// scheduleSlide moves to the next image after the slideshow's interval,
// counted again from each image shown.
func (v *viewer) scheduleSlide() {
	v.slideSeq++
	if !v.slideshow.Get() {
		return
	}
	v.img.Get()
	seq := v.slideSeq
	d := time.Duration(v.cfg.Get().SlideshowSeconds * float64(time.Second))
	time.AfterFunc(d, func() {
		v.cx.Post(func() {
			if seq == v.slideSeq {
				v.step(1)
			}
		})
	})
}

func (v *viewer) positionText() string {
	return locales.InfoPosition(v.cx, strconv.Itoa(v.index.Get()+1), strconv.Itoa(v.count.Get()))
}

func (v *viewer) updateTitle() {
	img := v.img.Get()
	if v.index.Get() < 0 || img.item.Name == "" {
		desktop.SetTitle(v.cx, locales.AppName(v.cx))
		return
	}
	name := img.item.Name
	if v.cat != nil && v.cat.Archive != "" {
		name = v.cat.Title() + " › " + name
	}
	desktop.SetTitle(v.cx, locales.Title(v.cx, name, v.positionText()))
}

// background is the viewport's color from the preferences.
func (v *viewer) background() core.Color {
	c, _ := settings.ParseColor(v.cfg.Get().Background)
	return core.RGB(c)
}

// infoText describes the shown image for the information overlay.
func (v *viewer) infoText() string {
	img := v.img.Get()
	if img.item.Name == "" {
		return ""
	}
	return img.item.Name + "\n" + v.positionText() + "   " +
		strconv.Itoa(img.pic.Width) + " × " + strconv.Itoa(img.pic.Height) + "   " +
		locales.InfoZoom(v.cx, strconv.Itoa(int(math.Round(v.currentScale()*100)))) + "   " +
		formatBytes(img.item.Size) + "   " + img.pic.Format
}

func formatBytes(n int64) string {
	switch {
	case n >= 1<<20:
		return strconv.FormatFloat(float64(n)/(1<<20), 'f', 1, 64) + " MB"
	case n >= 1<<10:
		return strconv.FormatFloat(float64(n)/(1<<10), 'f', 0, 64) + " KB"
	}
	return strconv.FormatInt(n, 10) + " B"
}

// imageStyle places the image: its unrotated box at the current scale,
// centered at the viewport's center plus the offset, then rotated and
// mirrored about its center.
func (v *viewer) imageStyle() core.Style {
	x, y, w, h := v.imageBox()
	sx := float32(1)
	if v.mirror.Get() {
		sx = -1
	}
	return core.Style{
		Position: core.Absolute,
		Left:     core.Pt(core.Points(x - w/2)),
		Top:      core.Pt(core.Points(y - h/2)),
		Width:    core.Pt(core.Points(w)), Height: core.Pt(core.Points(h)),
		Transform: core.Transform{Rotate: float32(v.rotate.Get()), ScaleX: sx, ScaleY: 1},
	}
}

// imageBox is the image's unrotated box in the viewport: its center and
// size in points.
func (v *viewer) imageBox() (x, y, w, h float64) {
	p := v.img.Get().pic
	vp := v.viewport.Get()
	pp := v.currentScale() / v.ratio() // points per image pixel
	// Clamping here keeps the image in place as the window resizes.
	o := v.clampOffset(v.offset.Get(), v.currentScale())
	return float64(vp.Width)/2 + float64(o.X), float64(vp.Height)/2 + float64(o.Y), float64(p.Width) * pp, float64(p.Height) * pp
}

func (v *viewer) render(cx *core.Scope) core.Node {
	overlayText := func(extra core.Style) core.Style {
		return core.Style{Color: core.RGB(0xffffff), Background: core.RGBA(0x000000, 0.55), Padding: core.Symmetric(6, 10),
			CornerRadius: 6, FontSize: 13, LineHeight: 19, PointerEvents: core.PointerEventsNone}.Merge(extra)
	}
	return core.View{
		TestID: "viewport",
		StyleFunc: func() core.Style {
			return core.Style{FlexGrow: 1, FlexShrink: 1, Overflow: core.Hidden, Background: v.background()}
		},
		OnLayout:      func(b core.Bounds) { v.viewport.Set(b.Size) },
		OnMouseMove:   func(e core.MouseEvent) { v.pointer = e.Position; v.updateHand() },
		OnWheel:       v.wheel,
		OnPan:         v.pan,
		OnPressDown:   func(e core.PressEvent) { v.downAt = e.Position },
		OnPress:       v.press,
		OnContextMenu: func(e core.ContextMenuEvent) { v.pointer = e.Position; v.gesture(settings.RightClick, &e.Position) },
		OnFileDrop: func(uris []string) {
			for _, u := range uris {
				if f, err := core.FileFromURL(u); err == nil && f.Path != "" {
					v.open(f.Path)
					return
				}
			}
		},
		Children: []core.Node{
			v.keyBindings(),
			v.imageView(0),
			v.imageView(1),
			// Without a title bar, a strip as tall as the hidden one moves the
			// window: Windows drags it, maximizes it on a double click and
			// opens the system menu on a right click. Its height is 0 in full
			// screen, where there is nothing to move.
			core.Show{
				When: func() bool { return !v.cfg.Get().ShowTitleBar },
				Then: func(cx *core.Scope) core.Node {
					return core.View{TestID: "drag", Desktop: core.DesktopViewProps{WindowDrag: core.WindowDragOn},
						StyleFunc: func() core.Style {
							return core.Style{Position: core.Absolute, Left: core.Pt(0), Right: core.Pt(0), Top: core.Pt(0),
								Height: core.Pt(desktop.TitleBar(cx).Top)}
						}}
				},
			},
			core.Show{
				When: func() bool { return v.index.Get() < 0 && v.openErr.Get() == "" },
				Then: v.renderEmpty,
			},
			core.Show{
				When: func() bool { return v.img.Get().err != nil || v.openErr.Get() != "" },
				Then: func(cx *core.Scope) core.Node {
					return core.View{Style: core.Style{Position: core.Absolute, Left: core.Pt(0), Right: core.Pt(0), Top: core.Pt(0), Bottom: core.Pt(0),
						AlignItems: core.AlignCenter, JustifyContent: core.JustifyCenter, PointerEvents: core.PointerEventsNone},
						Children: []core.Node{core.Text{TestID: "error", Style: overlayText(core.Style{}), ContentFunc: func() string {
							if e := v.openErr.Get(); e != "" {
								return e
							}
							img := v.img.Get()
							return locales.LoadFailed(cx, img.item.Name, loadError(cx, img.err))
						}}}}
				},
			},
			core.Show{
				When: func() bool { return v.info.Get() && v.img.Get().item.Name != "" },
				Then: func(cx *core.Scope) core.Node {
					return core.Text{TestID: "info", ContentFunc: v.infoText,
						Style: overlayText(core.Style{Position: core.Absolute, Left: core.Pt(12), Top: core.Pt(12)})}
				},
			},
			core.Show{
				When: func() bool { return v.message.Get() != "" },
				Then: func(cx *core.Scope) core.Node {
					return core.View{Style: core.Style{Position: core.Absolute, Left: core.Pt(0), Right: core.Pt(0), Bottom: core.Pt(24), AlignItems: core.AlignCenter, PointerEvents: core.PointerEventsNone},
						Children: []core.Node{core.Text{TestID: "message", ContentFunc: v.message.Get, Style: overlayText(core.Style{})}}}
				},
			},
		},
	}
}

// imageView is image view k: the shown image at front, else invisible
// while it decodes the next one.
func (v *viewer) imageView(k int) core.Node {
	return core.Image{TestID: "image-" + strconv.Itoa(k), SourceFunc: v.slots[k].Get,
		ResizeMode: core.Stretch, FadeDuration: time.Nanosecond,
		OnLoad:  func(core.Size) { v.loaded(k, nil) },
		OnError: func(err error) { v.loaded(k, err) },
		StyleFunc: func() core.Style {
			if v.front.Get() == k {
				return v.imageStyle()
			}
			return core.Style{Position: core.Absolute, Left: core.Pt(0), Top: core.Pt(0), Width: core.Pt(1), Height: core.Pt(1),
				Opacity: core.Alpha(0), PointerEvents: core.PointerEventsNone}
		}}
}

func (v *viewer) renderEmpty(cx *core.Scope) core.Node {
	return core.View{
		Style: core.Style{Position: core.Absolute, Left: core.Pt(0), Right: core.Pt(0), Top: core.Pt(0), Bottom: core.Pt(0),
			AlignItems: core.AlignCenter, JustifyContent: core.JustifyCenter, Gap: 14},
		Children: []core.Node{
			core.Text{Content: locales.EmptyTitle(cx), Style: core.Style{Color: core.RGB(0xe5e5e5), FontSize: 20, LineHeight: 28}},
			core.Text{Content: locales.EmptyHint(cx), Style: core.Style{Color: core.RGB(0x9a9a9a), FontSize: 13, LineHeight: 18}},
			ui.Button{Text: locales.OpenButton(cx), TestID: "open", OnPress: v.pick},
		},
	}
}

// wheel turns wheel notches, and touchpad scrolls in steps, into the
// gestures bound to them.
func (v *viewer) wheel(e core.WheelEvent) {
	v.pointer = e.Position
	v.updateHand()
	dy := e.Delta.Y
	if e.Touchpad {
		v.touchpad += dy
		if v.touchpad > -touchpadStep && v.touchpad < touchpadStep {
			return
		}
		dy, v.touchpad = v.touchpad, 0
	}
	if dy == 0 {
		return
	}
	// Delta is how far the content moves: turning the wheel away from the
	// user scrolls content down, so negative is "wheel down".
	down := dy < 0
	var g settings.Gesture
	switch m := e.Modifiers; {
	case m.Control:
		g = pick(down, settings.CtrlWheelDown, settings.CtrlWheelUp)
	case m.Shift:
		g = pick(down, settings.ShiftWheelDown, settings.ShiftWheelUp)
	case m.Alt:
		g = pick(down, settings.AltWheelDown, settings.AltWheelUp)
	default:
		g = pick(down, settings.WheelDown, settings.WheelUp)
	}
	v.gesture(g, &e.Position)
}

func pick[T any](cond bool, a, b T) T {
	if cond {
		return a
	}
	return b
}

// pan drags the image while it overflows the viewport. A drag that starts
// on the background beside the image moves the window instead, as
// Honeyview's does; clicks there keep their bindings, since only a drag
// past the pan's threshold starts the move.
func (v *viewer) pan(e core.PanEvent) {
	switch e.Phase {
	case core.GestureBegan:
		v.movingWin = !v.onImage(v.downAt) && !desktop.FullScreen(v.cx)
		if v.movingWin {
			winmouse.DragWindow()
			return
		}
		v.panning = v.pannable()
		v.updateHand()
		v.panStart = v.clampOffset(v.offset.Get(), v.currentScale())
		fallthrough
	case core.GestureChanged:
		if v.movingWin {
			return
		}
		next := core.Point{X: v.panStart.X + e.Translation.X, Y: v.panStart.Y + e.Translation.Y}
		v.offset.Set(v.clampOffset(next, v.currentScale()))
	case core.GestureEnded, core.GestureCancelled:
		v.panning = false
		v.updateHand()
	}
}

// pannable reports whether the image overflows the viewport, so a drag
// moves it.
func (v *viewer) pannable() bool {
	if v.img.Get().src.Empty() {
		return false
	}
	w, h := v.displaySize()
	k := v.currentScale() / v.ratio()
	vp := v.viewport.Get()
	return w*k > float64(vp.Width)+0.5 || h*k > float64(vp.Height)+0.5
}

// updateHand shows the open hand over an image that can be dragged and
// the closed one while it is dragged, as qView does. As an effect it
// follows zooms, turns and new images under a still pointer too.
func (v *viewer) updateHand() {
	h := winmouse.NoHand
	switch {
	case v.panning:
		h = winmouse.ClosedHand
	case v.pannable() && v.onImage(v.pointer):
		h = winmouse.OpenHand
	}
	winmouse.SetHand(h)
}

// onImage reports whether p, in the viewport, is on the shown image.
func (v *viewer) onImage(p core.Point) bool {
	if v.img.Get().src.Empty() {
		return false
	}
	x, y, w, h := v.imageBox()
	if v.rotate.Get()%180 != 0 {
		w, h = h, w // a quarter turn about the center swaps the box's sides
	}
	return math.Abs(float64(p.X)-x) <= w/2 && math.Abs(float64(p.Y)-y) <= h/2
}

func (v *viewer) press(e core.PressEvent) {
	v.pointer = e.Position
	if e.Clicks == 2 {
		v.gesture(settings.LeftDoubleClick, &e.Position)
	}
}

// gesture runs the action bound to g.
func (v *viewer) gesture(g settings.Gesture, at *core.Point) {
	if a := v.cfg.Get().Mouse[g]; a != "" && a != settings.None {
		v.run(a, at)
	}
}

// loadError says why an image cannot be shown; for a format Windows has
// no codec for, which Microsoft Store extension to install.
func loadError(cx *core.Scope, err error) string {
	var missing picture.MissingCodecError
	if !errors.As(err, &missing) {
		return err.Error()
	}
	switch missing.Codec {
	case picture.CodecHEIF:
		return locales.CodecHEIF(cx)
	case picture.CodecAV1:
		return locales.CodecAV1(cx)
	case picture.CodecJXL:
		return locales.CodecJXL(cx)
	}
	return locales.CodecRaw(cx)
}
