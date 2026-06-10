package application

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"html/template"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/TheQueenIsDead/budge/pkg/database/models"
	"github.com/labstack/echo/v4"
)

// frequencyWindowWeeks is the lookback window (in weeks) used when computing
// actual spend for each budget item frequency. A yearly item needs a 13-month
// window so the single annual transaction is always captured.
var frequencyWindowWeeks = map[string]float64{
	"weekly":      4,
	"fortnightly": 8,
	"monthly":     13,
	"yearly":      56,
}

// CategoryTargetRow is one row in the category-based budget setup table.
type CategoryTargetRow struct {
	BroadCategory string
	SubCategory   string
	RowID         string // HTML-safe id for the <tbody>
	ActualWeekly  float64
	// Populated from a saved BudgetItem, if one exists for this pair.
	ItemID        string
	Target        float64
	Frequency     string
	SubItems      []models.BudgetSubItem
	DerivedWeekly float64
	HasSubItems   bool
	// Comparison fields
	TargetWeekly float64
	Percentage   float64
	IsOver       bool
	HasTarget    bool
}

// BroadCategoryGroup groups target rows under their parent Akahu category.
type BroadCategoryGroup struct {
	Name string
	Rows []CategoryTargetRow
}

// CategorySetupData is the view-model for the budget.setup partial.
type CategorySetupData struct {
	Groups  []BroadCategoryGroup
	HasData bool
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
	actualWeekly, err := app.subcatActualWeekly(item.Category)
	if err != nil {
		return err
	}
	return c.Render(http.StatusOK, "budget.setup.row", buildSetupRowData(item, actualWeekly))
}

// subcatActualWeekly computes the weekly-normalised actual spend for one
// subcategory over the last 13 months.
func (app *Application) subcatActualWeekly(subCat string) (float64, error) {
	txs, err := app.store.ReadTransactionsByDate(time.Now().AddDate(0, -13, 0), time.Now())
	if err != nil {
		return 0, err
	}
	var total float64
	subCatLower := strings.ToLower(subCat)
	for _, tx := range txs {
		if tx.Amount >= 0 || tx.Type == "TRANSFER" {
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
	return total / 56.0, nil // 56 weeks ≈ 13 months
}

func buildSetupRowData(item models.BudgetItem, actualWeekly float64) CategoryTargetRow {
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
	var pct float64
	if hasTarget {
		pct = actualWeekly / targetWeekly
	}
	return CategoryTargetRow{
		BroadCategory: item.BroadCategory,
		SubCategory:   item.Category,
		RowID:         sanitizeID(item.Category),
		ActualWeekly:  actualWeekly,
		ItemID:        item.ID,
		Target:        item.Amount,
		Frequency:     item.Frequency,
		SubItems:      item.SubItems,
		DerivedWeekly: derived,
		HasSubItems:   hasSubItems,
		TargetWeekly:  targetWeekly,
		Percentage:    pct,
		IsOver:        hasTarget && actualWeekly > targetWeekly,
		HasTarget:     hasTarget,
	}
}

// buildSetupData groups 13 months of transactions by category pair and merges
// in any saved targets so the setup table can pre-populate target inputs.
func (app *Application) buildSetupData() (CategorySetupData, error) {
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
	var pairOrder []pairKey
	pairSeen := make(map[pairKey]bool)
	var broadOrder []string
	broadSeen := make(map[string]bool)

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
		totals[key] += math.Abs(tx.Amount)
		if !pairSeen[key] {
			pairSeen[key] = true
			pairOrder = append(pairOrder, key)
		}
		if !broadSeen[broad] {
			broadSeen[broad] = true
			broadOrder = append(broadOrder, broad)
		}
	}

	const windowWeeks = 56.0 // 13 months ≈ 56 weeks
	groupRows := make(map[string][]CategoryTargetRow)
	for _, key := range pairOrder {
		saved := itemBySubCat[strings.ToLower(key.specific)]
		actualWeekly := totals[key] / windowWeeks
		row := buildSetupRowData(saved, actualWeekly)
		// Override with transaction-derived broad category when item not yet saved.
		if row.BroadCategory == "" {
			row.BroadCategory = key.broad
		}
		if row.SubCategory == "" {
			row.SubCategory = key.specific
			row.RowID = sanitizeID(key.specific)
		}
		groupRows[key.broad] = append(groupRows[key.broad], row)
	}

	var groups []BroadCategoryGroup
	for _, broad := range broadOrder {
		groups = append(groups, BroadCategoryGroup{Name: broad, Rows: groupRows[broad]})
	}
	return CategorySetupData{Groups: groups, HasData: len(txs) > 0}, nil
}

// BudgetDeleteItem removes an item (e.g. to clear a target).
func (app *Application) BudgetDeleteItem(c echo.Context) error {
	return app.store.DeleteBudgetItem(c.Param("id"))
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
type BudgetPerformanceData struct {
	LabelsJSON  template.JS
	ActualsJSON template.JS
	Target      float64
	HasData     bool
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
		LabelsJSON:  template.JS(labelsJSON),
		ActualsJSON: template.JS(actualsJSON),
		Target:      math.Round(target*100) / 100,
		HasData:     len(txs) > 0 || target > 0,
	})
}

// isoWeekStart returns the Monday of the ISO week containing t.
func isoWeekStart(t time.Time) time.Time {
	y, m, d := t.Date()
	t0 := time.Date(y, m, d, 0, 0, 0, 0, t.Location())
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

// sanitizeID converts a string to a safe HTML id by lowercasing and replacing
// non-alphanumeric characters with hyphens, prefixed with "sc-".
func sanitizeID(s string) string {
	var b strings.Builder
	b.WriteString("sc-")
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		} else {
			b.WriteRune('-')
		}
	}
	return b.String()
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

// ---- actuals ----

// ActualsRow is one line in the budget vs actual table.
type ActualsRow struct {
	ID             string
	Name           string
	Category       string
	BudgetedWeekly float64
	ActualWeekly   float64
	Variance       float64
	Over           bool // actual > budgeted
	Significant    bool // variance > 10% of budgeted
}

// UnmatchedGroup is one Akahu PersonalFinance category worth of unmatched spend.
type UnmatchedGroup struct {
	Category    string
	WeeklyTotal float64
}

// ActualsData is the view-model for the budget.actuals partial.
type ActualsData struct {
	Rows            []ActualsRow
	UnmatchedGroups []UnmatchedGroup
	HasTransactions bool
}

func (app *Application) BudgetActuals(c echo.Context) error {
	items, err := app.store.ReadBudgetItems()
	if err != nil {
		return err
	}

	// One query covers all frequencies (13 months is the widest window).
	start := time.Now().AddDate(0, -13, 0)
	txs, err := app.store.ReadTransactionsByDate(start, time.Now())
	if err != nil {
		return err
	}

	matched, unmatched := matchTransactions(items, txs)

	var rows []ActualsRow
	for _, item := range items {
		windowWeeks := frequencyWindowWeeks[item.Frequency]
		if windowWeeks == 0 {
			windowWeeks = 4
		}
		cutoff := time.Now().AddDate(0, 0, -int(windowWeeks*7))

		var actualTotal float64
		for _, tx := range matched[item.ID] {
			if tx.Date.After(cutoff) {
				actualTotal += math.Abs(tx.Amount)
			}
		}
		actualWeekly := actualTotal / windowWeeks
		budgetedWeekly := weeklyTarget(item)
		variance := actualWeekly - budgetedWeekly

		rows = append(rows, ActualsRow{
			ID:             item.ID,
			Name:           item.Name,
			Category:       item.Category,
			BudgetedWeekly: budgetedWeekly,
			ActualWeekly:   actualWeekly,
			Variance:       variance,
			Over:           variance > 0,
			Significant:    budgetedWeekly > 0 && math.Abs(variance)/budgetedWeekly > 0.10,
		})
	}

	// Group unmatched by Akahu PersonalFinance category over a 4-week window.
	cutoff4w := time.Now().AddDate(0, 0, -28)
	unmatchedTotals := make(map[string]float64)
	var unmatchedOrder []string
	for _, tx := range unmatched {
		if !tx.Date.After(cutoff4w) {
			continue
		}
		cat := tx.Category.Groups.PersonalFinance.Name
		if cat == "" {
			cat = "Uncategorised"
		}
		if _, seen := unmatchedTotals[cat]; !seen {
			unmatchedOrder = append(unmatchedOrder, cat)
		}
		unmatchedTotals[cat] += math.Abs(tx.Amount)
	}
	var unmatchedGroups []UnmatchedGroup
	for _, cat := range unmatchedOrder {
		unmatchedGroups = append(unmatchedGroups, UnmatchedGroup{
			Category:    cat,
			WeeklyTotal: unmatchedTotals[cat] / 4,
		})
	}

	return c.Render(http.StatusOK, "budget.actuals", ActualsData{
		Rows:            rows,
		UnmatchedGroups: unmatchedGroups,
		HasTransactions: len(txs) > 0,
	})
}

// matchTransactions returns:
//   - matched: map of BudgetItemID → transactions matched to that item
//   - unmatched: transactions that didn't match any item
//
// Layer 1: keyword match on Merchant.Name / Description (case-insensitive).
// Layer 2: Akahu PersonalFinance category == item.Category (case-insensitive).
func matchTransactions(items []models.BudgetItem, txs []models.Transaction) (matched map[string][]models.Transaction, unmatched []models.Transaction) {
	matched = make(map[string][]models.Transaction)

	for _, tx := range txs {
		if tx.Amount >= 0 || tx.Type == "TRANSFER" {
			continue
		}
		merchantLower := strings.ToLower(tx.Merchant.Name)
		descLower := strings.ToLower(tx.Description)
		txCatBroad    := strings.ToLower(tx.Category.Groups.PersonalFinance.Name)
		txCatSpecific := strings.ToLower(tx.Category.Name)

		var matchedID string
		for _, item := range items {
			// Layer 1: explicit keywords
			for _, kw := range item.Keywords {
				kw = strings.ToLower(kw)
				if strings.Contains(merchantLower, kw) || strings.Contains(descLower, kw) {
					matchedID = item.ID
					break
				}
			}
			if matchedID != "" {
				break
			}
			// Layer 2: match against either the broad PersonalFinance category
			// (e.g. "Transport") or the specific subcategory (e.g. "Fuel stations").
			itemCat := strings.ToLower(item.Category)
			if itemCat != "" && (itemCat == txCatBroad || itemCat == txCatSpecific) {
				matchedID = item.ID
				break
			}
		}

		if matchedID != "" {
			matched[matchedID] = append(matched[matchedID], tx)
		} else {
			unmatched = append(unmatched, tx)
		}
	}
	return matched, unmatched
}

// MatchedTransaction is one transaction shown in the per-item drill-down.
type MatchedTransaction struct {
	Date        string
	Merchant    string
	Description string
	Amount      float64
	MatchedBy   string // "keyword" or "category"
}

// BudgetItemActuals returns the matched transactions for a single budget item.
func (app *Application) BudgetItemActuals(c echo.Context) error {
	item, err := app.store.GetBudgetItem(c.Param("id"))
	if err != nil {
		return err
	}

	start := time.Now().AddDate(0, -13, 0)
	txs, err := app.store.ReadTransactionsByDate(start, time.Now())
	if err != nil {
		return err
	}

	windowWeeks := frequencyWindowWeeks[item.Frequency]
	if windowWeeks == 0 {
		windowWeeks = 4
	}
	cutoff := time.Now().AddDate(0, 0, -int(windowWeeks*7))

	itemCat := strings.ToLower(item.Category)

	var rows []MatchedTransaction
	for _, tx := range txs {
		if tx.Amount >= 0 || tx.Type == "TRANSFER" || !tx.Date.After(cutoff) {
			continue
		}
		merchantLower := strings.ToLower(tx.Merchant.Name)
		descLower := strings.ToLower(tx.Description)

		matchedBy := ""
		for _, kw := range item.Keywords {
			if strings.Contains(merchantLower, strings.ToLower(kw)) ||
				strings.Contains(descLower, strings.ToLower(kw)) {
				matchedBy = "keyword: " + kw
				break
			}
		}
		if matchedBy == "" && itemCat != "" {
			broad    := strings.ToLower(tx.Category.Groups.PersonalFinance.Name)
			specific := strings.ToLower(tx.Category.Name)
			if itemCat == broad {
				matchedBy = "category: " + tx.Category.Groups.PersonalFinance.Name
			} else if itemCat == specific {
				matchedBy = "subcategory: " + tx.Category.Name
			}
		}
		if matchedBy == "" {
			continue
		}

		label := tx.Merchant.Name
		if label == "" {
			label = tx.Description
		}
		rows = append(rows, MatchedTransaction{
			Date:        tx.Date.Format("2 Jan 06"),
			Merchant:    label,
			Description: tx.Description,
			Amount:      math.Abs(tx.Amount),
			MatchedBy:   matchedBy,
		})
	}

	return c.Render(http.StatusOK, "budget.item.actuals", rows)
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

func (app *Application) BudgetItemKeywords(c echo.Context) error {
	item, err := app.store.GetBudgetItem(c.Param("id"))
	if err != nil {
		return err
	}
	return c.Render(http.StatusOK, "budget.item.keywords", item)
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
			return c.Render(http.StatusOK, "budget.item.keywords", item) // already present
		}
	}
	item.Keywords = append(item.Keywords, kw)
	if err := app.store.UpdateBudgetItem(item); err != nil {
		return err
	}
	return c.Render(http.StatusOK, "budget.item.keywords", item)
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
	return c.Render(http.StatusOK, "budget.item.keywords", item)
}