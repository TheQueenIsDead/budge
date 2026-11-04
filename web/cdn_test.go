package web

import (
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"hash"
	"io"
	"io/fs"
	"net/http"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"
)

// externalTag finds a script or stylesheet loaded from another site, with its
// attributes.
var externalTag = regexp.MustCompile(`<(?:script|link)\b[^>]*\b(?:src|href)="(https://[^"]+)"[^>]*>`)

// unpinnable are hosts whose files cannot carry a hash: Google Fonts answers
// each browser with its own stylesheet.
var unpinnable = []string{"https://fonts.googleapis.com/", "https://fonts.gstatic.com"}

// pinnedFile is a file the page loads from another site, and the hash the
// browser holds it to.
type pinnedFile struct {
	Template  string
	URL       string
	Integrity string
}

func pinnedFiles(t *testing.T) []pinnedFile {
	t.Helper()
	integrity := regexp.MustCompile(`\bintegrity="([^"]+)"`)
	var files []pinnedFile
	err := fs.WalkDir(Templates, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		content, err := fs.ReadFile(Templates, path)
		if err != nil {
			return err
		}
		for _, tag := range externalTag.FindAllStringSubmatch(string(content), -1) {
			if strings.HasPrefix(tag[0], "<link") && !strings.Contains(tag[0], `rel="stylesheet"`) {
				continue // a preconnect hint loads nothing
			}
			if hasPrefix(tag[1], unpinnable) {
				continue
			}
			file := pinnedFile{Template: path, URL: tag[1]}
			if m := integrity.FindStringSubmatch(tag[0]); m != nil {
				file.Integrity = m[1]
			}
			if !strings.Contains(tag[0], `crossorigin="anonymous"`) {
				t.Errorf("%s: %s has no crossorigin=\"anonymous\", without which the browser cannot check its hash", path, tag[1])
			}
			files = append(files, file)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

func hasPrefix(s string, prefixes []string) bool {
	for _, prefix := range prefixes {
		if strings.HasPrefix(s, prefix) {
			return true
		}
	}
	return false
}

// TestScriptsFromElsewhereArePinned keeps every script and stylesheet loaded
// from a CDN pinned to a version and to the hash of the file, so the browser
// refuses anything but the file that was published.
func TestScriptsFromElsewhereArePinned(t *testing.T) {
	version := regexp.MustCompile(`[/@]\d+\.\d+\.\d+/`)
	files := pinnedFiles(t)
	if len(files) == 0 {
		t.Fatal("found no scripts or stylesheets from a CDN; has the layout moved?")
	}
	for _, file := range files {
		if file.Integrity == "" {
			t.Errorf("%s: %s has no integrity hash", file.Template, file.URL)
		}
		if !version.MatchString(file.URL) {
			t.Errorf("%s: %s names no exact version", file.Template, file.URL)
		}
	}
}

// TestPinnedHashesMatchTheirFiles fetches every pinned file and checks it
// against its hash, as the browser will. Renovate moves the hashes of files on
// cdnjs with their versions, but not of the Alpine morph plugin, which is on
// jsdelivr: this fails until its hash is updated too. It needs the network, so
// runs when BUDGE_CHECK_CDN is set, as it is in CI.
func TestPinnedHashesMatchTheirFiles(t *testing.T) {
	if os.Getenv("BUDGE_CHECK_CDN") == "" {
		t.Skip("set BUDGE_CHECK_CDN=1 to fetch the pinned files and check their hashes")
	}
	client := &http.Client{Timeout: 30 * time.Second}
	for _, file := range pinnedFiles(t) {
		t.Run(file.URL, func(t *testing.T) {
			algorithm, want, ok := strings.Cut(file.Integrity, "-")
			if !ok {
				t.Fatalf("%s: integrity %q is not algorithm-hash", file.Template, file.Integrity)
			}
			var sum hash.Hash
			switch algorithm {
			case "sha256":
				sum = sha256.New()
			case "sha384":
				sum = sha512.New384()
			case "sha512":
				sum = sha512.New()
			default:
				t.Fatalf("%s: integrity uses %s, which browsers do not accept", file.Template, algorithm)
			}

			resp, err := client.Get(file.URL)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = resp.Body.Close() }()
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("%s answered %s", file.URL, resp.Status)
			}
			if _, err := io.Copy(sum, resp.Body); err != nil {
				t.Fatal(err)
			}
			if got := base64.StdEncoding.EncodeToString(sum.Sum(nil)); got != want {
				t.Errorf("%s: %s is %s-%s, but the page pins %s; update the integrity hash", file.Template, file.URL, algorithm, got, file.Integrity)
			}
		})
	}
}
