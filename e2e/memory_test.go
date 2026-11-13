//go:build e2e

package e2e

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestMakeAMemory ticks a transaction, makes a memory of it on a page of its
// own, adding something bought around then, and comes back to the list it
// started on.
func TestMakeAMemory(t *testing.T) {
	p := open(t)
	start := "/transactions?search=Z%20Energy"
	p.visit(start)

	id := eval[string](p, `document.querySelector('.b-tx-pick input').value`)
	p.click(".b-tx-pick")
	p.clickText("#tx-group button", "New memory")
	p.settle()
	p.eventually(func() error {
		if at := eval[string](p, `location.pathname`); at != "/memories/new" {
			return fmt.Errorf("the browser is at %s, not the new memory's page", at)
		}
		return nil
	})
	assert.Equal(t, 1, eval[int](p, `document.querySelectorAll('input[name=tx]:checked').length`), "what was ticked is in it")
	nearby := eval[int](p, `document.querySelectorAll('input[name=tx]:not(:checked)').length`)
	if nearby > 0 {
		p.click("input[name=tx]:not(:checked)")
	}

	p.click("#memory-title")
	p.typeText("Road trip")
	p.clickText("button.btn-primary", "Make memory")
	p.settle()
	p.eventually(p.at(start))

	// Its bookmark, filled now, opens the memory.
	p.eventually(func() error {
		if !eval[bool](p, fmt.Sprintf(`!!document.querySelector('a.b-txmem-in#txmem-d-%s')`, id)) {
			return fmt.Errorf("the row doesn't link to its memory")
		}
		return nil
	})
	p.click("a.b-txmem-in#txmem-d-" + id)
	p.settle()
	p.eventually(p.is("#memory-title", "Road trip", p.value))

	p.visit("/timeline?view=memories")
	p.eventually(p.contains("#main", "Road trip"))
	purchases := p.text(".b-tl-memory .b-tl-rows summary")
	want := fmt.Sprintf("%d purchase", 1+min(nearby, 1))
	assert.True(t, strings.HasPrefix(purchases, want), "%q, not %q", purchases, want)
}
