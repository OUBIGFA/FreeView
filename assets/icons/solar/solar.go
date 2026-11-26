// Package solar embeds the Solar Linear icons FreeView draws, copied
// unchanged from saoudi-h/solar-icons (see README.md). Each accessor
// parses its SVG once, on first use, with the upstream black strokes
// turned into currentColor so core.Icon paints them in the text color it
// inherits, as nsgo's Lucide icons are.
package solar

import (
	"embed"
	"strings"
	"sync"

	"github.com/nsgo-dev/nsgo/core"
)

//go:embed *.svg
var files embed.FS

// The icons, by their file names: each accessor is the Linear SVG of the
// same name, from the upstream folder README.md lists.
var (
	Magnifier               = icon("magnifier.svg")
	AltArrowDown            = icon("alt-arrow-down.svg")
	AltArrowRight           = icon("alt-arrow-right.svg")
	AltArrowLeft            = icon("alt-arrow-left.svg")
	Check                   = icon("check.svg")
	FolderOpen              = icon("folder-open.svg")
	Folder                  = icon("folder.svg")
	FolderWithFiles         = icon("folder-with-files.svg")
	Gallery                 = icon("gallery.svg")
	MagnifierZoomIn         = icon("magnifier-zoom-in.svg")
	RotateClockwise         = icon("rotate-clockwise.svg")
	ClapperboardPlay        = icon("clapperboard-play.svg")
	Copy                    = icon("copy.svg")
	LinkMinimalistic2       = icon("link-minimalistic-2.svg")
	TrashBinMinimalistic    = icon("trash-bin-minimalistic.svg")
	Settings                = icon("settings.svg")
	CloseCircle             = icon("close-circle.svg")
	PostsCarouselHorizontal = icon("posts-carousel-horizontal.svg")
	Mouse                   = icon("mouse.svg")
	Keyboard                = icon("keyboard.svg")
	InfoCircle              = icon("info-circle.svg")
)

// source is the embedded file name, as it is upstream.
func source(name string) string {
	data, err := files.ReadFile(name)
	if err != nil {
		panic("solar: no " + name) // the names above are fixed; a test reads each
	}
	return string(data)
}

// tinted turns the upstream black strokes into currentColor.
func tinted(src string) string {
	return strings.ReplaceAll(src, `stroke="black"`, `stroke="currentColor"`)
}

func icon(name string) func() *core.SVG {
	return sync.OnceValue(func() *core.SVG { return core.MustParseSVG([]byte(tinted(source(name)))) })
}
