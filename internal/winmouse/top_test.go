//go:build windows

package winmouse

import "testing"

func TestTopTracker(t *testing.T) {
	var tr topTracker

	for _, code := range []uintptr{htCaption, htTop, htTopLeft, htTopRight} {
		tr = topTracker{}
		over, changed := tr.nc(code)
		if !over || !changed {
			t.Fatalf("nc(%d) = (%v, %v), want (true, true)", code, over, changed)
		}
		over, changed = tr.nc(code)
		if !over || changed {
			t.Fatalf("repeat nc(%d) = (%v, %v), want (true, false)", code, over, changed)
		}
	}

	tr = topTracker{}
	for _, code := range []uintptr{htClient, 10, 0} {
		over, changed := tr.nc(code)
		if over || changed {
			t.Fatalf("nc(%d) from idle = (%v, %v), want (false, false)", code, over, changed)
		}
	}

	if tr.leave() {
		t.Fatal("leave() from idle reported changed")
	}

	tr.nc(htCaption)
	over, changed := tr.nc(htClient)
	if over || !changed {
		t.Fatalf("nc(htClient) after caption = (%v, %v), want (false, true)", over, changed)
	}

	tr.nc(htTop)
	if !tr.leave() {
		t.Fatal("leave() after top did not report changed")
	}
	if tr.leave() {
		t.Fatal("second leave() reported changed")
	}
}
