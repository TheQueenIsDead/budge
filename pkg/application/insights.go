package application

import (
	"net/http"
	"time"

	"github.com/TheQueenIsDead/budge/pkg/database/models"
	"github.com/labstack/echo/v4"
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

func (app *Application) Insights(c echo.Context) error {

	now := time.Now()

	transactions, err := app.store.ReadTransactionsByDate(now.AddDate(0, -insightsHistory, 0), now)
	if err != nil {
		app.Toast(c, "Error", "Could not load spending insights.")
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	groups := SpendGroups()

	// An unknown or missing group falls back to the first tracked one rather
	// than erroring, so a stale bookmark still lands somewhere useful.
	selected := groups[0]
	if group, ok := SpendGroupByKey(c.QueryParam("group")); ok {
		selected = group
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
