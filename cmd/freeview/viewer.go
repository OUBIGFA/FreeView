package main

import (
	"errors"
	"image"
	"io/fs"
	"math"
	"strconv"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/desktop"
	"github.com/nsgo-dev/nsgo/ui"

	"github.com/OUBIGFA/FreeView/assets/icons/solar"
	"github.com/OUBIGFA/FreeView/cmd/freeview/locales"
	"github.com/OUBIGFA/FreeView/internal/catalog"
	"github.com/OUBIGFA/FreeView/internal/picture"
	"github.com/OUBIGFA/FreeView/internal/settings"
	"github.com/OUBIGFA/FreeView/internal/winmouse"
)

const (
	minScale = 0.01
	maxScale = 64
	// cacheEntries bounds the images kept read ahead: the shown one, one
	// on either side and the last few left, so stepping back is instant.
	cacheEntries = 6
	// touchpadStep is how far a touchpad scrolls for one wheel gesture.
	touchpadStep = 120
	// revealWait is the longest the main window stays hidden waiting for
	// the image the app launched with.
	revealWait = time.Second
	// hoverHide delays hiding the top title pill briefly so crossing the
	// drag strip edge does not make it flicker.
	hoverHide = 300 * time.Millisecond
)

// cacheBytes bounds the bytes the read-ahead cache holds: an eighth of
// the installed memory, between 256 MiB and 2 GiB. Decoded pixels (BMP,
// TIFF and the WIC formats) take far more than file bytes: a 24 MP RAW is
// 96 MB of them.
var cacheBytes = func() int {
	return int(min(max(physicalMemory()/8, 256<<20), 2<<30))
}()

// cacheStats counts how the read-ahead cache served images, which the
// information overlay shows to explain a slow step.
type cacheStats struct{ hits, reads, evictions int }

// launching is whether the command line names a file to open, as nsgo
// hands to OnOpenFiles: the main window then waits for it to show.
var launching bool

// shown is the image on screen, or the error loading it.
type shown struct {
	cat  *catalog.Catalog // catalog that owns the displayed file, even while a new folder loads
	seq  uint64           // selection that requested this decode
	item catalog.Item
	pic  picture.Picture
	src  core.ImageSource
	err  error
}

// viewer owns FreeView's state. Its fields are used on the engine's
// goroutine only; loads run on goroutines and come back through Post.
type viewer struct {
	cx          *core.Scope
	cfgPath     string
	cfg         *core.Signal[settings.Config]
	configSaved <-chan struct{} // completion of the last ordered settings write

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
	topHover  *core.Signal[bool]
	menuOpen  *core.Signal[bool]
	menuAt    *core.Signal[core.Point]

	pointer    core.Point // last pointer position in the viewport
	downAt     core.Point // where the left button last went down
	panStart   core.Point
	movingWin  bool // the current drag moves the window, not the image
	panning    bool // the current drag moves the image
	touchpad   core.Points
	loadSeq    uint64
	openSeq    uint64
	reads      map[string]*pictureRead
	readSlots  chan struct{}
	ready      [2]bool
	messageSeq uint64
	hoverSeq   uint64
	slideSeq   uint64
	trimSeq    uint64
	revealed   bool
	cache      map[string]picture.Picture
	cacheOrder []string
	stats      cacheStats
	extras     extras // the features off the fast path (extras.go)
}

func newViewer(cx *core.Scope) *viewer {
	v := &viewer{cx: cx, cache: map[string]picture.Picture{}, reads: map[string]*pictureRead{}, readSlots: make(chan struct{}, 2)}
	cx.Cleanup(func() { v.cancelReads(); v.cancelExtras(); v.extras.thumbCancel() })
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
	v.topHover = core.NewSignal(cx, false)
	v.menuOpen = core.NewSignal(cx, false)
	v.menuAt = core.NewSignal(cx, core.Point{})
	v.initExtras(cx)
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
		cfg := v.cfg.Get()
		if (cfg.OpenMaximized || cfg.RememberWindow && frames.maximized("main")) && !desktop.Maximized(v.cx) {
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

// transform changes the rotation and mirroring, keeping the fit.
func (v *viewer) transform(fn func(rot int, mir bool) (int, bool)) {
	rot, mir := fn(v.rotate.Get(), v.mirror.Get())
	v.cx.Batch(func() {
		v.rotate.Set(((rot % 360) + 360) % 360)
		v.mirror.Set(mir)
		v.offset.Set(v.clampOffset(v.offset.Get(), v.currentScale()))
	})
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

func (v *viewer) titleText() string {
	img := v.img.Get()
	if v.index.Get() < 0 || img.item.Name == "" {
		return locales.AppName(v.cx)
	}
	name := img.item.Name
	if v.cat != nil && v.cat.Archive != "" {
		name = v.cat.Title() + " › " + name
	}
	return locales.Title(v.cx, name, v.positionText())
}

func (v *viewer) updateTitle() {
	desktop.SetTitle(v.cx, v.titleText())
}

func (v *viewer) hoverTop(over bool) {
	v.hoverSeq++
	if over {
		v.topHover.Set(true)
		return
	}
	seq := v.hoverSeq
	time.AfterFunc(hoverHide, func() {
		v.cx.Post(func() {
			if seq == v.hoverSeq {
				v.topHover.Set(false)
			}
		})
	})
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
		formatBytes(img.item.Size) + "   " + img.pic.Format + v.detailsText()
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
		return overlayStyle().Merge(core.Style{PointerEvents: core.PointerEventsNone}).Merge(extra)
	}
	return core.View{
		TestID: "viewport",
		StyleFunc: func() core.Style {
			return core.Style{FlexGrow: 1, FlexShrink: 1, Overflow: core.Hidden, Background: core.RGB(viewportColor)}
		},
		OnLayout:      func(b core.Bounds) { v.viewport.Set(b.Size) },
		OnMouseMove:   func(e core.MouseEvent) { v.pointer = e.Position; v.updateHand(); v.hoverStrip(e.Position) },
		OnMouseLeave:  func(core.MouseEvent) { v.extras.stripOn.Set(false) },
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
			core.Func(v.contextMenu),
			v.imageView(0),
			v.imageView(1),
			v.smoothView(),
			core.Show{When: func() bool { return v.navShown() && !v.img.Get().src.Empty() }, Then: v.navigator},
			core.Show{When: func() bool { return v.cfg.Get().Filmstrip && v.extras.stripOn.Get() && v.cat != nil }, Then: v.filmstrip},
			core.Show{When: func() bool { return v.extras.renaming.Get() && v.img.Get().item.Path != "" }, Then: v.renameBox},
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
						Padding: core.All(32), AlignItems: core.AlignCenter, JustifyContent: core.JustifyCenter, PointerEvents: core.PointerEventsNone},
						Children: []core.Node{core.Text{TestID: "error", Style: overlayText(core.Style{MaxWidth: core.Pct(100)}), ContentFunc: func() string {
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
					return core.Scroll{TestID: "info-panel", Style: core.Style{Position: core.Absolute, Left: core.Pt(16), Top: core.Pt(48),
						MaxWidth: core.Pct(72), MaxHeight: core.Pct(64), CornerRadius: radiusPanel},
						Children: []core.Node{core.Text{TestID: "info", ContentFunc: v.infoText, Style: overlayText(core.Style{MaxWidth: core.Pct(100)})}}}
				},
			},
			core.Show{
				When: func() bool {
					c := v.cfg.Get()
					return !c.ShowTitleBar && c.HoverTitle && v.topHover.Get() && v.img.Get().item.Name != ""
				},
				Then: func(cx *core.Scope) core.Node {
					return core.View{TestID: "hover-title",
						StyleFunc: func() core.Style {
							align := core.AlignEnd
							switch v.cfg.Get().HoverTitlePlace {
							case settings.TitleLeft:
								align = core.AlignStart
							case settings.TitleCenter:
								align = core.AlignCenter
							}
							return core.Style{Position: core.Absolute, Left: core.Pt(0), Right: core.Pt(0), Top: core.Pt(0),
								MinHeight: core.Pt(desktop.TitleBar(cx).Top), Padding: core.Symmetric(4, 16),
								AlignItems: align, JustifyContent: core.JustifyCenter, PointerEvents: core.PointerEventsNone}
						},
						Children: []core.Node{core.Text{TestID: "hover-title-text", ContentFunc: v.titleText,
							StyleFunc: func() core.Style {
								align := core.TextAlignRight
								switch v.cfg.Get().HoverTitlePlace {
								case settings.TitleLeft:
									align = core.TextAlignLeft
								case settings.TitleCenter:
									align = core.TextAlignCenter
								}
								return overlayText(core.Style{MaxWidth: core.Pct(100), TextAlign: align, Padding: core.Symmetric(4, 12), FontSize: textSmall})
							}}}}
				},
			},
			core.Show{
				When: func() bool { return v.message.Get() != "" },
				Then: func(cx *core.Scope) core.Node {
					return core.View{StyleFunc: func() core.Style {
						return core.Style{Position: core.Absolute, Left: core.Pt(16), Right: core.Pt(16), Bottom: core.Pt(v.bottomOverlayInset()), AlignItems: core.AlignCenter, PointerEvents: core.PointerEventsNone}
					},
						Children: []core.Node{core.Text{TestID: "message", ContentFunc: v.message.Get, Style: overlayText(core.Style{MaxWidth: core.Pct(100)})}}}
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
				// Under the resampled view the image is hidden, so a
				// transparent one does not show through twice.
				return v.imageStyle().Merge(core.Style{Opacity: core.Alpha(pick[float32](v.smoothShown(), 0, 1))})
			}
			return hiddenStyle
		}}
}

func (v *viewer) renderEmpty(cx *core.Scope) core.Node {
	cx.SetSubtreeAppearance(core.AppearanceDark)
	return core.View{TestID: "empty",
		Style: core.Style{Position: core.Absolute, Left: core.Pt(0), Right: core.Pt(0), Top: core.Pt(0), Bottom: core.Pt(0),
			Padding: core.All(32), AlignItems: core.AlignCenter, JustifyContent: core.JustifyCenter, Gap: 24},
		Children: []core.Node{
			ui.Icon(solar.Gallery, 48, ui.Fixed(core.RGB(overlayMutedColor))),
			core.Text{Content: locales.EmptyTitle(cx), Style: core.Style{Color: core.RGB(overlayTextColor),
				FontSize: textHeading, FontWeight: ui.Semibold, LineHeight: 32, TextAlign: core.TextAlignCenter, MaxWidth: core.Pct(100)}},
			core.View{Style: core.Style{Direction: core.Row, Gap: 12}, Children: []core.Node{
				ui.Button{Text: locales.OpenButton(cx), TestID: "open", OnPress: v.pick, Icon: solar.FolderOpen,
					TextStyle: core.Style{FontSize: textBody}, Style: buttonStyle().Merge(core.Style{MinWidth: core.Pt(120)})},
				ui.Button{Text: locales.PrefsTitle(cx), TestID: "empty-settings", Variant: ui.ButtonGhost,
					TextStyle: core.Style{FontSize: textBody}, Style: buttonStyle(), OnPress: func() { openPreferences(v) }},
			}},
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

// pan moves the window, as Honeyview's drag does, unless the image
// overflows the viewport and the drag starts on it: that drag moves the
// image, and only the background beside it moves the window. Clicks keep
// their bindings, since only a drag past the pan's threshold starts either.
func (v *viewer) pan(e core.PanEvent) {
	switch e.Phase {
	case core.GestureBegan:
		v.movingWin = !(v.pannable() && v.onImage(v.downAt)) && !desktop.FullScreen(v.cx)
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
	// Every click at an edge moves, a quick second one too, so clicking
	// through images is not taken for a double click.
	if v.cfg.Get().EdgeClicks {
		if a := v.edgeAction(e.Position); a != settings.None {
			v.run(a, nil)
			return
		}
	}
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

// loadError says why an image cannot be shown, by kind: the file is
// gone, its format unknown, or decoding failed; for a format Windows has
// no codec for, which Microsoft Store extension to install.
func loadError(cx *core.Scope, err error) string {
	var missing picture.MissingCodecError
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return locales.LoadNotFound(cx)
	case errors.Is(err, image.ErrFormat):
		return locales.LoadUnsupported(cx)
	case !errors.As(err, &missing):
		return locales.LoadDecodeFailed(cx, err.Error())
	}
	// HEIF and JPEG XL have fallback decoders, so only these two are missed.
	if missing.Codec == picture.CodecAV1 {
		return locales.CodecAV1(cx)
	}
	return locales.CodecRaw(cx)
}
