//go:build e2e

package e2e

import (
	"fmt"
	"strings"
	"testing"
	"time"

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

	// On the timeline the memory is just its header, which opens it, with
	// what it holds on its page.
	p.visit("/timeline?view=memories")
	p.eventually(p.contains("#main", "Road trip"))
	assert.False(t, eval[bool](p, `!!document.querySelector('.b-tl-memory .b-tl-rows, .b-tl-memory .b-tl-actions')`),
		"the card lists nothing and offers nothing past its header")
	p.click(".b-tl-open")
	p.settle()
	p.eventually(p.is("#memory-title", "Road trip", p.value))
	held := eval[int](p, `document.querySelectorAll('input[name=tx]:checked').length`)
	assert.Equal(t, 1+min(nearby, 1), held, "what was ticked, and the purchase added from around then")
}

// TestMemoryOffersItsDays starts a memory of two fill-ups ten days apart and
// checks its page offers what was bought between them, not just on and
// around their own days.
func TestMemoryOffersItsDays(t *testing.T) {
	p := open(t)
	p.visit("/transactions?search=Z%20Energy")

	// The two latest fill-ups, ten days apart in the seed.
	ids := eval[[]string](p, `[...document.querySelectorAll('.b-tx-pick input')].slice(0, 2).map(i => i.value)`)
	if len(ids) < 2 {
		t.Fatalf("want two fill-ups to tick, have %v", ids)
	}
	for _, id := range ids {
		p.click(fmt.Sprintf(`.b-tx-pick:has(input[value=%q])`, id))
	}
	p.clickText("#tx-group button", "New memory")
	p.settle()
	p.eventually(func() error {
		if at := eval[string](p, `location.pathname`); at != "/memories/new" {
			return fmt.Errorf("the browser is at %s, not the new memory's page", at)
		}
		return nil
	})

	// The seed's ids carry their day, as trans_seed_YYYYMMDD_n.
	day := func(id string) time.Time {
		d, err := time.Parse("20060102", strings.Split(id, "_")[2])
		if err != nil {
			t.Fatalf("%s carries no day: %v", id, err)
		}
		return d
	}
	held := eval[[]string](p, `[...document.querySelectorAll('input[name=tx]:checked')].map(i => i.value)`)
	offered := eval[[]string](p, `[...document.querySelectorAll('input[name=tx]:not(:checked)')].map(i => i.value)`)
	assert.ElementsMatch(t, ids, held, "what was ticked is in it")

	// Clear of the day either side of each fill-up, which were offered
	// before the whole range was.
	first, last := day(ids[0]), day(ids[1])
	if first.After(last) {
		first, last = last, first
	}
	var between []string
	for _, id := range offered {
		if d := day(id); d.After(first.AddDate(0, 0, 1)) && d.Before(last.AddDate(0, 0, -1)) {
			between = append(between, id)
		}
	}
	assert.NotEmpty(t, between, "something bought days into the stretch is offered, of %v", offered)
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

// name names the memory on the page and makes it, waiting until it is made:
// the page is left only once the memory is saved, and a test that moved on
// sooner could find it not there yet.
func name(p *page, title string) {
	p.t.Helper()
	p.click("#memory-title")
	p.typeText(title)
	p.clickText("button.btn-primary", "Make memory")
	p.settle()
	p.eventually(func() error {
		if at := eval[string](p, `location.pathname`); at == "/memories/new" {
			return fmt.Errorf("the memory isn't made yet")
		}
		return nil
	})
}
