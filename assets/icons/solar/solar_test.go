package solar

import (
	"os"
	"strings"
	"testing"

	"github.com/nsgo-dev/nsgo/core"
)

// The icons draw in the color they inherit, at 1.5 of 24 as upstream.
func TestIconsFollowTheTextColor(t *testing.T) {
	for name, src := range map[string]string{"magnifier.svg": magnifier, "alt-arrow-down.svg": altArrowDown} {
		disk, err := os.ReadFile(name)
		if err != nil || string(disk) != src {
			t.Fatalf("%s: embedded copy differs from the file", name)
		}
		got := tinted(src)
		if strings.Contains(got, "black") || !strings.Contains(got, `stroke="currentColor"`) ||
			!strings.Contains(got, `stroke-width="1.5"`) {
			t.Errorf("%s: tinted to %q", name, got)
		}
	}
	for _, svg := range []func() *core.SVG{Magnifier, AltArrowDown} {
		if w, h := svg().Size(); w != 24 || h != 24 {
			t.Errorf("size %v x %v, want 24 x 24", w, h)
		}
		if svg() != svg() {
			t.Error("parsed again on a second use")
		}
	}
}
