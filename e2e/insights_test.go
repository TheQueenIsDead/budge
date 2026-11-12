//go:build e2e

package e2e

import (
	"fmt"
	"testing"
)

// TestInsightsCadenceRedrawsTheChart switches Insights between weekly and
// monthly. Only the panel is swapped, and the chart's canvas is kept across
// it, so the chart must be redrawn with the new cadence's bars rather than
// left showing the old ones until a reload.
func TestInsightsCadenceRedrawsTheChart(t *testing.T) {
	p := open(t)
	p.visit("/insights?cadence=weekly")
	bars := func() string {
		return eval[string](p, `JSON.stringify(Chart.getChart(document.getElementById('insightsChart'))?.data.labels ?? null)`)
	}
	weekly := bars()

	p.clickText(".b-segment a", "Monthly")
	p.settle()
	p.eventually(p.contains(".b-segment a.active", "Monthly"))
	p.eventually(func() error {
		if got := bars(); got == weekly {
			return fmt.Errorf("the chart still has the weekly bars, %s", got)
		}
		return nil
	})

	p.clickText(".b-segment a", "Weekly")
	p.settle()
	p.eventually(func() error {
		if got := bars(); got != weekly {
			return fmt.Errorf("the chart has %s, not the weekly bars %s", got, weekly)
		}
		return nil
	})
}
