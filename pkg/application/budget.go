package application

import (
	"crypto/rand"
	"encoding/json"
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

// CategoryTargetRow is one category on the spending targets page.
type CategoryTargetRow struct {
	BroadCategory string
	SubCategory   string
	RowID         string // HTML-safe id, also how an edit names the row to keep open
	ActualWeekly  float64
	// Populated from a saved BudgetItem, if one exists for this pair.
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
	Kind          string
	IsSpending    bool // false for savings and ignored rows, which carry no target
	WindowWeeks   int  // how many weeks the actual is averaged over
	// SuggestedMonthly is a target to offer a category that has none: its
	// spend over the same 13 weeks the row is measured over, as a monthly
	// amount rounded up to whole dollars. Monthly targets are measured over
	// that window too, so accepting it lands the row on track rather than
	// tripping over a lumpy month.
	SuggestedMonthly float64
	// Merchants seen in this category, highest spend first. Informational
	// only — they are derived from transactions, never edited.
	Merchants []string
	Open      bool // the row being edited, drawn expanded
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

// uncategorisedGroup collects debits Akahu could not categorise. They are
// still real outgoings, so leaving them out of the setup table while the
// weekly chart counts them makes weeks look over budget for no visible reason.
const uncategorisedGroup = "Uncategorised"

// uncategorisedLabels names the rows inside the uncategorised group. Without
// a category the transaction type is the most telling thing left: loan
// interest, a standing order to an investment fund and cash withdrawals each
// get a row of their own that a target can be set against.
var uncategorisedLabels = map[string]string{
	"LOAN":           "Loan",
	"STANDING ORDER": "Standing orders",
	"PAYMENT":        "Payments",
	"DIRECT DEBIT":   "Direct debits",
	"ATM":            "ATM withdrawals",
	"EFTPOS":         "EFTPOS",
	"DEBIT":          "Card debits",
	"FEE":            "Fees",
}

// spendCategory decides whether a transaction is spending and, if it is,
// which broad and specific category it belongs to. Every budget view goes
// through here so the chart and the setup table always count the same money.
func spendCategory(tx models.Transaction) (broad, specific string, ok bool) {
	if tx.Amount >= 0 || tx.Type == "TRANSFER" {
		return "", "", false
	}
	broad = tx.Category.Groups.PersonalFinance.Name
	if broad == "" {
		label, known := uncategorisedLabels[tx.Type]
		if !known {
			label = "Other"
		}
		return uncategorisedGroup, label, true
	}
	specific = tx.Category.Name
	if specific == "" {
		specific = broad
	}
	return broad, specific, true
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

// TargetSection is one part of the spending targets page. Rows are sorted by
// what they need from the owner rather than by Akahu's groups, so settled
// categories stay out of the way until something changes.
type TargetSection struct {
	Key    string
	Title  string
	Rows   []CategoryTargetRow
	Actual float64 // weekly, across the section's rows
	Target float64 // weekly, across the section's rows
	Open   bool
}

// Section keys, in the order the page shows them.
const (
	sectionAttention  = "attention"
	sectionOnTrack    = "on-track"
	sectionSmall      = "small"
	sectionNotCounted = "not-counted"
)

// smallSpendWeekly is the weekly spend under which a category with no target
// is folded away rather than asking for one: a few dollars a week is not what
// tips a budget over.
const smallSpendWeekly = 5.0

// buildTargetSections sorts rows into the page's sections. Needs attention is
// always open; another section opens only when it holds the row being edited,
// so a row that moves section after an edit never disappears from view.
func buildTargetSections(rows []CategoryTargetRow, openRow string) []TargetSection {
	sections := []TargetSection{
		{Key: sectionAttention, Title: "Needs attention", Open: true},
		{Key: sectionOnTrack, Title: "On track"},
		{Key: sectionSmall, Title: "Small categories"},
		{Key: sectionNotCounted, Title: "Not counted"},
	}
	index := map[string]int{sectionAttention: 0, sectionOnTrack: 1, sectionSmall: 2, sectionNotCounted: 3}
	for _, row := range rows {
		key := sectionOnTrack
		switch {
		case !row.IsSpending:
			key = sectionNotCounted
		case row.IsOver:
			key = sectionAttention
		case row.HasTarget:
			key = sectionOnTrack
		case row.ActualWeekly >= smallSpendWeekly:
			key = sectionAttention
		default:
			key = sectionSmall
		}
		section := &sections[index[key]]
		row.Open = row.RowID == openRow
		section.Rows = append(section.Rows, row)
		section.Actual += row.ActualWeekly
		section.Target += row.TargetWeekly
		if row.RowID == openRow {
			section.Open = true
		}
	}

	// Overspends first, worst first, then untargeted spend, largest first.
	attention := sections[index[sectionAttention]].Rows
	sort.SliceStable(attention, func(i, j int) bool {
		a, b := attention[i], attention[j]
		if a.IsOver != b.IsOver {
			return a.IsOver
		}
		if a.IsOver {
			return a.ActualWeekly-a.TargetWeekly > b.ActualWeekly-b.TargetWeekly
		}
		return a.ActualWeekly > b.ActualWeekly
	})
	for _, key := range []string{sectionOnTrack, sectionNotCounted} {
		rows := sections[index[key]].Rows
		sort.SliceStable(rows, func(i, j int) bool {
			if rows[i].BroadCategory != rows[j].BroadCategory {
				return rows[i].BroadCategory < rows[j].BroadCategory
			}
			return rows[i].SubCategory < rows[j].SubCategory
		})
	}
	small := sections[index[sectionSmall]].Rows
	sort.SliceStable(small, func(i, j int) bool { return small[i].ActualWeekly > small[j].ActualWeekly })

	kept := sections[:0]
	for _, section := range sections {
		if len(section.Rows) > 0 {
			kept = append(kept, section)
		}
	}
	return kept
}

// CategorySetupData is the view-model for the budget.setup partial.
type CategorySetupData struct {
	Rows                 []CategoryTargetRow
	Sections             []TargetSection
	OpenRow              string // RowID of the row being edited, kept open
	HasData              bool
	NetWeekly            float64
	TotalTargetWeekly    float64
	SavingsGoal          float64
	SavingsGoalFrequency string
	SavingsGoalWeekly    float64
	Remaining            float64
	MeetsSavingsGoal     bool
	HasSavingsGoal       bool
	// Weekly spend in categories marked as savings, e.g. a standing order to
	// an investment fund. Shown beside the goal rather than counted as spending.
	TrackedSavingsWeekly float64
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
		"Income":  buildIncomeTile(salary),
		"Targets": buildTargetsTile(setupData),
	})
}

// BudgetTargets is the page for setting spending targets, kept off the budget
// page so that page can lead with how the weeks went.
func (app *Application) BudgetTargets(c echo.Context) error {
	setupData, err := app.buildSetupData()
	if err != nil {
		app.Toast(c, "Error", "Could not load category data.")
		return c.NoContent(http.StatusInternalServerError)
	}
	return c.Render(http.StatusOK, "budget.targets", map[string]interface{}{"SetupData": setupData})
}

// TargetsTileData is the view-model for the targets summary on the budget page.
type TargetsTileData struct {
	TotalTargetWeekly float64
	TargetCount       int
	// Spending categories with recent spend and no target: the usual reason a
	// week lands over, so the tile calls them out.
	NeedsTargetCount int
	UntargetedWeekly float64
}

func buildTargetsTile(setup CategorySetupData) TargetsTileData {
	tile := TargetsTileData{TotalTargetWeekly: setup.TotalTargetWeekly}
	for _, row := range setup.Rows {
		if row.HasTarget {
			tile.TargetCount++
		}
	}
	for _, section := range setup.Sections {
		if section.Key != sectionAttention {
			continue
		}
		for _, row := range section.Rows {
			if row.NeedsTarget {
				tile.NeedsTargetCount++
				tile.UntargetedWeekly += row.ActualWeekly
			}
		}
	}
	return tile
}

// BudgetSaveTarget creates or updates a BudgetItem for a category pair and
// returns the re-rendered page with that row kept open.
func (app *Application) BudgetSaveTarget(c echo.Context) error {
	subCat := c.FormValue("sub_category")
	if subCat == "" {
		return c.NoContent(http.StatusBadRequest)
	}
	broadCat := c.FormValue("broad_category")
	target, _ := strconv.ParseFloat(c.FormValue("target_amount"), 64)
	frequency := c.FormValue("target_frequency")
	if frequency == "" {
		frequency = "weekly"
	}

	items, err := app.store.ReadBudgetItems()
	if err != nil {
		return err
	}
	for _, item := range items {
		if strings.EqualFold(item.Category, subCat) {
			item.Amount = target
			item.Frequency = frequency
			item.BroadCategory = broadCat
			if err := app.store.UpdateBudgetItem(item); err != nil {
				return err
			}
			return app.renderSetup(c, sanitizeID(subCat))
		}
	}
	newItem := models.BudgetItem{
		ID:            newID(),
		Name:          subCat,
		Amount:        target,
		Frequency:     frequency,
		Category:      subCat,
		BroadCategory: broadCat,
	}
	if err := app.store.CreateBudgetItem(newItem); err != nil {
		return err
	}
	return app.renderSetup(c, sanitizeID(subCat))
}

type budgetKindOption struct{ Value, Label string }

// budgetKindOptions are the choices in each row's "Counts as" menu.
var budgetKindOptions = []budgetKindOption{
	{models.BudgetKindSpending, "Spending"},
	{models.BudgetKindSavings, "Savings"},
	{models.BudgetKindIgnore, "Ignore"},
}

// BudgetSaveKind sets whether a category counts as spending, savings or is
// ignored. Moving a row in or out of spending changes its group's totals and
// the savings footer, so the whole table is re-rendered rather than the row.
func (app *Application) BudgetSaveKind(c echo.Context) error {
	subCat := c.FormValue("sub_category")
	kind := c.FormValue("kind")
	switch kind {
	case models.BudgetKindSpending, models.BudgetKindSavings, models.BudgetKindIgnore:
	default:
		return c.NoContent(http.StatusBadRequest)
	}
	if subCat == "" {
		return c.NoContent(http.StatusBadRequest)
	}

	items, err := app.store.ReadBudgetItems()
	if err != nil {
		return err
	}
	saved := false
	for _, item := range items {
		if strings.EqualFold(item.Category, subCat) {
			item.Kind = kind
			if err := app.store.UpdateBudgetItem(item); err != nil {
				return err
			}
			saved = true
			break
		}
	}
	if !saved {
		if err := app.store.CreateBudgetItem(models.BudgetItem{
			ID:            newID(),
			Name:          subCat,
			Category:      subCat,
			BroadCategory: c.FormValue("broad_category"),
			Frequency:     "weekly",
			Kind:          kind,
		}); err != nil {
			return err
		}
	}

	return app.renderSetup(c, sanitizeID(subCat))
}

// renderSetup answers every edit on the targets page with the whole list.
// An edit can move a row between sections and changes the section totals,
// so re-rendering only the row would leave the rest out of date. openRow is
// the row being edited, which stays open wherever it lands.
func (app *Application) renderSetup(c echo.Context, openRow string) error {
	setup, err := app.buildSetupData()
	if err != nil {
		return err
	}
	setup.OpenRow = openRow
	setup.Sections = buildTargetSections(setup.Rows, openRow)
	return c.Render(http.StatusOK, "budget.setup", setup)
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
	var totalExpenses float64
	for _, it := range items {
		totalExpenses += weeklyTarget(it)
	}
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

func buildSetupRowData(item models.BudgetItem, actualWeekly, netWeekly float64) CategoryTargetRow {
	targetWeekly := weeklyTarget(item)
	isSpending := item.IsSpending()
	hasTarget := targetWeekly > 0

	freq := item.Frequency
	if freq == "" {
		freq = "weekly"
	}

	var pct, pctOfIncome float64
	if hasTarget {
		pct = actualWeekly / targetWeekly
	}
	if netWeekly > 0 {
		pctOfIncome = targetWeekly / netWeekly
	}
	return CategoryTargetRow{
		BroadCategory: item.BroadCategory,
		SubCategory:   item.Category,
		RowID:         sanitizeID(item.Category),
		ActualWeekly:  actualWeekly,
		ItemID:        item.ID,
		Target:        item.Amount,
		Frequency:     item.Frequency,
		ActualDisplay: actualInFrequency(actualWeekly, freq),
		TargetDisplay: item.Amount,
		Per:           frequencyPer(freq),
		WindowWeeks:   actualWindowWeeks(item.Frequency),
		TargetWeekly:  targetWeekly,
		Percentage:    pct,
		PctOfIncome:   pctOfIncome,
		IsOver:        hasTarget && actualWeekly > targetWeekly+overToleranceWeekly,
		HasTarget:     hasTarget,
		NeedsTarget:   isSpending && !hasTarget && actualWeekly > 0,
		Kind:          item.Kind,
		IsSpending:    isSpending,
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
	case "yearly":
		return 56
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

// buildSetupData groups 13 months of transactions by category pair and merges
// in any saved targets, one row per pair.
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
	itemBySubCat := budgetItemsByCategory(items)

	type pairKey struct{ broad, specific string }
	var pairOrder []pairKey
	pairSeen := make(map[pairKey]bool)
	// spend[key][weeks] is the total within that many weeks back; every window
	// a transaction falls inside counts it, so each is a plain total.
	spend := make(map[pairKey]map[int]float64)
	merchantSpend := make(map[pairKey]map[string]float64)
	windows := []int{4, 8, 13, 56}
	weekStart := isoWeekStart(time.Now())

	for _, tx := range txs {
		broad, specific, ok := spendCategory(tx)
		if !ok {
			continue
		}
		key := pairKey{broad, specific}
		if !pairSeen[key] {
			pairSeen[key] = true
			pairOrder = append(pairOrder, key)
			spend[key] = make(map[int]float64)
			merchantSpend[key] = make(map[string]float64)
		}
		amt := math.Abs(tx.Amount)
		if name := spendMerchant(tx); name != "" {
			merchantSpend[key][name] += amt
		}
		for _, weeks := range windows {
			if weeks == 56 || tx.Date.After(weekStart.AddDate(0, 0, -7*weeks)) {
				spend[key][weeks] += amt
			}
		}
	}

	var rows []CategoryTargetRow
	for _, key := range pairOrder {
		saved := itemBySubCat[strings.ToLower(key.specific)]
		weeks := actualWindowWeeks(saved.Frequency)
		if weeklyTarget(saved) == 0 {
			weeks = untargetedWindowWeeks
		}
		row := buildSetupRowData(saved, spend[key][weeks]/float64(weeks), netWeekly)
		row.WindowWeeks = weeks
		if row.BroadCategory == "" {
			row.BroadCategory = key.broad
		}
		if row.SubCategory == "" {
			row.SubCategory = key.specific
			row.RowID = sanitizeID(key.specific)
		}
		if !row.HasTarget && row.IsSpending {
			row.SuggestedMonthly = math.Ceil(row.ActualWeekly * 52 / 12)
		}
		row.Merchants = topMerchants(merchantSpend[key])
		rows = append(rows, row)
	}

	var totalTargetWeekly, trackedSavings float64
	for _, row := range rows {
		totalTargetWeekly += row.TargetWeekly
		if row.Kind == models.BudgetKindSavings {
			trackedSavings += row.ActualWeekly
		}
	}

	savingsGoalWeekly := toWeeklyAmount(salary.SavingsGoal, salary.SavingsGoalFrequency)
	remaining := netWeekly - totalTargetWeekly

	return CategorySetupData{
		Rows:                 rows,
		Sections:             buildTargetSections(rows, ""),
		HasData:              len(txs) > 0,
		NetWeekly:            netWeekly,
		TotalTargetWeekly:    totalTargetWeekly,
		SavingsGoal:          salary.SavingsGoal,
		SavingsGoalFrequency: salary.SavingsGoalFrequency,
		SavingsGoalWeekly:    savingsGoalWeekly,
		Remaining:            remaining,
		MeetsSavingsGoal:     savingsGoalWeekly > 0 && remaining >= savingsGoalWeekly,
		HasSavingsGoal:       salary.SavingsGoal > 0,
		TrackedSavingsWeekly: trackedSavings,
	}, nil
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

// ---- performance chart ----

// BudgetPerformanceData is the view-model for the budget.performance partial.
// Labels and Actuals are JSON strings embedded as data attributes on the canvas;
// Go's HTML-encoding of attribute values keeps them safe, and dataset decodes them.
type BudgetPerformanceData struct {
	Labels  string
	Actuals string
	Series  string // []PerformanceSeries, each bar split by budgeted and unbudgeted
	Weeks   string // ISO date of each bar's Monday, for opening its breakdown
	Target  float64
	HasData bool
}

// PerformanceSeries is one segment of every charted week's bar.
type PerformanceSeries struct {
	Label string    `json:"label"`
	Data  []float64 `json:"data"`
}

// The two segments of each week's bar. Splitting by whether spend has a
// target answers why a week went over at a glance: budgeted spend is what the
// target line was drawn for, so unbudgeted spend stacked on top of it is the
// usual culprit. The breakdown beneath the chart has the per-category detail.
const (
	budgetedSeries   = "Budgeted"
	unbudgetedSeries = "Unbudgeted"
)

// buildPerformanceSeries totals each week's counted spend and splits it into
// spend in categories with a target and spend in categories without one.
// Both series are always present, so each keeps its colour.
func buildPerformanceSeries(weeks []time.Time, txs []models.Transaction, items map[string]models.BudgetItem) ([]float64, []PerformanceSeries) {
	index := make(map[time.Time]int, len(weeks))
	for i, w := range weeks {
		index[w] = i
	}
	totals := make([]float64, len(weeks))
	budgeted := make([]float64, len(weeks))
	unbudgeted := make([]float64, len(weeks))
	for _, tx := range txs {
		i, charted := index[isoWeekStart(tx.Date)]
		if !charted {
			continue
		}
		_, specific, ok := countedSpend(tx, items)
		if !ok {
			continue
		}
		amount := math.Abs(tx.Amount)
		if weeklyTarget(items[strings.ToLower(specific)]) > 0 {
			budgeted[i] += amount
		} else {
			unbudgeted[i] += amount
		}
		totals[i] += amount
	}

	round := func(values []float64) []float64 {
		for i, v := range values {
			values[i] = math.Round(v*100) / 100
		}
		return values
	}
	return round(totals), []PerformanceSeries{
		{Label: budgetedSeries, Data: round(budgeted)},
		{Label: unbudgetedSeries, Data: round(unbudgeted)},
	}
}

// budgetItemsByCategory indexes items by their lower-cased subcategory, the
// key every budget view matches transactions on.
func budgetItemsByCategory(items []models.BudgetItem) map[string]models.BudgetItem {
	byCategory := make(map[string]models.BudgetItem, len(items))
	for _, item := range items {
		byCategory[strings.ToLower(item.Category)] = item
	}
	return byCategory
}

// countedSpend is spendCategory narrowed to what counts against the budget:
// categories marked as savings or ignored are left out. A category with no
// saved item is spending.
func countedSpend(tx models.Transaction, items map[string]models.BudgetItem) (broad, specific string, ok bool) {
	broad, specific, ok = spendCategory(tx)
	if !ok {
		return "", "", false
	}
	if item, saved := items[strings.ToLower(specific)]; saved && !item.IsSpending() {
		return "", "", false
	}
	return broad, specific, true
}

// performanceWeeks is the Monday of each of the 13 weeks the chart shows,
// oldest first.
func performanceWeeks(now time.Time) []time.Time {
	ws := isoWeekStart(now)
	weeks := make([]time.Time, 13)
	for i := range weeks {
		weeks[12-i] = ws.AddDate(0, 0, -7*i)
	}
	return weeks
}

func (app *Application) BudgetPerformance(c echo.Context) error {
	txs, err := app.store.ReadTransactionsByDate(time.Now().AddDate(0, -3, 0), time.Now())
	if err != nil {
		return err
	}
	items, err := app.store.ReadBudgetItems()
	if err != nil {
		return err
	}

	var target float64
	for _, item := range items {
		target += weeklyTarget(item)
	}
	weeks := performanceWeeks(time.Now())
	actuals, series := buildPerformanceSeries(weeks, txs, budgetItemsByCategory(items))
	labels := make([]string, len(weeks))
	starts := make([]string, len(weeks))
	for i, w := range weeks {
		labels[i] = w.Format("2 Jan")
		starts[i] = w.Format(time.DateOnly)
	}

	labelsJSON, _ := json.Marshal(labels)
	startsJSON, _ := json.Marshal(starts)
	actualsJSON, _ := json.Marshal(actuals)
	seriesJSON, _ := json.Marshal(series)

	return c.Render(http.StatusOK, "budget.performance", BudgetPerformanceData{
		Labels:  string(labelsJSON),
		Actuals: string(actualsJSON),
		Series:  string(seriesJSON),
		Weeks:   string(startsJSON),
		Target:  math.Round(target*100) / 100,
		HasData: len(txs) > 0 || target > 0,
	})
}

// ---- week breakdown ----

// largeTransactionCount is how many of a week's biggest debits are listed
// beneath its category breakdown.
const largeTransactionCount = 5

// WeekLine is one category's spend in a single week against its share of the
// weekly target.
type WeekLine struct {
	Broad     string
	Specific  string
	Actual    float64
	Target    float64
	Diff      float64 // Actual - Target; positive is over
	HasTarget bool
	Count     int
	// Billed is set when the target is monthly or yearly: the target is spread
	// across every week, but the bill lands in one, so that week spikes.
	Billed string
}

// WeekBreakdown explains one week of the performance chart. Its lines and
// UnspentTarget add up to Total - Target, so every dollar over is accounted for.
type WeekBreakdown struct {
	Start         time.Time
	Weeks         []time.Time // the chart's weeks, for the picker
	Total         float64
	Target        float64
	Diff          float64
	Lines         []WeekLine
	UnspentTarget float64 // targets for categories with no spend this week
	UnspentCount  int
	Largest       []WeekTransaction
}

// WeekTransaction is one of a week's biggest debits.
type WeekTransaction struct {
	Date     time.Time
	Title    string
	Category string
	Amount   float64
}

func (w WeekBreakdown) End() time.Time { return w.Start.AddDate(0, 0, 6) }

// buildWeekBreakdown splits one week's counted spend by category. Targets are
// the same weekly-normalised figures the chart's target line sums.
func buildWeekBreakdown(start time.Time, txs []models.Transaction, items []models.BudgetItem) WeekBreakdown {
	byCategory := budgetItemsByCategory(items)
	week := WeekBreakdown{Start: start}

	type key struct{ broad, specific string }
	lines := make(map[key]*WeekLine)
	var counted []models.Transaction
	for _, tx := range txs {
		if !isoWeekStart(tx.Date).Equal(start) {
			continue
		}
		broad, specific, ok := countedSpend(tx, byCategory)
		if !ok {
			continue
		}
		k := key{broad, specific}
		if lines[k] == nil {
			lines[k] = &WeekLine{Broad: broad, Specific: specific}
		}
		amount := math.Abs(tx.Amount)
		lines[k].Actual += amount
		lines[k].Count++
		week.Total += amount
		counted = append(counted, tx)
	}

	spent := make(map[string]bool, len(lines))
	for _, line := range lines {
		spent[strings.ToLower(line.Specific)] = true
		item := byCategory[strings.ToLower(line.Specific)]
		line.Target = weeklyTarget(item)
		line.HasTarget = line.Target > 0
		line.Diff = line.Actual - line.Target
		if line.HasTarget && (item.Frequency == "monthly" || item.Frequency == "yearly") {
			line.Billed = "billed " + item.Frequency
		}
		week.Lines = append(week.Lines, *line)
	}
	for _, item := range items {
		target := weeklyTarget(item)
		week.Target += target
		if target > 0 && !spent[strings.ToLower(item.Category)] {
			week.UnspentTarget += target
			week.UnspentCount++
		}
	}
	week.Diff = week.Total - week.Target

	sort.Slice(week.Lines, func(i, j int) bool {
		if week.Lines[i].Diff != week.Lines[j].Diff {
			return week.Lines[i].Diff > week.Lines[j].Diff
		}
		return week.Lines[i].Specific < week.Lines[j].Specific
	})

	sort.Slice(counted, func(i, j int) bool { return counted[i].Amount < counted[j].Amount })
	for i, tx := range counted {
		if i == largeTransactionCount {
			break
		}
		_, specific, _ := spendCategory(tx)
		week.Largest = append(week.Largest, WeekTransaction{
			Date:     tx.Date,
			Title:    spendMerchant(tx),
			Category: specific,
			Amount:   math.Abs(tx.Amount),
		})
	}
	return week
}

// BudgetWeek renders the breakdown for the week starting on ?start=, or the
// last full week when none is given, since the current one is still filling.
func (app *Application) BudgetWeek(c echo.Context) error {
	weeks := performanceWeeks(time.Now())
	start := weeks[len(weeks)-2]
	if raw := c.QueryParam("start"); raw != "" {
		parsed, err := time.Parse(time.DateOnly, raw)
		if err != nil {
			return c.NoContent(http.StatusBadRequest)
		}
		start = isoWeekStart(parsed)
	}

	txs, err := app.store.ReadTransactionsByDate(start.AddDate(0, 0, -1), start.AddDate(0, 0, 8))
	if err != nil {
		return err
	}
	items, err := app.store.ReadBudgetItems()
	if err != nil {
		return err
	}
	week := buildWeekBreakdown(start, txs, items)
	week.Weeks = weeks
	return c.Render(http.StatusOK, "budget.week", week)
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
