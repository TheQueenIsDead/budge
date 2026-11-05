//go:build e2e

package e2e

import (
	"testing"
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
