//go:build e2e

package e2e

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestNavigation moves around as a person does: the nav to Insights, a
// filter there, then the back button. htmx swaps each page in rather than
// loading it, so the address, the page and the history must still agree.
func TestNavigation(t *testing.T) {
	p := open(t)
	p.visit("/")
	location := func() string { return eval[string](p, `location.pathname + location.search`) }

	p.click(`a[data-nav][href="/insights"]`)
	p.settle()
	p.eventually(p.at("/insights"))
	p.eventually(p.contains("#main", "Insights"))

	p.click(".b-pill:not(.b-pill-active)")
	p.settle()
	p.eventually(func() error {
		if at := location(); !strings.Contains(at, "group=") {
			return fmt.Errorf("the address is %s, without the filter", at)
		}
		return nil
	})
	assert.True(t, eval[bool](p, `!!Chart.getChart(document.getElementById('insightsChart'))`), "the chart is drawn")

	eval[bool](p, `(history.back(), true)`)
	p.settle()
	p.eventually(p.at("/insights"))
	assert.True(t, eval[bool](p, `!!Chart.getChart(document.getElementById('insightsChart'))`), "the chart is drawn again")
}
