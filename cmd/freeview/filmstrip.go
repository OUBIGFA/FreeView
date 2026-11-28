package main

// The thumbnail strip requests only visible entries and owns its bounded cache.
import (
	"bytes"
	"errors"
	"image"
	"image/draw"
	"strconv"

	"github.com/nsgo-dev/nsgo/core"

	"github.com/OUBIGFA/FreeView/internal/catalog"
	"github.com/OUBIGFA/FreeView/internal/thumbs"
)

// hoverStrip shows the strip of thumbnails while the pointer is near the
// bottom of the viewport, when the strip is on.
func (v *viewer) hoverStrip(p core.Point) {
	on := v.cfg.Get().Filmstrip && v.count.Get() > 1 && float64(p.Y) > float64(v.viewport.Get().Height)-stripHeight-12
	if on != v.extras.stripOn.GetUntracked() {
		v.extras.stripOn.Set(on)
	}
}

// stripRange is the items the strip shows: as many as fit, around the
// shown one.
func (v *viewer) stripRange() (from, to int) {
	n := max(1, int(v.viewport.Get().Width)/stripCell)
	count, i := v.count.Get(), v.index.Get()
	from = max(0, min(i-n/2, count-n))
	return from, min(count, from+n)
}

func (v *viewer) filmstrip(cx *core.Scope) core.Node {
	e := &v.extras
	return core.View{TestID: "filmstrip",
		Style: core.Style{Position: core.Absolute, Left: core.Pt(0), Right: core.Pt(0), Bottom: core.Pt(0), Height: core.Pt(stripHeight),
			Direction: core.Row, JustifyContent: core.JustifyCenter, AlignItems: core.AlignCenter, Gap: 8,
			Background: core.RGBA(0x000000, 0.6)},
		Children: []core.Node{core.Dynamic(func(cx *core.Scope) core.Node {
			from, to := v.stripRange()
			cat := v.cat
			var cells []core.Node
			for j := from; j < to && cat != nil && j < len(cat.Items); j++ {
				item := cat.Items[j]
				v.wantThumb(cat, item)
				cells = append(cells, core.View{TestID: "strip-" + strconv.Itoa(j), OnPress: func(core.PressEvent) { v.show(j) },
					StyleFunc: func() core.Style {
						border := core.RGBA(0xffffff, 0.15)
						if v.index.Get() == j {
							border = core.RGB(0xffffff)
						}
						return core.Style{Width: core.Pt(stripCell - 8), Height: core.Pt(stripCell - 8), BorderWidth: 2, BorderColor: border,
							AlignItems: core.AlignCenter, JustifyContent: core.JustifyCenter, Overflow: core.Hidden}
					},
					Children: []core.Node{core.Image{ResizeMode: core.Contain, SourceFunc: func() core.ImageSource {
						e.thumbsAdded.Get()
						if t := e.thumbs[thumbKey(cat, item)]; t != nil {
							return core.ImageSource{Pixels: t}
						}
						return core.ImageSource{}
					}, Style: core.Style{Width: core.Pct(100), Height: core.Pct(100), PointerEvents: core.PointerEventsNone}}}})
			}
			return core.Group(cells)
		})}}
}

func thumbKey(cat *catalog.Catalog, item catalog.Item) string { return cat.Archive + "|" + item.Path }

// wantThumb makes item's thumbnail on a heavy worker unless it is made or
// being made. Files get the shell's; archive entries are decoded here.
func (v *viewer) wantThumb(cat *catalog.Catalog, item catalog.Item) {
	e := &v.extras
	key := thumbKey(cat, item)
	if e.thumbs[key] != nil || e.thumbAsked[key] {
		return
	}
	e.thumbAsked[key] = true
	ctx := e.thumbWork
	background(ctx, func() {
		img, err := makeThumb(cat, item)
		v.cx.Post(func() {
			if ctx.Err() != nil || !e.thumbAsked[key] {
				return
			}
			delete(e.thumbAsked, key)
			if err != nil || img == nil {
				return
			}
			e.thumbs[key] = img
			e.thumbOrder = append(e.thumbOrder, key)
			if len(e.thumbOrder) > thumbCache {
				delete(e.thumbs, e.thumbOrder[0])
				e.thumbOrder = e.thumbOrder[1:]
			}
			e.thumbsAdded.Update(func(n int) int { return n + 1 })
		})
	})
}

func makeThumb(cat *catalog.Catalog, item catalog.Item) (*image.RGBA, error) {
	if cat.Archive == "" {
		if img, err := thumbs.File(item.Path, thumbSize); err == nil {
			return img, nil
		}
	}
	pic, err := readPicture(cat, item)
	if err != nil {
		return nil, err
	}
	if pic.Pixels != nil {
		return thumbs.Image(pic.Pixels, thumbSize), nil
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(pic.Data))
	if err != nil {
		return nil, err
	}
	if int64(cfg.Width)*int64(cfg.Height) > 1<<28 {
		return nil, errors.New("too large for a thumbnail")
	}
	m, _, err := image.Decode(bytes.NewReader(pic.Data))
	if err != nil {
		return nil, err
	}
	rgba, ok := m.(*image.RGBA)
	if !ok {
		rgba = image.NewRGBA(m.Bounds())
		draw.Draw(rgba, rgba.Rect, m, m.Bounds().Min, draw.Src)
	}
	return thumbs.Image(rgba, thumbSize), nil
}
