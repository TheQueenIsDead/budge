package application

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/TheQueenIsDead/budge/pkg/database/models"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTransactionAddToMemory(t *testing.T) {
	app := newTestApp(t)
	require.NoError(t, app.store.SaveMemory(models.Memory{Id: "wgtn", Title: "Wellington weekend", Kind: "trip", TransactionIDs: []string{"hotel"}}))
	require.NoError(t, app.store.SaveMemory(models.Memory{Id: "ferries", Title: "Ferry rides", TransactionIDs: []string{"bluebridge"}}))
	require.NoError(t, app.store.SaveTimelinePreferences(models.TimelinePreferences{HiddenTransactions: []string{"museum"}}))

	add := func(tx string, values url.Values) (string, error) {
		t.Helper()
		c, rec := postForm(t, app, values)
		c.SetParamNames("id")
		c.SetParamValues(tx)
		err := app.TransactionAddToMemory(c)
		return rec.Body.String(), err
	}

	t.Run("adds to the memory picked", func(t *testing.T) {
		body, err := add("museum", url.Values{"memory": {"wgtn"}, "layout": {"d"}})
		require.NoError(t, err)

		memory, err := app.store.GetMemory("wgtn")
		require.NoError(t, err)
		assert.Equal(t, []string{"hotel", "museum"}, memory.TransactionIDs)

		// Only the control comes back: the one used in place, its twin out of band.
		assert.Contains(t, body, `id="txmem-d-museum"`)
		assert.Contains(t, body, `id="txmem-m-museum" hx-swap-oob="true"`)
		assert.NotContains(t, body, `id="txmem-d-museum" hx-swap-oob`)
		assert.Contains(t, body, "bi-bookmark-check-fill")

		prefs, err := app.store.GetTimelinePreferences()
		require.NoError(t, err)
		assert.Empty(t, prefs.HiddenTransactions, "a remembered purchase is no longer hidden")
	})

	t.Run("moves a transaction out of the memory that held it", func(t *testing.T) {
		_, err := add("museum", url.Values{"memory": {"ferries"}})
		require.NoError(t, err)

		memory, err := app.store.GetMemory("wgtn")
		require.NoError(t, err)
		assert.Equal(t, []string{"hotel"}, memory.TransactionIDs)
	})

	t.Run("new memories are not made from here", func(t *testing.T) {
		_, err := add("coffee", url.Values{"title": {"Coffee"}, "kind": {"other"}})
		assert.Error(t, err)

		memories, err := app.store.ReadMemories()
		require.NoError(t, err)
		assert.Len(t, memories, 2)
	})

	t.Run("a memory that has gone is an error", func(t *testing.T) {
		_, err := add("museum", url.Values{"memory": {"missing"}})
		assert.Error(t, err)
	})
}

// deleteQuery builds a DELETE the way htmx 2 sends one, with its values in
// the query string rather than a body.
func deleteQuery(app *Application, values url.Values) (echo.Context, *httptest.ResponseRecorder) {
	req := httptest.NewRequest(http.MethodDelete, "/?"+values.Encode(), nil)
	rec := httptest.NewRecorder()
	return app.http.NewContext(req, rec), rec
}

func TestTransactionRemoveFromMemory(t *testing.T) {
	app := newTestApp(t)
	require.NoError(t, app.store.SaveMemory(models.Memory{Id: "wgtn", Title: "Wellington weekend", TransactionIDs: []string{"museum", "ferry"}}))

	remove := func(tx string) string {
		t.Helper()
		c, rec := deleteQuery(app, url.Values{"layout": {"d"}})
		c.SetParamNames("id")
		c.SetParamValues(tx)
		require.NoError(t, app.TransactionRemoveFromMemory(c))
		return rec.Body.String()
	}

	t.Run("takes one transaction out, leaving the rest", func(t *testing.T) {
		body := remove("ferry")

		memory, err := app.store.GetMemory("wgtn")
		require.NoError(t, err)
		assert.Equal(t, []string{"museum"}, memory.TransactionIDs)

		assert.Contains(t, body, `id="txmem-d-ferry"`)
		assert.Contains(t, body, `id="txmem-m-ferry" hx-swap-oob="true"`)
		assert.Contains(t, body, "bi-bookmark-plus", "the control goes back to offering to add it")
		assert.NotContains(t, body, `id="tx-group-fields"`, "a single row leaves the selection bar alone")
	})

	t.Run("taking out the last one removes the memory", func(t *testing.T) {
		remove("museum")

		memories, err := app.store.ReadMemories()
		require.NoError(t, err)
		assert.Empty(t, memories)
	})
}

func TestTransactionsMemoryInBulk(t *testing.T) {
	app := newTestApp(t)
	require.NoError(t, app.store.SaveMemory(models.Memory{Id: "bach", Title: "Bach", TransactionIDs: []string{"groceries"}}))
	require.NoError(t, app.store.SaveMemory(models.Memory{Id: "wgtn", Title: "Wellington weekend", Kind: "trip", TransactionIDs: []string{"hotel"}}))

	t.Run("adds every ticked transaction to the memory picked", func(t *testing.T) {
		c, rec := postForm(t, app, url.Values{
			"tx":     {"museum", "ferry", "museum", "groceries"},
			"memory": {"wgtn"},
		})
		require.NoError(t, app.TransactionsAddToMemory(c))

		memories, err := app.store.ReadMemories()
		require.NoError(t, err)
		require.Len(t, memories, 1, "the bach lost its only transaction to Wellington")
		assert.Equal(t, []string{"hotel", "museum", "ferry", "groceries"}, memories[0].TransactionIDs)

		body := rec.Body.String()
		for _, id := range []string{"museum", "ferry", "groceries"} {
			assert.Contains(t, body, `id="txmem-m-`+id+`" hx-swap-oob="true"`)
			assert.Contains(t, body, `id="txmem-d-`+id+`" hx-swap-oob="true"`)
		}
		assert.Contains(t, body, `id="tx-group-fields" class="b-tl-new-wrap" hx-swap-oob="true"`)
		assert.NotContains(t, body, `<option value="bach">`, "a memory emptied by the move is no longer offered")
	})

	t.Run("takes every ticked transaction out of its memory", func(t *testing.T) {
		c, _ := deleteQuery(app, url.Values{"tx": {"museum", "ferry"}})
		require.NoError(t, app.TransactionsRemoveFromMemory(c))

		memory, err := app.store.GetMemory("wgtn")
		require.NoError(t, err)
		assert.Equal(t, []string{"hotel", "groceries"}, memory.TransactionIDs)
	})

	t.Run("nothing ticked is an error", func(t *testing.T) {
		c, _ := postForm(t, app, url.Values{"memory": {"wgtn"}})
		assert.Error(t, app.TransactionsAddToMemory(c))
	})

	t.Run("no memory picked is an error", func(t *testing.T) {
		c, _ := postForm(t, app, url.Values{"tx": {"coffee"}, "title": {"Coffee"}})
		assert.Error(t, app.TransactionsAddToMemory(c))
	})
}

func TestRenderTransactionsMemoryControls(t *testing.T) {
	transactions := []models.Transaction{
		purchase("museum", "Te Papa", 12, time.March, 2),
		purchase("coffee", "Cafe", 5, time.March, 1),
	}
	days := GroupTransactionsByDay(transactions, time.UTC)
	markMemories(days, map[string]models.Memory{"museum": {Id: "wgtn", Title: "Wellington weekend"}})

	page := renderTemplate(t, "transactions", map[string]interface{}{
		"days":     days,
		"memories": []TransactionMemoryOption{{Id: "wgtn", Label: "Wellington weekend · Trip"}, {Id: "bach", Label: "Bach · Moment"}},
		"in":       0.0,
		"out":      -17.0,
		"took":     time.Millisecond,
		"total":    2,
		"showing":  2,
	})

	t.Run("every row offers to add it to a memory, in both layouts", func(t *testing.T) {
		assert.Contains(t, page, `id="txmem-m-coffee"`)
		assert.Contains(t, page, `id="txmem-d-coffee"`)
		assert.Contains(t, page, `hx-post="/transactions/coffee/memory"`)
		assert.Contains(t, page, "bi-bookmark-plus")
		assert.Contains(t, page, `<option value="bach">Bach · Moment</option>`)
	})

	t.Run("only existing memories are offered, saved as soon as one is picked", func(t *testing.T) {
		control := page[indexOf(t, page, `id="txmem-d-coffee"`):]
		control = control[:indexOf(t, control, "</details>")]
		assert.Contains(t, control, `hx-trigger="change"`)
		assert.NotContains(t, control, "New memory")
		assert.NotContains(t, control, `name="title"`)
		assert.NotContains(t, control, "Save")
	})

	t.Run("a row already in a memory says which", func(t *testing.T) {
		assert.Contains(t, page, "bi-bookmark-check-fill")
		assert.Contains(t, page, `title="In “Wellington weekend”. Click to take it out"`)
	})

	t.Run("a row already in a memory is taken out with one click", func(t *testing.T) {
		control := page[indexOf(t, page, `id="txmem-d-museum"`):]
		control = control[:indexOf(t, control, "</button>")]
		assert.Contains(t, control, `hx-delete="/transactions/museum/memory"`)
		assert.NotContains(t, control, "<select", "nothing to fill in first")

		other := page[indexOf(t, page, `id="txmem-d-coffee"`):]
		other = other[:indexOf(t, other, "</details>")]
		assert.NotContains(t, other, "hx-delete", "nothing to take out of")
	})

	t.Run("every row can be ticked into the selection bar", func(t *testing.T) {
		assert.Equal(t, 4, strings.Count(page, `form="tx-group"`), "two rows, each drawn twice")
		assert.Equal(t, 4, strings.Count(page, `<label class="b-tx-pick"`), "the icon is the tick box")
		assert.Contains(t, page, `<form id="tx-group"`)
		assert.Contains(t, page, `hx-delete="/transactions/memory"`)
	})

	t.Run("only the filters reload the list", func(t *testing.T) {
		// Picking a memory in the selection bar used to reload the page,
		// clearing the ticks and the bar with them.
		assert.Contains(t, page, `change from:#account`)
		assert.NotContains(t, page, `from:select`)
	})
}

func indexOf(t *testing.T, s, substr string) int {
	t.Helper()
	i := strings.Index(s, substr)
	require.GreaterOrEqual(t, i, 0, "missing %q", substr)
	return i
}

func TestTransactionDateRange(t *testing.T) {
	from, to, ok := transactionDateRange("2026-07-09", "2026-07-11", time.UTC)
	require.True(t, ok)
	assert.Equal(t, time.Date(2026, time.July, 9, 0, 0, 0, 0, time.UTC), from)
	assert.Equal(t, time.Date(2026, time.July, 12, 0, 0, 0, 0, time.UTC), to, "the last day is included")
	assert.Equal(t, "9 – 11 Jul 2026", transactionDateLabel("2026-07-09", "2026-07-11", from, to))

	from, to, _ = transactionDateRange("2025-12-28", "2026-01-04", time.UTC)
	assert.Equal(t, "28 Dec 2025 – 4 Jan 2026", transactionDateLabel("2025-12-28", "2026-01-04", from, to))

	from, to, ok = transactionDateRange("2026-07-09", "", time.UTC)
	require.True(t, ok)
	assert.Equal(t, "From 9 Jul 2026", transactionDateLabel("2026-07-09", "", from, to))

	_, _, ok = transactionDateRange("nonsense", "", time.UTC)
	assert.False(t, ok, "a date that does not parse is ignored")
}
