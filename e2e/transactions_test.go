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
