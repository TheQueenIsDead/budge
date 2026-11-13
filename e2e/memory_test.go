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
	startMemory(p)
	assert.Equal(t, 1, eval[int](p, `document.querySelectorAll('input[name=tx]:checked').length`), "what was ticked is in it")
	nearby := eval[int](p, `document.querySelectorAll('input[name=tx]:not(:checked)').length`)
	if nearby > 0 {
		p.click("input[name=tx]:not(:checked)")
	}

	name(p, "Road trip")
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

	// Nothing changed yet, so nothing to save.
	saveable := func() bool {
		return !eval[bool](p, `document.querySelector('form.b-memory button.btn-primary').disabled`)
	}
	assert.False(t, saveable(), "Save waits for a change")
	p.click("#memory-note")
	p.typeText("Mostly petrol")
	p.eventually(func() error {
		if !saveable() {
			return fmt.Errorf("Save is still greyed out after the note changed")
		}
		return nil
	})

	p.visit("/timeline?view=memories")
	p.eventually(p.contains("#main", "Road trip"))
	defer func() {
		// The memory's header opens it.
		p.click(".b-tl-open")
		p.settle()
		p.eventually(p.is("#memory-title", "Road trip", p.value))
	}()
	purchases := p.text(".b-tl-memory .b-tl-rows summary")
	want := fmt.Sprintf("%d purchase", 1+min(nearby, 1))
	assert.True(t, strings.HasPrefix(purchases, want), "%q, not %q", purchases, want)
}

// TestDeleteAMemory deletes a memory from its page, opened from the timeline,
// and comes back to the timeline without it.
func TestDeleteAMemory(t *testing.T) {
	p := open(t)
	p.visit("/transactions?search=Z%20Energy")
	startMemory(p)
	name(p, "Road trip")

	p.visit("/timeline?view=memories")
	p.click(".b-tl-open")
	p.settle()
	p.eventually(p.is("#memory-title", "Road trip", p.value))

	// hx-confirm asks first, as a browser dialog, which is agreed to here.
	eval[bool](p, `(window.confirm = () => true, true)`)
	p.clickText("form.b-memory button", "Delete")
	p.settle()
	p.eventually(func() error {
		if at := eval[string](p, `location.pathname`); at != "/timeline" {
			return fmt.Errorf("the browser is at %s, not back on the timeline", at)
		}
		return nil
	})
	p.eventually(p.contains("#main", "No memories yet"))
}

// startMemory ticks the first transaction listed and opens a new memory's
// page with it.
func startMemory(p *page) {
	p.t.Helper()
	p.click(".b-tx-pick")
	p.clickText("#tx-group button", "New memory")
	p.settle()
	p.eventually(func() error {
		if at := eval[string](p, `location.pathname`); at != "/memories/new" {
			return fmt.Errorf("the browser is at %s, not the new memory's page", at)
		}
		return nil
	})
}

// name names the memory on the page and makes it.
func name(p *page, title string) {
	p.t.Helper()
	p.click("#memory-title")
	p.typeText(title)
	p.clickText("button.btn-primary", "Make memory")
	p.settle()
}
