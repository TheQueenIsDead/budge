package application

import (
	"net/url"
	"regexp"
	"testing"
	"time"

	"github.com/TheQueenIsDead/budge/pkg/database/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func sectionKeys(sections []TargetSection) []string {
	var keys []string
	for _, s := range sections {
		keys = append(keys, s.Key)
	}
	return keys
}

func rowNames(section TargetSection) []string {
	var names []string
	for _, r := range section.Rows {
		names = append(names, r.SubCategory)
	}
	return names
}

func TestBuildTargetSections(t *testing.T) {
	row := func(item models.BudgetItem, actual float64) CategoryTargetRow {
		r := buildSetupRowData(item, actual, 1_000)
		if r.BroadCategory == "" {
			r.BroadCategory = "Food"
		}
		return r
	}
	rows := []CategoryTargetRow{
		row(models.BudgetItem{Category: "Supermarkets", Amount: 150, Frequency: "weekly"}, 160), // over by 10
		row(models.BudgetItem{Category: "Takeaways", Amount: 20, Frequency: "weekly"}, 80),      // over by 60
		row(models.BudgetItem{Category: "Loan"}, 390),                                           // no target
		row(models.BudgetItem{Category: "Payments"}, 250),                                       // no target
		row(models.BudgetItem{Category: "Fuel", BroadCategory: "Transport", Amount: 60, Frequency: "weekly"}, 50),
		row(models.BudgetItem{Category: "Bakeries", Amount: 5, Frequency: "weekly"}, 0),
		row(models.BudgetItem{Category: "Fees"}, 0),
		row(models.BudgetItem{Category: "Card debits"}, 4.99),
		row(models.BudgetItem{Category: "Standing orders", Kind: models.BudgetKindSavings}, 100),
	}

	sections := buildTargetSections(rows, "")

	require.Equal(t, []string{sectionAttention, sectionOnTrack, sectionSmall, sectionNotCounted}, sectionKeys(sections))
	assert.Equal(t, []string{"Takeaways", "Supermarkets", "Loan", "Payments"}, rowNames(sections[0]),
		"worst overspend first, then the largest untargeted spend")
	assert.Equal(t, []string{"Bakeries", "Fuel"}, rowNames(sections[1]), "on track sorts by group, then name")
	assert.Equal(t, []string{"Card debits", "Fees"}, rowNames(sections[2]), "untargeted spend under $5 a week is folded away")
	assert.Equal(t, []string{"Standing orders"}, rowNames(sections[3]))

	assert.InDelta(t, 50, sections[1].Actual, 0.001)
	assert.InDelta(t, 65, sections[1].Target, 0.001)

	assert.True(t, sections[0].Open, "needs attention is always open")
	for _, s := range sections[1:] {
		assert.False(t, s.Open, "%s stays folded until something in it is being edited", s.Key)
	}

	t.Run("the section holding the row being edited opens with it", func(t *testing.T) {
		sections := buildTargetSections(rows, sanitizeID("Fuel"))
		assert.True(t, sections[1].Open)
		assert.False(t, sections[2].Open)
		for _, r := range sections[1].Rows {
			assert.Equal(t, r.SubCategory == "Fuel", r.Open)
		}
	})

	t.Run("empty sections are left out", func(t *testing.T) {
		only := buildTargetSections(rows[4:5], "")
		assert.Equal(t, []string{sectionOnTrack}, sectionKeys(only))
	})
}

func TestSuggestedTargets(t *testing.T) {
	app := newTestApp(t)
	weekStart := isoWeekStart(time.Now())
	// $100 in each of the last 13 full weeks, then a $1 straggler.
	for i := 1; i <= 13; i++ {
		date := weekStart.AddDate(0, 0, -7*i+2)
		require.NoError(t, app.store.CreateTransaction(spendTx("loan"+string(rune('a'+i)), "LOAN", "", "", "", "LOAN INTEREST", -100, date)))
		require.NoError(t, app.store.CreateTransaction(spendTx("food"+string(rune('a'+i)), "EFTPOS", "Food", "Supermarkets", "", "", -100, date)))
	}
	require.NoError(t, app.store.CreateTransaction(spendTx("straggler", "LOAN", "", "", "", "LOAN INTEREST", -1, weekStart.AddDate(0, 0, -3))))
	require.NoError(t, app.store.CreateBudgetItem(models.BudgetItem{ID: "food", Category: "Supermarkets", BroadCategory: "Food", Amount: 120, Frequency: "weekly"}))

	setup, err := app.buildSetupData()
	require.NoError(t, err)

	byName := map[string]CategoryTargetRow{}
	for _, row := range rowsIn(t, setup, uncategorisedGroup) {
		byName[row.SubCategory] = row
	}
	loan := byName["Loan"]
	assert.Equal(t, untargetedWindowWeeks, loan.WindowWeeks, "a category with no target is measured over 13 weeks")
	assert.InDelta(t, 1301.0/13, loan.ActualWeekly, 0.001)
	assert.InDelta(t, 434, loan.SuggestedMonthly, 0.001, "$100.08 a week is $433.67 a month, rounded up")
	assert.Zero(t, rowsIn(t, setup, "Food")[0].SuggestedMonthly, "a category with a target is not offered one")

	t.Run("accepting the suggestion lands the row on track", func(t *testing.T) {
		c, _ := postForm(t, app, url.Values{
			"sub_category": {"Loan"}, "broad_category": {uncategorisedGroup},
			"target_amount": {"434"}, "target_frequency": {"monthly"},
		})
		require.NoError(t, app.BudgetSaveTarget(c))

		setup, err := app.buildSetupData()
		require.NoError(t, err)
		for _, section := range setup.Sections {
			for _, row := range section.Rows {
				if row.SubCategory == "Loan" {
					assert.Equal(t, sectionOnTrack, section.Key)
				}
			}
		}
	})
}

// openAttr finds whether the element with the given id renders open.
func openAttr(html, id string) bool {
	return regexp.MustCompile(`id="` + id + `">\s*<details open`).MatchString(html)
}

func TestTargetEditsKeepTheRowOpen(t *testing.T) {
	app := newTestApp(t)
	recent := time.Now().Add(-48 * time.Hour)
	require.NoError(t, app.store.CreateTransaction(spendTx("interest", "LOAN", "", "", "", "LOAN INTEREST", -400, recent)))
	require.NoError(t, app.store.CreateTransaction(spendTx("fuel", "EFTPOS", "Transport", "Fuel", "", "", -40, recent)))
	require.NoError(t, app.store.CreateBudgetItem(models.BudgetItem{ID: "fuel", Category: "Fuel", BroadCategory: "Transport", Amount: 60, Frequency: "weekly"}))

	c, rec := postForm(t, app, url.Values{
		"sub_category": {"Loan"}, "broad_category": {uncategorisedGroup},
		"target_amount": {"400"}, "target_frequency": {"weekly"},
	})
	c.Request().Header.Set("HX-Request", "true")
	require.NoError(t, app.BudgetSaveTarget(c))
	html := rec.Body.String()

	assert.True(t, openAttr(html, "sc-loan"), "the edited row stays open")
	assert.False(t, openAttr(html, "sc-fuel"), "other rows stay closed")
	assert.Regexp(t, `data-section="on-track" open`, html, "the row moved to on track, so that section opens to show it")
	assert.NotContains(t, html, `data-section="attention"`, "nothing is left needing attention")
}

func TestQuickActionsOnlyOfferedForUntargetedSpend(t *testing.T) {
	untargeted := buildSetupRowData(models.BudgetItem{Category: "Loan", BroadCategory: "Uncategorised"}, 390, 1_000)
	untargeted.SuggestedMonthly = 1_700
	html := renderTemplate(t, "budget.setup.row", untargeted)
	assert.Contains(t, html, "Use $1,700 a month")
	assert.Contains(t, html, `name="target_amount" value="1700"`)
	assert.Contains(t, html, `name="target_frequency" value="monthly"`)
	assert.Contains(t, html, `value="ignore" class="btn btn-outline-secondary btn-sm">Ignore`)

	targeted := buildSetupRowData(models.BudgetItem{Category: "Fuel", Amount: 60, Frequency: "weekly"}, 50, 1_000)
	html = renderTemplate(t, "budget.setup.row", targeted)
	assert.NotContains(t, html, "b-target-quick")
	assert.Contains(t, html, "$50</span> of $60 a week", "the read-only line is one figure against its target")
}

// TestTargetsWithoutSpendStillHaveARow is the regression test for a target
// that counted toward the total but had no row, because nothing had been
// spent in its category, so it could be neither seen nor changed.
func TestTargetsWithoutSpendStillHaveARow(t *testing.T) {
	app := newTestApp(t)
	require.NoError(t, app.store.CreateTransaction(spendTx("fuel", "EFTPOS", "Transport", "Fuel", "", "", -40, time.Now().Add(-48*time.Hour))))
	require.NoError(t, app.store.CreateBudgetItem(models.BudgetItem{ID: "fuel", Category: "Fuel", BroadCategory: "Transport", Amount: 60, Frequency: "weekly"}))
	require.NoError(t, app.store.CreateBudgetItem(models.BudgetItem{ID: "taxi", Category: "Taxis", BroadCategory: "Transport", Amount: 312, Frequency: "yearly"}))

	setup, err := app.buildSetupData()
	require.NoError(t, err)
	cards, err := app.buildBudgetCards()
	require.NoError(t, err)

	rows := rowsIn(t, setup, "Transport")
	require.Len(t, rows, 2)
	byName := map[string]CategoryTargetRow{}
	for _, row := range rows {
		byName[row.SubCategory] = row
	}
	taxi := byName["Taxis"]
	assert.True(t, taxi.HasTarget)
	assert.Zero(t, taxi.ActualWeekly)
	assert.InDelta(t, cards.TotalWeeklyExpenses, setup.TotalTargetWeekly, 0.001, "the page and the allocation bar total the same targets")

	for _, section := range setup.Sections {
		for _, row := range section.Rows {
			if row.SubCategory == "Taxis" {
				assert.Equal(t, sectionOnTrack, section.Key, "nothing spent against a target is on track")
			}
		}
	}
}
