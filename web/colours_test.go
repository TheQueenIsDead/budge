package web

import (
	"io/fs"
	"regexp"
	"strings"
	"testing"
)

// colourLiteral matches a hex colour or an rgb()/rgba()/hsl() written out with
// numbers. The leading guard skips HTML entities such as &#8226; and the
// middle of identifiers; colours built from a token at runtime, like the
// "rgb(" + channel mix in charts.js, start with no digit and are not matched.
var colourLiteral = regexp.MustCompile(`(?:^|[^&\w])(#(?:[0-9a-fA-F]{8}|[0-9a-fA-F]{6}|[0-9a-fA-F]{3})\b|(?:rgba?|hsla?)\(\s*\d)`)

// colourFile is the one file allowed to write a colour down.
const colourFile = "public/colours.css"

// TestColoursOnlyComeFromTheColourFile keeps colours.css the single source of
// every colour: anywhere else, a colour must be a token reference. A literal
// that slips in elsewhere drifts from the palette the next time it changes.
func TestColoursOnlyComeFromTheColourFile(t *testing.T) {
	for name, files := range map[string]fs.FS{"public": Public, "templates": Templates} {
		err := fs.WalkDir(files, ".", func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || path == colourFile {
				return err
			}
			if !strings.HasSuffix(path, ".css") && !strings.HasSuffix(path, ".js") && !strings.HasSuffix(path, ".gohtml") {
				return nil // images carry their own artwork colours
			}
			content, err := fs.ReadFile(files, path)
			if err != nil {
				return err
			}
			for n, line := range strings.Split(string(content), "\n") {
				if match := colourLiteral.FindStringSubmatch(line); match != nil {
					t.Errorf("%s:%d: colour %q written outside %s; reference a token instead", path, n+1, match[1], colourFile)
				}
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walking %s: %v", name, err)
		}
	}
}

func TestColourFileIsEmbedded(t *testing.T) {
	content, err := fs.ReadFile(Public, colourFile)
	if err != nil {
		t.Fatal(err)
	}
	if !colourLiteral.Match(content) {
		t.Fatal("the colour file defines no colours")
	}
}
