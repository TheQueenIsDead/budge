package web

import (
	"io/fs"
	"path"
	"regexp"
	"strings"
	"testing"
)

// templateFiles calls visit with the name and content of every template.
func templateFiles(t *testing.T, visit func(name, content string)) {
	t.Helper()
	err := fs.WalkDir(Templates, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		content, err := fs.ReadFile(Templates, path)
		if err != nil {
			return err
		}
		visit(path, string(content))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// TestNothingLoadsFromElsewhere keeps every script, stylesheet and font served
// by budge, from /static/lib, so the Content Security Policy can refuse every
// other site.
func TestNothingLoadsFromElsewhere(t *testing.T) {
	external := regexp.MustCompile(`<(?:script|link)\b[^>]*\b(?:src|href)="(?:https?:)?//[^"]+"[^>]*>`)
	templateFiles(t, func(name, content string) {
		for _, tag := range external.FindAllString(content, -1) {
			t.Errorf("%s: %s loads from another site; add the library to web/package.json and embed.go instead", name, tag)
		}
	})
}

// TestLibraryFilesAreEmbedded checks that the library files the pages load,
// and the fonts their stylesheets load in turn, are among those embed.go
// embeds from node_modules.
func TestLibraryFilesAreEmbedded(t *testing.T) {
	library := regexp.MustCompile(`"/static/lib/([^"]+)"`)
	cssURL := regexp.MustCompile(`url\(\s*["']?([^"')]+)["']?\s*\)`)
	elsewhere := regexp.MustCompile(`^(data:|[a-z]+://|/)`)
	var count int
	templateFiles(t, func(name, content string) {
		for _, m := range library.FindAllStringSubmatch(content, -1) {
			count++
			file := path.Join("node_modules", m[1])
			data, err := fs.ReadFile(Libraries, file)
			if err != nil {
				t.Errorf("%s: %s is not embedded; add it to the go:embed lines in embed.go", name, file)
				continue
			}
			if !strings.HasSuffix(file, ".css") {
				continue
			}
			for _, u := range cssURL.FindAllStringSubmatch(string(data), -1) {
				if elsewhere.MatchString(u[1]) {
					continue
				}
				loaded := path.Join(path.Dir(file), strings.SplitN(strings.SplitN(u[1], "?", 2)[0], "#", 2)[0])
				if _, err := fs.Stat(Libraries, loaded); err != nil {
					t.Errorf("%s loads %s, which is not embedded; add it to the go:embed lines in embed.go", file, loaded)
				}
			}
		}
	})
	if count == 0 {
		t.Fatal("found no library files in the templates; has the layout moved?")
	}
}

// TestScriptsInTheMarkupCarryTheNonce keeps every inline script marked as the
// page's own, and leaves out inline event handlers, which the Content Security
// Policy would stop without a word.
func TestScriptsInTheMarkupCarryTheNonce(t *testing.T) {
	scriptTag := regexp.MustCompile(`<script\b[^>]*>`)
	handler := regexp.MustCompile(`(?:\s|}})on[a-z]+="`) // after a space, or straight after an {{if}}
	err := fs.WalkDir(Templates, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		content, err := fs.ReadFile(Templates, path)
		if err != nil {
			return err
		}
		for _, tag := range scriptTag.FindAllString(string(content), -1) {
			if !strings.Contains(tag, " src=") && !strings.Contains(tag, `nonce="{{ nonce }}"`) {
				t.Errorf("%s: %s has no nonce=\"{{ nonce }}\", so the Content Security Policy will not run it", path, tag)
			}
		}
		for _, match := range handler.FindAllString(string(content), -1) {
			t.Errorf("%s: inline event handler%q will not run under the Content Security Policy; use hx-on or Alpine", path, match)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
