package application

import (
	"context"
	"crypto/rand"
	"fmt"
	"math"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/TheQueenIsDead/budge/pkg/database/models"
	"github.com/labstack/echo/v4"
)

// spendCategory decides whether a transaction is spending and, if it is,
// which budge category it budgets under. Every budget view goes through here
// so the chart and the targets always count the same money. Savings and loan
// interest leave without being spent, so they are not spending here.
func spendCategory(tx models.Transaction) (models.BudgetCategory, bool) {
	category, ok := debitCategory(tx)
	if !ok || category.NotSpending {
		return models.BudgetCategory{}, false
	}
	return category, true
}

// debitCategory is the budge category money leaving went to, whether or not
// it was spent. Transfers between the owner's own accounts went nowhere.
func debitCategory(tx models.Transaction) (models.BudgetCategory, bool) {
	if !tx.IsDebit() {
		return models.BudgetCategory{}, false
	}
	return models.TopBudgetCategory(tx.BudgetCategory()), true
}

// spendMerchant names who a transaction was paid to. Uncategorised debits
// rarely carry a merchant, so the cleaned bank description stands in for it.
// Sync names every transfer's merchant "Transfer", which tells two transfers
// apart no better than nothing, so they always use the description.
func spendMerchant(tx models.Transaction) string {
	if tx.IsTransfer() {
		tx.Merchant.Name = ""
	}
	if tx.Merchant.Name != "" {
		return tx.Merchant.Name
	}
	return timelineTitle(BuildTransactionRow(tx))
}

// TargetSection is the categories on budget, or those off it, on the budget
// page.
type TargetSection struct {
	Key      string
	Title    string
	Budgeted bool // whether its categories take targets
	Rows     []CategoryTargetRow
	Actual   float64 // weekly, across the section's rows
	Target   float64 // weekly, across the section's rows
	Open     bool
	OOB      bool // drawn as an out-of-band update after an edit
}

// smallSpendWeekly is the weekly spend under which a category with no target
// is not asked for one: a few dollars a week is not what tips a budget over.
const smallSpendWeekly = 5.0

// budgetSections are the budget page's two lists: the categories on budget,
// and those off it.
var budgetSections = []TargetSection{
	{Key: "on-budget", Title: "On budget", Budgeted: true, Open: true},
	{Key: "off-budget", Title: "Off budget"},
}

// buildTargetSections lays rows out on budget and off it, in the order the
// categories are listed. On budget is open; off budget opens only when it
// holds the row being edited.
func buildTargetSections(rows []CategoryTargetRow, openRow string) []TargetSection {
	sections := append([]TargetSection(nil), budgetSections...)
	for _, row := range rows {
		section := &sections[0]
		if !row.Budgeted {
			section = &sections[1]
		}
		row.Open = row.RowID == openRow
		section.Rows = append(section.Rows, row)
		section.Actual += row.ActualWeekly
		section.Target += row.TargetWeekly
		if row.Open {
			section.Open = true
		}
	}

	kept := sections[:0]
	for _, section := range sections {
		if len(section.Rows) > 0 {
			sort.SliceStable(section.Rows, func(i, j int) bool { return section.Rows[i].Order < section.Rows[j].Order })
			kept = append(kept, section)
		}
	}
	return kept
}

// CategorySetupData is the view-model for the budget.setup partial.
type CategorySetupData struct {
	Tracking          TrackingSummary
	Rows              []CategoryTargetRow
	Sections          []TargetSection
	OpenRow           string // RowID of the row being edited, kept open
	HasData           bool
	NetWeekly         float64
	TotalTargetWeekly float64
	// Generous names the categories whose targets could come down, and Small
	// those spending too little to need one, for the note at the top.
	Generous []string
	Small    []string
	OOB      bool // drawn as an out-of-band update after an edit
}

// Note words the tidying up the budget could do, for the top of the page:
// targets that could come down, and categories too small to need one.
func (d CategorySetupData) Note() string {
	var parts []string
	if n := len(d.Generous); n > 0 {
		parts = append(parts, sentenceList(d.Generous)+" could have "+wordFor(n, "a lower target", "lower targets"))
	}
	if n := len(d.Small); n > 0 {
		parts = append(parts, sentenceList(d.Small)+" "+wordFor(n, "spends", "spend")+" too little to need "+wordFor(n, "a target", "targets"))
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, " · ") + "."
}

// sentenceList joins names as prose: "A", "A and B", "A, B and C".
func sentenceList(names []string) string {
	if len(names) < 2 {
		return strings.Join(names, "")
	}
	return strings.Join(names[:len(names)-1], ", ") + " and " + names[len(names)-1]
}

// wordFor picks the wording for one thing or several.
func wordFor(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// Budget shows how spending is tracking against the targets. Each category is
// measured over a window matched to its target's frequency rather than week
// by week, so a fortnightly mortgage or a monthly power bill reads as on
// track instead of spiking in the week it is paid.
func (app *Application) Budget(c echo.Context) error {
	ctx := c.Request().Context()
	salary, err := app.store.GetBudgetSalary(ctx)
	if err != nil {
		app.Toast(c, "Error", "Could not load salary config.")
		return c.NoContent(http.StatusInternalServerError)
	}
	setupData, err := app.buildSetupData(ctx)
	if err != nil {
		app.Toast(c, "Error", "Could not load category data.")
		return c.NoContent(http.StatusInternalServerError)
	}
	return c.Render(http.StatusOK, "budget", map[string]interface{}{
		"Income":    buildIncomeTile(salary),
		"SetupData": setupData,
	})
}

// buildSetupData measures 13 months of money leaving against the targets,
// one row for every budge category, so a target can be set for a category
// before anything has been spent in it.
func (app *Application) buildSetupData(ctx context.Context) (CategorySetupData, error) {
	salary, err := app.store.GetBudgetSalary(ctx)
	if err != nil {
		return CategorySetupData{}, err
	}
	netWeekly := computeNetWeekly(salary)

	txs, err := app.store.ReadTransactionsByDate(ctx, time.Now().AddDate(0, -13, 0), time.Now())
	if err != nil {
		return CategorySetupData{}, err
	}

	items, err := app.store.ReadBudgetItems(ctx)
	if err != nil {
		return CategorySetupData{}, err
	}
	targets := budgetTargets(items)

	// spend[key][weeks] is the total within that many weeks back; every window
	// a transaction falls inside counts it, so each is a plain total. Keys are
	// categories, and subcategories for the split under each row.
	spend := make(map[string]map[int]float64)
	merchantSpend := make(map[string]map[string]float64)
	windows := []int{4, 8, 13, 26, 52}
	weekStart := isoWeekStart(time.Now())
	add := func(key string, weeks int, amount float64) {
		if spend[key] == nil {
			spend[key] = make(map[int]float64)
		}
		spend[key][weeks] += amount
	}

	for _, tx := range txs {
		category, ok := debitCategory(tx)
		if !ok {
			continue
		}
		amt := math.Abs(tx.Amount)
		filed := tx.BudgetCategory()
		if filed == category.Key {
			// Filed under the category itself, which a split category
			// budgets as its general row.
			filed = generalKey(category.Key)
		}
		if name := spendMerchant(tx); name != "" {
			for _, key := range []string{category.Key, filed} {
				if merchantSpend[key] == nil {
					merchantSpend[key] = make(map[string]float64)
				}
				merchantSpend[key][name] += amt
			}
		}
		for _, weeks := range windows {
			if tx.Date.After(weekStart.AddDate(0, 0, -7*weeks)) {
				add(category.Key, weeks, amt)
				add(filed, weeks, amt)
			}
		}
	}

	measure := func(category models.BudgetCategory) CategoryTargetRow {
		saved := targets[category.Key]
		if saved.Frequency == "" {
			saved.Frequency = category.Frequency
		}
		// A category with no target is measured over 13 weeks, so one odd
		// month does not set what it is offered, or over its own longer window
		// when it is judged quarterly or yearly.
		weeks := actualWindowWeeks(saved.Frequency)
		if weeklyTarget(saved) == 0 {
			weeks = max(untargetedWindowWeeks, weeks)
		}
		row := buildSetupRowData(category, saved, spend[category.Key][weeks]/float64(weeks), netWeekly)
		row.WindowWeeks = weeks
		if row.NeedsTarget {
			measured := actualWindowWeeks(saved.Frequency)
			suggestTarget(&row, spend[category.Key][measured]/float64(measured))
		}
		if row.HasTarget && !row.IsOver {
			markGenerous(&row, spend[category.Key][untargetedWindowWeeks]/untargetedWindowWeeks)
		}
		row.Merchants = topMerchants(merchantSpend[category.Key])
		return row
	}

	var rows []CategoryTargetRow
	for _, category := range models.TopBudgetCategories() {
		subs := models.BudgetSubcategories(category.Key)
		if !budgetedSplit(category, targets) {
			row := measure(category)
			row.Splittable = len(subs) > 0 && category.Budgeted()
			row.Subs = subcategorySpend(category, spend, row.WindowWeeks)
			rows = append(rows, row)
			continue
		}

		// Split: each subcategory against its own target and period, what is
		// filed under the category itself as a general bucket of its own, and
		// the category the sum of them. The general bucket is always there,
		// so it is plain where anything not in a subcategory goes.
		var subRows []CategoryTargetRow
		for _, sub := range subs {
			subRow := measure(sub)
			subRow.ParentRowID = sanitizeID(category.Key)
			subRows = append(subRows, subRow)
		}
		generalRow := measure(generalCategory(category))
		// Its own prefix, so it cannot share an id with a category whose
		// name happens to read the same, like one called "Shopping general".
		generalRow.RowID = sanitizeWithPrefix("sg-", category.Key)
		generalRow.ParentRowID = sanitizeID(category.Key)
		generalRow.General = true
		subRows = append(subRows, generalRow)
		rows = append(rows, splitRow(category, subRows, netWeekly))
	}

	var totalTargetWeekly float64
	var generous, small []string
	for _, row := range rows {
		totalTargetWeekly += row.TargetWeekly
		switch {
		case row.Generous:
			generous = append(generous, row.Label)
		case row.Budgeted && !row.HasTarget && row.ActualWeekly > 0 && row.ActualWeekly < smallSpendWeekly:
			small = append(small, row.Label)
		}
		for _, sub := range row.SubRows {
			if sub.Generous {
				generous = append(generous, sub.Label)
			}
		}
	}

	return CategorySetupData{
		Tracking:          buildTrackingSummary(rows),
		Rows:              rows,
		Sections:          buildTargetSections(rows, ""),
		HasData:           len(txs) > 0,
		NetWeekly:         netWeekly,
		TotalTargetWeekly: totalTargetWeekly,
		Generous:          generous,
		Small:             small,
	}, nil
}

// spendingOnly drops money that left without being spent, savings and loan
// interest, so every view of spending agrees with the budget: a contribution
// to an investment fund is not spending, and loan interest is not counted
// again on top of the repayments that pay it.
func spendingOnly(txs []models.Transaction) []models.Transaction {
	kept := make([]models.Transaction, 0, len(txs))
	for _, tx := range txs {
		if category, ok := debitCategory(tx); ok && category.NotSpending {
			continue
		}
		kept = append(kept, tx)
	}
	return kept
}

// isoWeekStart returns the Monday of the ISO week containing t, normalised to
// UTC midnight so that map keys from transaction dates (UTC) and time.Now()
// (local) always compare equal for the same calendar week.
func isoWeekStart(t time.Time) time.Time {
	t = t.UTC()
	y, m, d := t.Date()
	t0 := time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
	wd := int(t0.Weekday())
	if wd == 0 {
		wd = 7
	}
	return t0.AddDate(0, 0, -(wd - 1))
}

// sanitizeID converts a string to a safe HTML id, prefixed with "sc-" (subcategory rows).
func sanitizeID(s string) string { return sanitizeWithPrefix("sc-", s) }

func sanitizeWithPrefix(prefix, s string) string {
	var b strings.Builder
	b.WriteString(prefix)
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		} else {
			b.WriteRune('-')
		}
	}
	return b.String()
}

func newID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return fmt.Sprintf("%x", b)
}
