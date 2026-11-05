//go:build e2e

package e2e

import (
	"fmt"
	"math"
	"testing"

	"github.com/chromedp/chromedp/kb"
	"github.com/stretchr/testify/assert"
)

const (
	insurance     = "#category-insurance"
	insuranceName = "#cat-label-insurance"
	// The heading of the Insurance card, which follows its name as typed.
	insuranceHeading = insurance + " .b-pv-body .fw-semibold"
	// Transport's Fuel subcategory's name.
	fuelName = `#category-transport .b-cat-subs form[key="fuel"] input[name=label]`
)

// selectAll selects the text of a field, as a double-click would, so what is
// typed next replaces it.
func (p *page) selectAll(sel string) {
	p.t.Helper()
	eval[bool](p, fmt.Sprintf(`(document.querySelector(%s).select(), true)`, quote(sel)))
}

func (p *page) is(sel, want string, read func(string) string) func() error {
	return func() error {
		if got := read(sel); got != want {
			return fmt.Errorf("%s is %q, not %q", sel, got, want)
		}
		return nil
	}
}

// TestCategoryNameLeftBlankGoesBack clears a name the way a person does, with
// Backspace, and moves on. The name can't be blank, so the last one saved
// comes back, rather than the box and heading staying empty while the save is
// quietly refused.
func TestCategoryNameLeftBlankGoesBack(t *testing.T) {
	p := open(t)
	p.visit("/settings")
	p.click(insurance + " > summary")

	// A rename first, so the name to go back to is the saved one, not the one
	// the page was drawn with.
	p.click(insuranceName)
	p.selectAll(insuranceName)
	p.typeText("Cover")
	p.press(kb.Tab)
	p.settle()
	p.eventually(p.is(insuranceHeading, "Cover", p.text))

	p.click(insuranceName)
	p.selectAll(insuranceName)
	p.press(kb.Backspace)
	p.press(kb.Tab)
	p.settle()
	p.eventually(p.is(insuranceName, "Cover", p.value))
	assert.Equal(t, "Cover", p.text(insuranceHeading), "the heading names it again")

	p.visit("/settings")
	assert.Equal(t, "Cover", p.text(insuranceHeading), "the rename is what was saved")

	t.Run("subcategory", func(t *testing.T) {
		p.click("#category-transport > summary")
		p.click(fuelName)
		p.selectAll(fuelName)
		p.press(kb.Backspace)
		p.press(kb.Tab)
		p.settle()
		p.eventually(p.is(fuelName, "Fuel", p.value))

		p.visit("/settings")
		p.click("#category-transport > summary")
		assert.Equal(t, "Fuel", p.value(fuelName), "the saved name is unchanged")
	})
}

// TestCategoryEditsKeepYourPlace adds a subcategory and a rule. Each redraws
// the categories, which must leave the page where it was, keep open what was
// open, and clear the form that was sent.
func TestCategoryEditsKeepYourPlace(t *testing.T) {
	p := open(t)
	p.visit("/settings")
	p.click(insurance + " > summary")
	p.click("#category-pets > summary")

	// sends clicks a form's button and returns how far the page moved while
	// the categories were redrawn.
	sends := func(button string) float64 {
		p.click(button)
		before := eval[float64](p, `scrollY`)
		p.settle()
		return math.Abs(eval[float64](p, `scrollY`) - before)
	}

	subName := insurance + " form:has(input[name=parent]) input[name=label]"
	p.click(subName)
	p.typeText("Contents")
	assert.LessOrEqual(t, sends(insurance+" form:has(input[name=parent]) button"), 2.0, "the page stays put")
	p.eventually(p.is(insurance+` .b-cat-subs form[key="contents"] input[name=label]`, "Contents", p.value))
	assert.Empty(t, p.value(subName), "the form is cleared")

	p.click(insurance + " input[name=contains]")
	p.typeText("ZZ TEST RULE")
	assert.LessOrEqual(t, sends(insurance+` form[hx-post="/settings/rules"] button:not([type=button])`), 2.0, "the page stays put")
	p.eventually(p.is(insurance+" ol li .font-monospace", "“ZZ TEST RULE”", p.text))
	assert.Empty(t, p.value(insurance+" input[name=contains]"), "the form is cleared")

	assert.True(t, eval[bool](p, `document.querySelector('#category-insurance').open && document.querySelector('#category-pets').open`), "open categories stay open")
}
