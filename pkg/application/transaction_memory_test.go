package application

import (
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/TheQueenIsDead/budge/pkg/database/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTransactionAddToMemory(t *testing.T) {
	app := newTestApp(t)
	for _, tx := range []models.Transaction{
		purchase("museum", "Te Papa", 12, time.March, 2),
		purchase("ferry", "East by West", 30, time.March, 2),
	} {
		require.NoError(t, app.store.CreateTransaction(tx))
	}
	require.NoError(t, app.store.SaveTimelinePreferences(models.TimelinePreferences{HiddenTransactions: []string{"museum"}}))

	add := func(tx string, values url.Values) string {
		t.Helper()
		c, rec := postForm(t, app, values)
		c.SetParamNames("id")
		c.SetParamValues(tx)
		require.NoError(t, app.TransactionAddToMemory(c))
		return rec.Body.String()
	}

	var wellington string

	t.Run("starts a new memory", func(t *testing.T) {
		body := add("museum", url.Values{"title": {"Wellington weekend"}, "kind": {"trip"}, "layout": {"d"}})

		memories, err := app.store.ReadMemories()
		require.NoError(t, err)
		require.Len(t, memories, 1)
		wellington = memories[0].Id
		assert.Equal(t, "Wellington weekend", memories[0].Title)
		assert.Equal(t, []string{"museum"}, memories[0].TransactionIDs)

		// Only the control comes back: the one used in place, its twin out of band.
		assert.Contains(t, body, `id="txmem-d-museum"`)
		assert.Contains(t, body, `id="txmem-m-museum" hx-swap-oob="true"`)
		assert.NotContains(t, body, `id="txmem-d-museum" hx-swap-oob`)
		assert.Contains(t, body, "bi-bookmark-check-fill")

		prefs, err := app.store.GetTimelinePreferences()
		require.NoError(t, err)
		assert.Empty(t, prefs.HiddenTransactions, "a remembered purchase is no longer hidden")
	})

	t.Run("adds to an existing memory", func(t *testing.T) {
		add("ferry", url.Values{"memory": {wellington}, "layout": {"m"}})

		memory, err := app.store.GetMemory(wellington)
		require.NoError(t, err)
		assert.Equal(t, []string{"museum", "ferry"}, memory.TransactionIDs)
	})

	t.Run("moves a transaction out of the memory that held it", func(t *testing.T) {
		add("ferry", url.Values{"title": {"Ferry rides"}, "kind": {"experience"}})

		memory, err := app.store.GetMemory(wellington)
		require.NoError(t, err)
		assert.Equal(t, []string{"museum"}, memory.TransactionIDs)

		memories, err := app.store.ReadMemories()
		require.NoError(t, err)
		assert.Len(t, memories, 2)
	})

	t.Run("a memory that has gone is an error", func(t *testing.T) {
		c, _ := postForm(t, app, url.Values{"memory": {"missing"}})
		c.SetParamNames("id")
		c.SetParamValues("museum")
		assert.Error(t, app.TransactionAddToMemory(c))
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
		"kinds":    models.MemoryKinds,
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
		assert.Contains(t, page, "New memory&hellip;")
	})

	t.Run("a row already in a memory says which", func(t *testing.T) {
		assert.Contains(t, page, "bi-bookmark-check-fill")
		assert.Contains(t, page, `title="In “Wellington weekend”"`)
	})

	t.Run("moving offers every memory but the current one", func(t *testing.T) {
		control := page[indexOf(t, page, `id="txmem-m-museum"`):]
		control = control[:indexOf(t, control, "</details>")]
		assert.NotContains(t, control, `<option value="wgtn">`)
		assert.Contains(t, control, `<option value="bach">`)
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
