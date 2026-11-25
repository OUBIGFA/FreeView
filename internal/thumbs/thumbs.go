// Package thumbs makes the small pictures of a folder's images that the
// viewer's strip shows. Files go through the Windows shell, which reuses
// Explorer's thumbnail cache and every codec the system has; decoded
// images, such as archive entries the shell cannot see, are scaled here.
// Both are shrunk with internal/resample's area averaging, so they stay
// free of aliasing at any size.
package thumbs

import (
	"image"

	"github.com/OUBIGFA/FreeView/internal/resample"
)

// Image scales an already decoded image down to fit size×size (never
// enlarges), for archive entries the shell cannot see. It returns img
// itself when it fits already, and nil when img is empty or size < 1.
func Image(img *image.RGBA, size int) *image.RGBA {
	if img == nil || img.Rect.Empty() || size < 1 {
		return nil
	}
	w, h := fit(img.Rect.Dx(), img.Rect.Dy(), size)
	if w == img.Rect.Dx() && h == img.Rect.Dy() {
		return img
	}
	return resample.Region(img, img.Rect, w, h)
}

// fit returns w×h shrunk to fit size×size with the aspect ratio kept,
// rounded to whole pixels and at least 1 each way; unchanged when it fits.
func fit(w, h, size int) (int, int) {
	long := max(w, h)
	if long <= size {
		return w, h
	}
	return max(1, (w*size+long/2)/long), max(1, (h*size+long/2)/long)
}
