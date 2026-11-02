package application

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"strconv"

	"github.com/TheQueenIsDead/budge/pkg/database/models"
	"github.com/labstack/echo/v4"
)

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

// BudgetCards returns the summary cards (income / expenses / savings).
func (app *Application) BudgetCards(c echo.Context) error {
	ctx := c.Request().Context()
	data, err := app.buildBudgetCards(ctx)
	if err != nil {
		return err
	}
	return c.Render(http.StatusOK, "budget.cards", data)
}

// BudgetAllocation returns the bar showing how much of take-home pay the
// targets claim.
func (app *Application) BudgetAllocation(c echo.Context) error {
	ctx := c.Request().Context()
	data, err := app.buildBudgetCards(ctx)
	if err != nil {
		return err
	}
	return c.Render(http.StatusOK, "budget.allocation", data)
}

func (app *Application) buildBudgetCards(ctx context.Context) (BudgetCardsData, error) {
	salary, err := app.store.GetBudgetSalary(ctx)
	if err != nil {
		return BudgetCardsData{}, err
	}
	items, err := app.store.ReadBudgetItems(ctx)
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
	ctx := c.Request().Context()
	goal, _ := strconv.ParseFloat(c.FormValue("savings_goal"), 64)
	freq := c.FormValue("savings_goal_frequency")
	if freq == "" {
		freq = "weekly"
	}
	existing, err := app.store.GetBudgetSalary(ctx)
	if err != nil {
		return err
	}
	existing.SavingsGoal = goal
	existing.SavingsGoalFrequency = freq
	return app.store.SaveBudgetSalary(ctx, existing)
}

// BudgetSaveSalary persists salary config (called on field change, hx-swap="none").
func (app *Application) BudgetSaveSalary(c echo.Context) error {
	ctx := c.Request().Context()
	salary, _ := strconv.ParseFloat(c.FormValue("salary"), 64)
	kiwiSaverRate, _ := strconv.ParseFloat(c.FormValue("kiwisaver_rate"), 64)
	existing, err := app.store.GetBudgetSalary(ctx)
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
	return app.store.SaveBudgetSalary(ctx, existing)
}

// BudgetSummary reads saved state and returns calculated summary HTML.
func (app *Application) BudgetSummary(c echo.Context) error {
	ctx := c.Request().Context()
	salary, err := app.store.GetBudgetSalary(ctx)
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
	ctx := c.Request().Context()
	salary, err := app.store.GetBudgetSalary(ctx)
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
