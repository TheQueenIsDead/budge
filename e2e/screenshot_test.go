//go:build e2e

package e2e

import (
	"bytes"
	"flag"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/TheQueenIsDead/budge/pkg/test"
	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/chromedp"
)

// takeScreenshots turns the screenshots on. They are taken in CI, which
// commits any that changed to the pull request, so it shows each page that
// moved, side by side, as GitHub diffs images. Taken by hand they are drawn by
// another build of Chrome, so they are left to CI unless asked for.
var takeScreenshots = flag.Bool("screenshots", false, "draw every page into testdata/screenshots, replacing those that changed")

// screenshots is where each page's picture is kept, one per page and width.
const screenshots = "testdata/screenshots"

// A page's picture is replaced only once it has changed: a pixel counts as
// changed past a small difference in colour, and a page once more than a
// sliver of it has. Below that, drawing the same page again is not a change,
// and replacing its picture would only make noise in the pull request.
const (
	pixelTolerance = 24     // of 255, in any channel
	pageTolerance  = 0.0025 // of the page's pixels
)

// TestScreenshots draws every page, at a desktop's width and a phone's, into
// testdata/screenshots, replacing each picture whose page has changed. It
// never fails over a change: the pictures are there to be looked at, not to
// hold a page still. budge's clock is stopped in the e2e build, so the pages
// draw the same on any day. Admin is left out: it is timings and uptime,
// which change with every run.
func TestScreenshots(t *testing.T) {
	if !*takeScreenshots {
		t.Skip("run with -screenshots to draw every page")
	}
	base := startBudge(t)
	pages := []struct{ name, path string }{
		{"dashboard", "/"},
		{"insights", "/insights"},
		{"budget", "/budget"},
		{"income", "/budget/income"},
		{"transactions", "/transactions"},
		{"timeline", "/timeline"},
		{"portfolio", "/portfolio"},
		{"account", "/portfolio/accounts/" + test.EverydayAccount},
		{"asset", "/portfolio/assets/" + test.House},
		{"new-asset", "/portfolio/new"},
		{"notifications", "/notifications"},
		{"settings", "/settings"},
		{"new-memory", ""}, // found from the transactions list below
	}
	widths := []struct {
		name          string
		width, height int64
	}{{"desktop", 1280, 800}, {"phone", 390, 844}}

	for _, pg := range pages {
		for _, w := range widths {
			t.Run(pg.name+"-"+w.name, func(t *testing.T) {
				p := openOn(t, base)
				if _, err := chromedp.Call(p.ctx, emulation.SetDeviceMetricsOverride, emulation.SetDeviceMetricsOverrideParams{
					Width: w.width, Height: w.height, DeviceScaleFactor: 1, Mobile: w.width < 600,
				}); err != nil {
					t.Fatal(err)
				}
				// Asking for less motion draws each chart at once, rather than
				// leaving it to be pictured partway through growing into place.
				if _, err := chromedp.Call(p.ctx, emulation.SetEmulatedMedia, emulation.SetEmulatedMediaParams{
					Features: []*emulation.MediaFeature{{Name: "prefers-reduced-motion", Value: "reduce"}},
				}); err != nil {
					t.Fatal(err)
				}
				path := pg.path
				if pg.name == "new-memory" {
					p.visit("/transactions?search=Z%20Energy")
					path = "/memories/new?tx=" + eval[string](p, `document.querySelector('.b-tx-pick input').value`) + "&return=%2Ftransactions"
				}
				p.visit(path)
				still(p)

				// Quality 100 is a PNG, which keeps every pixel as drawn.
				shot, err := chromedp.Run(p.ctx, chromedp.FullScreenshot(100))
				if err != nil {
					t.Fatal(err)
				}
				keep(t, filepath.Join(screenshots, pg.name+"-"+w.name+".png"), shot)
			})
		}
	}
}

// still readies a page for its picture, the same every time. The caret blinks, so nothing keeps the focus. The
// phone's tab bar stays put at the foot of the screen, which a picture of the
// whole page would show halfway down, over what it covers, so for the
// picture it sits at the end of the page instead.
func still(p *page) {
	p.t.Helper()
	// It resolves two frames on, once the page has been laid out again,
	// as the picture's size is read straight after.
	eval[bool](p, `new Promise(done => {
		document.activeElement?.blur();
		const style = document.createElement('style');
		style.textContent = '.b-tabbar { position: static !important; }';
		document.head.appendChild(style);
		void document.body.offsetHeight;
		requestAnimationFrame(() => requestAnimationFrame(() => done(true)));
	})`)
}

// keep writes a page's picture to path, unless the one there already shows
// the same page.
func keep(t *testing.T, path string, shot []byte) {
	t.Helper()
	if kept, err := os.ReadFile(path); err == nil && same(kept, shot) {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, shot, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Logf("%s has changed", path)
}

// same reports whether two pictures show the same page: the same size, and
// no more than a sliver of their pixels apart.
func same(a, b []byte) bool {
	if bytes.Equal(a, b) {
		return true
	}
	was, err := png.Decode(bytes.NewReader(a))
	if err != nil {
		return false
	}
	now, err := png.Decode(bytes.NewReader(b))
	if err != nil || was.Bounds() != now.Bounds() {
		return false
	}
	bounds := now.Bounds()
	changed := 0
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			if apart(was.At(x, y), now.At(x, y)) {
				changed++
			}
		}
	}
	return float64(changed) <= pageTolerance*float64(bounds.Dx()*bounds.Dy())
}

// apart reports whether two colours differ by more than the tolerance in
// any channel.
func apart(a, b color.Color) bool {
	ar, ag, ab, _ := a.RGBA()
	br, bg, bb, _ := b.RGBA()
	for _, d := range [][2]uint32{{ar, br}, {ag, bg}, {ab, bb}} {
		x, y := int(d[0]>>8), int(d[1]>>8)
		if x-y > pixelTolerance || y-x > pixelTolerance {
			return true
		}
	}
	return false
}
