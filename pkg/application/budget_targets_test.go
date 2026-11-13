package application

import (
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/TheQueenIsDead/budge/pkg/database/models"
	"github.com/labstack/echo/v5"
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
		names = append(names, r.Key)
	}
	return names
}

// targetRow is a budget row for a category with the given target and weekly
// spending.
func targetRow(key string, amount float64, frequency string, actual float64) CategoryTargetRow {
	category, _ := models.BudgetCategoryByKey(key)
	item := models.BudgetItem{BudgetCategory: key, Amount: amount, Frequency: frequency}
	return buildSetupRowData(category, item, actual, 1_000)
}

func TestBuildTargetSections(t *testing.T) {
	rows := []CategoryTargetRow{
		targetRow("groceries", 150, "weekly", 160), // over by 10
		targetRow("eating-out", 20, "weekly", 80),  // over by 60
		targetRow("maintenance", 0, "", 390),       // no target
		targetRow("shopping", 0, "", 250),          // no target
		targetRow("transport", 60, "weekly", 50),   // on track
		targetRow("pets", 5, "weekly", 0),          // on track
		targetRow("health", 0, "", 0),              // small
		targetRow("entertainment", 0, "", 4.99),    // small
		targetRow("savings", 0, "", 100),           // not budgeted
	}

	sections := buildTargetSections(rows, "")

	require.Equal(t, []string{"on-budget", "off-budget"}, sectionKeys(sections))
	assert.Equal(t, []string{"groceries", "transport", "eating-out", "pets", "health", "maintenance", "shopping", "entertainment"}, rowNames(sections[0]),
		"in the order the categories are listed, over target or not")
	assert.Equal(t, []string{"savings"}, rowNames(sections[1]))

	assert.InDelta(t, 934.99, sections[0].Actual, 0.001)
	assert.InDelta(t, 235, sections[0].Target, 0.001)

	assert.True(t, sections[0].Open, "on budget is open")
	assert.False(t, sections[1].Open, "off budget is folded")

	t.Run("off budget opens when a row in it is being edited", func(t *testing.T) {
		sections := buildTargetSections(rows, sanitizeID("savings"))
		assert.True(t, sections[1].Open)
		assert.True(t, sections[1].Rows[0].Open)
	})

	t.Run("with nothing off budget, there is one list", func(t *testing.T) {
		assert.Equal(t, []string{"on-budget"}, sectionKeys(buildTargetSections(rows[:3], "")))
	})
}

func TestSuggestedTargets(t *testing.T) {
	app := newTestApp(t)
	weekStart := isoWeekStart(time.Now())
	// $100 in each of the last 13 full weeks, then a $1 straggler.
	for i := 1; i <= 13; i++ {
		date := weekStart.AddDate(0, 0, -7*i+2)
		require.NoError(t, app.store.CreateTransaction(t.Context(), spendTx("diy"+string(rune('a'+i)), "EFTPOS", "Household", "Building supplies", "Bunnings", "", -100, date)))
		require.NoError(t, app.store.CreateTransaction(t.Context(), spendTx("food"+string(rune('a'+i)), "EFTPOS", "Food", "Supermarkets and grocery stores", "", "", -100, date)))
	}
	require.NoError(t, app.store.CreateTransaction(t.Context(), spendTx("straggler", "EFTPOS", "Household", "Building supplies", "Bunnings", "", -1, weekStart.AddDate(0, 0, -3))))
	require.NoError(t, app.store.CreateBudgetItem(t.Context(), models.BudgetItem{ID: "food", BudgetCategory: "groceries", Amount: 120, Frequency: "weekly"}))

	setup, err := app.buildSetupData(t.Context())
	require.NoError(t, err)

	maintenance := rowByKey(t, setup, "maintenance")
	assert.Equal(t, untargetedWindowWeeks, maintenance.WindowWeeks, "a category with no target is measured over 13 weeks")
	assert.InDelta(t, 1301.0/13, maintenance.ActualWeekly, 0.001)
	assert.Equal(t, "weekly", maintenance.SuggestedFrequency, "offered in the frequency its target picker shows")
	assert.InDelta(t, 101, maintenance.Suggested, 0.001,
		"the last 4 weeks, $100.25 a week with the straggler, beat the 13 week average and round up")
	assert.Zero(t, rowByKey(t, setup, "groceries").Suggested, "a category with a target is not offered one")
	assert.Contains(t, renderTemplate(t, "budget.setup.row", maintenance), "Use $101 a week")

	t.Run("a lumpy category is offered a yearly target", func(t *testing.T) {
		require.NoError(t, app.store.CreateTransaction(t.Context(), spendTx("bike", "PAYMENT", "Transport", "Car, truck, and motorcycle dealers", "Casbolts", "", -5200, weekStart.AddDate(0, 0, -10))))
		setup, err := app.buildSetupData(t.Context())
		require.NoError(t, err)
		big := rowByKey(t, setup, "big-purchases")
		assert.Equal(t, 52, big.WindowWeeks, "measured over the year it is judged on")
		assert.Equal(t, "yearly", big.SuggestedFrequency)
		assert.InDelta(t, 5200, big.Suggested, 0.001, "a year of big purchases, not a busy month's worth every week")
		html := renderTemplate(t, "budget.setup.row", big)
		assert.Contains(t, html, "Use $5,200 a year")
		assert.Regexp(t, `<option value="yearly"\s+selected>Year</option>`, html, "the picker shows the same year")
	})

	t.Run("accepting the suggestion lands the row on track", func(t *testing.T) {
		c, _ := postForm(t, app, url.Values{
			"category": {"maintenance"}, "target_amount": {"101"}, "target_frequency": {"weekly"},
		})
		require.NoError(t, app.BudgetSaveTarget(c))

		setup, err := app.buildSetupData(t.Context())
		require.NoError(t, err)
		for _, section := range setup.Sections {
			for _, row := range section.Rows {
				if row.Key == "maintenance" {
					assert.False(t, row.IsOver, "on track")
				}
			}
		}
	})
}

// TestWeeklyAveragesCountWholeWeeks keeps this week out of the averages: it
// is only partly spent, so adding it to whole weeks and dividing by their
// number overstated every category.
func TestWeeklyAveragesCountWholeWeeks(t *testing.T) {
	app := newTestApp(t)
	weekStart := isoWeekStart(time.Now())
	// $100 in each of the last 4 whole weeks, and $400 already this week,
	// at its very start so it is never in the future.
	for i := 1; i <= 4; i++ {
		require.NoError(t, app.store.CreateTransaction(t.Context(), spendTx("food"+string(rune('a'+i)), "EFTPOS", "Food", "Supermarkets and grocery stores", "", "", -100, weekStart.AddDate(0, 0, -7*i))))
	}
	require.NoError(t, app.store.CreateTransaction(t.Context(), spendTx("now", "EFTPOS", "Food", "Supermarkets and grocery stores", "", "", -400, weekStart)))
	require.NoError(t, app.store.CreateBudgetItem(t.Context(), models.BudgetItem{ID: "food", BudgetCategory: "groceries", Amount: 100, Frequency: "weekly"}))

	setup, err := app.buildSetupData(t.Context())
	require.NoError(t, err)

	groceries := rowByKey(t, setup, "groceries")
	assert.Equal(t, 4, groceries.WindowWeeks)
	assert.InDelta(t, 100, groceries.ActualWeekly, 0.001, "four whole weeks of $100, and nothing from this one")
	assert.False(t, groceries.IsOver, "a big start to the week does not put it over")
	assert.Empty(t, setup.Tracking.Over, "nor raise a notification")
}

func TestBudgetSaveTarget(t *testing.T) {
	app := newTestApp(t)

	save := func(values url.Values) int {
		t.Helper()
		c, rec := postForm(t, app, values)
		require.NoError(t, app.BudgetSaveTarget(c))
		return rec.Code
	}

	t.Run("saves against the category, and again in place", func(t *testing.T) {
		save(url.Values{"category": {"groceries"}, "target_amount": {"150"}, "target_frequency": {"weekly"}})
		save(url.Values{"category": {"groceries"}, "target_amount": {"160"}, "target_frequency": {"weekly"}})

		items, err := app.store.ReadBudgetItems(t.Context())
		require.NoError(t, err)
		require.Len(t, items, 1)
		assert.Equal(t, "groceries", items[0].BudgetCategory)
		assert.Equal(t, 160.0, items[0].Amount)
	})

	t.Run("a subcategory takes one too, for splitting its category", func(t *testing.T) {
		assert.Equal(t, 200, save(url.Values{"category": {"fuel"}, "target_amount": {"60"}, "target_frequency": {"weekly"}}))
	})

	t.Run("only categories that take a target", func(t *testing.T) {
		for _, key := range []string{"savings", "uncategorised", "nonsense"} {
			assert.Equal(t, 400, save(url.Values{"category": {key}, "target_amount": {"10"}}), key)
		}
	})
}

// TestTargetsFromBeforeBudgeCategoriesAreLeftOut keeps the targets set
// against Akahu's categories, which the budget started afresh from, from
// counting. Two of them share a name with a budge category.
func TestTargetsFromBeforeBudgeCategoriesAreLeftOut(t *testing.T) {
	app := newTestApp(t)
	// Targets saved against Akahu's categories name no budge category.
	for _, item := range []models.BudgetItem{
		{ID: "old-insurance", Name: "Insurance", Amount: 28, Frequency: "weekly"},
		{ID: "old-savings", Name: "Savings", Amount: 100, Frequency: "weekly"},
		{ID: "old-fuel", Name: "Fuel stations", Amount: 60, Frequency: "weekly"},
	} {
		require.NoError(t, app.store.CreateBudgetItem(t.Context(), item))
	}

	setup, err := app.buildSetupData(t.Context())
	require.NoError(t, err)
	cards, err := app.buildBudgetCards(t.Context())
	require.NoError(t, err)

	assert.Zero(t, setup.TotalTargetWeekly)
	assert.Zero(t, cards.TotalWeeklyExpenses)
	assert.False(t, rowByKey(t, setup, "insurance").HasTarget)

	items, err := app.store.ReadBudgetItems(t.Context())
	require.NoError(t, err)
	assert.Len(t, items, 3, "they are kept, only not read")
}

// openAttr finds whether the element with the given id renders open.
func openAttr(html, id string) bool {
	return regexp.MustCompile(`id="` + id + `">\s*<details open`).MatchString(html)
}

// TestTargetEditsUpdateTheRowInPlace is the regression test for the page
// jumping about while editing: every edit re-rendered the whole list, which
// moved rows between sections, reordered them and closed sections opened by
// hand. An edit now answers with just its row, which stays where it is drawn.
func TestTargetEditsUpdateTheRowInPlace(t *testing.T) {
	app := newTestApp(t)
	recent := lastWeek()
	require.NoError(t, app.store.CreateTransaction(t.Context(), spendTx("diy", "EFTPOS", "Household", "Building supplies", "Bunnings", "", -390, recent)))
	require.NoError(t, app.store.CreateTransaction(t.Context(), spendTx("fuel", "EFTPOS", "Transport", "Fuel stations", "", "", -40, recent)))
	require.NoError(t, app.store.CreateBudgetItem(t.Context(), models.BudgetItem{ID: "car", BudgetCategory: "transport", Amount: 60, Frequency: "weekly"}))

	c, rec := postForm(t, app, url.Values{
		"category": {"maintenance"}, "target_amount": {"50"}, "target_frequency": {"weekly"},
	})
	c.Request().Header.Set("HX-Request", "true")
	require.NoError(t, app.BudgetSaveTarget(c))
	html := rec.Body.String()

	assert.True(t, openAttr(html, "sc-maintenance"), "the edited row stays open")
	assert.NotContains(t, html, `id="sc-transport"`, "no other row is redrawn")
	assert.NotContains(t, html, "<details class=\"b-card", "no section is redrawn, so none opens or closes")
	assert.Regexp(t, `id="tsec-sum-on-budget" hx-swap-oob="true">\s*\$108 a week against \$110 of targets`, html,
		"the on budget totals update alongside")
}

func TestQuickActionsOnlyOfferedForUntargetedSpend(t *testing.T) {
	untargeted := targetRow("maintenance", 0, "", 390)
	suggestTarget(&untargeted, 410)
	html := renderTemplate(t, "budget.setup.row", untargeted)
	assert.Contains(t, html, "Use $410 a week", "in the week its target picker shows, judged on the busier recent weeks")
	assert.Contains(t, html, `name="target_amount" value="410"`)
	assert.Contains(t, html, `name="target_frequency" value="weekly"`)
	assert.Contains(t, html, `name="category" value="maintenance"`)
	editor := html[indexOf(t, html, `class="b-target-editor"`):indexOf(t, html, "</details>")]
	assert.Contains(t, editor, "Use $410 a week", "the suggestion only shows once the category is opened")
	assert.Equal(t, 1, strings.Count(html, "Use $410 a week"))
	assert.NotContains(t, html, "Ignore", "spending is spending: there is nothing to mark it as instead")

	targeted := targetRow("transport", 60, "weekly", 50)
	html = renderTemplate(t, "budget.setup.row", targeted)
	assert.NotContains(t, html, "b-target-quick")
	assert.Contains(t, html, "$50</span> of $60 a week", "the read-only line is one figure against its target")
	assert.Contains(t, html, "Transport")
	assert.Contains(t, html, "bi-car-front")
}

// TestTargetsWithoutSpendStillHaveARow is the regression test for a target
// that counted toward the total but had no row, because nothing had been
// spent in its category, so it could be neither seen nor changed.
func TestTargetsWithoutSpendStillHaveARow(t *testing.T) {
	app := newTestApp(t)
	require.NoError(t, app.store.CreateTransaction(t.Context(), spendTx("fuel", "EFTPOS", "Transport", "Fuel stations", "", "", -40, time.Now().Add(-48*time.Hour))))
	require.NoError(t, app.store.CreateBudgetItem(t.Context(), models.BudgetItem{ID: "car", BudgetCategory: "transport", Amount: 60, Frequency: "weekly"}))
	require.NoError(t, app.store.CreateBudgetItem(t.Context(), models.BudgetItem{ID: "trips", BudgetCategory: "entertainment", Amount: 312, Frequency: "yearly"}))

	setup, err := app.buildSetupData(t.Context())
	require.NoError(t, err)
	cards, err := app.buildBudgetCards(t.Context())
	require.NoError(t, err)

	trips := rowByKey(t, setup, "entertainment")
	assert.True(t, trips.HasTarget)
	assert.Zero(t, trips.ActualWeekly)
	assert.InDelta(t, cards.TotalWeeklyExpenses, setup.TotalTargetWeekly, 0.001, "the page and the allocation bar total the same targets")

	for _, section := range setup.Sections {
		for _, row := range section.Rows {
			if row.Key == "entertainment" {
				assert.False(t, row.IsOver, "nothing spent against a target is on track")
			}
		}
	}
}

func TestGenerousTargets(t *testing.T) {
	row := func(key string, amount float64, frequency string, actual, recent float64) CategoryTargetRow {
		r := targetRow(key, amount, frequency, actual)
		markGenerous(&r, recent)
		return r
	}

	pets := row("pets", 150, "weekly", 10, 12)
	assert.True(t, pets.Generous, "spending $12 against $150 is well under")
	assert.InDelta(t, 14, pets.LoweredTarget, 0.001, "lowered to the higher of its own window and the last 13 weeks, $12, plus 10% headroom, rounded up")

	quietMonth := row("pets", 150, "weekly", 10, 99)
	assert.False(t, quietMonth.Generous, "a quiet month is not enough when the last 13 weeks ran near the target")

	groceries := row("groceries", 200, "weekly", 157, 150)
	assert.False(t, groceries.Generous, "spending over half the target is close enough")

	trips := row("entertainment", 300, "yearly", 0, 0)
	assert.False(t, trips.Generous, "an untouched small budget is under $20 a week below, so it is not flagged")

	shopping := row("shopping", 105, "weekly", 0, 0)
	assert.True(t, shopping.Generous)
	assert.Zero(t, shopping.LoweredTarget, "nothing spent offers to remove the target")
	assert.Contains(t, renderTemplate(t, "budget.setup.row", shopping), "Remove target, nothing spent")

	bills := row("bills", 400, "monthly", 20, 25)
	assert.True(t, bills.Generous)
	assert.InDelta(t, 120, bills.LoweredTarget, 0.001, "$25 a week with 10% headroom is $119.17 a month, rounded up, in the target's own frequency")
	assert.Contains(t, renderTemplate(t, "budget.setup.row", bills), "Lower to $120 a month")

	over := row("eating-out", 20, "weekly", 80, 80)
	assert.False(t, over.Generous)

	interest := row("loan-interest", 400, "weekly", 0, 0)
	assert.False(t, interest.Generous, "only categories that take a target are judged")

	sections := buildTargetSections([]CategoryTargetRow{groceries, pets, shopping}, "")
	assert.Equal(t, []string{"on-budget"}, sectionKeys(sections), "under target stays in the list; the note at the top mentions it")

	summary := buildTrackingSummary([]CategoryTargetRow{groceries, pets, shopping})
	assert.Equal(t, 2, summary.GenerousCount)
}

func TestBudgetNote(t *testing.T) {
	assert.Empty(t, CategorySetupData{}.Note(), "nothing to tidy, nothing said")
	assert.Equal(t, "Pets could have a lower target.", CategorySetupData{Generous: []string{"Pets"}}.Note())
	assert.Equal(t, "Pets and Shopping could have lower targets · Health, Other and Charity spend too little to need targets.",
		CategorySetupData{Generous: []string{"Pets", "Shopping"}, Small: []string{"Health", "Other", "Charity"}}.Note())
	assert.Equal(t, "Health spends too little to need a target.", CategorySetupData{Small: []string{"Health"}}.Note())

	t.Run("the page says it under the headline", func(t *testing.T) {
		rows := []CategoryTargetRow{targetRow("pets", 150, "weekly", 10)}
		markGenerous(&rows[0], 12)
		html := renderTemplate(t, "budget", map[string]any{
			"Income":    buildIncomeTile(models.BudgetSalary{}),
			"SetupData": CategorySetupData{HasData: true, Rows: rows, Sections: buildTargetSections(rows, ""), Generous: []string{"Pets"}},
		})
		assert.Contains(t, html, "Pets could have a lower target.")
		tracking := html[strings.Index(html, `b-tracking`):]
		tracking = tracking[:strings.Index(tracking, `id="budget-periods"`)]
		assert.Contains(t, tracking, "Pets could have a lower target.", "a row of the overview card")
		assert.Contains(t, tracking, `class="d-flex align-items-center gap-3 b-tracking-row"`)
		assert.Contains(t, tracking, `href="#budget-setup">Review targets</a>`)
		assert.NotContains(t, html, "Could be lower")
		assert.NotContains(t, html, "Small categories")
	})
}

func TestBuildSetupDataGathersTheNote(t *testing.T) {
	app := newTestApp(t)
	recent := lastWeek()
	require.NoError(t, app.store.CreateTransaction(t.Context(), spendTx("vet", "EFTPOS", "Health", "Veterinary services", "", "", -10, recent)))
	require.NoError(t, app.store.CreateTransaction(t.Context(), spendTx("pharm", "EFTPOS", "Health", "Pharmacies", "", "", -26, recent)))
	require.NoError(t, app.store.CreateBudgetItem(t.Context(), models.BudgetItem{ID: "pets", BudgetCategory: "pets", Amount: 150, Frequency: "weekly"}))

	setup, err := app.buildSetupData(t.Context())
	require.NoError(t, err)
	assert.Equal(t, []string{"Pets"}, setup.Generous, "$2.50 a week against $150")
	assert.Equal(t, []string{"Health"}, setup.Small, "$2 a week, with no target")
}

func TestSplitCategory(t *testing.T) {
	app := newTestApp(t)
	recent := lastWeek()
	require.NoError(t, app.store.CreateTransaction(t.Context(), spendTx("rates", "DIRECT DEBIT", "Housing", "Local government", "CCC", "", -860, recent)))
	require.NoError(t, app.store.CreateTransaction(t.Context(), spendTx("mortgage", models.TypeCountedTransfer, "Housing", "Mortgage", "", "AP#1 TO D J KING", -1284, recent)))
	require.NoError(t, app.store.CreateTransaction(t.Context(), spendTx("other", "DEBIT", "Housing", "Housing", "Body corp", "", -130, recent)))
	require.NoError(t, app.store.CreateBudgetItem(t.Context(), models.BudgetItem{ID: "home", BudgetCategory: "home", Amount: 700, Frequency: "weekly"}))

	post := func(handler echo.HandlerFunc, values url.Values) string {
		t.Helper()
		c, rec := postForm(t, app, values)
		c.Request().Header.Set("HX-Request", "true")
		require.NoError(t, handler(c))
		require.Equal(t, 200, rec.Code)
		return rec.Body.String()
	}

	t.Run("a category with subcategories offers to split", func(t *testing.T) {
		setup, err := app.buildSetupData(t.Context())
		require.NoError(t, err)
		home := rowByKey(t, setup, "home")
		assert.True(t, home.Splittable)
		assert.False(t, home.Split)
		html := renderTemplate(t, "budget.setup.row", home)
		assert.Contains(t, html, `hx-vals='{"category": "home", "split": "1"}'`, "a toggle beside its name splits it")
		assert.Contains(t, html, `aria-pressed="false"`)
		assert.False(t, rowByKey(t, setup, "groceries").Splittable, "nothing to split it into")
	})

	body := post(app.BudgetSaveSplit, url.Values{"category": {"home"}, "split": {"1"}})
	assert.Contains(t, body, `id="sc-mortgage"`, "its subcategories are drawn under it")
	assert.Contains(t, body, `hx-vals='{"category": "home", "split": "0"}'`, "and the same toggle puts it back")
	assert.Contains(t, body, `aria-pressed="true"`)
	assert.Contains(t, body, "bi-diagram-2-fill")
	home := body[indexOf(t, body, `id="sc-home"`):indexOf(t, body, `class="b-target-children"`)]
	assert.NotContains(t, home, "b-target-editor", "a split category has nothing of its own to open")
	assert.Contains(t, home, "b-target-chev invisible", "its chevron keeps its place, hidden, so the figures line up")
	assert.NotContains(t, home, "Power, internet", "nor any text to say so")
	post(app.BudgetSaveTarget, url.Values{"category": {"mortgage"}, "target_amount": {"1284"}, "target_frequency": {"fortnightly"}})
	body = post(app.BudgetSaveTarget, url.Values{"category": {"rates"}, "target_amount": {"860"}, "target_frequency": {"quarterly"}})
	assert.Contains(t, body, `id="sc-home"`, "an edit to a subcategory redraws its category")

	t.Run("each subcategory is measured against its own period", func(t *testing.T) {
		setup, err := app.buildSetupData(t.Context())
		require.NoError(t, err)
		home := rowByKey(t, setup, "home")
		require.True(t, home.Split)
		require.Len(t, home.SubRows, 4)
		mortgage, rates, general := home.SubRows[0], home.SubRows[1], home.SubRows[3]
		assert.Equal(t, "fortnightly", mortgage.Frequency)
		assert.Equal(t, 8, mortgage.WindowWeeks)
		assert.Equal(t, "a quarter", rates.Per)
		assert.Equal(t, 26, rates.WindowWeeks, "two quarters, so a quarterly bill is caught")
		assert.InDelta(t, 860.0/26*13, rates.ActualDisplay, 0.01)

		assert.InDelta(t, 642+860.0/13, home.TargetWeekly, 0.01, "the category's target is theirs added up, per week")
		assert.True(t, general.General, "what is filed under Home itself has a row of its own")
		assert.Equal(t, "Home (general)", general.Label)
		assert.InDelta(t, 130.0/13, general.ActualWeekly, 0.01)
		assert.Equal(t, []string{"Body corp"}, general.Merchants)
		assert.InDelta(t, mortgage.ActualWeekly+rates.ActualWeekly+general.ActualWeekly, home.ActualWeekly, 0.01)

		assert.Empty(t, home.Merchants, "a split category leaves its merchants to its subcategories")
		assert.Equal(t, []string{"CCC"}, rates.Merchants)

		cards, err := app.buildBudgetCards(t.Context())
		require.NoError(t, err)
		assert.InDelta(t, home.TargetWeekly, cards.TotalWeeklyExpenses, 0.01, "Home's own $700 is set aside while it is split")
	})

	t.Run("what is filed under the category itself takes a target too", func(t *testing.T) {
		body := post(app.BudgetSaveTarget, url.Values{"category": {"home:general"}, "target_amount": {"40"}, "target_frequency": {"monthly"}})
		assert.Contains(t, body, `id="sg-home"`, "a general row has an id of its own, apart from any category's")

		setup, err := app.buildSetupData(t.Context())
		require.NoError(t, err)
		home := rowByKey(t, setup, "home")
		general := home.SubRows[3]
		assert.Equal(t, 40.0, general.Target)
		assert.Equal(t, "a month", general.Per)
		assert.InDelta(t, 642+860.0/13+40.0*12/52, home.TargetWeekly, 0.01, "and counts towards its category")

		cards, err := app.buildBudgetCards(t.Context())
		require.NoError(t, err)
		assert.InDelta(t, home.TargetWeekly, cards.TotalWeeklyExpenses, 0.01)

		items, err := app.store.ReadBudgetItems(t.Context())
		require.NoError(t, err)
		charts := BuildPeriodCharts([]models.Transaction{
			spendTx("other", "DEBIT", "Housing", "Housing", "Body corp", "", -30, time.Now().Add(-time.Hour)),
			spendTx("rates", "DIRECT DEBIT", "Housing", "Local government", "CCC", "", -860, time.Now().Add(-time.Hour)),
		}, budgetTargets(items), time.Now())
		for _, chart := range charts {
			if chart.Frequency == "monthly" {
				assert.Equal(t, []string{"Home (general)"}, chart.Categories)
				assert.Equal(t, 30.0, chart.Total, "only what is filed under Home itself, not its rates")
			}
		}
	})

	t.Run("each lands in its own period chart", func(t *testing.T) {
		items, err := app.store.ReadBudgetItems(t.Context())
		require.NoError(t, err)
		charts := BuildPeriodCharts([]models.Transaction{
			spendTx("rates", "DIRECT DEBIT", "Housing", "Local government", "CCC", "", -860, time.Now().Add(-time.Hour)),
		}, budgetTargets(items), time.Now())
		byFrequency := map[string]PeriodChart{}
		for _, chart := range charts {
			byFrequency[chart.Frequency] = chart
		}
		require.Contains(t, byFrequency, "quarterly")
		assert.Equal(t, []string{"Rates"}, byFrequency["quarterly"].Categories)
		assert.Equal(t, 860.0, byFrequency["quarterly"].Total, "the rates bill counts in the quarter, not the fortnight")
		assert.Zero(t, byFrequency["fortnightly"].Total)
		assert.NotContains(t, byFrequency, "weekly", "Home's own weekly target is not in force")
	})

	t.Run("going back to one target keeps both", func(t *testing.T) {
		post(app.BudgetSaveSplit, url.Values{"category": {"home"}, "split": {"0"}})
		setup, err := app.buildSetupData(t.Context())
		require.NoError(t, err)
		home := rowByKey(t, setup, "home")
		assert.False(t, home.Split)
		assert.Equal(t, 700.0, home.Target, "its own target is back")

		post(app.BudgetSaveSplit, url.Values{"category": {"home"}, "split": {"1"}})
		setup, err = app.buildSetupData(t.Context())
		require.NoError(t, err)
		home = rowByKey(t, setup, "home")
		assert.Equal(t, 1284.0, home.SubRows[0].Target, "and so are its subcategories'")
		assert.Equal(t, 40.0, home.SubRows[3].Target, "and its general target")
	})

	t.Run("a category with nothing to split into is refused", func(t *testing.T) {
		c, rec := postForm(t, app, url.Values{"category": {"groceries"}, "split": {"1"}})
		require.NoError(t, app.BudgetSaveSplit(c))
		assert.Equal(t, 400, rec.Code)
	})
}

func TestEveryRowLeadsToItsTrend(t *testing.T) {
	html := renderTemplate(t, "budget.setup.row", targetRow("groceries", 150, "weekly", 120))
	assert.Contains(t, html, `href="/insights?group=groceries"`)

	group, ok := SpendGroupFor("rates")
	require.True(t, ok, "insights shows any category, not only the tracked ones")
	assert.Equal(t, "Rates", group.Label)
	assert.Equal(t, CadenceMonthly, group.Cadence, "a bill is shown by month")
	group, _ = SpendGroupFor("eating-out")
	assert.Equal(t, CadenceWeekly, group.Cadence)
	_, ok = SpendGroupFor("nonsense")
	assert.False(t, ok)
}

func TestSplitCategoryAlwaysHasAGeneralBucket(t *testing.T) {
	app := newTestApp(t)
	require.NoError(t, app.store.CreateTransaction(t.Context(), spendTx("rates", "DIRECT DEBIT", "Housing", "Local government", "CCC", "", -860, time.Now().Add(-48*time.Hour))))
	require.NoError(t, app.store.CreateBudgetItem(t.Context(), models.BudgetItem{ID: "home", BudgetCategory: "home", Split: true}))

	setup, err := app.buildSetupData(t.Context())
	require.NoError(t, err)
	home := rowByKey(t, setup, "home")
	last := home.SubRows[len(home.SubRows)-1]
	assert.True(t, last.General, "nothing filed under Home itself yet, but it is plain where it would go")
	assert.Equal(t, "Home (general)", last.Label)
	assert.Zero(t, last.ActualWeekly)
}

func TestBudgetModeIsSaidInWords(t *testing.T) {
	combined := targetRow("transport", 150, "weekly", 120)
	combined.Splittable = true
	html := renderTemplate(t, "budget.setup.row", combined)
	assert.Regexp(t, `bi-square" aria-hidden="true"></i>Combined`, html)
	assert.Contains(t, html, "One target for everything in Transport")

	transport, _ := models.BudgetCategoryByKey("transport")
	split := splitRow(transport, nil, 1_000)
	html = renderTemplate(t, "budget.setup.row", split)
	assert.Regexp(t, `bi-diagram-2-fill" aria-hidden="true"></i>Split`, html)
	assert.Contains(t, html, "one for what is filed under Transport itself")

	assert.NotContains(t, renderTemplate(t, "budget.setup.row", targetRow("groceries", 150, "weekly", 120)), "b-target-mode",
		"nothing to split, so nothing to say")
}

func TestSplitCategoryIsJudgedPartByPart(t *testing.T) {
	transport, _ := models.BudgetCategoryByKey("transport")
	fuel := targetRow("fuel", 75, "weekly", 74)
	general := measureGeneral(t, transport, 16)
	split := splitRow(transport, []CategoryTargetRow{fuel, general}, 1_000)

	html := renderTemplate(t, "budget.setup.row", split)
	heading := html[:indexOf(t, html, "</summary>")]
	assert.NotContains(t, heading, "b-target-bar", "no bar for a split category: its parts each have their own")
	assert.Contains(t, heading, "$90 a week", "it shows everything it spends, the general bucket's too")

	// The row agrees with the summary: the targeted parts are under their
	// targets, so it is not marked over for what has no target.
	assert.False(t, split.IsOver, "$74 against $75; the general $16 has no target to be over")
	assert.NotContains(t, html[:indexOf(t, html, "<details")], "b-target-over")
	assert.InDelta(t, 74.0/75, split.Percentage, 0.001)
	assert.InDelta(t, 90, split.ActualWeekly, 0.001)

	summary := buildTrackingSummary([]CategoryTargetRow{split})
	assert.InDelta(t, 74, summary.Actual, 0.001, "only what has a target is measured against targets")
	assert.InDelta(t, 75, summary.Target, 0.001)
	assert.False(t, summary.IsOver(), "the general bucket's spending does not push the targeted parts over")
	require.Len(t, summary.Untargeted, 1)
	assert.Equal(t, "Transport (general)", summary.Untargeted[0].Label, "it is spending with no target, said as such")
}

// measureGeneral is a split category's general bucket spending the given
// amount a week, with no target.
func measureGeneral(t *testing.T, category models.BudgetCategory, weekly float64) CategoryTargetRow {
	t.Helper()
	row := buildSetupRowData(generalCategory(category), models.BudgetItem{}, weekly, 1_000)
	row.General = true
	return row
}

func TestSplitWithNoSubcategoriesLeftIsCombined(t *testing.T) {
	app := newTestApp(t)
	t.Cleanup(func() { models.UseBudgetCategories(models.BudgetCategorySettings{}) })
	require.NoError(t, app.store.CreateBudgetItem(t.Context(), models.BudgetItem{ID: "big", BudgetCategory: "big-purchases", Amount: 5200, Frequency: "yearly", Split: true}))
	settings, err := models.BudgetCategorySettings{}.DeleteBudgetCategory("vehicles", "")
	require.NoError(t, err)
	require.NoError(t, app.store.SaveBudgetCategorySettings(t.Context(), settings))

	setup, err := app.buildSetupData(t.Context())
	require.NoError(t, err)
	big := rowByKey(t, setup, "big-purchases")
	assert.False(t, big.Split, "nothing left to split it into")
	assert.InDelta(t, 100, big.TargetWeekly, 0.001)

	cards, err := app.buildBudgetCards(t.Context())
	require.NoError(t, err)
	assert.InDelta(t, big.TargetWeekly, cards.TotalWeeklyExpenses, 0.001, "the totals agree with the row")

	items, err := app.store.ReadBudgetItems(t.Context())
	require.NoError(t, err)
	charts := BuildPeriodCharts(nil, budgetTargets(items), time.Now())
	require.Len(t, charts, 1)
	assert.Equal(t, "yearly", charts[0].Frequency, "and so do the charts")
	assert.Equal(t, 5200.0, charts[0].Target)
}

func TestSplittingAMergedKeyIsRefused(t *testing.T) {
	app := newTestApp(t)
	t.Cleanup(func() { models.UseBudgetCategories(models.BudgetCategorySettings{}) })
	settings, err := models.BudgetCategorySettings{}.DeleteBudgetCategory("home", "bills")
	require.NoError(t, err)
	require.NoError(t, app.store.SaveBudgetCategorySettings(t.Context(), settings))

	c, rec := postForm(t, app, url.Values{"category": {"home"}, "split": {"1"}})
	require.NoError(t, app.BudgetSaveSplit(c))
	assert.Equal(t, 400, rec.Code, "a stale page naming a deleted category does not split the one it went into")
	items, err := app.store.ReadBudgetItems(t.Context())
	require.NoError(t, err)
	assert.Empty(t, items)
}
