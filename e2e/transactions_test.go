//go:build e2e

package e2e

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestTransactionSearch types into the search box: the list narrows to what
// matches, the address keeps the search, and the box keeps the focus, so
// typing can go on.
func TestTransactionSearch(t *testing.T) {
	p := open(t)
	p.visit("/transactions")
	p.click("#search")
	p.typeText("pak")

	p.eventually(func() error {
		if search := eval[string](p, `location.search`); !strings.Contains(search, "search=pak") {
			return fmt.Errorf("the address is %s, without the search", search)
		}
		return nil
	})
	p.settle()
	assert.Equal(t, "search", eval[string](p, `document.activeElement.id`), "the search box keeps the focus")
	assert.Equal(t, "pak", p.value("#search"), "what was typed is still there")
	assert.True(t, eval[bool](p, `document.querySelector('#main').textContent.includes("Pak'nSave")`), "matching transactions are listed")
	assert.False(t, eval[bool](p, `document.querySelector('#main').textContent.includes('Z Energy')`), "others are not")
}

// TestFileATransaction files a fuel purchase under Health from its row. Its
// category badge changes at once, and stays changed.
func TestFileATransaction(t *testing.T) {
	p := open(t)
	p.visit("/transactions?search=energy")
	id := eval[string](p, `document.querySelector('details.b-txcat[id^="txcat-d-"]').id.slice('txcat-d-'.length)`)
	picker, badges := "#txcat-d-"+id, "#txcats-d-"+id

	assert.NotContains(t, p.text(badges), "Health", "it starts elsewhere")
	p.click(picker + " > summary")
	p.eventually(func() error {
		if !eval[bool](p, fmt.Sprintf(`!!document.querySelector(%s)`, quote(picker+" select[name=category]"))) {
			return fmt.Errorf("the category picker hasn't loaded")
		}
		return nil
	})
	p.choose(picker+" select[name=category]", "health")
	p.click(picker + " button.btn-primary")
	p.settle()
	p.eventually(p.contains(badges, "Health"))

	p.visit("/transactions?search=energy")
	assert.Contains(t, p.text(badges), "Health", "the filing is saved")
}

// TestFileIncome files money coming in, which is offered Income alone.
func TestFileIncome(t *testing.T) {
	p := open(t)
	p.visit("/transactions?search=salary")
	id := eval[string](p, `document.querySelector('details.b-txcat[id^="txcat-d-"]').id.slice('txcat-d-'.length)`)
	picker, badges := "#txcat-d-"+id, "#txcats-d-"+id

	assert.NotContains(t, p.text(badges), "Income", "it starts filed nowhere")
	p.click(picker + " > summary")
	p.eventually(func() error {
		if !eval[bool](p, fmt.Sprintf(`!!document.querySelector(%s)`, quote(picker+" select[name=category]"))) {
			return fmt.Errorf("the category picker hasn't loaded")
		}
		return nil
	})
	assert.Equal(t, `["","income"]`, eval[string](p, fmt.Sprintf(`JSON.stringify([...document.querySelectorAll(%s)].map(o => o.value))`, quote(picker+" select[name=category] option"))),
		"money coming in is offered Income alone")
	p.choose(picker+" select[name=category]", "income")
	p.click(picker + " button.btn-primary")
	p.settle()
	p.eventually(p.contains(badges, "Income"))

	p.visit("/transactions?search=salary")
	assert.Contains(t, p.text(badges), "Income", "the filing is saved")
}

// TestShowOnlyUncategorised narrows the list to what has no category and
// back, first with the icon toggle in the filters and then with the link on
// the row that counts them. The toggle says whether it is pressed, and its
// tooltip is not left behind over the list it reloaded.
func TestShowOnlyUncategorised(t *testing.T) {
	p := open(t)
	p.visit("/transactions")
	everything := eval[int](p, `document.querySelectorAll('.b-tx').length`)
	p.eventually(p.contains("#main .b-card-title", "no category"))
	assert.Equal(t, "false", p.attr("#uncategorised", "aria-pressed"), "the toggle starts off")

	filtered := func() {
		p.t.Helper()
		p.eventually(func() error {
			if search := eval[string](p, `location.search`); !strings.Contains(search, "uncategorised=1") {
				return fmt.Errorf("the address is %s, without the filter", search)
			}
			return nil
		})
		p.settle()
		p.eventually(p.contains("#main .b-card-title", "Showing only spending with no category"))
		assert.Equal(t, "true", p.attr("#uncategorised", "aria-pressed"), "the toggle is pressed")
		assert.True(t, eval[bool](p, `document.querySelector('#uncategorised').checked`))
		assert.Less(t, eval[int](p, `document.querySelectorAll('.b-tx').length`), everything, "the list narrows")
	}
	unfiltered := func() {
		p.t.Helper()
		p.eventually(func() error {
			if search := eval[string](p, `location.search`); strings.Contains(search, "uncategorised=1") {
				return fmt.Errorf("the address is %s, still filtered", search)
			}
			return nil
		})
		p.settle()
		p.eventually(p.contains("#main .b-card-title", "no category"))
		assert.NotContains(t, p.text("#main .b-card-title"), "Showing only")
		assert.Equal(t, "false", p.attr("#uncategorised", "aria-pressed"), "the toggle is off again")
		assert.Equal(t, everything, eval[int](p, `document.querySelectorAll('.b-tx').length`), "everything is back")
	}

	p.click(`label[for="uncategorised"]`)
	filtered()
	// The toggle's own tooltip may show again over the redrawn toggle, as the
	// mouse is still on it; one left behind by the toggle the swap replaced is
	// the bug, and its element is gone.
	assert.Zero(t, eval[int](p, `[...document.querySelectorAll('.tooltip')].filter(tip => !document.querySelector('[aria-describedby="' + tip.id + '"]')).length`), "no tooltip is left floating")
	p.click(`label[for="uncategorised"]`)
	unfiltered()

	p.clickText("#main .b-track-link", "Show them")
	filtered()
	p.clickText("#main .b-track-link", "Show everything")
	unfiltered()
}
