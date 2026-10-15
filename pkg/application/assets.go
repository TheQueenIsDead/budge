package application

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"

	"github.com/TheQueenIsDead/budge/web"
)

// staticFiles is the public folder as served under /static.
var staticFiles = func() fs.FS {
	public, err := fs.Sub(web.Public, "public")
	if err != nil {
		panic(err)
	}
	return public
}()

// assetVersions maps each /static path to a short hash of the file's contents.
// It changes only when the file does, so a release that leaves the stylesheet
// alone leaves its URL alone too, and caches keep their copy.
var assetVersions = func() map[string]string {
	versions := map[string]string{}
	err := fs.WalkDir(staticFiles, ".", func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		data, err := fs.ReadFile(staticFiles, path)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(data)
		versions["/static/"+path] = hex.EncodeToString(sum[:5])
		return nil
	})
	if err != nil {
		panic(err)
	}
	return versions
}()

// assetURL stamps a /static path with the version of its contents. Cloudflare
// and browsers cache static files by URL, so without the stamp a deploy was
// rendered with whatever stylesheet they held from before it. A path with no
// file behind it is returned as it is.
func assetURL(path string) string {
	if version, ok := assetVersions[path]; ok {
		return path + "?v=" + version
	}
	return path
}
