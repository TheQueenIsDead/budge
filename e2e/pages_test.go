//go:build e2e

package e2e

import (
	"testing"

	"github.com/TheQueenIsDead/budge/pkg/test"
	"github.com/stretchr/testify/assert"
)

// TestPagesLoadClean opens each page as a person landing on it would, and
// checks it loads whole: no script error or blocked script, nothing failing to
// load or loaded from elsewhere, budge's fonts in, and its charts drawn.
func TestPagesLoadClean(t *testing.T) {
	base := startBudge(t)
	for _, page := range []struct {
		name, path string
		charts     int // at least
	}{
		{"dashboard", "/", 1},
		{"insights", "/insights", 1},
		{"budget", "/budget", 1},
		{"income", "/budget/income", 0},
		{"transactions", "/transactions", 1},
		{"timeline", "/timeline", 0},
		{"portfolio", "/portfolio", 0},
		{"account", "/portfolio/accounts/" + test.EverydayAccount, 1},
		{"asset", "/portfolio/assets/" + test.House, 0},
		{"new asset", "/portfolio/new", 0},
		{"notifications", "/notifications", 0},
		{"admin", "/admin", 0},
		{"settings", "/settings", 0},
	} {
		t.Run(page.name, func(t *testing.T) {
			p := openOn(t, base)
			p.visit(page.path)

			assert.True(t, eval[bool](p, `document.fonts.check('16px "Inter Variable"') && [...document.fonts].some(f => f.family.includes('Inter') && f.status === 'loaded')`), "Inter is loaded")
			assert.True(t, eval[bool](p, `[...document.fonts].some(f => f.family.includes('bootstrap-icons') && f.status === 'loaded')`), "the icon font is loaded")
			charts := eval[int](p, `[...document.querySelectorAll('canvas')].filter(c => window.Chart && Chart.getChart(c)).length`)
			assert.GreaterOrEqual(t, charts, page.charts, "charts drawn")
		})
	}
}
