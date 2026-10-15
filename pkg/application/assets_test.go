package application

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"

	"github.com/TheQueenIsDead/budge/pkg/database"
	"github.com/TheQueenIsDead/budge/pkg/database/models"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAssetURL(t *testing.T) {
	stylesheet := assetURL("/static/styles.css")
	assert.Regexp(t, `^/static/styles\.css\?v=[0-9a-f]{10}$`, stylesheet)
	assert.Equal(t, stylesheet, assetURL("/static/styles.css"), "the same contents keep the same URL")
	assert.NotEqual(t, stylesheet[len("/static/styles.css"):], assetURL("/static/charts.js")[len("/static/charts.js"):],
		"each file is stamped with its own contents")

	assert.Equal(t, "/static/missing.js", assetURL("/static/missing.js"), "a path with no file is left alone")
}

func TestLayoutStampsAssets(t *testing.T) {
	page := renderTemplate(t, "layout", map[string]interface{}{"content": ""})
	for _, asset := range []string{"styles.css", "charts.js", "toast.js", "budget.js"} {
		assert.Contains(t, page, assetURL("/static/"+asset), "%s is linked by its stamped URL", asset)
	}
}

func TestServeStatic(t *testing.T) {
	// The real application, so this covers the route as it is mounted rather
	// than a copy of it.
	t.Setenv("BUDGE_BOLT_PATH", t.TempDir())
	store, err := database.NewStore()
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	app, err := NewApplication(store, nil)
	require.NoError(t, err)

	get := func(target string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		app.http.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
		return rec
	}

	t.Run("serves the embedded files whatever the working directory", func(t *testing.T) {
		// Tests run from pkg/application, where there is no web folder on
		// disk; the stylesheet can only have come from the binary.
		rec := get("/static/styles.css")
		require.Equal(t, http.StatusOK, rec.Code)
		assert.Contains(t, rec.Header().Get(echo.HeaderContentType), "text/css")
		assert.Contains(t, rec.Body.String(), ".b-tl-item")
	})

	t.Run("a stamped URL is kept for a year", func(t *testing.T) {
		rec := get(assetURL("/static/styles.css"))
		require.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, "public, max-age=31536000, immutable", rec.Header().Get(echo.HeaderCacheControl))
	})

	t.Run("an unstamped URL is checked every time", func(t *testing.T) {
		rec := get("/static/charts.js")
		require.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, "no-cache", rec.Header().Get(echo.HeaderCacheControl))
	})

	t.Run("a missing file is not cached", func(t *testing.T) {
		// The application answers a missing page with a redirect to its
		// not-found page, which must never be kept for a year.
		rec := get("/static/nope.css?v=0123456789")
		assert.NotEqual(t, http.StatusOK, rec.Code)
		assert.Empty(t, rec.Header().Get(echo.HeaderCacheControl))
	})
}

// TestTemplatesLinkOnlyStampedAssets keeps a new /static link from going in
// unstamped, where a cache would hold onto it across deploys.
func TestTemplatesLinkOnlyStampedAssets(t *testing.T) {
	for _, page := range []string{
		renderTemplate(t, "layout", map[string]interface{}{"content": ""}),
		renderTemplate(t, "settings", map[string]interface{}{"accounts": []models.Account{}, "sync": SyncStatus{}, "schedule": buildScheduleView(models.ScheduleSettings{})}),
	} {
		for _, link := range regexp.MustCompile(`(?:src|href)="(/static/[^"]+)"`).FindAllStringSubmatch(page, -1) {
			assert.Contains(t, link[1], "?v=", "%s is linked without a version", link[1])
		}
	}
}
