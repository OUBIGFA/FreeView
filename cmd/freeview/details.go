package main

// Metadata is read only while the information overlay is open.
import (
	"strconv"
	"strings"

	"github.com/nsgo-dev/nsgo/core"

	"github.com/OUBIGFA/FreeView/cmd/freeview/locales"
	"github.com/OUBIGFA/FreeView/internal/livephoto"
	"github.com/OUBIGFA/FreeView/internal/meta"
)

// wantDetails reads the shown image's details once the information
// overlay shows it.
func (v *viewer) wantDetails() {
	e := &v.extras
	if !v.info.Get() || v.img.Get().src.Empty() || e.metaSeq == e.seq {
		return
	}
	e.metaSeq = e.seq
	seq, s := e.seq, v.img.Get()
	cat := s.cat
	v.background(func() {
		data := s.pic.Data
		if data == nil || s.pic.Format == "svg" {
			var err error
			if data, err = cat.Read(s.item); err != nil {
				return
			}
		}
		d := details{info: meta.Read(data), seq: seq}
		if livephoto.IsLIVP(s.item.Name) {
			d.live = true
		} else {
			_, d.live = livephoto.FindVideo(data)
		}
		v.cx.Post(func() {
			if seq == e.seq {
				e.meta.Set(d)
			}
		})
	})
}

// detailsText is the overlay's lines past the first: the details read so
// far and the read-ahead cache's counts.
func (v *viewer) detailsText() string {
	cx, e := v.cx, &v.extras
	var b strings.Builder
	if d := e.meta.Get(); d.seq == e.seq {
		for _, f := range d.info.Fields {
			b.WriteString("\n" + detailLabel(cx, f.Key) + "  " + f.Value)
		}
		if d.live {
			b.WriteString("\n" + locales.InfoLive(cx))
		}
		if p := d.info.Prompt; p != "" {
			if r := []rune(p); len(r) > promptShown {
				p = string(r[:promptShown]) + "…"
			}
			b.WriteString("\n" + locales.InfoPrompt(cx) + "\n" + p)
		}
	}
	st := v.stats
	b.WriteString("\n" + locales.InfoCache(cx, strconv.Itoa(st.hits), strconv.Itoa(st.reads), strconv.Itoa(st.evictions)))
	return b.String()
}

func detailLabel(cx *core.Scope, key string) string {
	switch key {
	case meta.Camera:
		return locales.InfoCamera(cx)
	case meta.Lens:
		return locales.InfoLens(cx)
	case meta.Exposure:
		return locales.InfoExposure(cx)
	case meta.Taken:
		return locales.InfoTaken(cx)
	case meta.GPS:
		return locales.InfoGPS(cx)
	case meta.Software:
		return locales.InfoSoftware(cx)
	case meta.Comment:
		return locales.InfoComment(cx)
	}
	return key
}
