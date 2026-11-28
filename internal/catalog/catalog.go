// Package catalog lists the images FreeView moves between: the images of a
// folder, or the images inside an archive (ZIP/CBZ, RAR/CBR, 7z/CB7, TAR),
// in the order the user chose, and reads an image's bytes from either.
//
// Archives are read through github.com/mholt/archives, which wraps the
// standard library's zip and tar readers, nwaples/rardecode and
// bodgit/sevenzip behind one io/fs interface.
package catalog

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/mholt/archives"

	"github.com/OUBIGFA/FreeView/internal/picture"
)

// imageExts are the formats FreeView shows. JPEG, PNG, GIF, WebP and SVG
// are decoded by nsgo (animated GIF and WebP included); BMP and TIFF by
// golang.org/x/image; ICO, HEIF, AVIF, JPEG XL and RAW by Windows
// (package picture).
var imageExts = append([]string{".jpg", ".jpeg", ".jpe", ".jfif", ".png", ".gif", ".webp", ".bmp", ".dib", ".tif", ".tiff"},
	picture.Extensions()...)

// archiveExts are the archives FreeView opens like folders.
var archiveExts = []string{".zip", ".cbz", ".rar", ".cbr", ".7z", ".cb7", ".tar", ".cbt"}

// ImageExtensions returns the image file extensions, with their dots.
func ImageExtensions() []string { return slices.Clone(imageExts) }

// ArchiveExtensions returns the archive file extensions, with their dots.
func ArchiveExtensions() []string { return slices.Clone(archiveExts) }

// IsImage reports whether name has an image extension.
func IsImage(name string) bool {
	return slices.Contains(imageExts, strings.ToLower(filepath.Ext(name)))
}

// IsArchive reports whether name has an archive extension.
func IsArchive(name string) bool {
	return slices.Contains(archiveExts, strings.ToLower(filepath.Ext(name)))
}

// Item is one image: a file on disk, or an entry of Archive.
type Item struct {
	// Name is the file name shown to the user.
	Name string
	// Path is the file's path on disk, or the entry's slash path inside
	// the archive.
	Path    string
	Size    int64
	ModTime time.Time
}

// Order says how a catalog is sorted.
type Order struct {
	By         string // "name", "modified" or "size"
	Descending bool
}

// Catalog is the images of one folder or archive.
type Catalog struct {
	// Archive is the archive's path on disk, empty for a folder.
	Archive string
	// Dir is the folder holding the images, or holding the archive.
	Dir   string
	Items []Item
	fsys  fs.FS
}

// Open lists the catalog that target belongs to and returns it with the
// index of target in it: an image's folder at that image, a folder at its
// first image, an archive at its first image.
func Open(ctx context.Context, target string, order Order) (*Catalog, int, error) {
	target, err := filepath.Abs(target)
	if err != nil {
		return nil, 0, err
	}
	info, err := os.Stat(target)
	if err != nil {
		return nil, 0, err
	}
	switch {
	case info.IsDir():
		c, err := openDir(target, order)
		return c, 0, err
	case IsArchive(target):
		c, err := openArchive(ctx, target, order)
		return c, 0, err
	case IsImage(target):
		c, err := openDir(filepath.Dir(target), order)
		if err != nil {
			return nil, 0, err
		}
		return c, max(c.Index(target), 0), nil
	}
	return nil, 0, fmt.Errorf("%s: %w", filepath.Base(target), ErrUnsupported)
}

// ErrUnsupported is the error for a file FreeView cannot show.
var ErrUnsupported = errors.New("not a supported image or archive")

func openDir(dir string, order Order) (*Catalog, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	c := &Catalog{Dir: dir}
	for _, e := range entries {
		if e.IsDir() || !IsImage(e.Name()) {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue // removed while listing
		}
		c.Items = append(c.Items, Item{Name: e.Name(), Path: filepath.Join(dir, e.Name()), Size: info.Size(), ModTime: info.ModTime()})
	}
	c.sort(order)
	return c, nil
}

func openArchive(ctx context.Context, file string, order Order) (*Catalog, error) {
	fsys, err := archives.FileSystem(ctx, file, nil)
	if err != nil {
		return nil, err
	}
	c := &Catalog{Archive: file, Dir: filepath.Dir(file), fsys: fsys}
	err = fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !IsImage(p) {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		c.Items = append(c.Items, Item{Name: path.Base(p), Path: p, Size: info.Size(), ModTime: info.ModTime()})
		return nil
	})
	if err != nil {
		return nil, err
	}
	c.sort(order)
	return c, nil
}

// sort orders the items; names compare as Explorer compares them, and
// archive entries by their whole path so folders inside stay together.
func (c *Catalog) sort(order Order) {
	slices.SortStableFunc(c.Items, func(a, b Item) int {
		r := 0
		switch order.By {
		case "modified":
			r = a.ModTime.Compare(b.ModTime)
		case "size":
			r = cmpInt(a.Size, b.Size)
		}
		if r == 0 {
			r = compareNames(a.Path, b.Path)
		}
		if order.Descending {
			r = -r
		}
		return r
	})
}

func cmpInt(a, b int64) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

// Index returns the position of the item at path, or -1.
func (c *Catalog) Index(p string) int {
	return slices.IndexFunc(c.Items, func(it Item) bool {
		if c.Archive != "" {
			return it.Path == p
		}
		return strings.EqualFold(it.Path, p)
	})
}

// Title names the catalog for the window: the folder or archive name.
func (c *Catalog) Title() string {
	if c.Archive != "" {
		return filepath.Base(c.Archive)
	}
	return filepath.Base(c.Dir)
}

// Read returns the bytes of item, from disk or from the archive.
func (c *Catalog) Read(item Item) ([]byte, error) {
	if c.fsys == nil {
		return os.ReadFile(item.Path)
	}
	f, err := c.fsys.Open(item.Path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(f)
}

// Remove drops the item at i, after it was deleted, and returns the index
// to show next: the item that took its place, or the new last one.
func (c *Catalog) Remove(i int) int {
	c.Items = slices.Delete(c.Items, i, i+1)
	return min(i, len(c.Items)-1)
}

// Step returns the index delta places from i, wrapping around when loop
// is set and stopping at the ends otherwise.
func (c *Catalog) Step(i, delta int, loop bool) int {
	n := len(c.Items)
	if n == 0 {
		return -1
	}
	j := i + delta
	if loop {
		return ((j % n) + n) % n
	}
	return min(max(j, 0), n-1)
}
