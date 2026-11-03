package application

import (
	"net/http"
	"time"

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
}

// insightsHistory is how far back the page reads. Twelve monthly buckets need
// a year, plus a month of slack so the oldest bucket is not clipped.
const insightsHistory = 14

func (app *Application) Insights(c *echo.Context) error {
	ctx := c.Request().Context()

	now := time.Now()

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
	})
}
