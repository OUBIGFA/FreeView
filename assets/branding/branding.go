// Package branding embeds FreeView's brand image, so the app shows the
// same logo its icons are made from without another copy in the
// repository.
package branding

import _ "embed"

// Logo is FreeView_logo.png, a 1200 by 1200 RGBA PNG.
//
//go:embed FreeView_logo.png
var Logo []byte
