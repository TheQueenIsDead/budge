//go:build e2e

package e2e

import (
	"fmt"
	"testing"

	"github.com/TheQueenIsDead/budge/pkg/test"
	"github.com/stretchr/testify/assert"
)

// TestAddAnAsset adds a car to the portfolio, opens it, and values it.
func TestAddAnAsset(t *testing.T) {
	p := open(t)
	p.visit("/portfolio/new")

	p.click(`a[href="/portfolio/new?type=vehicle"]`)
	p.settle()
	p.click("#asset-name")
	p.typeText("Family Car")
	p.click("#asset-price")
	p.typeText("25000")
	p.click(`form[hx-post="/portfolio/assets"] button[type=submit]`)
	p.settle()

	p.eventually(p.at("/portfolio"))
	p.eventually(p.contains("#main", "Family Car"))

	p.clickText(`#main a[href^="/portfolio/assets/"]`, "Family Car")
	p.settle()
	p.eventually(p.contains("#main", "What you paid"))

	p.click("#valuation-value")
	p.typeText("23000")
	p.click("#valuation-note")
	p.typeText("Dealer quote")
	p.click(`form[hx-post$="/valuations"] button[type=submit]`)
	p.settle()
	p.eventually(p.contains("#main", "Dealer quote"))
}

// TestLoanPayoff opens the home loan, switches its chart to all time to see
// when it will be paid off, and back to the year. The chart is the same one
// throughout, redrawn in place.
func TestLoanPayoff(t *testing.T) {
	p := open(t)
	path := "/portfolio/accounts/" + test.HomeLoan
	p.visit(path)

	chart := `Chart.getChart(document.querySelector('#main canvas'))`
	assert.Equal(t, "bar", eval[string](p, chart+`.data.datasets[0].type`), "a year is drawn as bars")
	eval[bool](p, `(window.loanChart = `+chart+`, true)`)

	p.clickText("#main .b-segment a", "All time")
	p.settle()
	p.eventually(p.at(path + "?range=all"))
	p.eventually(p.contains("#loan-overview", "Paid off by"))
	p.eventually(p.contains("#loan-overview", "estimate, at the recent rate"))
	assert.NotContains(t, p.text("#loan-overview"), "—", "every tile has a figure")
	p.eventually(func() error {
		if !eval[bool](p, chart+` === window.loanChart`) {
			return fmt.Errorf("the chart was drawn again rather than updated")
		}
		labels := eval[[]string](p, `window.loanChart.data.datasets.map(d => d.label)`)
		if len(labels) != 2 || labels[1] != "Estimate" {
			return fmt.Errorf("the chart shows %v, not what is owed and an estimate", labels)
		}
		return nil
	})

	p.clickText("#main .b-segment a", "Year")
	p.settle()
	p.eventually(p.at(path))
	p.eventually(p.contains("#main", "End of month balance across 2026"))
	assert.False(t, eval[bool](p, `!!document.querySelector('#loan-overview')`), "the payoff tiles are all time's")
	assert.True(t, eval[bool](p, chart+` === window.loanChart && window.loanChart.data.datasets.length === 1 && window.loanChart.data.datasets[0].type === 'bar'`),
		"the same chart is back to a year's bars")
}
