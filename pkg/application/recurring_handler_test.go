package application

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/TheQueenIsDead/budge/pkg/database/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// recurringApp is an app whose spending has a subscription whose price has
// just gone up, a yearly insurance due in a few days, and a cafe visited
// weekly, each counted back from today, as the handlers read the real clock.
func recurringApp(t *testing.T) *Application {
	t.Helper()
	app := newTestApp(t)
	today := localDay(time.Now(), time.Local)
	var txs []models.Transaction
	for i := 6; i >= 1; i-- {
		amount := 17.99
		if i == 1 {
			amount = 19.99
		}
		txs = append(txs, paid("Neon", "Entertainment (not elsewhere classified)", amount, today.AddDate(0, 0, -30*i+10)))
	}
	txs = append(txs, payments("AA Insurance", "Insurance", 1140, today.AddDate(0, 0, -365-361), today.AddDate(0, 0, -361))...)
	txs = append(txs, payments("Little Bird Cafe", "Cafes and restaurants", 6.50, every(7, 10, today.AddDate(0, 0, -1))...)...)
	for _, tx := range txs {
		require.NoError(t, app.store.CreateTransaction(t.Context(), tx))
	}
	return app
}

// recurringPage requests the recurring view, as a swap from the pills does.
func recurringPage(t *testing.T, app *Application) string {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/insights?view=recurring", nil)
	req.Header.Set("HX-Request", "true")
	rec := httptest.NewRecorder()
	require.NoError(t, app.Insights(app.http.NewContext(req, rec)))
	require.Equal(t, http.StatusOK, rec.Code)
	return rec.Body.String()
}

func TestRenderRecurring(t *testing.T) {
	app := recurringApp(t)
	page := recurringPage(t, app)

	t.Run("the pill is on, and the panel is swapped as the others are", func(t *testing.T) {
		assert.Contains(t, page, `href="/insights?view=recurring"`)
		assert.Regexp(t, `b-pill-active" href="/insights\?view=recurring"`, page)
		assert.Contains(t, page, `id="insights-panel"`)
		assert.NotContains(t, page, "insightsChart", "no category chart")
	})

	t.Run("what they come to", func(t *testing.T) {
		assert.Contains(t, page, "Recurring payments")
		// Neon at $19.99 a month, the insurance at $1,140 a year, and the cafe.
		yearly := 19.99*12 + 1140 + 6.5*52
		assert.Contains(t, page, fmtWhole(yearly))
		assert.Contains(t, page, fmtWhole(yearly/12))
	})

	t.Run("the insurance is coming up, with a rail as it is large", func(t *testing.T) {
		coming := page[strings.Index(page, `id="recurring-coming-up"`):]
		coming = coming[:strings.Index(coming, `id="recurring-weekly"`)]
		assert.Contains(t, coming, "AA Insurance")
		assert.Contains(t, coming, "b-rail")
		assert.Contains(t, coming, "$1,140.00")
	})

	t.Run("each is grouped by how often it comes round", func(t *testing.T) {
		for _, id := range []string{"recurring-weekly", "recurring-monthly", "recurring-yearly"} {
			assert.Contains(t, page, `id="`+id+`"`)
		}
		assert.Contains(t, page, "usual\u00a0$19.99 · monthly")
		assert.Contains(t, page, `href="/transactions?search=Little&#43;Bird&#43;Cafe"`, "each opens its payments")
	})

	t.Run("a price rise is marked", func(t *testing.T) {
		assert.Contains(t, page, "from $17.99")
	})

	t.Run("each can be marked either way", func(t *testing.T) {
		assert.Contains(t, page, `hx-post="/insights/recurring/mark"`)
		assert.Contains(t, page, `&#34;mark&#34;:&#34;not-recurring&#34;`)
		assert.Contains(t, page, `&#34;mark&#34;:&#34;cancelled&#34;`)
		assert.Contains(t, page, `title="Not recurring: hide it"`)
	})

	t.Run("with nothing set aside, nothing folded", func(t *testing.T) {
		assert.NotContains(t, page, `id="recurring-stopped"`)
		assert.NotContains(t, page, `id="recurring-hidden"`)
	})
}

func TestRenderRecurringEmpty(t *testing.T) {
	page := recurringPage(t, newTestApp(t))
	assert.Contains(t, page, "Nothing recurring yet")
	assert.Contains(t, page, "b-empty")
}

func TestRecurringMarkAndUndo(t *testing.T) {
	app := recurringApp(t)

	t.Run("not recurring hides it", func(t *testing.T) {
		c, rec := postForm(t, app, url.Values{"merchant": {"little bird cafe"}, "mark": {models.RecurringNotRecurring}})
		require.NoError(t, app.InsightsRecurringMark(c))
		page := rec.Body.String()
		hidden := page[strings.Index(page, `id="recurring-hidden"`):]
		assert.Contains(t, hidden, "Little Bird Cafe")
		assert.Contains(t, hidden, `hx-post="/insights/recurring/undo"`)
		assert.NotContains(t, page, `id="recurring-weekly"`, "it has left the active list")

		marks, err := app.store.GetRecurringMarks(t.Context())
		require.NoError(t, err)
		assert.Equal(t, map[string]string{"little bird cafe": models.RecurringNotRecurring}, marks.Marks)
	})

	t.Run("cancelled stops it", func(t *testing.T) {
		c, rec := postForm(t, app, url.Values{"merchant": {"neon"}, "mark": {models.RecurringCancelled}})
		require.NoError(t, app.InsightsRecurringMark(c))
		page := rec.Body.String()
		stopped := page[strings.Index(page, `id="recurring-stopped"`):strings.Index(page, `id="recurring-hidden"`)]
		assert.Contains(t, stopped, "Neon")
		assert.NotContains(t, page, `id="recurring-monthly"`)
		assert.Contains(t, page, fmtWhole(1140), "and leaves the totals")
	})

	t.Run("undo brings it back", func(t *testing.T) {
		c, rec := postForm(t, app, url.Values{"merchant": {"little bird cafe"}})
		require.NoError(t, app.InsightsRecurringUndo(c))
		page := rec.Body.String()
		assert.Contains(t, page, `id="recurring-weekly"`)
		assert.NotContains(t, page, `id="recurring-hidden"`)

		marks, err := app.store.GetRecurringMarks(t.Context())
		require.NoError(t, err)
		assert.Equal(t, map[string]string{"neon": models.RecurringCancelled}, marks.Marks)
	})

	t.Run("a mark budge does not know is refused", func(t *testing.T) {
		c, _ := postForm(t, app, url.Values{"merchant": {"neon"}, "mark": {"whatever"}})
		assert.Error(t, app.InsightsRecurringMark(c))
	})

	t.Run("as is one naming no payment", func(t *testing.T) {
		c, _ := postForm(t, app, url.Values{"mark": {models.RecurringCancelled}})
		assert.Error(t, app.InsightsRecurringMark(c))
		c, _ = postForm(t, app, url.Values{})
		assert.Error(t, app.InsightsRecurringUndo(c))
	})
}
