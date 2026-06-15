package application

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/TheQueenIsDead/budge/pkg/database/models"
	"github.com/labstack/echo/v4"
)


// CategoryTargetRow is one row in the category-based budget setup table.
type CategoryTargetRow struct {
	BroadCategory string
	SubCategory   string
	RowID         string // HTML-safe id for the <tbody>
	BroadRowID    string // HTML-safe id/class for collapsible broad category
	ActualWeekly  float64
	// Populated from a saved BudgetItem, if one exists for this pair.
	ItemID        string
	Target        float64
	Frequency     string
	SubItems      []models.BudgetSubItem
	DerivedWeekly float64
	HasSubItems   bool
	// Comparison fields (all in target frequency units for display)
	ActualDisplay float64
	TargetDisplay float64
	ActualLabel   string
	Percentage    float64
	IsOver        bool
	HasTarget     bool
	NeedsTarget   bool    // has actual spend but no target set
	TrendUp       bool    // spending increased >10% vs prior 4 weeks
	TrendDown     bool    // spending decreased >10%
	PctOfIncome   float64 // targetWeekly / netWeekly (fraction)
	TargetWeekly  float64 // weekly-normalised target for internal use
}

// BroadCategoryGroup groups target rows under their parent Akahu category.
type BroadCategoryGroup struct {
	Name              string
	RowID             string // HTML-safe id for collapse targeting
	Rows              []CategoryTargetRow
	TotalActualWeekly float64
	TotalTargetWeekly float64
	PctOfIncome       float64
	HasTarget         bool
	IsOver            bool
	Percentage        float64
}

// CategorySetupData is the view-model for the budget.setup partial.
type CategorySetupData struct {
	Groups               []BroadCategoryGroup
	HasData              bool
	NetWeekly            float64
	TotalTargetWeekly    float64
	SavingsGoal          float64
	SavingsGoalFrequency string
	SavingsGoalWeekly    float64
	Remaining            float64
	MeetsSavingsGoal     bool
	HasSavingsGoal       bool
}

// AllocationData is the view-model for the budget.allocation partial.
type AllocationData struct {
	NetWeekly         float64
	TotalTargetWeekly float64
	Remaining         float64
	PctAllocated      float64 // 0–1+, used for progress bar
	PctAllocatedCss   float64 // clamped to 100 for CSS width
	IsOverAllocated   bool
	HasSalary         bool
}

// BudgetCardsData is the view-model for the three summary cards.
type BudgetCardsData struct {
	NetWeekly           float64
	TotalWeeklyExpenses float64
	WeeklySavings       float64
	HasDeficit          bool
}

// BudgetCalculatedItem is a BudgetItem with its weekly-normalised amount attached.
type BudgetCalculatedItem struct {
	models.BudgetItem
	Weekly float64
}

// SummaryGroup is one category section in the weekly overview.
type SummaryGroup struct {
	Name        string
	Items       []BudgetCalculatedItem
	WeeklyTotal float64
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
	Groups               []SummaryGroup
	TotalWeeklyExpenses  float64
	WeeklySavings        float64
	HasDeficit           bool
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
		"Salary":    salary,
		"SetupData": setupData,
	})
}

// BudgetSaveTarget creates or updates a BudgetItem for a category pair and
// returns the re-rendered row so the comparison display updates immediately.
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
			return app.renderSetupRow(c, item.ID)
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
	return app.renderSetupRow(c, newItem.ID)
}

// BudgetAddSubItem finds or creates the BudgetItem for a subcategory, adds an
// empty sub-item, and returns the re-rendered setup row.
func (app *Application) BudgetAddSubItem(c echo.Context) error {
	subCat := c.FormValue("sub_category")
	broadCat := c.FormValue("broad_category")

	items, err := app.store.ReadBudgetItems()
	if err != nil {
		return err
	}
	var itemID string
	for _, it := range items {
		if strings.EqualFold(it.Category, subCat) {
			itemID = it.ID
			break
		}
	}
	if itemID == "" {
		newItem := models.BudgetItem{
			ID:            newID(),
			Name:          subCat,
			Category:      subCat,
			BroadCategory: broadCat,
			Frequency:     "monthly",
		}
		if err := app.store.CreateBudgetItem(newItem); err != nil {
			return err
		}
		itemID = newItem.ID
	}
	if err := app.store.AddBudgetSubItem(itemID, models.BudgetSubItem{
		ID:        newID(),
		Frequency: "yearly",
	}); err != nil {
		return err
	}
	return app.renderSetupRow(c, itemID)
}

// BudgetUpdateSubItem saves a sub-item and returns the re-rendered row so the
// derived weekly total updates immediately.
func (app *Application) BudgetUpdateSubItem(c echo.Context) error {
	amount, _ := strconv.ParseFloat(c.FormValue("subitem_amount"), 64)
	if err := app.store.UpdateBudgetSubItem(c.Param("id"), models.BudgetSubItem{
		ID:        c.Param("subid"),
		Name:      c.FormValue("subitem_name"),
		Amount:    amount,
		Frequency: c.FormValue("subitem_frequency"),
	}); err != nil {
		return err
	}
	return app.renderSetupRow(c, c.Param("id"))
}

// BudgetDeleteSubItem removes a sub-item and returns the re-rendered setup row.
func (app *Application) BudgetDeleteSubItem(c echo.Context) error {
	if err := app.store.DeleteBudgetSubItem(c.Param("id"), c.Param("subid")); err != nil {
		return err
	}
	return app.renderSetupRow(c, c.Param("id"))
}

func (app *Application) renderSetupRow(c echo.Context, itemID string) error {
	item, err := app.store.GetBudgetItem(itemID)
	if err != nil {
		return err
	}
	actualWeekly, err := app.subcatActualWeekly(item.Category, item.Frequency)
	if err != nil {
		return err
	}
	salary, _ := app.store.GetBudgetSalary()
	return c.Render(http.StatusOK, "budget.setup.row",
		buildSetupRowData(item, actualWeekly, computeNetWeekly(salary), false, false))
}

// BudgetAllocation returns the unallocated income indicator partial.
func (app *Application) BudgetAllocation(c echo.Context) error {
	salary, err := app.store.GetBudgetSalary()
	if err != nil {
		return err
	}
	items, err := app.store.ReadBudgetItems()
	if err != nil {
		return err
	}
	netWeekly := computeNetWeekly(salary)
	var totalTarget float64
	for _, it := range items {
		totalTarget += weeklyTarget(it)
	}
	remaining := netWeekly - totalTarget
	pct := 0.0
	if netWeekly > 0 {
		pct = totalTarget / netWeekly
	}
	return c.Render(http.StatusOK, "budget.allocation", AllocationData{
		NetWeekly:         netWeekly,
		TotalTargetWeekly: totalTarget,
		Remaining:         remaining,
		PctAllocated:      pct,
		PctAllocatedCss:   math.Min(pct*100, 100),
		IsOverAllocated:   remaining < 0,
		HasSalary:         salary.Salary > 0,
	})
}

// BudgetCards returns the three summary cards (income / expenses / savings).
func (app *Application) BudgetCards(c echo.Context) error {
	salary, err := app.store.GetBudgetSalary()
	if err != nil {
		return err
	}
	items, err := app.store.ReadBudgetItems()
	if err != nil {
		return err
	}
	netWeekly := computeNetWeekly(salary)
	var totalExpenses float64
	for _, it := range items {
		totalExpenses += weeklyTarget(it)
	}
	savings := netWeekly - totalExpenses
	return c.Render(http.StatusOK, "budget.cards", BudgetCardsData{
		NetWeekly:           netWeekly,
		TotalWeeklyExpenses: totalExpenses,
		WeeklySavings:       savings,
		HasDeficit:          savings < 0,
	})
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

// subcatActualWeekly computes the weekly-normalised actual spend for one
// subcategory using a window matched to the item's target frequency.
func (app *Application) subcatActualWeekly(subCat, freq string) (float64, error) {
	txs, err := app.store.ReadTransactionsByDate(time.Now().AddDate(0, -13, 0), time.Now())
	if err != nil {
		return 0, err
	}
	var windowWeeks float64
	var cutoff time.Time
	now := time.Now()
	switch freq {
	case "fortnightly":
		windowWeeks, cutoff = 8, isoWeekStart(now).AddDate(0, 0, -8*7)
	case "monthly":
		windowWeeks, cutoff = 13, isoWeekStart(now).AddDate(0, 0, -13*7)
	case "yearly":
		windowWeeks, cutoff = 56, time.Time{} // no cutoff needed
	default: // weekly or unset
		windowWeeks, cutoff = 4, isoWeekStart(now).AddDate(0, 0, -4*7)
	}

	var total float64
	subCatLower := strings.ToLower(subCat)
	for _, tx := range txs {
		if tx.Amount >= 0 || tx.Type == "TRANSFER" {
			continue
		}
		if !cutoff.IsZero() && !tx.Date.After(cutoff) {
			continue
		}
		specific := strings.ToLower(tx.Category.Name)
		broad := strings.ToLower(tx.Category.Groups.PersonalFinance.Name)
		if specific == "" {
			specific = broad
		}
		if specific == subCatLower || broad == subCatLower {
			total += math.Abs(tx.Amount)
		}
	}
	return total / windowWeeks, nil
}

func buildSetupRowData(item models.BudgetItem, actualWeekly, netWeekly float64, trendUp, trendDown bool) CategoryTargetRow {
	var derived float64
	for _, si := range item.SubItems {
		derived += toWeeklyAmount(si.Amount, si.Frequency)
	}
	hasSubItems := len(item.SubItems) > 0
	targetWeekly := toWeeklyAmount(item.Amount, item.Frequency)
	if hasSubItems {
		targetWeekly = derived
	}
	hasTarget := targetWeekly > 0

	freq := item.Frequency
	if hasSubItems || freq == "" {
		freq = "weekly"
	}
	actualDisplay := actualInFrequency(actualWeekly, freq)
	targetDisplay := item.Amount
	if hasSubItems {
		targetDisplay = derived
	}
	label := frequencyLabel(freq)

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
		BroadRowID:    sanitizeBroadID(item.BroadCategory),
		ActualWeekly:  actualWeekly,
		ItemID:        item.ID,
		Target:        item.Amount,
		Frequency:     item.Frequency,
		SubItems:      item.SubItems,
		DerivedWeekly: derived,
		HasSubItems:   hasSubItems,
		ActualDisplay: actualDisplay,
		TargetDisplay: targetDisplay,
		ActualLabel:   label,
		TargetWeekly:  targetWeekly,
		Percentage:    pct,
		PctOfIncome:   pctOfIncome,
		IsOver:        hasTarget && actualWeekly > targetWeekly,
		HasTarget:     hasTarget,
		NeedsTarget:   !hasTarget && actualWeekly > 0,
		TrendUp:       trendUp,
		TrendDown:     trendDown,
	}
}

// buildSetupData groups 13 months of transactions by category pair and merges
// in any saved targets so the setup table can pre-populate target inputs.
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
	itemBySubCat := make(map[string]models.BudgetItem)
	for _, it := range items {
		itemBySubCat[strings.ToLower(it.Category)] = it
	}

	type pairKey struct{ broad, specific string }
	totals := make(map[pairKey]float64)
	recent4 := make(map[pairKey]float64) // last 4 weeks spend
	prev4 := make(map[pairKey]float64)   // 4–8 weeks ago spend
	var pairOrder []pairKey
	pairSeen := make(map[pairKey]bool)
	var broadOrder []string
	broadSeen := make(map[string]bool)

	now := time.Now()
	// Cumulative window cutoffs — a transaction contributes to every window
	// whose cutoff it is newer than, so each window is naturally additive.
	cut4 := isoWeekStart(now).AddDate(0, 0, -4*7)
	cut8 := isoWeekStart(now).AddDate(0, 0, -8*7)
	cut13 := isoWeekStart(now).AddDate(0, 0, -13*7)

	win4 := make(map[pairKey]float64)  // last  4 weeks  (weekly target)
	win8 := make(map[pairKey]float64)  // last  8 weeks  (fortnightly target)
	win13 := make(map[pairKey]float64) // last 13 weeks  (monthly target)
	// totals = last 56 weeks (yearly target), declared above

	for _, tx := range txs {
		if tx.Amount >= 0 || tx.Type == "TRANSFER" {
			continue
		}
		broad := tx.Category.Groups.PersonalFinance.Name
		if broad == "" {
			continue
		}
		specific := tx.Category.Name
		if specific == "" {
			specific = broad
		}
		key := pairKey{broad, specific}
		amt := math.Abs(tx.Amount)
		totals[key] += amt // always (56-week window)
		// Independent ifs — each window is cumulative (newer txs hit multiple windows).
		if tx.Date.After(cut13) { win13[key] += amt }
		if tx.Date.After(cut8)  { win8[key] += amt }
		if tx.Date.After(cut4)  { win4[key] += amt }
		// Trend: compare last 4 weeks vs prior 4 weeks.
		if tx.Date.After(cut4) {
			recent4[key] += amt
		} else if tx.Date.After(cut8) {
			prev4[key] += amt
		}
		if !pairSeen[key] {
			pairSeen[key] = true
			pairOrder = append(pairOrder, key)
		}
		if !broadSeen[broad] {
			broadSeen[broad] = true
			broadOrder = append(broadOrder, broad)
		}
	}

	groupRows := make(map[string][]CategoryTargetRow)
	for _, key := range pairOrder {
		saved := itemBySubCat[strings.ToLower(key.specific)]

		// Pick actual window based on the item's target frequency so weekly
		// categories reflect recent spend while yearly ones amortise correctly.
		var rawTotal, windowWeeks float64
		switch saved.Frequency {
		case "weekly", "":
			rawTotal, windowWeeks = win4[key], 4
		case "fortnightly":
			rawTotal, windowWeeks = win8[key], 8
		case "monthly":
			rawTotal, windowWeeks = win13[key], 13
		default: // yearly
			rawTotal, windowWeeks = totals[key], 56
		}
		actualWeekly := rawTotal / windowWeeks

		// Trend: compare weekly average of recent 4 weeks vs prior 4 weeks.
		recentAvg := recent4[key] / 4.0
		prevAvg := prev4[key] / 4.0
		var tUp, tDown bool
		if prevAvg > 0 {
			change := (recentAvg - prevAvg) / prevAvg
			tUp = change > 0.10
			tDown = change < -0.10
		} else if recentAvg > 0 {
			tUp = true // new spending appeared
		}

		row := buildSetupRowData(saved, actualWeekly, netWeekly, tUp, tDown)
		if row.BroadCategory == "" {
			row.BroadCategory = key.broad
		}
		if row.SubCategory == "" {
			row.SubCategory = key.specific
			row.RowID = sanitizeID(key.specific)
		}
		if row.BroadRowID == "" {
			row.BroadRowID = sanitizeBroadID(key.broad)
		}
		groupRows[key.broad] = append(groupRows[key.broad], row)
	}

	var groups []BroadCategoryGroup
	var totalTargetWeekly float64
	for _, broad := range broadOrder {
		rows := groupRows[broad]
		var totalActual, totalTarget float64
		for _, row := range rows {
			totalActual += row.ActualWeekly
			totalTarget += row.TargetWeekly
		}
		hasTarget := totalTarget > 0
		var pct, pctOfIncome float64
		if hasTarget {
			pct = totalActual / totalTarget
		}
		if netWeekly > 0 {
			pctOfIncome = totalTarget / netWeekly
		}
		groups = append(groups, BroadCategoryGroup{
			Name:              broad,
			RowID:             sanitizeBroadID(broad),
			Rows:              rows,
			TotalActualWeekly: totalActual,
			TotalTargetWeekly: totalTarget,
			PctOfIncome:       pctOfIncome,
			HasTarget:         hasTarget,
			IsOver:            hasTarget && totalActual > totalTarget,
			Percentage:        pct,
		})
		totalTargetWeekly += totalTarget
	}

	savingsGoalWeekly := toWeeklyAmount(salary.SavingsGoal, salary.SavingsGoalFrequency)
	remaining := netWeekly - totalTargetWeekly

	return CategorySetupData{
		Groups:               groups,
		HasData:              len(txs) > 0,
		NetWeekly:            netWeekly,
		TotalTargetWeekly:    totalTargetWeekly,
		SavingsGoal:          salary.SavingsGoal,
		SavingsGoalFrequency: salary.SavingsGoalFrequency,
		SavingsGoalWeekly:    savingsGoalWeekly,
		Remaining:            remaining,
		MeetsSavingsGoal:     savingsGoalWeekly > 0 && remaining >= savingsGoalWeekly,
		HasSavingsGoal:       salary.SavingsGoal > 0,
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
	return app.store.SaveBudgetSalary(models.BudgetSalary{
		Salary:          salary,
		SalaryFrequency: c.FormValue("salary_frequency"),
		IncludePAYE:     c.FormValue("include_paye") == "on",
		KiwiSaverRate:   kiwiSaverRate,
		StudentLoan:     c.FormValue("student_loan") == "on",
		Categories:      existing.Categories,
	})
}

// BudgetSummary reads saved state and returns calculated summary HTML.
func (app *Application) BudgetSummary(c echo.Context) error {
	salary, err := app.store.GetBudgetSalary()
	if err != nil {
		return err
	}
	items, err := app.store.ReadBudgetItems()
	if err != nil {
		return err
	}

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

	// Group items by their Category field, preserving first-seen order.
	byCategory := make(map[string][]BudgetCalculatedItem)
	var categoryOrder []string
	categorySeen := make(map[string]bool)
	for _, it := range items {
		w := weeklyTarget(it)
		byCategory[it.Category] = append(byCategory[it.Category], BudgetCalculatedItem{BudgetItem: it, Weekly: w})
		if !categorySeen[it.Category] {
			categorySeen[it.Category] = true
			categoryOrder = append(categoryOrder, it.Category)
		}
	}

	var groups []SummaryGroup
	var totalWeekly float64
	for _, name := range categoryOrder {
		its := byCategory[name]
		var sub float64
		for _, it := range its {
			sub += it.Weekly
		}
		groups = append(groups, SummaryGroup{Name: name, Items: its, WeeklyTotal: sub})
		totalWeekly += sub
	}

	netWeekly := netAnnual / 52
	savings := netWeekly - totalWeekly

	return c.Render(http.StatusOK, "budget.summary", BudgetSummary{
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
		NetWeekly:            netWeekly,
		Groups:               groups,
		TotalWeeklyExpenses:  totalWeekly,
		WeeklySavings:        savings,
		HasDeficit:           savings < 0,
	})
}

// ---- performance chart ----

// BudgetPerformanceData is the view-model for the budget.performance partial.
// Labels and Actuals are JSON strings embedded as data attributes on the canvas;
// Go's HTML-encoding of attribute values keeps them safe, and dataset decodes them.
type BudgetPerformanceData struct {
	Labels  string
	Actuals string
	Target  float64
	HasData bool
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

	// Build 13 weekly buckets, most recent on the right.
	ws := isoWeekStart(time.Now())
	weeks := make([]time.Time, 13)
	for i := range weeks {
		weeks[12-i] = ws.AddDate(0, 0, -7*i)
	}

	weekTotals := make(map[time.Time]float64)
	for _, tx := range txs {
		if tx.Amount >= 0 || tx.Type == "TRANSFER" {
			continue
		}
		weekTotals[isoWeekStart(tx.Date)] += math.Abs(tx.Amount)
	}

	labels := make([]string, 13)
	actuals := make([]float64, 13)
	for i, w := range weeks {
		labels[i] = w.Format("2 Jan")
		actuals[i] = math.Round(weekTotals[w]*100) / 100
	}

	labelsJSON, _ := json.Marshal(labels)
	actualsJSON, _ := json.Marshal(actuals)

	return c.Render(http.StatusOK, "budget.performance", BudgetPerformanceData{
		Labels:  string(labelsJSON),
		Actuals: string(actualsJSON),
		Target:  math.Round(target*100) / 100,
		HasData: len(txs) > 0 || target > 0,
	})
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
// When sub-items are present their amounts are summed; otherwise the item's
// own Amount/Frequency is used.
func weeklyTarget(item models.BudgetItem) float64 {
	if len(item.SubItems) == 0 {
		return toWeeklyAmount(item.Amount, item.Frequency)
	}
	var total float64
	for _, si := range item.SubItems {
		total += toWeeklyAmount(si.Amount, si.Frequency)
	}
	return total
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

func frequencyLabel(frequency string) string {
	switch frequency {
	case "weekly", "":
		return "per Week"
	case "fortnightly":
		return "per Fortnight"
	case "monthly":
		return "per Month"
	case "yearly":
		return "per Year"
	}
	return "per Week"
}

// sanitizeID converts a string to a safe HTML id, prefixed with "sc-" (subcategory rows).
func sanitizeID(s string) string { return sanitizeWithPrefix("sc-", s) }

// sanitizeBroadID produces an HTML-safe id for broad-category collapse targets ("bc-" prefix).
func sanitizeBroadID(s string) string { return sanitizeWithPrefix("bc-", s) }

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

// ---- suggestions ----

// SuggestionsData powers the two datalists on the budget page.
type SuggestionsData struct {
	Categories []string
	Merchants  []string
}

func (app *Application) BudgetSuggestions(c echo.Context) error {
	start := time.Now().AddDate(0, -13, 0)
	txs, err := app.store.ReadTransactionsByDate(start, time.Now())
	if err != nil {
		return err
	}

	catSeen := make(map[string]bool)
	merchSeen := make(map[string]bool)
	var categories, merchants []string

	for _, tx := range txs {
		// Broad category (e.g. "Transport")
		if cat := tx.Category.Groups.PersonalFinance.Name; cat != "" && !catSeen[cat] {
			catSeen[cat] = true
			categories = append(categories, cat)
		}
		// Specific subcategory (e.g. "Fuel stations")
		if cat := tx.Category.Name; cat != "" && !catSeen[cat] {
			catSeen[cat] = true
			categories = append(categories, cat)
		}
		if m := tx.Merchant.Name; m != "" && !merchSeen[m] {
			merchSeen[m] = true
			merchants = append(merchants, m)
		}
	}

	return c.Render(http.StatusOK, "budget.suggestions", SuggestionsData{
		Categories: categories,
		Merchants:  merchants,
	})
}

// ---- keyword CRUD ----

// ItemTagsData is the view-model for the combined keyword+merchant tags view.
type ItemTagsData struct {
	models.BudgetItem
	AutoMerchants []string // merchants matched by category, not yet saved as keywords
}

func (app *Application) BudgetItemKeywords(c echo.Context) error {
	item, err := app.store.GetBudgetItem(c.Param("id"))
	if err != nil {
		return err
	}

	// Discover merchants from transactions matched by category (not keyword).
	txs, _ := app.store.ReadTransactionsByDate(time.Now().AddDate(0, -13, 0), time.Now())

	kwSet := make(map[string]bool)
	for _, kw := range item.Keywords {
		kwSet[strings.ToLower(kw)] = true
	}

	seen := make(map[string]bool)
	var autoMerchants []string
	for _, tx := range txs {
		if tx.Amount >= 0 {
			continue
		}
		broad    := strings.ToLower(tx.Category.Groups.PersonalFinance.Name)
		specific := strings.ToLower(tx.Category.Name)
		itemCat  := strings.ToLower(item.Category)
		if itemCat == "" || (itemCat != broad && itemCat != specific) {
			continue
		}
		name := tx.Merchant.Name
		if name == "" {
			continue
		}
		lower := strings.ToLower(name)
		if !seen[lower] && !kwSet[lower] {
			seen[lower] = true
			autoMerchants = append(autoMerchants, name)
		}
	}

	return c.Render(http.StatusOK, "budget.item.keywords", ItemTagsData{
		BudgetItem:    item,
		AutoMerchants: autoMerchants,
	})
}

func (app *Application) BudgetAddKeyword(c echo.Context) error {
	kw := strings.TrimSpace(c.FormValue("keyword"))
	if kw == "" {
		return c.NoContent(http.StatusBadRequest)
	}
	item, err := app.store.GetBudgetItem(c.Param("id"))
	if err != nil {
		return err
	}
	for _, existing := range item.Keywords {
		if strings.EqualFold(existing, kw) {
			return app.BudgetItemKeywords(c) // already present — refresh view
		}
	}
	item.Keywords = append(item.Keywords, kw)
	if err := app.store.UpdateBudgetItem(item); err != nil {
		return err
	}
	return app.BudgetItemKeywords(c)
}

func (app *Application) BudgetRemoveKeyword(c echo.Context) error {
	target := c.Param("keyword")
	item, err := app.store.GetBudgetItem(c.Param("id"))
	if err != nil {
		return err
	}
	filtered := item.Keywords[:0]
	for _, kw := range item.Keywords {
		if !strings.EqualFold(kw, target) {
			filtered = append(filtered, kw)
		}
	}
	item.Keywords = filtered
	if err := app.store.UpdateBudgetItem(item); err != nil {
		return err
	}
	return app.BudgetItemKeywords(c)
}