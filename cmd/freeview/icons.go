package main

import (
	"github.com/nsgo-dev/nsgo/icons/lucide"

	"github.com/OUBIGFA/FreeView/assets/icons/solar"
)

// FreeView draws Solar Linear icons. Its own icons name them directly;
// ui.Select ends its field with lucide.ChevronDown and takes no other
// mark, so that shared icon is redrawn as Solar's arrow before any
// window shows. A Lucide accessor returns one SVG for the program's
// life, which every select then draws.
func init() {
	*lucide.ChevronDown() = *solar.AltArrowDown()
}
