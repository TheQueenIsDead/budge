//go:build e2e

package e2e

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestRecurringMarkAndUndo opens the recurring payments from Insights'
// pills and finds the seeded subscription with its price rise. Marked not
// recurring, it moves to the hidden list, and its Undo brings it back. Each
// is a post from inside the panel, which comes back with the payment moved.
func TestRecurringMarkAndUndo(t *testing.T) {
	p := open(t)
	p.visit("/insights")

	p.clickText("#main .b-pills a", "Recurring")
	p.settle()
	p.eventually(p.at("/insights?view=recurring"))
	p.eventually(p.contains("#main .b-pill-active", "Recurring"))

	neon := `li:has(a[href="/transactions?search=Neon"])`
	p.eventually(p.contains("#recurring-monthly "+neon, "Neon"))
	assert.Contains(t, p.text("#recurring-monthly "+neon), "from $17.99", "its price rise is marked")
	assert.Contains(t, p.text("#recurring-coming-up"), "AA Insurance", "the insurance is due within the fortnight")

	p.click("#recurring-monthly " + neon + ` button[title="Not recurring: hide it"]`)
	p.settle()
	p.eventually(p.contains("#recurring-hidden summary", "Hidden"))
	assert.False(t, eval[bool](p, `!!document.querySelector('#recurring-monthly `+neon+`')`), "it has left the monthly list")

	p.click("#recurring-hidden summary")
	p.clickText("#recurring-hidden "+neon+" button", "Undo")
	p.settle()
	p.eventually(p.contains("#recurring-monthly "+neon, "Neon"))
	assert.False(t, eval[bool](p, `!!document.querySelector('#recurring-hidden')`), "nothing is hidden now")

	p.visit("/insights?view=recurring")
	assert.Contains(t, p.text("#recurring-monthly "+neon), "Neon", "and it is still back on a fresh load")
}
