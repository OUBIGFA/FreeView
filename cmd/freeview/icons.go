package main

import (
	"github.com/nsgo-dev/nsgo/icons/lucide"

	"github.com/OUBIGFA/FreeView/assets/icons/solar"
)

// FreeView uses Solar Linear throughout. These marks are fixed inside
// nsgo's select and menu controls, so replace their shared SVGs before
// any window shows; all other icons name Solar directly.
func init() {
	*lucide.ChevronDown() = *solar.AltArrowDown()
	*lucide.ChevronRight() = *solar.AltArrowRight()
	*lucide.ChevronLeft() = *solar.AltArrowLeft()
	*lucide.Check() = *solar.Check()
}
