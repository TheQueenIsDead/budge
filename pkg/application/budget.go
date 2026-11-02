package application

import (
	"crypto/rand"
	"fmt"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/TheQueenIsDead/budge/pkg/database/models"
	"github.com/labstack/echo/v4"
)

// CategoryTargetRow is one budge category on the budget page.
type CategoryTargetRow struct {
	models.BudgetCategory
	RowID        string // HTML-safe id, also how an edit names the row to keep open
	Order        int    // the category's place in the registry, for sorting
	ActualWeekly float64
	// Populated from a saved BudgetItem, if one exists for the category.
	ItemID    string
	Target    float64
	Frequency string
	// Comparison fields, in the target's own frequency for display.
	ActualDisplay float64
	TargetDisplay float64
	Per           string // "a week", "a month"...
	Percentage    float64
	IsOver        bool
	HasTarget     bool
	NeedsTarget   bool    // has actual spend but no target set
	PctOfIncome   float64 // targetWeekly / netWeekly (fraction)
	TargetWeekly  float64 // weekly-normalised target for internal use
	// Budgeted is false for the categories that take no target: savings and
	// loan interest, which are not spending, and what is not filed yet.
	Budgeted    bool
	WindowWeeks int // how many weeks the actual is averaged over
	// Suggested is a target to offer a category that has none, in the
	// frequency its target picker shows, rounded up to whole dollars. It is
	// the higher of its 13 week average and its spend over the window that
	// frequency is measured over, so accepting it lands the row on track
	// rather than tripping over a busy few weeks.
	Suggested          float64
	SuggestedFrequency string
	SuggestedPer       string // "a week", "a month"...
	SuggestedWindow    int    // weeks the frequency is judged over
	// Generous marks a target set well above what the category spends, and
	// LoweredTarget is what it could come down to, in the target's own
	// frequency. Zero means the category spends nothing to budget for.
	Generous      bool
	LoweredTarget float64
	// Subs is the row's spending split by subcategory, over the same window.
	Subs []SubcategorySpend
	// Splittable is whether the category has subcategories to budget by
	// instead. Split marks one that does: SubRows are its subcategories, each
	// against its own target, and its general row for what is filed under the
	// category itself, marked General. ParentRowID names the row a
	// subcategory row sits in, which an edit to it redraws.
	Splittable  bool
	Split       bool
	SubRows     []CategoryTargetRow
	General     bool
	ParentRowID string
	// Merchants seen in this category, highest spend first. Informational
	// only — they are derived from transactions, never edited.
	Merchants []string
	Open      bool // the row being edited, drawn expanded
}

// SubcategorySpend is what one subcategory, or the category itself when
// nothing finer filed it, spent each week.
type SubcategorySpend struct {
	Label  string
	Weekly float64
}

// BarWidth is how full the row's progress bar is drawn, capped at full.
func (r CategoryTargetRow) BarWidth() float64 {
	return math.Min(r.Percentage*100, 100)
}

// topMerchants orders merchant names by total spend, highest first, so that
// truncating the list keeps the most significant ones. Ties break by name to
// keep rendering deterministic.
func topMerchants(spend map[string]float64) []string {
	if len(spend) == 0 {
		return nil
	}
	names := make([]string, 0, len(spend))
	for name := range spend {
		names = append(names, name)
	}
	sort.Slice(names, func(i, j int) bool {
		if spend[names[i]] != spend[names[j]] {
			return spend[names[i]] > spend[names[j]]
		}
		return names[i] < names[j]
	})
	return names
}

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
	if tx.Amount >= 0 || tx.Type == "TRANSFER" {
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

// A target counts as generous when spending is under this share of it and at
// least this far below it each week. Both have to hold, so a small yearly
// budget that has not been touched yet is not flagged for a few dollars.
const (
	generousShare     = 0.5
	generousGapWeekly = 20.0
)

// loweredHeadroom is added to what a generous category spends when offering
// a lower target, so a slightly bigger week does not tip it straight over.
const loweredHeadroom = 1.1

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

// BudgetCardsData is the view-model for the summary cards on the income page
// and the allocation bar on the budget page. Both are driven by the same three
// numbers, so they share one calculation.
type BudgetCardsData struct {
	NetWeekly           float64
	TotalWeeklyExpenses float64
	WeeklySavings       float64
	HasDeficit          bool
	PctAllocatedCss     float64 // clamped to 100 for CSS width
	IsOverAllocated     bool
	HasSalary           bool
	// SavingsGoalWeekly is what is wanted left after targets, judged against
	// WeeklySavings on the Savings card.
	SavingsGoalWeekly float64
	MeetsSavingsGoal  bool
}

// GoalShortfall is how far what is left after targets falls short of the
// savings goal.
func (d BudgetCardsData) GoalShortfall() float64 {
	return math.Max(0, d.SavingsGoalWeekly-d.WeeklySavings)
}

// BudgetSummary is the view-model rendered by GET /budget/summary.
type BudgetSummary struct {
	GrossHourly          float64
	GrossAnnual          float64
	GrossMonthly         float64
	GrossFortnightly     float64
	GrossWeekly          float64
	PAYEHourly           float64
	PAYEAnnual           float64
	PAYEMonthly          float64
	PAYEFortnightly      float64
	PAYEWeekly           float64
	ACCAnnual            float64
	ACCMonthly           float64
	ACCFortnightly       float64
	ACCWeekly            float64
	ACCHourly            float64
	KiwiSaverHourly      float64
	KiwiSaverAnnual      float64
	KiwiSaverMonthly     float64
	KiwiSaverFortnightly float64
	KiwiSaverWeekly      float64
	StudentLoanHourly    float64
	StudentLoanAnnual    float64
	StudentLoanMonthly   float64
	StudentLoanFortnight float64
	StudentLoanWeekly    float64
	NetHourly            float64
	NetAnnual            float64
	NetMonthly           float64
	NetFortnightly       float64
	NetWeekly            float64
}

// Budget shows how spending is tracking against the targets. Each category is
// measured over a window matched to its target's frequency rather than week
// by week, so a fortnightly mortgage or a monthly power bill reads as on
// track instead of spiking in the week it is paid.
func (app *Application) Budget(c echo.Context) error {
	salary, err := app.store.GetBudgetSalary()
	if err != nil {
		app.Toast(c, "Error", "Could not load salary config.")
		return c.NoContent(http.StatusInternalServerError)
	}
	setupData, err := app.buildSetupData()
	if err != nil {
		app.Toast(c, "Error", "Could not load category data.")
		return c.NoContent(http.StatusInternalServerError)
	}
	return c.Render(http.StatusOK, "budget", map[string]interface{}{
		"Income":    buildIncomeTile(salary),
		"SetupData": setupData,
	})
}

// TrackingSummary is the budget page's headline, totalled across categories,
// each measured over its own window and expressed per week.
type TrackingSummary struct {
	Actual    float64 // spend in categories with a target
	Target    float64
	OverCount int
	// Spend with no target to measure it against, the usual reason a budget
	// does not add up.
	UntargetedWeekly float64
	UntargetedCount  int
	// Targets set well above what their categories spend.
	GenerousCount int

	// Over names what is over target, by how much each week, worst first:
	// a category, or the subcategory of a split one that is. Untargeted
	// names the largest spending with no target, and UntargetedMore how many
	// more there are, for the notifications that say what to look at.
	Over           []TrackingItem
	Untargeted     []TrackingItem
	UntargetedMore int
}

// TrackingItem is a category the budget has something to say about, and
// how much a week it is about.
type TrackingItem struct {
	Label  string
	Weekly float64
}

// untargetedNamed is how many categories with no target are named before
// saying how many more there are.
const untargetedNamed = 3

// listed sorts items largest first, keeping the first limit and counting the
// rest. A limit of zero keeps them all.
func listed(items []TrackingItem, limit int) ([]TrackingItem, int) {
	sort.SliceStable(items, func(i, j int) bool { return items[i].Weekly > items[j].Weekly })
	if limit > 0 && len(items) > limit {
		return items[:limit], len(items) - limit
	}
	return items, 0
}

// IsOver reports whether spending in targeted categories is over their
// targets in total, allowing the same dollar a week each row is allowed.
func (t TrackingSummary) IsOver() bool {
	return t.Target > 0 && t.Actual > t.Target+overToleranceWeekly
}

// Gap is how far spending is from the targets each week: positive when over,
// negative when under.
func (t TrackingSummary) Gap() float64 { return t.Actual - t.Target }

// BarWidth is how full the headline bar is drawn, capped at full.
func (t TrackingSummary) BarWidth() float64 {
	if t.Target <= 0 {
		return 0
	}
	return math.Min(t.Actual/t.Target*100, 100)
}

func buildTrackingSummary(rows []CategoryTargetRow) TrackingSummary {
	var summary TrackingSummary
	for _, category := range rows {
		// A split category is judged part by part: each subcategory against
		// its own target, and one with none, its general bucket included, as
		// spending with no target, rather than all of it against theirs.
		lines := []CategoryTargetRow{category}
		if category.Split {
			lines = category.SubRows
		}
		for _, row := range lines {
			switch {
			case row.HasTarget:
				summary.Actual += row.ActualWeekly
				summary.Target += row.TargetWeekly
				if row.Generous {
					summary.GenerousCount++
				}
				if row.IsOver {
					summary.Over = append(summary.Over, TrackingItem{Label: row.Label, Weekly: row.ActualWeekly - row.TargetWeekly})
				}
			case row.NeedsTarget:
				summary.UntargetedWeekly += row.ActualWeekly
				summary.UntargetedCount++
				summary.Untargeted = append(summary.Untargeted, TrackingItem{Label: row.Label, Weekly: row.ActualWeekly})
			}
		}
	}
	summary.OverCount = len(summary.Over)
	summary.Over, _ = listed(summary.Over, 0)
	summary.Untargeted, summary.UntargetedMore = listed(summary.Untargeted, untargetedNamed)
	return summary
}

// BudgetSaveTarget creates or updates the target for a budge category, or
// a subcategory of one that is split, and returns the re-rendered row: the
// category's, which a subcategory's target adds up into.
func (app *Application) BudgetSaveTarget(c echo.Context) error {
	if key, general := strings.CutSuffix(c.FormValue("category"), generalSuffix); general {
		return app.saveGeneralTarget(c, key)
	}
	category, ok := models.BudgetCategoryByKey(c.FormValue("category"))
	if !ok || category.Key != c.FormValue("category") || !category.Budgeted() {
		return c.NoContent(http.StatusBadRequest)
	}
	rowID := sanitizeID(models.TopBudgetCategory(category.Key).Key)
	target, _ := strconv.ParseFloat(c.FormValue("target_amount"), 64)
	frequency := c.FormValue("target_frequency")
	if frequency == "" {
		frequency = "weekly"
	}

	items, err := app.store.ReadBudgetItems()
	if err != nil {
		return err
	}
	if item, saved := budgetTargets(items)[category.Key]; saved {
		item.Amount = target
		item.Frequency = frequency
		if err := app.store.UpdateBudgetItem(item); err != nil {
			return err
		}
		return app.renderTargetRow(c, rowID)
	}
	if err := app.store.CreateBudgetItem(models.BudgetItem{
		ID:             newID(),
		Name:           category.Label,
		Amount:         target,
		Frequency:      frequency,
		BudgetCategory: category.Key,
	}); err != nil {
		return err
	}
	return app.renderTargetRow(c, rowID)
}

// saveGeneralTarget sets the target for what is filed under a split
// category itself, kept beside its own.
func (app *Application) saveGeneralTarget(c echo.Context, key string) error {
	category, ok := models.BudgetCategoryByKey(key)
	if !ok || category.Key != key || category.IsSub() || !category.Budgeted() {
		return c.NoContent(http.StatusBadRequest)
	}
	target, _ := strconv.ParseFloat(c.FormValue("target_amount"), 64)
	frequency := c.FormValue("target_frequency")
	if frequency == "" {
		frequency = "weekly"
	}
	items, err := app.store.ReadBudgetItems()
	if err != nil {
		return err
	}
	item, saved := budgetTargets(items)[key]
	if !saved {
		item = models.BudgetItem{ID: newID(), Name: category.Label, BudgetCategory: key, Frequency: category.Frequency, Split: true}
	}
	item.GeneralAmount = target
	item.GeneralFrequency = frequency
	if saved {
		err = app.store.UpdateBudgetItem(item)
	} else {
		err = app.store.CreateBudgetItem(item)
	}
	if err != nil {
		return err
	}
	return app.renderTargetRow(c, sanitizeID(key))
}

// BudgetSaveSplit budgets a category by its subcategories, or by one target
// again. Both sets of targets are kept, so switching back and forth loses
// neither.
func (app *Application) BudgetSaveSplit(c echo.Context) error {
	category, ok := models.BudgetCategoryByKey(c.FormValue("category"))
	if !ok || category.IsSub() || !category.Budgeted() || len(models.BudgetSubcategories(category.Key)) == 0 {
		return c.NoContent(http.StatusBadRequest)
	}
	split := c.FormValue("split") == "1"

	items, err := app.store.ReadBudgetItems()
	if err != nil {
		return err
	}
	if item, saved := budgetTargets(items)[category.Key]; saved {
		item.Split = split
		if err := app.store.UpdateBudgetItem(item); err != nil {
			return err
		}
	} else if err := app.store.CreateBudgetItem(models.BudgetItem{
		ID:             newID(),
		Name:           category.Label,
		Frequency:      category.Frequency,
		BudgetCategory: category.Key,
		Split:          split,
	}); err != nil {
		return err
	}
	return app.renderTargetRow(c, sanitizeID(category.Key))
}

// TargetRowUpdate answers an edit on the targets page: the edited row, and
// the figures elsewhere on the page that it changes.
type TargetRowUpdate struct {
	Row   CategoryTargetRow
	Setup CategorySetupData
}

// renderTargetRow answers an edit with just the edited row, swapped in place,
// and the totals of the group it sits in. Re-rendering the list instead
// closed groups opened by hand and moved the page under the pointer.
func (app *Application) renderTargetRow(c echo.Context, rowID string) error {
	setup, err := app.buildSetupData()
	if err != nil {
		return err
	}
	setup.Sections = buildTargetSections(setup.Rows, rowID)
	setup.OOB = true
	for i := range setup.Sections {
		setup.Sections[i].OOB = true
	}
	for _, section := range setup.Sections {
		for _, row := range section.Rows {
			if row.RowID == rowID {
				return c.Render(http.StatusOK, "budget.setup.row.update", TargetRowUpdate{Row: row, Setup: setup})
			}
		}
	}
	return echo.NewHTTPError(http.StatusNotFound, "That category is no longer in the budget.")
}

// BudgetCards returns the summary cards (income / expenses / savings).
func (app *Application) BudgetCards(c echo.Context) error {
	data, err := app.buildBudgetCards()
	if err != nil {
		return err
	}
	return c.Render(http.StatusOK, "budget.cards", data)
}

// BudgetAllocation returns the bar showing how much of take-home pay the
// targets claim.
func (app *Application) BudgetAllocation(c echo.Context) error {
	data, err := app.buildBudgetCards()
	if err != nil {
		return err
	}
	return c.Render(http.StatusOK, "budget.allocation", data)
}

func (app *Application) buildBudgetCards() (BudgetCardsData, error) {
	salary, err := app.store.GetBudgetSalary()
	if err != nil {
		return BudgetCardsData{}, err
	}
	items, err := app.store.ReadBudgetItems()
	if err != nil {
		return BudgetCardsData{}, err
	}
	netWeekly := computeNetWeekly(salary)
	totalExpenses := weeklyTargetTotal(budgetTargets(items))
	savings := netWeekly - totalExpenses
	pct := 0.0
	if netWeekly > 0 {
		pct = totalExpenses / netWeekly
	}
	return BudgetCardsData{
		NetWeekly:           netWeekly,
		TotalWeeklyExpenses: totalExpenses,
		WeeklySavings:       savings,
		HasDeficit:          savings < 0,
		PctAllocatedCss:     math.Min(pct*100, 100),
		IsOverAllocated:     savings < 0,
		HasSalary:           salary.Salary > 0,
		SavingsGoalWeekly:   toWeeklyAmount(salary.SavingsGoal, salary.SavingsGoalFrequency),
		MeetsSavingsGoal:    savings >= toWeeklyAmount(salary.SavingsGoal, salary.SavingsGoalFrequency),
	}, nil
}

// BudgetSaveSavingsGoal persists the weekly savings goal.
func (app *Application) BudgetSaveSavingsGoal(c echo.Context) error {
	goal, _ := strconv.ParseFloat(c.FormValue("savings_goal"), 64)
	freq := c.FormValue("savings_goal_frequency")
	if freq == "" {
		freq = "weekly"
	}
	existing, err := app.store.GetBudgetSalary()
	if err != nil {
		return err
	}
	existing.SavingsGoal = goal
	existing.SavingsGoalFrequency = freq
	return app.store.SaveBudgetSalary(existing)
}

func buildSetupRowData(category models.BudgetCategory, item models.BudgetItem, actualWeekly, netWeekly float64) CategoryTargetRow {
	targetWeekly := weeklyTarget(item)
	budgeted := category.Budgeted()
	hasTarget := budgeted && targetWeekly > 0

	freq := item.Frequency
	if freq == "" {
		freq = "weekly"
	}

	var pct, pctOfIncome float64
	if hasTarget {
		pct = actualWeekly / targetWeekly
		if netWeekly > 0 {
			pctOfIncome = targetWeekly / netWeekly
		}
	} else {
		targetWeekly = 0
	}
	return CategoryTargetRow{
		BudgetCategory: category,
		RowID:          sanitizeID(category.Key),
		Order:          categoryOrder(category.Key),
		ActualWeekly:   actualWeekly,
		ItemID:         item.ID,
		Target:         item.Amount,
		Frequency:      item.Frequency,
		ActualDisplay:  actualInFrequency(actualWeekly, freq),
		TargetDisplay:  item.Amount,
		Per:            frequencyPer(freq),
		WindowWeeks:    actualWindowWeeks(item.Frequency),
		TargetWeekly:   targetWeekly,
		Percentage:     pct,
		PctOfIncome:    pctOfIncome,
		IsOver:         hasTarget && actualWeekly > targetWeekly+overToleranceWeekly,
		HasTarget:      hasTarget,
		NeedsTarget:    budgeted && !hasTarget && actualWeekly > 0,
		Budgeted:       budgeted,
	}
}

// actualWindowWeeks is how far back a category's actual spend is averaged,
// matched to its target's frequency so weekly categories reflect recent spend
// while yearly ones amortise their bills.
func actualWindowWeeks(frequency string) int {
	switch frequency {
	case "fortnightly":
		return 8
	case "monthly":
		return 13
	case "quarterly":
		// Two quarters, so a quarterly bill is caught twice rather than once
		// or not at all depending on when it lands.
		return 26
	case "yearly":
		return 52
	}
	return 4
}

// untargetedWindowWeeks is how far back a category with no target is measured,
// and so what a suggested target is based on. Long enough that one unusual
// month does not set the figure.
const untargetedWindowWeeks = 13

// overToleranceWeekly is how far past its target spend must go to count as
// over, so a target set to the dollar is not flagged for a few cents.
const overToleranceWeekly = 1.0

// markGenerous flags a target set well above what its category spends, and
// works out what it could be lowered to. Spending is judged on the higher of
// the row's own window and the last 13 weeks, so a quiet month does not flag
// a weekly target and a yearly one is not judged on its quiet weeks alone.
func markGenerous(row *CategoryTargetRow, recentWeekly float64) {
	if !row.HasTarget || !row.Budgeted {
		return
	}
	spent := math.Max(row.ActualWeekly, recentWeekly)
	if spent >= row.TargetWeekly*generousShare || row.TargetWeekly-spent < generousGapWeekly {
		return
	}
	row.Generous = true
	freq := row.Frequency
	if freq == "" {
		freq = "weekly"
	}
	row.LoweredTarget = math.Ceil(actualInFrequency(spent*loweredHeadroom, freq))
}

// buildSetupData measures 13 months of money leaving against the targets,
// one row for every budge category, so a target can be set for a category
// before anything has been spent in it.
func (app *Application) buildSetupData() (CategorySetupData, error) {
	salary, err := app.store.GetBudgetSalary()
	if err != nil {
		return CategorySetupData{}, err
	}
	netWeekly := computeNetWeekly(salary)

	txs, err := app.store.ReadTransactionsByDate(time.Now().AddDate(0, -13, 0), time.Now())
	if err != nil {
		return CategorySetupData{}, err
	}

	items, err := app.store.ReadBudgetItems()
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
		if !targets[category.Key].Split || len(subs) == 0 || !category.Budgeted() {
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

// suggestTarget offers a target for a row with none, in the row's own
// frequency. measuredWeekly is its spend over the window that frequency is
// judged on, which for a weekly target is only the last 4 weeks.
func suggestTarget(row *CategoryTargetRow, measuredWeekly float64) {
	freq := row.Frequency
	if freq == "" {
		freq = "weekly"
	}
	row.SuggestedFrequency = freq
	row.SuggestedPer = frequencyPer(freq)
	row.SuggestedWindow = actualWindowWeeks(freq)
	row.Suggested = math.Ceil(actualInFrequency(math.Max(row.ActualWeekly, measuredWeekly), freq))
}

// splitRow is a category budgeted by its subcategories. It is measured per
// week, since its subcategories can each have a different period: the sum of
// what they spend, plus anything filed under the category itself, against
// the sum of their targets. Its merchants are left to its subcategories,
// each listing its own, rather than repeated above them.
func splitRow(category models.BudgetCategory, subRows []CategoryTargetRow, netWeekly float64) CategoryTargetRow {
	row := CategoryTargetRow{
		BudgetCategory: category,
		RowID:          sanitizeID(category.Key),
		Order:          categoryOrder(category.Key),
		Budgeted:       true,
		Splittable:     true,
		Split:          true,
		SubRows:        subRows,
		Frequency:      "weekly",
		Per:            frequencyPer("weekly"),
		WindowWeeks:    untargetedWindowWeeks,
	}
	for _, sub := range subRows {
		row.ActualWeekly += sub.ActualWeekly
		row.TargetWeekly += sub.TargetWeekly
	}
	row.ActualDisplay = row.ActualWeekly
	row.TargetDisplay = row.TargetWeekly
	row.Target = row.TargetWeekly
	row.HasTarget = row.TargetWeekly > 0
	row.NeedsTarget = !row.HasTarget && row.ActualWeekly > 0
	row.IsOver = row.HasTarget && row.ActualWeekly > row.TargetWeekly+overToleranceWeekly
	if row.HasTarget {
		row.Percentage = row.ActualWeekly / row.TargetWeekly
		if netWeekly > 0 {
			row.PctOfIncome = row.TargetWeekly / netWeekly
		}
	}
	return row
}

// generalSuffix marks the key of a split category's general row, for what is
// filed under the category itself. It is not a category of its own.
const generalSuffix = ":general"

// generalKey is the key of a category's general row.
func generalKey(key string) string { return key + generalSuffix }

// generalCategory is a split category's general row, dressed as the category
// it belongs to.
func generalCategory(category models.BudgetCategory) models.BudgetCategory {
	general := category
	general.Key = generalKey(category.Key)
	general.Label = category.Label + " (general)"
	general.Blurb = "Filed under " + category.Label + " itself rather than one of its subcategories"
	return general
}

// categoryOrder is a category's place in the registry, which is the order
// the budget lists categories in.
func categoryOrder(key string) int {
	for i, category := range models.TopBudgetCategories() {
		if category.Key == key {
			return i
		}
	}
	return -1
}

// subcategorySpend splits a category's weekly spending by subcategory, over
// the window its row is measured over. Spending filed under the category
// itself is listed last, as "Other <category>". A category with nothing finer
// returns nothing, since the split would only repeat the row.
func subcategorySpend(category models.BudgetCategory, spend map[string]map[int]float64, weeks int) []SubcategorySpend {
	var subs []SubcategorySpend
	for _, sub := range models.BudgetSubcategories(category.Key) {
		if total := spend[sub.Key][weeks]; total > 0 {
			subs = append(subs, SubcategorySpend{Label: sub.Label, Weekly: total / float64(weeks)})
		}
	}
	if len(subs) == 0 {
		return nil
	}
	if direct := spend[category.Key][weeks]; direct > 0 {
		var filed float64
		for _, sub := range models.BudgetSubcategories(category.Key) {
			filed += spend[sub.Key][weeks]
		}
		if rest := direct - filed; rest > 0.005 {
			subs = append(subs, SubcategorySpend{Label: "Other " + strings.ToLower(category.Label), Weekly: rest / float64(weeks)})
		}
	}
	sort.SliceStable(subs, func(i, j int) bool { return subs[i].Weekly > subs[j].Weekly })
	return subs
}

// BudgetSaveSalary persists salary config (called on field change, hx-swap="none").
func (app *Application) BudgetSaveSalary(c echo.Context) error {
	salary, _ := strconv.ParseFloat(c.FormValue("salary"), 64)
	kiwiSaverRate, _ := strconv.ParseFloat(c.FormValue("kiwisaver_rate"), 64)
	existing, err := app.store.GetBudgetSalary()
	if err != nil {
		return err
	}
	// Mutate in place rather than rebuilding the struct, so fields owned by
	// other forms (savings goal, categories) survive an income edit.
	existing.Salary = salary
	existing.SalaryFrequency = c.FormValue("salary_frequency")
	existing.IncludePAYE = c.FormValue("include_paye") == "on"
	existing.KiwiSaverRate = kiwiSaverRate
	existing.StudentLoan = c.FormValue("student_loan") == "on"
	return app.store.SaveBudgetSalary(existing)
}

// BudgetSummary reads saved state and returns calculated summary HTML.
func (app *Application) BudgetSummary(c echo.Context) error {
	salary, err := app.store.GetBudgetSalary()
	if err != nil {
		return err
	}
	return c.Render(http.StatusOK, "budget.summary", buildBudgetSummary(salary))
}

// IncomeTileData is the view-model for the income summary on the budget page.
type IncomeTileData struct {
	Salary     models.BudgetSalary
	Summary    BudgetSummary
	Deductions []string // the deductions that apply, named for the tile
}

func buildIncomeTile(salary models.BudgetSalary) IncomeTileData {
	tile := IncomeTileData{Salary: salary, Summary: buildBudgetSummary(salary)}
	if salary.IncludePAYE {
		tile.Deductions = append(tile.Deductions, "PAYE", "ACC")
	}
	if salary.KiwiSaverRate > 0 {
		tile.Deductions = append(tile.Deductions, fmt.Sprintf("KiwiSaver %g%%", salary.KiwiSaverRate))
	}
	if salary.StudentLoan {
		tile.Deductions = append(tile.Deductions, "Student loan")
	}
	return tile
}

// BudgetIncome is the page for configuring income, kept off the budget page
// because it is set once and rarely revisited.
func (app *Application) BudgetIncome(c echo.Context) error {
	salary, err := app.store.GetBudgetSalary()
	if err != nil {
		app.Toast(c, "Error", "Could not load salary config.")
		return c.NoContent(http.StatusInternalServerError)
	}
	return c.Render(http.StatusOK, "budget.income", map[string]interface{}{"Salary": salary})
}

func buildBudgetSummary(salary models.BudgetSalary) BudgetSummary {
	annual := toAnnual(salary.Salary, salary.SalaryFrequency)
	var payeAnnual, accAnnual, studentLoanAnnual float64
	if salary.IncludePAYE {
		payeAnnual = calculateNZPAYE(annual)
		accAnnual = calculateNZACC(annual)
	}
	if salary.StudentLoan {
		studentLoanAnnual = calculateNZStudentLoan(annual)
	}
	kiwiSaverAnnual := annual * (salary.KiwiSaverRate / 100)
	netAnnual := annual - payeAnnual - accAnnual - kiwiSaverAnnual - studentLoanAnnual

	return BudgetSummary{
		GrossHourly:          annual / 52 / 40,
		GrossAnnual:          annual,
		GrossMonthly:         annual / 12,
		GrossFortnightly:     annual / 26,
		GrossWeekly:          annual / 52,
		PAYEHourly:           payeAnnual / 52 / 40,
		PAYEAnnual:           payeAnnual,
		PAYEMonthly:          payeAnnual / 12,
		PAYEFortnightly:      payeAnnual / 26,
		PAYEWeekly:           payeAnnual / 52,
		ACCHourly:            accAnnual / 52 / 40,
		ACCAnnual:            accAnnual,
		ACCMonthly:           accAnnual / 12,
		ACCFortnightly:       accAnnual / 26,
		ACCWeekly:            accAnnual / 52,
		KiwiSaverHourly:      kiwiSaverAnnual / 52 / 40,
		KiwiSaverAnnual:      kiwiSaverAnnual,
		KiwiSaverMonthly:     kiwiSaverAnnual / 12,
		KiwiSaverFortnightly: kiwiSaverAnnual / 26,
		KiwiSaverWeekly:      kiwiSaverAnnual / 52,
		StudentLoanHourly:    studentLoanAnnual / 52 / 40,
		StudentLoanAnnual:    studentLoanAnnual,
		StudentLoanMonthly:   studentLoanAnnual / 12,
		StudentLoanFortnight: studentLoanAnnual / 26,
		StudentLoanWeekly:    studentLoanAnnual / 52,
		NetHourly:            netAnnual / 52 / 40,
		NetAnnual:            netAnnual,
		NetMonthly:           netAnnual / 12,
		NetFortnightly:       netAnnual / 26,
		NetWeekly:            netAnnual / 52,
	}
}

// budgetTargets indexes the targets set against budge categories and
// subcategories by key.
// Targets from before budge had its own categories name none, and are left
// out, as are the targets of deleted categories: the category their
// transactions went to keeps its own target.
func budgetTargets(items []models.BudgetItem) map[string]models.BudgetItem {
	targets := make(map[string]models.BudgetItem, len(items))
	for _, item := range items {
		if category, ok := models.BudgetCategoryByKey(item.BudgetCategory); ok && category.Key == item.BudgetCategory && category.Budgeted() {
			targets[item.BudgetCategory] = item
			if !category.IsSub() {
				targets[generalKey(item.BudgetCategory)] = models.BudgetItem{
					Name:           category.Label + " (general)",
					Amount:         item.GeneralAmount,
					Frequency:      item.GeneralFrequency,
					BudgetCategory: generalKey(item.BudgetCategory),
				}
			}
		}
	}
	return targets
}

// weeklyTargetTotal adds up the targets in force, per week: a category's
// own, or its subcategories' when it is split.
func weeklyTargetTotal(targets map[string]models.BudgetItem) float64 {
	var total float64
	for _, category := range models.TopBudgetCategories() {
		if !targets[category.Key].Split {
			total += weeklyTarget(targets[category.Key])
			continue
		}
		for _, sub := range models.BudgetSubcategories(category.Key) {
			total += weeklyTarget(targets[sub.Key])
		}
		total += weeklyTarget(targets[generalKey(category.Key)])
	}
	return total
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

// ---- pure functions ----

// weeklyTarget returns the weekly-normalised budget target for an item.
// Savings and ignored items are not spending, so they contribute nothing
// even if an amount was saved before the switch.
func weeklyTarget(item models.BudgetItem) float64 {
	if !item.IsSpending() {
		return 0
	}
	return toWeeklyAmount(item.Amount, item.Frequency)
}

func actualInFrequency(weeklyActual float64, frequency string) float64 {
	switch frequency {
	case "weekly", "":
		return weeklyActual
	case "fortnightly":
		return weeklyActual * 2
	case "monthly":
		return weeklyActual * 52 / 12
	case "quarterly":
		return weeklyActual * 13
	case "yearly":
		return weeklyActual * 52
	}
	return weeklyActual
}

// frequencyPer reads a target's frequency the way the targets page phrases
// it after an amount: "$150 a week".
func frequencyPer(frequency string) string {
	switch frequency {
	case "fortnightly":
		return "a fortnight"
	case "monthly":
		return "a month"
	case "quarterly":
		return "a quarter"
	case "yearly":
		return "a year"
	}
	return "a week"
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

// computeNetWeekly returns weekly take-home pay after all deductions.
func computeNetWeekly(salary models.BudgetSalary) float64 {
	annual := toAnnual(salary.Salary, salary.SalaryFrequency)
	var payeAnnual, accAnnual, slAnnual float64
	if salary.IncludePAYE {
		payeAnnual = calculateNZPAYE(annual)
		accAnnual = calculateNZACC(annual)
	}
	if salary.StudentLoan {
		slAnnual = calculateNZStudentLoan(annual)
	}
	return (annual - payeAnnual - accAnnual - annual*(salary.KiwiSaverRate/100) - slAnnual) / 52
}

func toAnnual(amount float64, frequency string) float64 {
	switch frequency {
	case "yearly":
		return amount
	case "monthly":
		return amount * 12
	case "fortnightly":
		return amount * 26
	}
	return amount
}

// calculateNZPAYE computes annual PAYE using NZ brackets updated 31 July 2024.
func calculateNZPAYE(annual float64) float64 {
	type bracket struct{ max, rate float64 }
	brackets := []bracket{
		{15600, 0.105},
		{53500, 0.175},
		{78100, 0.300},
		{180000, 0.330},
		{math.MaxFloat64, 0.390},
	}
	tax, prev := 0.0, 0.0
	for _, b := range brackets {
		if annual <= prev {
			break
		}
		tax += (math.Min(annual, b.max) - prev) * b.rate
		prev = b.max
	}
	return tax
}

// calculateNZACC computes the annual ACC earners' levy.
// Current rate: 1.75% on earnings up to $142,283 (2025–26).
// Rises to 1.83% on earnings up to $160,244 from 1 April 2027.
func calculateNZACC(annual float64) float64 {
	const rate, maxEarnings = 0.0175, 142283.0
	return math.Min(annual, maxEarnings) * rate
}

// calculateNZStudentLoan computes the annual student loan repayment (12% above threshold).
func calculateNZStudentLoan(annual float64) float64 {
	const rate, threshold = 0.12, 22828.0
	if annual <= threshold {
		return 0
	}
	return (annual - threshold) * rate
}

func toWeeklyAmount(amount float64, frequency string) float64 {
	switch frequency {
	case "weekly":
		return amount
	case "fortnightly":
		return amount / 2
	case "monthly":
		return amount * 12 / 52
	case "quarterly":
		return amount * 4 / 52
	case "yearly":
		return amount / 52
	}
	return amount
}

func newID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return fmt.Sprintf("%x", b)
}
