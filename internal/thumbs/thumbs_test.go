package thumbs

import (
	"image"
	"testing"
)

func TestImageAspect(t *testing.T) {
	for _, c := range []struct{ w, h, size, ww, wh int }{
		{400, 200, 100, 100, 50},
		{200, 400, 100, 50, 100},
		{300, 300, 64, 64, 64},
		{33000, 40, 256, 256, 1}, // never thinner than one pixel
		{1000, 333, 100, 100, 33},
	} {
		out := Image(image.NewRGBA(image.Rect(0, 0, c.w, c.h)), c.size)
		if got := out.Rect.Size(); got != image.Pt(c.ww, c.wh) {
			t.Errorf("%d×%d to %d: got %v, want %d×%d", c.w, c.h, c.size, got, c.ww, c.wh)
		}
	}
}

func TestImageNoEnlarge(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 40, 30))
	if out := Image(img, 100); out != img {
		t.Fatalf("a small image should come back as is, got %v", out.Rect)
	}
	if Image(nil, 100) != nil || Image(img, 0) != nil {
		t.Fatal("empty input should give nil")
	}
}
