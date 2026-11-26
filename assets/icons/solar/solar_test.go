package solar

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nsgo-dev/nsgo/core"
)

// all are the accessors, by the file each draws.
var all = map[string]func() *core.SVG{
	"magnifier.svg": Magnifier, "alt-arrow-down.svg": AltArrowDown, "alt-arrow-right.svg": AltArrowRight,
	"alt-arrow-left.svg": AltArrowLeft, "check.svg": Check, "folder-open.svg": FolderOpen, "folder.svg": Folder,
	"folder-with-files.svg": FolderWithFiles, "gallery.svg": Gallery, "magnifier-zoom-in.svg": MagnifierZoomIn,
	"rotate-clockwise.svg": RotateClockwise, "clapperboard-play.svg": ClapperboardPlay,
	"copy.svg": Copy, "link-minimalistic-2.svg": LinkMinimalistic2, "trash-bin-minimalistic.svg": TrashBinMinimalistic,
	"settings.svg": Settings, "close-circle.svg": CloseCircle, "posts-carousel-horizontal.svg": PostsCarouselHorizontal,
	"mouse.svg": Mouse, "keyboard.svg": Keyboard, "info-circle.svg": InfoCircle,
}

// Every file has an accessor, the icons draw in the color they inherit,
// at 1.5 of 24 as upstream, and each is parsed once.
func TestIconsFollowTheTextColor(t *testing.T) {
	names, err := filepath.Glob("*.svg")
	if err != nil || len(names) != len(all) {
		t.Fatalf("%d SVG files, %d accessors (%v)", len(names), len(all), err)
	}
	for _, name := range names {
		svg, ok := all[name]
		if !ok {
			t.Errorf("%s: no accessor", name)
			continue
		}
		disk, err := os.ReadFile(name)
		if err != nil || string(disk) != source(name) {
			t.Fatalf("%s: embedded copy differs from the file", name)
		}
		got := tinted(source(name))
		if strings.Contains(got, "black") || !strings.Contains(got, `stroke="currentColor"`) ||
			!strings.Contains(got, `stroke-width="1.5"`) {
			t.Errorf("%s: tinted to %q", name, got)
		}
		if w, h := svg().Size(); w != 24 || h != 24 {
			t.Errorf("%s: size %v x %v, want 24 x 24", name, w, h)
		}
		if svg() != svg() {
			t.Errorf("%s: parsed again on a second use", name)
		}
	}
}
