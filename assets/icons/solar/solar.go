// Package solar embeds the Solar Linear icons FreeView draws, copied
// unchanged from saoudi-h/solar-icons (see README.md). Each accessor
// parses its SVG once, on first use, with the upstream black strokes
// turned into currentColor so core.Icon paints them in the text color it
// inherits, as nsgo's Lucide icons are.
package solar

import (
	_ "embed"
	"strings"
	"sync"

	"github.com/nsgo-dev/nsgo/core"
)

var (
	//go:embed magnifier.svg
	magnifier string
	//go:embed alt-arrow-down.svg
	altArrowDown string
)

// Magnifier is search/Linear/magnifier.svg.
var Magnifier = icon(magnifier)

// AltArrowDown is arrows/Linear/alt-arrow-down.svg.
var AltArrowDown = icon(altArrowDown)

// tinted turns the upstream black strokes into currentColor.
func tinted(src string) string {
	return strings.ReplaceAll(src, `stroke="black"`, `stroke="currentColor"`)
}

func icon(src string) func() *core.SVG {
	return sync.OnceValue(func() *core.SVG { return core.MustParseSVG([]byte(tinted(src))) })
}
