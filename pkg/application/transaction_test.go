package application

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"testing"
	"time"

	"github.com/TheQueenIsDead/budge/pkg/database/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTransactionsByGroup(t *testing.T) {
	app := newTestApp(t)
	recent := time.Now().Add(-48 * time.Hour)
	for _, tx := range []models.Transaction{
		spendTx("pak", "EFTPOS", "Food", "Supermarkets and grocery stores", "PAK'nSAVE", "", -80, recent),
		spendTx("refund", "CREDIT", "Food", "Supermarkets and grocery stores", "Countdown Refund", "", 20, recent),
		spendTx("z", "EFTPOS", "", "", "Z Energy", "", -60, recent),
		spendTx("metro", "EFTPOS", "", "", "Metro Card", "", -15, recent),
		spendTx("sweep", "TRANSFER", "Transfer", "Transfer", "", "TO SAVINGS", -900, recent),
	} {
		require.NoError(t, app.store.CreateTransaction(t.Context(), tx))
	}
	setCategory(t, app, "z", url.Values{"category": {"fuel"}})
	setCategory(t, app, "metro", url.Values{"category": {"transport"}})

	get := func(query string) string {
		req := httptest.NewRequest(http.MethodGet, "/transactions?"+query, nil)
		req.Header.Set("HX-Request", "true")
		rec := httptest.NewRecorder()
		require.NoError(t, app.Transactions(app.http.NewContext(req, rec)))
		return rec.Body.String()
	}
	shows := func(t *testing.T, page string, want ...string) {
		t.Helper()
		for _, name := range []string{"PAK&#39;nSAVE", "Countdown Refund", "Z Energy", "Metro Card", "TO SAVINGS"} {
			if slices.Contains(want, name) {
				assert.Contains(t, page, name)
			} else {
				assert.NotContains(t, page, name)
			}
		}
	}

	t.Run("a category takes in its subcategories", func(t *testing.T) {
		page := get("group=transport")
		shows(t, page, "Z Energy", "Metro Card")
		assert.Regexp(t, `bi-tags"></i> Transport <i class="bi bi-x">`, page, "a chip says what the list is narrowed to")
	})

	t.Run("a general row is only what is filed under the category itself", func(t *testing.T) {
		page := get("group=transport:general")
		shows(t, page, "Metro Card")
		assert.Contains(t, page, "Transport (general)")
		assert.Contains(t, page, `name="group" value="transport:general"`, "searching keeps the groups")
	})

	t.Run("several groups add up to the spending in each, and nothing else", func(t *testing.T) {
		page := get("group=groceries&group=fuel")
		shows(t, page, "PAK&#39;nSAVE", "Z Energy")
		assert.Contains(t, page, "Groceries, Fuel")
		assert.Regexp(t, `bi-arrow-up-right"></i>\$140\.00`, page, "money in is left out, so the total is the spending")
	})

	t.Run("a category that no longer exists is ignored", func(t *testing.T) {
		page := get("group=nonsense")
		shows(t, page, "PAK&#39;nSAVE", "Countdown Refund", "Z Energy", "Metro Card", "TO SAVINGS")
		assert.NotContains(t, page, "bi-tags")
	})

	t.Run("a category tag narrows the list to its category", func(t *testing.T) {
		page := get("")
		assert.Contains(t, page, `href="/transactions?group=transport"`, "the category")
		assert.Contains(t, page, `href="/transactions?group=fuel"`, "and the subcategory")
		assert.Contains(t, page, `hx-include="#search, #account, #filter-from, #filter-to"`, "keeping the other filters")
		assert.Contains(t, page, `id="filter-from" name="from"`)

		rec := setCategory(t, app, "metro", url.Values{"category": {"parking"}})
		assert.Contains(t, rec.Body.String(), `href="/transactions?group=parking"`, "and so does a tag redrawn after filing")
	})

	t.Run("clearing the dates keeps the groups", func(t *testing.T) {
		page := get("group=fuel&from=2020-01-01")
		assert.Contains(t, page, `href="/transactions?search=&account=&group=fuel"`)
	})
}

func TestTransactionsLink(t *testing.T) {
	start := time.Date(2026, time.August, 4, 0, 0, 0, 0, time.Local)
	assert.Equal(t, "/transactions?from=2026-08-04&group=rates&group=home%3Ageneral&to=2026-11-03",
		transactionsLink([]string{"rates", "home:general"}, start, time.Date(2026, time.November, 4, 0, 0, 0, 0, time.Local)),
		"the end is exclusive, and the page reads its last day inclusively")
}
