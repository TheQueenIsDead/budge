//go:build e2e

package e2e

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/cdproto/input"
	"github.com/chromedp/cdproto/log"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
)

// page is a browser tab on one budge. It notes anything that goes wrong on
// the page as the test runs, whatever the test is looking at: a script error,
// a Content Security Policy violation (which Chrome logs as an error), a
// request that fails, or one to any site but budge. The test fails if there
// were any.
type page struct {
	t    *testing.T
	ctx  context.Context
	base string

	mu       sync.Mutex
	problems []string
}

// slow runs the tests in a tab with its processor slowed four times, as a
// busy CI runner can be, to bring out a test that only passes because the
// page is quick: one that acts before what it waits on is done.
var slow = flag.Bool("slow", false, "slow each tab's processor fourfold, to find tests that race the page")

// open starts a budge for the test and opens a tab on it.
func open(t *testing.T) *page {
	t.Helper()
	return openOn(t, startBudge(t))
}

// openOn opens a tab on a budge already started, at base.
func openOn(t *testing.T, base string) *page {
	t.Helper()
	ctx, cancel := chromedp.NewContext(browser)
	t.Cleanup(cancel)
	p := &page{t: t, ctx: ctx, base: base}

	host := strings.TrimPrefix(base, "http://")
	err := chromedp.Listen(ctx, func(ev any) {
		switch ev := ev.(type) {
		case *runtime.EventExceptionThrown:
			text := ev.ExceptionDetails.Text
			if e := ev.ExceptionDetails.Exception; e != nil && e.Description != "" {
				text = e.Description
			}
			p.note("uncaught: %s at %s:%d", text, ev.ExceptionDetails.URL, ev.ExceptionDetails.LineNumber+1)
		case *runtime.EventConsoleAPICalled:
			if ev.Type == runtime.ConsoleAPICalledTypeError {
				var args []string
				for _, a := range ev.Args {
					args = append(args, strings.Trim(string(a.Value), `"`)+a.Description)
				}
				p.note("console error: %s", strings.Join(args, " "))
			}
		case *log.EventEntryAdded:
			if ev.Entry.Level == log.EntryLevelError {
				p.note("%s error: %s %s", ev.Entry.Source, ev.Entry.Text, ev.Entry.URL)
			}
		case *network.EventRequestWillBeSent:
			u, err := url.Parse(ev.Request.URL)
			if err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != host {
				p.note("request to another site: %s", ev.Request.URL)
			}
			// budge answers anything it doesn't have, a script or a font as
			// much as a page, by redirecting to its not found page.
			if err == nil && ev.RedirectResponse != nil && u.Path == "/4XX" {
				p.note("not found: %s", ev.RedirectResponse.URL)
			}
		case *network.EventResponseReceived:
			if ev.Response.Status >= 400 {
				p.note("%d for %s", ev.Response.Status, ev.Response.URL)
			}
		case *network.EventLoadingFailed:
			if !ev.Canceled {
				p.note("loading failed: %s", ev.ErrorText)
			}
		}
	})
	if err != nil {
		t.Fatalf("opening a tab: %v", err)
	}
	if *slow {
		if _, err := chromedp.Call(ctx, emulation.SetCPUThrottlingRate, emulation.SetCPUThrottlingRateParams{Rate: 4}); err != nil {
			t.Fatalf("slowing the tab: %v", err)
		}
	}
	if _, err := chromedp.Call(ctx, emulation.SetDeviceMetricsOverride, emulation.SetDeviceMetricsOverrideParams{
		Width: 1280, Height: 800, DeviceScaleFactor: 1,
	}); err != nil {
		t.Fatalf("sizing the tab: %v", err)
	}

	// Registered after the tab's cancel, so it runs first, while the tab is
	// still open to take a screenshot of.
	t.Cleanup(p.finish)
	return p
}

func (p *page) note(format string, args ...any) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.problems = append(p.problems, fmt.Sprintf(format, args...))
}

// finish fails the test for anything that went wrong on the page, and keeps
// a screenshot and the page's HTML of a failed test in its artifact directory.
func (p *page) finish() {
	p.mu.Lock()
	for _, problem := range p.problems {
		p.t.Errorf("on the page: %s", problem)
	}
	p.mu.Unlock()
	if !p.t.Failed() {
		return
	}

	ctx, cancel := context.WithTimeout(p.ctx, 10*time.Second)
	defer cancel()
	dir := p.t.ArtifactDir()
	if shot, err := chromedp.Run(ctx, chromedp.FullScreenshot(90)); err == nil {
		_ = os.WriteFile(filepath.Join(dir, "page.jpg"), shot, 0o644)
	}
	if html, err := chromedp.Run(ctx, chromedp.Evaluate[string](`document.documentElement.outerHTML`)); err == nil {
		_ = os.WriteFile(filepath.Join(dir, "page.html"), []byte(html), 0o644)
	}
	if f := flag.Lookup("test.artifacts"); f != nil && f.Value.String() == "true" {
		p.t.Logf("screenshot and HTML of the page in %s", dir)
	} else {
		p.t.Log("run with -artifacts to keep a screenshot and the HTML of the page")
	}
}

// do runs steps in the tab, failing the test if one fails.
func (p *page) do(steps ...chromedp.Action[chromedp.Void]) {
	p.t.Helper()
	ctx, cancel := context.WithTimeout(p.ctx, 30*time.Second)
	defer cancel()
	if err := chromedp.Do(ctx, steps...); err != nil {
		p.t.Fatal(err)
	}
}

// visit loads a page of budge and waits for it to settle: loaded, its fonts
// in, and no htmx request under way. The page must be the one asked for: budge
// answers a page it doesn't have by redirecting to its not found page, which
// loads cleanly.
func (p *page) visit(path string) {
	p.t.Helper()
	p.do(chromedp.Navigate(p.base + path))
	p.settle()
	if at := eval[string](p, `location.pathname + location.search`); at != path {
		p.t.Fatalf("asked for %s, and the browser ended up at %s", path, at)
	}
}

// settle waits until no htmx request is under way, no swap is still being
// put in or settled, and the page's fonts are in. A swap scrolls the page,
// as show: asks, only once it settles. A page a link is loading is waited
// for too.
func (p *page) settle() {
	p.t.Helper()
	ctx, cancel := context.WithTimeout(p.ctx, 30*time.Second)
	defer cancel()
	for {
		err := chromedp.Do(ctx, chromedp.Poll[chromedp.Void](`document.readyState === 'complete' && !document.querySelector('.htmx-request, .htmx-swapping, .htmx-settling, .htmx-added') && document.fonts.status === 'loaded'`))
		if gone(err) {
			continue
		}
		if err != nil {
			p.t.Fatal(err)
		}
		return
	}
}

// eval returns the value of a JavaScript expression, awaited if a promise.
func eval[T any](p *page, expression string) T {
	p.t.Helper()
	ctx, cancel := context.WithTimeout(p.ctx, 30*time.Second)
	defer cancel()
	v, err := chromedp.Run(ctx, chromedp.Evaluate[T](expression, chromedp.EvalAwaitPromise))
	for gone(err) {
		v, err = chromedp.Run(ctx, chromedp.Evaluate[T](expression, chromedp.EvalAwaitPromise))
	}
	if err != nil {
		p.t.Fatalf("evaluating %s: %v", expression, err)
	}
	return v
}

// gone reports whether err is Chrome saying the page an expression ran in
// went away under it: a click on a plain link, or the back button, loaded
// another. Run again, the expression runs in the new page.
func gone(err error) bool {
	return err != nil && (strings.Contains(err.Error(), "Cannot find context with specified id") ||
		strings.Contains(err.Error(), "Execution context was destroyed"))
}

// click clicks the middle of the first visible element sel matches, with
// the mouse, after scrolling it into the middle of the view.
func (p *page) click(sel string) {
	p.t.Helper()
	p.clickText(sel, "")
}

// clickText is click, for the first visible element sel matches whose text
// holds text.
func (p *page) clickText(sel, text string) {
	p.t.Helper()
	// Where an element is is read, then clicked, so a page still being swapped
	// in, or about to scroll once it settles, would take the click elsewhere.
	p.settle()
	rect := eval[struct{ X, Y, Width, Height float64 }](p, fmt.Sprintf(`(() => {
		const el = [...document.querySelectorAll(%s)].find(e => e.getBoundingClientRect().width > 0 && e.textContent.includes(%s));
		if (!el) throw new Error('nothing visible matches ' + %[1]s + ' holding ' + %[2]s);
		el.scrollIntoView({block: 'center', behavior: 'instant'});
		const r = el.getBoundingClientRect();
		return {X: r.x, Y: r.y, Width: r.width, Height: r.height};
	})()`, quote(sel), quote(text)))
	p.do(chromedp.MouseClickXY(rect.X+rect.Width/2, rect.Y+rect.Height/2))
}

// typeText types into whatever has the focus, as a keyboard would.
func (p *page) typeText(text string) {
	p.t.Helper()
	if _, err := chromedp.Call(p.ctx, input.InsertText, input.InsertTextParams{Text: text}); err != nil {
		p.t.Fatal(err)
	}
}

// press presses a key, such as kb.Tab or kb.Backspace.
func (p *page) press(key string) {
	p.t.Helper()
	p.do(chromedp.KeyEvent(key))
}

// choose picks an option of a select. Chrome draws a select's list outside
// the page, where the mouse can't reach it, so this does what picking does:
// sets the value and says it changed.
func (p *page) choose(sel, value string) {
	p.t.Helper()
	eval[bool](p, fmt.Sprintf(`(() => {
		const s = document.querySelector(%s);
		s.value = %s;
		s.dispatchEvent(new Event('change', {bubbles: true}));
		return true;
	})()`, quote(sel), quote(value)))
}

// is is a check, for eventually, that read gives want for sel, as p.text
// and p.value do.
func (p *page) is(sel, want string, read func(string) string) func() error {
	return func() error {
		if got := read(sel); got != want {
			return fmt.Errorf("%s is %q, not %q", sel, got, want)
		}
		return nil
	}
}

// at is a check, for eventually, that the browser is at path, with its query.
func (p *page) at(path string) func() error {
	return func() error {
		if got := eval[string](p, `location.pathname + location.search`); got != path {
			return fmt.Errorf("the browser is at %s, not %s", got, path)
		}
		return nil
	}
}

// contains is a check, for eventually, that the first element sel matches
// holds text.
func (p *page) contains(sel, text string) func() error {
	return func() error {
		if got := p.text(sel); !strings.Contains(got, text) {
			return fmt.Errorf("%s doesn't hold %q: %.300q", sel, text, got)
		}
		return nil
	}
}

// text is the text of the first element sel matches.
func (p *page) text(sel string) string {
	p.t.Helper()
	return eval[string](p, fmt.Sprintf(`document.querySelector(%s)?.textContent.trim() ?? '(nothing matches)'`, quote(sel)))
}

// value is the value of the first field sel matches.
func (p *page) value(sel string) string {
	p.t.Helper()
	return eval[string](p, fmt.Sprintf(`document.querySelector(%s)?.value ?? '(nothing matches)'`, quote(sel)))
}

// attr is an attribute of the first element sel matches.
func (p *page) attr(sel, name string) string {
	p.t.Helper()
	return eval[string](p, fmt.Sprintf(`document.querySelector(%s)?.getAttribute(%s) ?? '(nothing matches)'`, quote(sel), quote(name)))
}

// eventually retries check until it returns no error, failing the test with
// its last error if a few seconds pass first. Pages change after an htmx
// request returns, so a check made at once can come too soon.
func (p *page) eventually(check func() error) {
	p.t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		err := check()
		if err == nil {
			return
		}
		if time.Now().After(deadline) {
			p.t.Fatal(err)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// quote writes s as a JavaScript string.
func quote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
