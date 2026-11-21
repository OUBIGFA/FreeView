package catalog

import (
	"archive/zip"
	"bytes"
	"context"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func pngBytes(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func names(c *Catalog) []string {
	var out []string
	for _, it := range c.Items {
		out = append(out, it.Name)
	}
	return out
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestFolderSortsLikeExplorerAndStartsAtTheOpenedImage(t *testing.T) {
	dir := t.TempDir()
	data := pngBytes(t)
	for _, n := range []string{"img10.png", "img2.png", "IMG1.PNG", "notes.txt", "photo.JPG"} {
		if err := os.WriteFile(filepath.Join(dir, n), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(dir, "sub.png"), 0o755); err != nil {
		t.Fatal(err)
	}
	c, i, err := Open(context.Background(), filepath.Join(dir, "img2.png"), Order{By: "name"})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"IMG1.PNG", "img2.png", "img10.png", "photo.JPG"}; !equal(names(c), want) {
		t.Fatalf("order = %v, want %v", names(c), want)
	}
	if i != 1 {
		t.Errorf("index = %d, want 1", i)
	}
	got, err := c.Read(c.Items[i])
	if err != nil || !bytes.Equal(got, data) {
		t.Errorf("Read = %d bytes, %v", len(got), err)
	}
}

func TestOrderByModifiedAndDescending(t *testing.T) {
	dir := t.TempDir()
	data := pngBytes(t)
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i, n := range []string{"c.png", "a.png", "b.png"} {
		p := filepath.Join(dir, n)
		if err := os.WriteFile(p, data, 0o644); err != nil {
			t.Fatal(err)
		}
		at := base.Add(time.Duration(i) * time.Hour)
		if err := os.Chtimes(p, at, at); err != nil {
			t.Fatal(err)
		}
	}
	c, _, err := Open(context.Background(), dir, Order{By: "modified", Descending: true})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"b.png", "a.png", "c.png"}; !equal(names(c), want) {
		t.Fatalf("order = %v, want %v", names(c), want)
	}
}

func TestArchiveListsItsImagesInOrder(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "comic.cbz")
	f, err := os.Create(file)
	if err != nil {
		t.Fatal(err)
	}
	z := zip.NewWriter(f)
	data := pngBytes(t)
	for _, n := range []string{"ch2/p1.png", "ch1/p10.png", "ch1/p2.png", "readme.txt"} {
		w, err := z.Create(n)
		if err != nil {
			t.Fatal(err)
		}
		w.Write(data)
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	f.Close()
	c, i, err := Open(context.Background(), file, Order{By: "name"})
	if err != nil {
		t.Fatal(err)
	}
	if i != 0 || c.Archive != file {
		t.Errorf("index %d, archive %q", i, c.Archive)
	}
	var paths []string
	for _, it := range c.Items {
		paths = append(paths, it.Path)
	}
	if want := []string{"ch1/p2.png", "ch1/p10.png", "ch2/p1.png"}; !equal(paths, want) {
		t.Fatalf("entries = %v, want %v", paths, want)
	}
	got, err := c.Read(c.Items[1])
	if err != nil || !bytes.Equal(got, data) {
		t.Errorf("Read = %d bytes, %v", len(got), err)
	}
}

func TestUnsupportedFileIsAnError(t *testing.T) {
	p := filepath.Join(t.TempDir(), "notes.txt")
	os.WriteFile(p, []byte("x"), 0o644)
	if _, _, err := Open(context.Background(), p, Order{}); err == nil {
		t.Fatal("opened a text file")
	}
}

func TestStepWrapsOnlyWhenLooping(t *testing.T) {
	c := &Catalog{Items: make([]Item, 3)}
	cases := []struct {
		i, d int
		loop bool
		want int
	}{{2, 1, true, 0}, {0, -1, true, 2}, {2, 1, false, 2}, {0, -1, false, 0}, {1, 1, false, 2}}
	for _, tc := range cases {
		if got := c.Step(tc.i, tc.d, tc.loop); got != tc.want {
			t.Errorf("Step(%d, %d, %v) = %d, want %d", tc.i, tc.d, tc.loop, got, tc.want)
		}
	}
	if got := (&Catalog{}).Step(0, 1, true); got != -1 {
		t.Errorf("empty Step = %d", got)
	}
}

func TestRemoveReturnsTheNextItem(t *testing.T) {
	c := &Catalog{Items: []Item{{Name: "a"}, {Name: "b"}, {Name: "c"}}}
	if got := c.Remove(1); got != 1 || c.Items[got].Name != "c" {
		t.Errorf("Remove middle = %d", got)
	}
	if got := c.Remove(1); got != 0 {
		t.Errorf("Remove last = %d", got)
	}
	if got := c.Remove(0); got != -1 {
		t.Errorf("Remove only = %d", got)
	}
}
