package main

import (
	"archive/zip"
	"bytes"
	"errors"
	"image"
	"image/png"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/desktop"
	"github.com/nsgo-dev/nsgo/icons/lucide"
	"github.com/nsgo-dev/nsgo/nsgotest"
	"go.yaml.in/yaml/v3"

	"github.com/OUBIGFA/FreeView/assets/icons/solar"
	"github.com/OUBIGFA/FreeView/internal/catalog"
	"github.com/OUBIGFA/FreeView/internal/settings"
)

// TestMain also supplies the real app with a waiting browser for the
// private-desktop regression script, without opening the user's browser.
func TestMain(m *testing.M) {
	if path := os.Getenv("FREEVIEW_DESKTOP_BROWSER_LOG"); path != "" {
		runBrowserWaitDesktop(path)
		return
	}
	nsgotest.Main(m)
}

func pngOf(t *testing.T, w, h int) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, w, h))); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func fileURI(p string) string {
	return (&url.URL{Scheme: "file", Path: "/" + filepath.ToSlash(p)}).String()
}

// start runs the viewer with its preferences in a fresh profile.
func start(t *testing.T) *nsgotest.App {
	t.Setenv("AppData", t.TempDir())
	return nsgotest.Start(t, Root, nsgotest.Size(800, 600))
}

// loadImages reports that both image views decoded their sources, as
// the platform does once it has: the one decoding the next image then
// shows it.
func loadImages(app *nsgotest.App) {
	for _, id := range []string{"id:image-0", "id:image-1"} {
		app.Load(id, core.Size{Width: 1, Height: 1})
	}
}

// waitInfo waits for the information overlay to hold want, as the
// images to show decode.
func waitInfo(t *testing.T, app *nsgotest.App, want string) string {
	t.Helper()
	var got string
	app.WaitUntil(5*time.Second, func() bool {
		loadImages(app)
		if !app.Exists("id:info") {
			return false
		}
		got = app.Find("id:info").Text
		return strings.Contains(got, want)
	})
	if !strings.Contains(got, want) {
		t.Fatalf("info = %q, want it to hold %q", got, want)
	}
	return got
}

func TestBrowsesAFolderWithKeysAndTheWheel(t *testing.T) {
	dir := t.TempDir()
	for name, size := range map[string][2]int{"a1.png": {400, 300}, "a2.png": {1600, 1200}, "a10.png": {100, 50}} {
		if err := os.WriteFile(filepath.Join(dir, name), pngOf(t, size[0], size[1]), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	app := start(t)
	app.DropFiles("id:viewport", fileURI(filepath.Join(dir, "a2.png")))
	app.Key("i")
	// Large images shrink to the 800 by 600 window by default.
	waitInfo(t, app, "a2.png\n2 / 3   1600 × 1200   50%")

	app.Key("right")
	waitInfo(t, app, "a10.png\n3 / 3   100 × 50   100%")
	app.Key("right") // loops by default
	waitInfo(t, app, "a1.png\n1 / 3")

	// The wheel moves between images; Ctrl+wheel zooms by the zoom step.
	app.Wheel("id:viewport", core.Point{Y: -33}, core.Modifiers{})
	waitInfo(t, app, "a2.png\n2 / 3")
	app.Wheel("id:viewport", core.Point{Y: 33}, core.Modifiers{Control: true})
	waitInfo(t, app, "63%")
	app.Key("ctrl+1")
	waitInfo(t, app, "100%")
	app.Key("ctrl+0")
	waitInfo(t, app, "50%")
	app.Key("end")
	waitInfo(t, app, "3 / 3")
	app.Key("home")
	waitInfo(t, app, "1 / 3")
}

// The shown image stays until the next one decoded, so moving between
// images never shows the background alone.
func TestKeepsTheImageUntilTheNextDecodes(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"b1.png", "b2.png"} {
		if err := os.WriteFile(filepath.Join(dir, name), pngOf(t, 40, 30), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	app := start(t)
	app.DropFiles("id:viewport", fileURI(filepath.Join(dir, "b1.png")))
	app.Key("i")
	waitInfo(t, app, "b1.png\n1 / 2")

	app.Key("right")
	// Long enough for b2.png to be read, never decoded.
	time.Sleep(200 * time.Millisecond)
	app.Settle()
	if got := app.Find("id:info").Text; !strings.Contains(got, "b1.png") {
		t.Fatalf("info = %q before b2.png decoded, want b1.png still shown", got)
	}
	waitInfo(t, app, "b2.png\n2 / 2")
	// Back to b1.png, which the other view holds decoded: at once.
	app.Key("left")
	app.WaitUntil(5*time.Second, func() bool { return strings.Contains(app.Find("id:info").Text, "b1.png") })
}

func TestOpensAnArchiveLikeAFolder(t *testing.T) {
	file := filepath.Join(t.TempDir(), "book.cbz")
	f, err := os.Create(file)
	if err != nil {
		t.Fatal(err)
	}
	z := zip.NewWriter(f)
	for _, n := range []string{"p10.png", "p9.png"} {
		w, _ := z.Create(n)
		w.Write(pngOf(t, 30, 40))
	}
	z.Close()
	f.Close()

	app := start(t)
	app.DropFiles("id:viewport", fileURI(file))
	app.Key("i")
	waitInfo(t, app, "p9.png\n1 / 2   30 × 40")
	app.Key("pagedown")
	waitInfo(t, app, "p10.png\n2 / 2")
}

func TestShowsWhyAFileCannotBeShown(t *testing.T) {
	p := filepath.Join(t.TempDir(), "broken.png")
	os.WriteFile(p, []byte("not a png"), 0o644)
	app := start(t)
	app.DropFiles("id:viewport", fileURI(p))
	v := app.WaitFor("id:error", 5*time.Second)
	if !strings.Contains(v.Text, "broken.png") {
		t.Errorf("error = %q", v.Text)
	}
}

// Without a title bar, the default, a strip at the top moves the window.
func TestDragStripWithoutTitleBar(t *testing.T) {
	app := start(t)
	app.WaitFor("id:open", 5*time.Second)
	// Its height is the hidden title bar's, 0 in the test model.
	if len(app.QueryMounted("id:drag")) == 0 {
		t.Error("no drag strip without the title bar")
	}
}

// With the title bar, the system's moves the window and the strip goes.
func TestNoDragStripWithTitleBar(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("AppData", dir)
	os.MkdirAll(filepath.Join(dir, "FreeView"), 0o755)
	os.WriteFile(filepath.Join(dir, "FreeView", "config.yaml"), []byte("show_title_bar: true\n"), 0o644)
	app := nsgotest.Start(t, Root, nsgotest.Size(800, 600))
	app.WaitFor("id:open", 5*time.Second)
	if len(app.QueryMounted("id:drag")) > 0 {
		t.Error("drag strip shown with the title bar")
	}
}

// The preferences show one category at a time beside their own sidebar,
// and every matching row while searching.
func TestPreferencesNavigateAndSearch(t *testing.T) {
	t.Setenv("AppData", t.TempDir())
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node { return preferences{v: newViewer(cx)}.Render(cx) },
		nsgotest.Size(1040, 700))
	app.WaitFor("id:settings-view-fit", 5*time.Second)
	if app.Exists("id:settings-mouse-middle_click") {
		t.Error("mouse rows shown on the view page")
	}
	app.Press("id:settings-nav-mouse")
	app.WaitFor("id:settings-mouse-middle_click", 5*time.Second)
	if app.Exists("id:settings-view-fit") {
		t.Error("view rows shown on the mouse page")
	}
	// "top" matches the view page's switch and the keyboard page's action (the tests run in English).
	app.Type("id:settings-search", "top")
	app.WaitFor("id:settings-view-always-on-top", 5*time.Second)
	app.WaitFor("id:settings-keyboard-key-always_on_top", 5*time.Second)
	if app.Exists("id:settings-view-fit") || app.Exists("id:settings-mouse-middle_click") {
		t.Error("rows that do not match shown while searching")
	}
}

// A maximized or full-screen window keeps the frame it had before, even
// when the platform reports the whole screen a moment before the state,
// so the next start does not open it over the whole screen.
func TestRemembersTheFrameBeforeMaximizing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "windows.yaml")
	s := &frameStore{path: path, saved: settings.Windows{}}
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		s.remember(cx, "main", func(err error) { t.Error(err) })
		return core.View{}
	}, nsgotest.Size(800, 600))
	saved := func() settings.Frame {
		w, err := settings.LoadWindows(path)
		if err != nil {
			t.Fatal(err)
		}
		return w["main"]
	}
	normal := core.WindowFrame{X: 100, Y: 80, Move: true, Width: 900, Height: 640}
	want := settings.Frame{X: 100, Y: 80, Width: 900, Height: 640}
	app.SetEnvironment(core.Environment{WindowFrame: normal})
	app.WaitUntil(5*time.Second, func() bool { return saved() == want })
	if got := saved(); got != want {
		t.Fatalf("saved %+v, want %+v", got, want)
	}
	whole := core.WindowFrame{X: -8, Y: -8, Move: true, Width: 3456, Height: 1408}
	// The frame first, then the state, as Windows reports maximizing.
	app.SetEnvironment(core.Environment{WindowFrame: whole})
	app.SetEnvironment(core.Environment{WindowFrame: whole, Maximized: true})
	app.SetEnvironment(core.Environment{WindowFrame: whole, Maximized: true, FullScreen: true})
	// Past the half second a save waits, and the write it starts.
	for end := time.Now().Add(1500 * time.Millisecond); time.Now().Before(end); time.Sleep(50 * time.Millisecond) {
		app.Settle()
		if got := saved(); got != want {
			t.Fatalf("saved %+v while maximized, want the frame before, %+v", got, want)
		}
	}
}

// The About page shows the logo and sends each link to the browser.
func TestAboutPage(t *testing.T) {
	if logoPixels() == nil {
		t.Fatal("the embedded logo does not decode")
	}
	t.Setenv("AppData", t.TempDir())
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node { return preferences{v: newViewer(cx)}.Render(cx) },
		nsgotest.Size(1040, 700))
	app.WaitFor("id:settings-nav-about", 5*time.Second)
	app.Press("id:settings-nav-about")
	app.WaitFor("id:about-logo", 5*time.Second)
	opened := make(chan string, 1)
	stubBrowser(t, func(url string) error { opened <- url; return nil })
	for _, link := range []struct{ id, url string }{
		{"about-github", "https://github.com/OUBIGFA/FreeView"},
		{"about-feedback", "https://github.com/OUBIGFA/FreeView/issues"},
		{"about-releases", "https://github.com/OUBIGFA/FreeView/releases"},
		{"about-license", "https://github.com/OUBIGFA/FreeView/blob/main/LICENSE"},
		{"about-notices", "https://github.com/OUBIGFA/FreeView/blob/main/third_party/README.md"},
		{"about-icons", "https://github.com/OUBIGFA/FreeView/blob/main/assets/icons/solar/README.md"},
	} {
		app.Press("id:" + link.id)
		select {
		case got := <-opened:
			if got != link.url {
				t.Errorf("%s opened %q, want %q", link.id, got, link.url)
			}
		case <-time.After(time.Second):
			t.Fatalf("%s did not launch the browser", link.id)
		}
	}
	// "update" finds the update rows wherever the search starts.
	app.Type("id:settings-search", "update")
	app.WaitFor("id:about-check-updates", 5*time.Second)
}

// Only the OS browser handoff is replaced; buttons, scopes and windows
// still use the real app. No tests may launch the user's browser.
func stubBrowser(t *testing.T, launch func(string) error) {
	t.Helper()
	previous := launchBrowser
	launchBrowser = launch
	t.Cleanup(func() { launchBrowser = previous })
}

// A slow browser launch must not hold up the preferences event handler.
func TestAboutLinkDoesNotBlockSettings(t *testing.T) {
	for _, id := range []string{"about-license", "about-notices"} {
		t.Run(id, func(t *testing.T) {
			t.Setenv("APPDATA", t.TempDir())
			app := nsgotest.StartApp(t, func(cx *core.Scope) {
				desktop.SetMainWindow(cx, desktop.OpenWindow(cx, desktop.WindowOptions{Width: 1040, Height: 700},
					func(cx *core.Scope) core.Node { return preferences{v: newViewer(cx)}.Render(cx) }))
			}, nsgotest.Environment(core.Environment{OS: core.OSWindows}))
			app.Press("id:settings-nav-about")
			entered, release := make(chan struct{}), make(chan struct{})
			// A watchdog releases a regressed synchronous call so the test
			// fails instead of hanging the whole suite.
			timer := time.AfterFunc(2*time.Second, func() { close(release) })
			defer func() {
				if timer.Stop() {
					close(release)
				}
			}()
			stubBrowser(t, func(string) error { close(entered); <-release; return nil })
			app.Press("id:" + id)
			select {
			case <-entered:
			case <-time.After(time.Second):
				t.Fatal("the browser handoff did not start")
			}
			app.Press("id:settings-nav-view")
			app.WaitForGone("id:"+id, time.Second)
			if !app.CloseWindow() {
				t.Fatal("settings did not close while the browser was waiting")
			}
			select {
			case <-release:
				t.Fatal("settings waited for the browser before switching or closing")
			default:
			}
		})
	}
}

func TestAboutLinkReportsBrowserFailure(t *testing.T) {
	t.Setenv("APPDATA", t.TempDir())
	stubBrowser(t, func(string) error { return errors.New("browser unavailable") })
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node { return preferences{v: newViewer(cx)}.Render(cx) },
		nsgotest.Size(1040, 700), nsgotest.PendingAlerts())
	app.Press("id:settings-nav-about")
	app.Press("id:about-license")
	app.WaitUntil(time.Second, func() bool { return len(app.Alerts()) == 1 })
	alert := app.Alerts()[0]
	if !strings.Contains(alert.Message, "browser unavailable") || !strings.Contains(alert.Message, "/blob/main/LICENSE") {
		t.Fatalf("browser failure omitted its cause or link: %+v", alert)
	}
}

// nsgo.yaml registers FreeView for the formats it opens, no more and no fewer.
func TestFileTypesMatchTheCatalog(t *testing.T) {
	data, err := os.ReadFile("nsgo.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var project struct {
		FileTypes []struct {
			Name       string   `yaml:"name"`
			Extensions []string `yaml:"extensions"`
		} `yaml:"file_types"`
	}
	if err := yaml.Unmarshal(data, &project); err != nil {
		t.Fatal(err)
	}
	want := map[string][]string{"Image": catalog.ImageExtensions(), "Comic archive": catalog.ArchiveExtensions()}
	for _, ft := range project.FileTypes {
		var got []string
		for _, e := range ft.Extensions {
			got = append(got, "."+e)
		}
		if !slices.Equal(got, want[ft.Name]) {
			t.Errorf("%s: nsgo.yaml lists %v, the catalog %v", ft.Name, got, want[ft.Name])
		}
		delete(want, ft.Name)
	}
	if len(want) > 0 {
		t.Errorf("nsgo.yaml registers no %v", want)
	}
}

// Every select ends in Solar's arrow, not Lucide's chevron.
func TestSelectsDrawSolarArrows(t *testing.T) {
	if !reflect.DeepEqual(*lucide.ChevronDown(), *solar.AltArrowDown()) {
		t.Fatal("lucide.ChevronDown still draws Lucide's chevron")
	}
}
