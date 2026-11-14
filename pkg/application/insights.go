package application

import (
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/TheQueenIsDead/budge/pkg/clock"
	"github.com/TheQueenIsDead/budge/pkg/database/models"
	"github.com/labstack/echo/v5"
)

// InsightsData drives the per-category deep dive. The page shows one group at a
// time so a phone screen carries a full chart rather than five cramped ones.
type InsightsData struct {
	Groups   []SpendGroup
	Selected SpendGroup
	Cadence  Cadence

	Summary   SpendSummary
	Merchants []models.MerchantTotal

	WindowStart time.Time
	WindowEnd   time.Time

	// Links open the transactions behind each bar of the chart, oldest first,
	// so the last two are this period and the one before.
	Links []string

	// View is InsightsRecurring for the recurring payments, in place of a
	// category, or empty.
	View      string
	Recurring RecurringView
}

// InsightsRecurring is the view of the recurring payments, one more pill
// after the categories.
const InsightsRecurring = "recurring"

// CurrentLink opens the transactions behind the period in progress.
func (d InsightsData) CurrentLink() string { return d.link(1) }

// PreviousLink opens the transactions behind the last completed period.
func (d InsightsData) PreviousLink() string { return d.link(2) }

// MerchantLink opens a merchant's transactions behind its total: in the
// selected category, over the window the chart covers.
func (d InsightsData) MerchantLink(merchant string) string {
	return transactionsLink([]string{d.Selected.Key}, d.WindowStart, d.WindowEnd) + "&search=" + url.QueryEscape(merchant)
}

// link is the nth bar's link counting back from the last, or none.
func (d InsightsData) link(n int) string {
	if len(d.Links) < n {
		return ""
	}
	return d.Links[len(d.Links)-n]
}

// insightsHistory is how far back the page reads. Twelve monthly buckets need
// a year, plus a month of slack so the oldest bucket is not clipped.
const insightsHistory = 14

func (app *Application) Insights(c *echo.Context) error {
	if c.QueryParam("view") == InsightsRecurring {
		return app.renderRecurring(c)
	}
	ctx := c.Request().Context()

	now := clock.Now()

	transactions, err := app.store.ReadTransactionsByDate(ctx, now.AddDate(0, -insightsHistory, 0), now)
	transactions = spendingOnly(transactions)
	if err != nil {
		app.Toast(c, "Error", "Could not load spending insights.")
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	groups := SpendGroups()
	if len(groups) == 0 {
		return echo.NewHTTPError(http.StatusNotFound, "Every tracked category has been deleted.")
	}

	// An unknown or missing group falls back to the first tracked one rather
	// than erroring, so a stale bookmark still lands somewhere useful.
	selected := groups[0]
	if group, ok := SpendGroupFor(c.QueryParam("group")); ok {
		selected = group
		// A category opened from the budget joins the tracked ones while it
		// is looked at, so the pills say what is shown.
		if _, tracked := SpendGroupByKey(group.Key); !tracked {
			groups = append(groups, group)
		}
	}

	cadence := Cadence(c.QueryParam("cadence"))
	if !cadence.Valid() {
		cadence = selected.Cadence
	}

	summary := BuildSpendSummary(transactions, selected, cadence, now)
	start, end := summary.Window()

	return c.Render(http.StatusOK, "insights", InsightsData{
		Groups:      groups,
		Selected:    selected,
		Cadence:     cadence,
		Summary:     summary,
		Merchants:   BuildSpendMerchants(transactions, selected, start, end, 12),
		WindowStart: start,
		WindowEnd:   end,
		Links:       summary.Links(),
	})
}

// renderRecurring draws the Insights page on its recurring payments.
func (app *Application) renderRecurring(c *echo.Context) error {
	found, marks, err := app.readRecurring(c.Request().Context(), clock.Now())
	if err != nil {
		app.Toast(c, "Error", "Could not load the recurring payments.")
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	return c.Render(http.StatusOK, "insights", InsightsData{
		Groups:    SpendGroups(),
		View:      InsightsRecurring,
		Recurring: BuildRecurringView(found, marks.Marks),
	})
}

// InsightsRecurringMark records what the owner says of a recurring payment:
// that it is not recurring, or has been cancelled. Either leaves it out of
// the totals and stops it raising anything.
func (app *Application) InsightsRecurringMark(c *echo.Context) error {
	mark := c.FormValue("mark")
	if !models.ValidRecurringMark(mark) {
		return echo.NewHTTPError(http.StatusBadRequest, "Mark it not recurring, or cancelled.")
	}
	return app.updateRecurringMarks(c, func(marks map[string]string, key string) { marks[key] = mark })
}

// InsightsRecurringUndo takes back what the owner said of a recurring
// payment, so it is found as its payments say again.
func (app *Application) InsightsRecurringUndo(c *echo.Context) error {
	return app.updateRecurringMarks(c, func(marks map[string]string, key string) { delete(marks, key) })
}

func (app *Application) updateRecurringMarks(c *echo.Context, apply func(marks map[string]string, key string)) error {
	ctx := c.Request().Context()
	key := strings.TrimSpace(c.FormValue("merchant"))
	if key == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "Say which payment.")
	}
	marks, err := app.store.GetRecurringMarks(ctx)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	apply(marks.Marks, key)
	if err := app.store.SaveRecurringMarks(ctx, marks); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	return app.renderRecurring(c)
}
