package application

import (
	"strings"
	"testing"
	"time"

	"github.com/TheQueenIsDead/budge/pkg/database/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Expected values below are derived from the published NZ rates the code cites,
// not from running the code, so a change in behaviour fails the test rather
// than silently redefining "correct".

func TestCalculateNZPAYE(t *testing.T) {
	// Composite rates in force from 31 July 2024:
	//        0 –  15,600 @ 10.5%
	//   15,600 –  53,500 @ 17.5%
	//   53,500 –  78,100 @ 30.0%
	//   78,100 – 180,000 @ 33.0%
	//  180,000 +         @ 39.0%
	tests := []struct {
		name     string
		annual   float64
		expected float64
	}{
		{"zero income", 0, 0},
		{"part way through first bracket", 10_000, 1_050},     // 10000 * .105
		{"exactly first threshold", 15_600, 1_638},            // 15600 * .105
		{"one dollar into second bracket", 15_601, 1_638.175}, // +1 * .175
		{"exactly second threshold", 53_500, 8_270.50},        // 1638 + 37900*.175
		{"mid third bracket", 60_000, 10_220.50},              // 8270.50 + 6500*.30
		{"exactly third threshold", 78_100, 15_650.50},        // 8270.50 + 24600*.30
		{"mid fourth bracket", 100_000, 22_877.50},            // 15650.50 + 21900*.33
		{"exactly fourth threshold", 180_000, 49_277.50},      // 15650.50 + 101900*.33
		{"into top bracket", 250_000, 76_577.50},              // 49277.50 + 70000*.39
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.InDelta(t, test.expected, calculateNZPAYE(test.annual), 0.005)
		})
	}
}

func TestCalculateNZPAYEIsProgressive(t *testing.T) {
	// Crossing a threshold must never cost more tax than the extra income.
	for _, threshold := range []float64{15_600, 53_500, 78_100, 180_000} {
		before := calculateNZPAYE(threshold)
		after := calculateNZPAYE(threshold + 1_000)
		assert.Greater(t, after, before, "tax should rise across threshold %.0f", threshold)
		assert.Less(t, after-before, 1_000.0,
			"crossing threshold %.0f should not cost more than the extra earned", threshold)
	}
}

func TestCalculateNZACC(t *testing.T) {
	// 1.75% on earnings up to the $142,283 cap (2025-26).
	tests := []struct {
		name     string
		annual   float64
		expected float64
	}{
		{"zero", 0, 0},
		{"below cap", 50_000, 875},              // 50000 * .0175
		{"exactly at cap", 142_283, 2_489.9525}, // 142283 * .0175
		{"above cap is capped", 200_000, 2_489.9525},
		{"far above cap is capped", 1_000_000, 2_489.9525},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.InDelta(t, test.expected, calculateNZACC(test.annual), 0.005)
		})
	}
}

func TestCalculateNZStudentLoan(t *testing.T) {
	// 12% of earnings above the $22,828 threshold.
	tests := []struct {
		name     string
		annual   float64
		expected float64
	}{
		{"zero", 0, 0},
		{"below threshold", 20_000, 0},
		{"exactly at threshold", 22_828, 0},
		{"one dollar over", 22_829, 0.12},
		{"well over", 30_000, 860.64}, // (30000 - 22828) * .12
		{"six figures", 100_000, 9_260.64},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.InDelta(t, test.expected, calculateNZStudentLoan(test.annual), 0.005)
		})
	}
}

func TestToAnnual(t *testing.T) {
	tests := []struct {
		name      string
		amount    float64
		frequency string
		expected  float64
	}{
		{"yearly", 80_000, "yearly", 80_000},
		{"monthly", 5_000, "monthly", 60_000},
		{"fortnightly", 2_000, "fortnightly", 52_000},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.InDelta(t, test.expected, toAnnual(test.amount, test.frequency), 0.005)
		})
	}
}

// TestToAnnualDoesNotHandleWeekly pins a known gap rather than endorsing it:
// toAnnual has no "weekly" case, so a weekly figure is returned unscaled. The
// salary form only offers yearly/monthly/fortnightly, so this is unreachable
// today. If a weekly option is ever added, this test should start failing.
func TestToAnnualDoesNotHandleWeekly(t *testing.T) {
	assert.Equal(t, 1_000.0, toAnnual(1_000, "weekly"),
		"weekly is not a recognised salary frequency; see the salary form options")
}

func TestToWeeklyAmount(t *testing.T) {
	tests := []struct {
		name      string
		amount    float64
		frequency string
		expected  float64
	}{
		{"weekly is unchanged", 100, "weekly", 100},
		{"fortnightly halves", 100, "fortnightly", 50},
		{"monthly scales by 12/52", 100, "monthly", 100.0 * 12 / 52},
		{"yearly divides by 52", 5_200, "yearly", 100},
		{"empty frequency treated as weekly", 100, "", 100},
		{"unknown frequency falls back to as-is", 100, "daily", 100},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.InDelta(t, test.expected, toWeeklyAmount(test.amount, test.frequency), 0.0001)
		})
	}
}

func TestActualInFrequency(t *testing.T) {
	tests := []struct {
		name      string
		weekly    float64
		frequency string
		expected  float64
	}{
		{"weekly is unchanged", 100, "weekly", 100},
		{"empty frequency treated as weekly", 100, "", 100},
		{"fortnightly doubles", 100, "fortnightly", 200},
		{"monthly scales by 52/12", 100, "monthly", 100.0 * 52 / 12},
		{"yearly multiplies by 52", 100, "yearly", 5_200},
		{"unknown frequency falls back to weekly", 100, "daily", 100},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.InDelta(t, test.expected, actualInFrequency(test.weekly, test.frequency), 0.0001)
		})
	}
}

// TestFrequencyConversionRoundTrips guards the invariant the UI depends on:
// normalising a target to weekly and back must return the original amount.
// budget.js reimplements this conversion, so drift here means the displayed
// amount and the stored amount disagree.
func TestFrequencyConversionRoundTrips(t *testing.T) {
	for _, frequency := range []string{"weekly", "fortnightly", "monthly", "yearly"} {
		t.Run(frequency, func(t *testing.T) {
			for _, amount := range []float64{1, 42.50, 100, 9_999.99} {
				weekly := toWeeklyAmount(amount, frequency)
				assert.InDelta(t, amount, actualInFrequency(weekly, frequency), 0.0001,
					"%.2f %s did not round trip", amount, frequency)
			}
		})
	}
}

func TestWeeklyTarget(t *testing.T) {
	tests := []struct {
		name     string
		item     models.BudgetItem
		expected float64
	}{
		{
			"no sub-items uses the item's own amount",
			models.BudgetItem{Amount: 100, Frequency: "monthly"},
			100.0 * 12 / 52,
		},
		{
			"empty sub-item slice still uses the item's own amount",
			models.BudgetItem{Amount: 50, Frequency: "weekly", SubItems: []models.BudgetSubItem{}},
			50,
		},
		{
			"sub-items are summed and override the parent amount",
			models.BudgetItem{
				Amount:    999, // deliberately ignored
				Frequency: "weekly",
				SubItems: []models.BudgetSubItem{
					{Amount: 100, Frequency: "monthly"}, // 23.0769
					{Amount: 52, Frequency: "yearly"},   // 1
				},
			},
			100.0*12/52 + 1,
		},
		{
			"sub-items with zero amounts total zero",
			models.BudgetItem{
				Amount:   500,
				SubItems: []models.BudgetSubItem{{Frequency: "yearly"}},
			},
			0,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.InDelta(t, test.expected, weeklyTarget(test.item), 0.0001)
		})
	}
}

func TestComputeNetWeekly(t *testing.T) {
	tests := []struct {
		name     string
		salary   models.BudgetSalary
		expected float64
	}{
		{
			"no salary configured",
			models.BudgetSalary{},
			0,
		},
		{
			"gross only, no deductions",
			models.BudgetSalary{Salary: 52_000, SalaryFrequency: "yearly"},
			1_000,
		},
		{
			"PAYE and ACC only",
			// 100000 - 22877.50 PAYE - 1750 ACC = 75372.50
			models.BudgetSalary{Salary: 100_000, SalaryFrequency: "yearly", IncludePAYE: true},
			75_372.50 / 52,
		},
		{
			"PAYE, ACC and KiwiSaver",
			// 75372.50 - 3000 KiwiSaver = 72372.50
			models.BudgetSalary{
				Salary: 100_000, SalaryFrequency: "yearly",
				IncludePAYE: true, KiwiSaverRate: 3,
			},
			72_372.50 / 52,
		},
		{
			"all deductions including student loan",
			// 72372.50 - 9260.64 student loan = 63111.86
			models.BudgetSalary{
				Salary: 100_000, SalaryFrequency: "yearly",
				IncludePAYE: true, KiwiSaverRate: 3, StudentLoan: true,
			},
			63_111.86 / 52,
		},
		{
			"monthly salary is annualised first",
			models.BudgetSalary{Salary: 5_000, SalaryFrequency: "monthly"},
			60_000.0 / 52,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.InDelta(t, test.expected, computeNetWeekly(test.salary), 0.005)
		})
	}
}

func TestIsoWeekStart(t *testing.T) {
	// 2026-08-17 is a Monday.
	monday := time.Date(2026, 8, 17, 0, 0, 0, 0, time.UTC)

	tests := []struct {
		name     string
		input    time.Time
		expected time.Time
	}{
		{
			"a Monday is its own week start",
			monday,
			monday,
		},
		{
			"midweek snaps back to Monday",
			time.Date(2026, 8, 19, 13, 45, 0, 0, time.UTC), // Wednesday
			monday,
		},
		{
			"Sunday belongs to the week that started on Monday",
			time.Date(2026, 8, 23, 23, 59, 59, 0, time.UTC),
			monday,
		},
		{
			"time of day is discarded",
			time.Date(2026, 8, 17, 23, 59, 59, 0, time.UTC),
			monday,
		},
		{
			"week spanning a year boundary",
			time.Date(2027, 1, 1, 9, 0, 0, 0, time.UTC), // Friday
			time.Date(2026, 12, 28, 0, 0, 0, 0, time.UTC),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := isoWeekStart(test.input)
			assert.True(t, test.expected.Equal(got), "expected %s, got %s", test.expected, got)
			assert.Equal(t, time.Monday, got.Weekday())
			assert.Equal(t, time.UTC, got.Location())
		})
	}
}

func TestIsoWeekStartIsIdempotent(t *testing.T) {
	for day := 1; day <= 28; day++ {
		at := time.Date(2026, 8, day, 15, 0, 0, 0, time.UTC)
		once := isoWeekStart(at)
		assert.True(t, once.Equal(isoWeekStart(once)), "not idempotent for %s", at)
	}
}

// TestIsoWeekStartConvertsToUTC documents a real consequence of the UTC
// normalisation for a NZ-facing app: NZST is UTC+12, so Monday morning in
// Auckland is still Sunday in UTC and therefore lands in the *previous* ISO
// week. See the note in the review; this pins current behaviour so a
// deliberate fix has to update the test.
func TestIsoWeekStartConvertsToUTC(t *testing.T) {
	nz := time.FixedZone("NZST", 12*60*60)

	mondayMorningNZ := time.Date(2026, 8, 17, 9, 0, 0, 0, nz) // Sunday 21:00 UTC
	assert.True(t,
		time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC).Equal(isoWeekStart(mondayMorningNZ)),
		"Monday morning NZ maps to the previous UTC ISO week")

	mondayAfternoonNZ := time.Date(2026, 8, 17, 13, 0, 0, 0, nz) // Monday 01:00 UTC
	assert.True(t,
		time.Date(2026, 8, 17, 0, 0, 0, 0, time.UTC).Equal(isoWeekStart(mondayAfternoonNZ)),
		"Monday afternoon NZ maps to the current UTC ISO week")
}

func TestSanitizeIDs(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"simple word", "Groceries", "sc-groceries"},
		{"space becomes a dash", "Fuel stations", "sc-fuel-stations"},
		{"digits are preserved", "Mitre 10", "sc-mitre-10"},
		{"ampersand and spaces each become a dash", "Food & Drink", "sc-food---drink"},
		{"leading and trailing punctuation", "*Rent*", "sc--rent-"},
		{"empty string yields just the prefix", "", "sc-"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, test.expected, sanitizeID(test.input))
		})
	}

	t.Run("broad prefix differs from subcategory prefix", func(t *testing.T) {
		assert.Equal(t, "bc-transport", sanitizeBroadID("Transport"))
		assert.NotEqual(t, sanitizeID("Transport"), sanitizeBroadID("Transport"))
	})
}

// TestSanitizeIDCollision documents that distinct category names can collapse
// to the same DOM id, which would make htmx swap the wrong row. Akahu's
// category set does not appear to contain such a pair today.
func TestSanitizeIDCollision(t *testing.T) {
	assert.Equal(t, sanitizeID("Food & Drink"), sanitizeID("Food - Drink"),
		"known limitation: every non-alphanumeric maps to the same dash")
}

func TestFrequencyLabel(t *testing.T) {
	tests := []struct {
		frequency string
		expected  string
	}{
		{"weekly", "per Week"},
		{"", "per Week"},
		{"fortnightly", "per Fortnight"},
		{"monthly", "per Month"},
		{"yearly", "per Year"},
		{"daily", "per Week"},
	}

	for _, test := range tests {
		t.Run(test.frequency, func(t *testing.T) {
			assert.Equal(t, test.expected, frequencyLabel(test.frequency))
		})
	}
}

func TestBuildSetupRowData(t *testing.T) {
	const netWeekly = 1_000

	t.Run("spend with no target is flagged as needing one", func(t *testing.T) {
		row := buildSetupRowData(models.BudgetItem{Category: "Groceries"}, 120, netWeekly, false, false)

		assert.False(t, row.HasTarget)
		assert.True(t, row.NeedsTarget)
		assert.False(t, row.IsOver, "cannot be over a target that does not exist")
		assert.Zero(t, row.Percentage)
		assert.Equal(t, "per Week", row.ActualLabel)
		assert.InDelta(t, 120, row.ActualDisplay, 0.0001)
	})

	t.Run("no spend and no target needs nothing", func(t *testing.T) {
		row := buildSetupRowData(models.BudgetItem{Category: "Groceries"}, 0, netWeekly, false, false)

		assert.False(t, row.HasTarget)
		assert.False(t, row.NeedsTarget)
	})

	t.Run("under a weekly target", func(t *testing.T) {
		item := models.BudgetItem{Category: "Groceries", Amount: 200, Frequency: "weekly"}
		row := buildSetupRowData(item, 150, netWeekly, false, false)

		assert.True(t, row.HasTarget)
		assert.False(t, row.NeedsTarget)
		assert.False(t, row.IsOver)
		assert.InDelta(t, 0.75, row.Percentage, 0.0001)
		assert.InDelta(t, 0.20, row.PctOfIncome, 0.0001, "200/1000 of weekly income")
	})

	t.Run("over a weekly target", func(t *testing.T) {
		item := models.BudgetItem{Category: "Groceries", Amount: 100, Frequency: "weekly"}
		row := buildSetupRowData(item, 150, netWeekly, false, false)

		assert.True(t, row.IsOver)
		assert.InDelta(t, 1.5, row.Percentage, 0.0001)
	})

	t.Run("monthly target displays actuals in monthly units", func(t *testing.T) {
		item := models.BudgetItem{Category: "Power", Amount: 260, Frequency: "monthly"}
		row := buildSetupRowData(item, 50, netWeekly, false, false)

		assert.Equal(t, "per Month", row.ActualLabel)
		assert.InDelta(t, 50.0*52/12, row.ActualDisplay, 0.0001)
		assert.InDelta(t, 260, row.TargetDisplay, 0.0001, "target displays in its own frequency")
		assert.InDelta(t, 260.0*12/52, row.TargetWeekly, 0.0001)
	})

	t.Run("sub-items override the parent amount and force weekly display", func(t *testing.T) {
		item := models.BudgetItem{
			Category:  "Vehicle",
			Amount:    999, // deliberately ignored
			Frequency: "monthly",
			SubItems: []models.BudgetSubItem{
				{Amount: 520, Frequency: "yearly"}, // 10/wk
				{Amount: 10, Frequency: "weekly"},  // 10/wk
			},
		}
		row := buildSetupRowData(item, 15, netWeekly, false, false)

		assert.True(t, row.HasSubItems)
		assert.InDelta(t, 20, row.DerivedWeekly, 0.0001)
		assert.InDelta(t, 20, row.TargetWeekly, 0.0001)
		assert.InDelta(t, 20, row.TargetDisplay, 0.0001)
		assert.Equal(t, "per Week", row.ActualLabel, "sub-item rows always display weekly")
		assert.InDelta(t, 0.75, row.Percentage, 0.0001)
	})

	t.Run("zero income leaves share of income at zero", func(t *testing.T) {
		item := models.BudgetItem{Category: "Groceries", Amount: 200, Frequency: "weekly"}
		row := buildSetupRowData(item, 150, 0, false, false)

		assert.Zero(t, row.PctOfIncome, "must not divide by zero income")
	})

	t.Run("trend flags are carried through", func(t *testing.T) {
		item := models.BudgetItem{Category: "Groceries", Amount: 100, Frequency: "weekly"}

		up := buildSetupRowData(item, 100, netWeekly, true, false)
		assert.True(t, up.TrendUp)
		assert.False(t, up.TrendDown)

		down := buildSetupRowData(item, 100, netWeekly, false, true)
		assert.False(t, down.TrendUp)
		assert.True(t, down.TrendDown)
	})

	t.Run("ids are derived from the category names", func(t *testing.T) {
		item := models.BudgetItem{Category: "Fuel stations", BroadCategory: "Transport"}
		row := buildSetupRowData(item, 0, netWeekly, false, false)

		assert.Equal(t, "sc-fuel-stations", row.RowID)
		assert.Equal(t, "bc-transport", row.BroadRowID)
	})
}

// TestBudgetTemplatesRender parses the real template set and executes every
// budget partial. It catches field references that no longer exist on the
// view-models, which the compiler cannot see.
func TestBudgetTemplatesRender(t *testing.T) {
	setup := CategorySetupData{
		HasData:   true,
		NetWeekly: 1_000,
		Groups: []BroadCategoryGroup{{
			Name:              "Transport",
			RowID:             "bc-transport",
			TotalActualWeekly: 120,
			TotalTargetWeekly: 100,
			HasTarget:         true,
			IsOver:            true,
			Percentage:        1.2,
			PctOfIncome:       0.1,
			Rows: []CategoryTargetRow{
				buildSetupRowData(models.BudgetItem{
					ID: "item-1", Category: "Fuel stations", BroadCategory: "Transport",
					Amount: 100, Frequency: "weekly",
				}, 120, 1_000, true, false),
				buildSetupRowData(models.BudgetItem{
					ID: "item-2", Category: "Vehicle", BroadCategory: "Transport",
					SubItems: []models.BudgetSubItem{{ID: "sub-1", Name: "Rego", Amount: 520, Frequency: "yearly"}},
				}, 5, 1_000, false, true),
			},
		}},
		TotalTargetWeekly:    100,
		SavingsGoal:          200,
		SavingsGoalFrequency: "weekly",
		SavingsGoalWeekly:    200,
		Remaining:            900,
		MeetsSavingsGoal:     true,
		HasSavingsGoal:       true,
	}

	tests := []struct {
		name     string
		template string
		data     any
	}{
		{
			"full page",
			"budget",
			map[string]any{
				"Salary":    models.BudgetSalary{Salary: 100_000, SalaryFrequency: "yearly", IncludePAYE: true, KiwiSaverRate: 3},
				"SetupData": setup,
			},
		},
		{
			"page with no transactions still renders the savings goal",
			"budget",
			map[string]any{
				"Salary":    models.BudgetSalary{},
				"SetupData": CategorySetupData{HasData: false},
			},
		},
		{"income breakdown", "budget.summary", BudgetSummary{GrossAnnual: 100_000, NetAnnual: 72_372.50}},
		{"cards in surplus", "budget.cards", BudgetCardsData{NetWeekly: 1_000, TotalWeeklyExpenses: 400, WeeklySavings: 600, PctAllocatedCss: 40, HasSalary: true}},
		{"cards in deficit", "budget.cards", BudgetCardsData{NetWeekly: 1_000, TotalWeeklyExpenses: 1_200, WeeklySavings: -200, HasDeficit: true, PctAllocatedCss: 100, IsOverAllocated: true, HasSalary: true}},
		{"cards without a salary", "budget.cards", BudgetCardsData{}},
		{"performance with data", "budget.performance", BudgetPerformanceData{Labels: `["1 Jan"]`, Actuals: `[12.5]`, Target: 100, HasData: true}},
		{"performance without data", "budget.performance", BudgetPerformanceData{}},
		{"setup with data", "budget.setup", setup},
		{"setup without data", "budget.setup", CategorySetupData{HasData: false}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.NotEmpty(t, renderTemplate(t, test.template, test.data))
		})
	}
}

func TestTopMerchants(t *testing.T) {
	t.Run("orders by spend, highest first", func(t *testing.T) {
		got := topMerchants(map[string]float64{
			"Countdown":   120,
			"New World":   340,
			"Four Square": 15,
		})
		assert.Equal(t, []string{"New World", "Countdown", "Four Square"}, got)
	})

	t.Run("ties break alphabetically so rendering is deterministic", func(t *testing.T) {
		got := topMerchants(map[string]float64{"Zeta": 50, "Alpha": 50, "Mid": 50})
		assert.Equal(t, []string{"Alpha", "Mid", "Zeta"}, got)
	})

	t.Run("no merchants yields nil", func(t *testing.T) {
		assert.Nil(t, topMerchants(nil))
		assert.Nil(t, topMerchants(map[string]float64{}))
	})
}

func TestSetMerchants(t *testing.T) {
	tests := []struct {
		name          string
		names         []string
		expectedShown []string
		expectedExtra int
	}{
		{"none", nil, nil, 0},
		{"fewer than the inline limit", []string{"a", "b"}, []string{"a", "b"}, 0},
		{"exactly the inline limit", []string{"a", "b", "c"}, []string{"a", "b", "c"}, 0},
		{"one over the limit", []string{"a", "b", "c", "d"}, []string{"a", "b", "c"}, 1},
		{"well over the limit", []string{"a", "b", "c", "d", "e", "f"}, []string{"a", "b", "c"}, 3},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var row CategoryTargetRow
			row.setMerchants(test.names)

			assert.Equal(t, test.expectedShown, row.MerchantsShown)
			assert.Equal(t, test.expectedExtra, row.MerchantsExtra)
			assert.Equal(t, test.names, row.Merchants, "the popover always gets the full list")
			assert.Len(t, row.MerchantsShown, len(test.names)-test.expectedExtra)
		})
	}
}

// TestMerchantColumnRenders covers the informational tag column: a truncated
// row must offer the "+N more" trigger and carry every merchant in the hidden
// popover source, while a short row must not render a trigger at all.
func TestMerchantColumnRenders(t *testing.T) {
	t.Run("truncated row offers a popover with the full list", func(t *testing.T) {
		row := buildSetupRowData(models.BudgetItem{
			ID: "item-1", Category: "Groceries", BroadCategory: "Food",
			Amount: 100, Frequency: "weekly",
		}, 80, 1_000, false, false)
		row.setMerchants([]string{"New World", "Countdown", "Pak n Save", "Four Square", "Z/Caltex"})

		html := renderTemplate(t, "budget.setup.row", row)

		assert.Contains(t, html, "+2 more")
		assert.Contains(t, html, "budge-merchants-more")
		assert.Contains(t, html, "data-merchant-list")
		for _, merchant := range []string{"New World", "Countdown", "Pak n Save", "Four Square"} {
			assert.Contains(t, html, merchant, "popover source must list every merchant")
		}
		assert.Contains(t, html, "Z/Caltex", "a slash is just text now, not a URL segment")
	})

	t.Run("short row renders tags with no popover trigger", func(t *testing.T) {
		row := buildSetupRowData(models.BudgetItem{
			ID: "item-2", Category: "Power", BroadCategory: "Utilities",
		}, 40, 1_000, false, false)
		row.setMerchants([]string{"Meridian", "Contact"})

		html := renderTemplate(t, "budget.setup.row", row)

		assert.Contains(t, html, "Meridian")
		assert.Contains(t, html, "Contact")
		assert.NotContains(t, html, "budge-merchants-more")
		assert.NotContains(t, html, "data-merchant-list")
	})

	t.Run("row with no merchants renders a placeholder", func(t *testing.T) {
		row := buildSetupRowData(models.BudgetItem{ID: "item-3", Category: "Misc"}, 0, 1_000, false, false)

		html := renderTemplate(t, "budget.setup.row", row)

		assert.NotContains(t, html, "budge-merchants-more")
		assert.Contains(t, html, "—")
	})
}

// TestControlCellsStaySingleLine guards the row alignment. A caption rendered
// underneath the target input makes that cell taller than the "Per" cell next
// to it, so with vertical-align:middle the input and the select stop lining
// up — and because the caption is conditional, the misalignment varies row to
// row. Read-only metrics therefore belong in the subcategory cell, which holds
// no form controls.
func TestControlCellsStaySingleLine(t *testing.T) {
	row := buildSetupRowData(models.BudgetItem{
		ID: "item-1", Category: "Fuel stations", BroadCategory: "Transport",
		Amount: 100, Frequency: "weekly",
	}, 80, 1_000, false, false)

	html := renderTemplate(t, "budget.setup.row", row)

	require.Positive(t, row.PctOfIncome, "fixture must actually produce a share of income")
	assert.Contains(t, html, "of income", "the metric is still shown somewhere")

	cells := tableCells(t, html)
	require.NotEmpty(t, cells)

	subcategory := cells[0]
	assert.Contains(t, subcategory, "Fuel stations")
	assert.Contains(t, subcategory, "of income",
		"the metric belongs in the cell that holds no controls")

	var target string
	for _, cell := range cells {
		if strings.Contains(cell, `name="target_amount"`) {
			target = cell
		}
	}
	require.NotEmpty(t, target, "fixture should render a target input")
	assert.NotContains(t, target, "of income",
		"a caption under the target input breaks alignment with the Per column")
}

// tableCells splits rendered markup into the contents of each <td>. Note that
// the row's hidden inputs sit outside any cell, so a plain string search would
// match those first.
func tableCells(t *testing.T, html string) []string {
	t.Helper()

	var cells []string
	for rest := html; ; {
		start := strings.Index(rest, "<td")
		if start < 0 {
			return cells
		}
		rest = rest[start:]

		end := strings.Index(rest, "</td>")
		require.GreaterOrEqual(t, end, 0, "unclosed table cell")

		cells = append(cells, rest[:end])
		rest = rest[end+len("</td>"):]
	}
}
