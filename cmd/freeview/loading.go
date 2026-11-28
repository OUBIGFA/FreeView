package main

// Loading, read-ahead and the two display slots share one lifecycle here.
// Only the engine goroutine mutates this state; workers return through Post.
import (
	"context"
	"path/filepath"
	"runtime/debug"
	"slices"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/desktop"

	"github.com/OUBIGFA/FreeView/cmd/freeview/locales"
	"github.com/OUBIGFA/FreeView/internal/catalog"
	"github.com/OUBIGFA/FreeView/internal/picture"
)

// open shows target: an image in its folder, a folder or an archive.
func (v *viewer) open(target string) {
	v.openSeq++
	seq := v.openSeq
	cfg := v.cfg.Get()
	order := catalog.Order{By: string(cfg.Sort), Descending: cfg.SortDescending}
	go func() {
		cat, i, err := catalog.Open(context.Background(), target, order)
		v.cx.Post(func() {
			if seq != v.openSeq {
				return
			}
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
	v.openSeq++
	v.loadSeq++
	v.cancelReads()
	v.extras.thumbCancel()
	v.extras.thumbWork, v.extras.thumbCancel = context.WithCancel(context.Background())
	v.extras.thumbAsked = map[string]bool{}
	v.next = shown{}
	v.slots[1-v.front.Get()].Set(core.ImageSource{})
	v.ready[1-v.front.Get()] = false
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
	v.next = shown{}
	// Cancel queued work outside the current image and its neighbors.
	// An already running codec finishes, but cannot change this selection.
	wanted := map[string]bool{item.Path: true}
	for _, d := range []int{-1, 1} {
		wanted[v.cat.Items[v.cat.Step(i, d, v.cfg.Get().Loop)].Path] = true
	}
	for path, job := range v.reads {
		if !wanted[path] {
			job.cancel()
			delete(v.reads, path)
		}
	}
	if pic, ok := v.cache[item.Path]; ok {
		v.stats.hits++
		v.present(item, pic, nil)
		v.prefetch(i)
		return
	}
	v.read(item, v.loadSeq)
}

// pictureRead unifies foreground and speculative reads. Navigation can
// promote an in-flight prefetch without reading or decoding the file twice.
type pictureRead struct {
	cancel    context.CancelFunc
	selection uint64 // 0 for a speculative read
}

func (v *viewer) cancelReads() {
	for path, job := range v.reads {
		job.cancel()
		delete(v.reads, path)
	}
}

func (v *viewer) read(item catalog.Item, selection uint64) {
	if job := v.reads[item.Path]; job != nil {
		if selection != 0 {
			job.selection = selection
		}
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	job := &pictureRead{cancel: cancel, selection: selection}
	v.reads[item.Path] = job
	cat := v.cat
	v.stats.reads++
	go func() {
		defer cancel()
		select {
		case v.readSlots <- struct{}{}:
		case <-ctx.Done():
			return
		}
		defer func() { <-v.readSlots }()
		if ctx.Err() != nil {
			return
		}
		pic, err := readPicture(cat, item)
		v.cx.Post(func() {
			if cat != v.cat || v.reads[item.Path] != job {
				return
			}
			delete(v.reads, item.Path)
			if err == nil {
				v.remember(item.Path, pic)
			}
			if job.selection != 0 && job.selection == v.loadSeq {
				v.present(item, pic, err)
				v.prefetch(v.index.Get())
			}
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
// to them skips the disk or archive. An image taking over half the cache
// is not read ahead of: its neighbors would push it out.
func (v *viewer) prefetch(i int) {
	cfg := v.cfg.Get()
	cat := v.cat
	if cat == nil || i < 0 || i >= len(cat.Items) {
		return
	}
	if pic, ok := v.cache[cat.Items[i].Path]; ok && pictureBytes(pic) > cacheBytes/2 {
		return
	}
	for _, d := range []int{1, -1} {
		j := cat.Step(i, d, cfg.Loop)
		if j < 0 || j == i {
			continue
		}
		item := cat.Items[j]
		if _, ok := v.cache[item.Path]; ok {
			continue
		}
		v.read(item, 0)
	}
}

// remember keeps pic in the read-ahead cache, dropping the oldest entries
// past its bounds. The shown image and the one the back view decodes
// stay: the views hold them anyway, so dropping them frees nothing.
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
	keep := func(p string) bool { return p == path || p == v.img.Get().item.Path || p == v.next.item.Path }
	for i := 0; i < len(v.cacheOrder) && (len(v.cacheOrder) > cacheEntries || total > cacheBytes); {
		old := v.cacheOrder[i]
		if keep(old) {
			i++
			continue
		}
		v.cacheOrder = slices.Delete(v.cacheOrder, i, i+1)
		total -= pictureBytes(v.cache[old])
		delete(v.cache, old)
		v.stats.evictions++
	}
}

func pictureBytes(p picture.Picture) int {
	if p.Pixels != nil {
		return len(p.Pixels.Pix)
	}
	return len(p.Data)
}

// forget invalidates every cached form of a changed file and its pending
// read, so a late completion cannot bring a deleted or renamed path back.
func (v *viewer) forget(path string) {
	delete(v.cache, path)
	v.cacheOrder = slices.DeleteFunc(v.cacheOrder, func(p string) bool { return p == path })
	if job := v.reads[path]; job != nil {
		job.cancel()
		delete(v.reads, path)
	}
	key := thumbKey(v.cat, catalog.Item{Path: path})
	delete(v.extras.thumbs, key)
	delete(v.extras.thumbAsked, key)
	v.extras.thumbOrder = slices.DeleteFunc(v.extras.thumbOrder, func(p string) bool { return p == key })
}

// present puts a read image on screen: at once when there is nothing to
// decode or a view holds it decoded, else once the back view decoded it
// (loaded), the current image staying until then.
func (v *viewer) present(item catalog.Item, pic picture.Picture, err error) {
	s := shown{cat: v.cat, seq: v.loadSeq, item: item, pic: pic, err: err, src: core.ImageSource{Data: pic.Data, Pixels: pic.Pixels}}
	front := v.front.Get()
	back := 1 - front
	switch {
	case err == nil && pic.Pixels == nil && v.ready[front] && sameData(s.src, v.slots[front].Get()):
		v.commit(s, front)
	case err != nil || pic.Pixels != nil || (v.ready[back] && sameData(s.src, v.slots[back].Get())):
		// nsgo shows pixels without a blank, as an error shows nothing.
		v.commit(s, back)
	default:
		v.next = s
		v.ready[back] = false
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
	v.ready[k] = err == nil
	if k == v.front.Get() || v.next.src.Empty() || v.next.seq != v.loadSeq {
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
	v.ready[k] = s.err == nil && !s.src.Empty()
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
		v.commitExtras(s)
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
