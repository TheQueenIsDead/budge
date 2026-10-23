package application

import (
	"encoding/json"
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"testing"
	"time"

	"github.com/TheQueenIsDead/budge/pkg/database/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func spendTx(id, txType, broad, specific, merchant, description string, amount float64, date time.Time) models.Transaction {
	tx := models.Transaction{Id: id, Type: txType, Amount: amount, Date: date, Description: description}
	tx.Category.Name = specific
	tx.Category.Groups.PersonalFinance.Name = broad
	tx.Merchant.Name = merchant
	return tx
}

func TestSpendCategory(t *testing.T) {
	now := time.Now()
	tests := []struct {
		name             string
		tx               models.Transaction
		expectedBroad    string
		expectedSpecific string
		expectedOK       bool
	}{
		{"categorised debit keeps its categories", spendTx("1", "EFTPOS", "Food", "Supermarkets", "", "", -50, now), "Food", "Supermarkets", true},
		{"missing specific falls back to broad", spendTx("2", "EFTPOS", "Food", "", "", "", -50, now), "Food", "Food", true},
		{"uncategorised loan interest is grouped by type", spendTx("3", "LOAN", "", "", "", "LOAN INTEREST", -400, now), uncategorisedGroup, "Loan", true},
		{"uncategorised standing order is grouped by type", spendTx("4", "STANDING ORDER", "", "", "", "AP#1 TO FISHER FUNDS", -385, now), uncategorisedGroup, "Standing orders", true},
		{"unknown type still counts", spendTx("5", "MYSTERY", "", "", "", "", -10, now), uncategorisedGroup, "Other", true},
		{"income is not spending", spendTx("6", "CREDIT", "", "", "", "", 1000, now), "", "", false},
		{"transfers are not spending", spendTx("7", "TRANSFER", "", "", "", "", -500, now), "", "", false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			broad, specific, ok := spendCategory(test.tx)
			assert.Equal(t, test.expectedOK, ok)
			assert.Equal(t, test.expectedBroad, broad)
			assert.Equal(t, test.expectedSpecific, specific)
		})
	}
}

func TestSpendMerchantFallsBackToCleanedDescription(t *testing.T) {
	now := time.Now()
	assert.Equal(t, "Countdown", spendMerchant(spendTx("1", "EFTPOS", "", "", "Countdown", "POS W/D COUNTDOWN-10:01", -5, now)))
	assert.Equal(t, "Holland Suzuk", spendMerchant(spendTx("2", "EFTPOS", "", "", "", "POS W/D Holland Suzuk-13:04", -5, now)),
		"card terminal noise is stripped so repeat visits share one tag")
	assert.Equal(t, "AP#19781426 TO D J KING", spendMerchant(spendTx("3", models.TypeCountedTransfer, "", "", "Transfer", "AP#19781426 TO D J KING", -641, now)),
		"sync names every transfer's merchant Transfer, so the description identifies it")
}

// TestChartAndSetupCountTheSameSpend is the regression test for weeks
// showing as over budget with nothing in the setup table to explain it: the
// chart counted uncategorised debits that the table silently dropped.
func TestChartAndSetupCountTheSameSpend(t *testing.T) {
	app := newTestApp(t)
	recent := time.Now().Add(-48 * time.Hour)
	for _, tx := range []models.Transaction{
		spendTx("groceries", "EFTPOS", "Food", "Supermarkets", "Countdown", "", -120, recent),
		spendTx("interest", "LOAN", "", "", "", "LOAN INTEREST", -410, recent),
		spendTx("fund", "STANDING ORDER", "", "", "", "AP#23533701 TO FISHER FUNDS", -385, recent),
		spendTx("salary", "CREDIT", "", "", "", "SALARY", 3000, recent),
		spendTx("sweep", "TRANSFER", "", "", "", "TO SAVINGS", -1000, recent),
	} {
		require.NoError(t, app.store.CreateTransaction(tx))
	}

	setup, err := app.buildSetupData()
	require.NoError(t, err)

	var uncategorised *BroadCategoryGroup
	var setupTotal float64
	for i, group := range setup.Groups {
		// Every row here is untargeted, so its actual is a 4-week average.
		setupTotal += group.TotalActualWeekly * 4
		if group.Name == uncategorisedGroup {
			uncategorised = &setup.Groups[i]
		}
	}
	require.NotNil(t, uncategorised, "uncategorised debits must be visible in the table")

	rows := map[string]CategoryTargetRow{}
	for _, row := range uncategorised.Rows {
		rows[row.SubCategory] = row
	}
	assert.Equal(t, []string{"LOAN INTEREST"}, rows["Loan"].Merchants)
	assert.Equal(t, []string{"AP#23533701 TO FISHER FUNDS"}, rows["Standing orders"].Merchants)
	assert.True(t, rows["Loan"].NeedsTarget, "untargeted spend is flagged so it can be budgeted")

	chartTotal := chartTotal(t, app)
	assert.InDelta(t, 915, chartTotal, 0.001, "income and transfers are excluded")
	assert.InDelta(t, chartTotal, setupTotal, 0.001, "the chart and the table must count the same money")
}

func TestNonSpendingRowsCarryNoTarget(t *testing.T) {
	for _, kind := range []string{models.BudgetKindSavings, models.BudgetKindIgnore} {
		t.Run(kind, func(t *testing.T) {
			item := models.BudgetItem{ID: "fund", Category: "Standing orders", Amount: 385, Frequency: "weekly", Kind: kind}
			row := buildSetupRowData(item, 400, 1_000, false, false)

			assert.False(t, row.IsSpending)
			assert.False(t, row.HasTarget)
			assert.False(t, row.IsOver, "a non-spending row can never be over budget")
			assert.False(t, row.NeedsTarget, "a non-spending row should not nag for a target")
			assert.Zero(t, row.PctOfIncome)

			html := renderTemplate(t, "budget.setup.row", row)
			assert.NotContains(t, html, `name="target_amount"`)
			assert.NotContains(t, html, `hx-post="/budget/subitems"`)
			assert.Contains(t, html, `hx-post="/budget/kind"`, "the row can still be switched back")
		})
	}
}

func chartTotal(t *testing.T, app *Application) float64 {
	t.Helper()
	rec := httptest.NewRecorder()
	require.NoError(t, app.BudgetPerformance(app.http.NewContext(httptest.NewRequest(http.MethodGet, "/", nil), rec)))
	match := regexp.MustCompile(`data-actuals="([^"]*)"`).FindStringSubmatch(rec.Body.String())
	require.Len(t, match, 2)
	var actuals []float64
	require.NoError(t, json.Unmarshal([]byte(html.UnescapeString(match[1])), &actuals))
	var total float64
	for _, v := range actuals {
		total += v
	}
	return total
}

func groupNamed(t *testing.T, setup CategorySetupData, name string) BroadCategoryGroup {
	t.Helper()
	for _, group := range setup.Groups {
		if group.Name == name {
			return group
		}
	}
	t.Fatalf("no %q group", name)
	return BroadCategoryGroup{}
}

func TestBudgetSaveKind(t *testing.T) {
	app := newTestApp(t)
	recent := time.Now().Add(-48 * time.Hour)
	for _, tx := range []models.Transaction{
		spendTx("interest", "LOAN", "", "", "", "LOAN INTEREST", -400, recent),
		spendTx("fund", "STANDING ORDER", "", "", "", "AP#1 TO FISHER FUNDS", -200, recent),
	} {
		require.NoError(t, app.store.CreateTransaction(tx))
	}
	require.InDelta(t, 600, chartTotal(t, app), 0.001)

	saveKind := func(kind string) *httptest.ResponseRecorder {
		c, rec := postForm(t, app, url.Values{
			"sub_category":   {"Standing orders"},
			"broad_category": {uncategorisedGroup},
			"kind":           {kind},
		})
		require.NoError(t, app.BudgetSaveKind(c))
		return rec
	}

	t.Run("savings leave the chart and the group total but are tracked", func(t *testing.T) {
		rec := saveKind(models.BudgetKindSavings)
		require.Equal(t, http.StatusOK, rec.Code)
		assert.Contains(t, rec.Body.String(), "going to savings categories", "the re-rendered table reports tracked savings")

		assert.InDelta(t, 400, chartTotal(t, app), 0.001)
		setup, err := app.buildSetupData()
		require.NoError(t, err)
		assert.InDelta(t, 400.0/4, groupNamed(t, setup, uncategorisedGroup).TotalActualWeekly, 0.001)
		assert.InDelta(t, 200.0/4, setup.TrackedSavingsWeekly, 0.001)
	})

	t.Run("ignored spend is dropped without counting as savings", func(t *testing.T) {
		saveKind(models.BudgetKindIgnore)

		assert.InDelta(t, 400, chartTotal(t, app), 0.001)
		setup, err := app.buildSetupData()
		require.NoError(t, err)
		assert.Zero(t, setup.TrackedSavingsWeekly)
	})

	t.Run("switching back to spending counts it again", func(t *testing.T) {
		saveKind(models.BudgetKindSpending)
		assert.InDelta(t, 600, chartTotal(t, app), 0.001)

		items, err := app.store.ReadBudgetItems()
		require.NoError(t, err)
		assert.Len(t, items, 1, "repeat changes update the same item")
	})

	t.Run("an unknown kind is rejected", func(t *testing.T) {
		c, rec := postForm(t, app, url.Values{"sub_category": {"Standing orders"}, "kind": {"bogus"}})
		require.NoError(t, app.BudgetSaveKind(c))
		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})
}

func TestSavingTargetKeepsKind(t *testing.T) {
	app := newTestApp(t)
	require.NoError(t, app.store.CreateBudgetItem(models.BudgetItem{ID: "fund", Category: "Standing orders", Kind: models.BudgetKindSavings}))

	c, _ := postForm(t, app, url.Values{"sub_category": {"Standing orders"}, "target_amount": {"100"}, "target_frequency": {"weekly"}})
	require.NoError(t, app.BudgetSaveTarget(c))

	item, err := app.store.GetBudgetItem("fund")
	require.NoError(t, err)
	assert.Equal(t, models.BudgetKindSavings, item.Kind)
}

// TestRowsCollapseWithTheirGroup is the regression test for untargeted rows
// staying visible when their group was collapsed: with no saved item they
// were tagged with an empty group id instead of the group they sit under.
func TestRowsCollapseWithTheirGroup(t *testing.T) {
	app := newTestApp(t)
	recent := time.Now().Add(-48 * time.Hour)
	require.NoError(t, app.store.CreateTransaction(spendTx("untargeted", "EFTPOS", "Food", "Bakeries", "", "", -5, recent)))
	require.NoError(t, app.store.CreateTransaction(spendTx("targeted", "EFTPOS", "Food", "Supermarkets", "", "", -50, recent)))
	require.NoError(t, app.store.CreateBudgetItem(models.BudgetItem{ID: "s", Category: "Supermarkets", BroadCategory: "Food", Amount: 100, Frequency: "weekly"}))

	setup, err := app.buildSetupData()
	require.NoError(t, err)

	food := groupNamed(t, setup, "Food")
	require.Len(t, food.Rows, 2)
	for _, row := range food.Rows {
		assert.Equal(t, food.RowID, row.BroadRowID, row.SubCategory)
	}
}

func TestBuildWeekBreakdown(t *testing.T) {
	monday := time.Date(2026, 8, 3, 0, 0, 0, 0, time.UTC)
	wed := monday.AddDate(0, 0, 2)
	items := []models.BudgetItem{
		{Category: "Supermarkets", BroadCategory: "Food", Amount: 150, Frequency: "weekly"},
		{Category: "Local government", BroadCategory: "Housing", Amount: 3_640, Frequency: "yearly"}, // $70/wk
		{Category: "Cafes", BroadCategory: "Lifestyle", Amount: 50, Frequency: "weekly"},
		{Category: "Standing orders", Kind: models.BudgetKindSavings},
	}
	txs := []models.Transaction{
		spendTx("g1", "EFTPOS", "Food", "Supermarkets", "Countdown", "", -80, wed),
		spendTx("g2", "EFTPOS", "Food", "Supermarkets", "New World", "", -40, wed),
		spendTx("rates", "DEBIT", "Housing", "Local government", "CCC", "", -862, wed),
		spendTx("interest", "LOAN", "", "", "", "LOAN INTEREST", -410, wed),
		spendTx("fund", "STANDING ORDER", "", "", "", "AP#1 TO FISHER FUNDS", -385, wed),
		spendTx("next-week", "EFTPOS", "Food", "Supermarkets", "Countdown", "", -500, monday.AddDate(0, 0, 7)),
		spendTx("pay", "CREDIT", "", "", "", "SALARY", 3_000, wed),
	}

	week := buildWeekBreakdown(monday, txs, items)

	assert.InDelta(t, 80+40+862+410, week.Total, 0.001, "savings, income and other weeks are left out")
	assert.InDelta(t, 150+70+50, week.Target, 0.001)
	assert.InDelta(t, week.Total-week.Target, week.Diff, 0.001)

	var linesDiff float64
	for _, line := range week.Lines {
		linesDiff += line.Diff
	}
	assert.InDelta(t, week.Diff, linesDiff-week.UnspentTarget, 0.001,
		"every dollar over or under is accounted for by a line or the unspent targets")
	assert.Equal(t, 1, week.UnspentCount, "cafes had a target and no spend")
	assert.InDelta(t, 50, week.UnspentTarget, 0.001)

	require.Len(t, week.Lines, 3)
	assert.Equal(t, "Local government", week.Lines[0].Specific, "biggest overspend first")
	assert.Equal(t, "billed yearly", week.Lines[0].Billed)
	assert.Equal(t, "Loan", week.Lines[1].Specific)
	assert.False(t, week.Lines[1].HasTarget)
	assert.InDelta(t, 410, week.Lines[1].Diff, 0.001, "untargeted spend is all over")
	assert.Equal(t, "Supermarkets", week.Lines[2].Specific)
	assert.Equal(t, 2, week.Lines[2].Count)
	assert.Empty(t, week.Lines[2].Billed, "weekly targets do not spike")

	require.NotEmpty(t, week.Largest)
	assert.Equal(t, "CCC", week.Largest[0].Title)
	assert.InDelta(t, 862, week.Largest[0].Amount, 0.001)
}

func TestBuildWeekBreakdownCapsLargestTransactions(t *testing.T) {
	monday := time.Date(2026, 8, 3, 0, 0, 0, 0, time.UTC)
	var txs []models.Transaction
	for i := 1; i <= 8; i++ {
		txs = append(txs, spendTx(string(rune('a'+i)), "EFTPOS", "Food", "Supermarkets", "", "", -float64(i*10), monday))
	}

	week := buildWeekBreakdown(monday, txs, nil)

	require.Len(t, week.Largest, largeTransactionCount)
	assert.InDelta(t, 80, week.Largest[0].Amount, 0.001)
	assert.InDelta(t, 40, week.Largest[4].Amount, 0.001)
}

func TestBudgetWeek(t *testing.T) {
	app := newTestApp(t)
	recent := time.Now().Add(-48 * time.Hour)
	require.NoError(t, app.store.CreateTransaction(spendTx("interest", "LOAN", "", "", "", "LOAN INTEREST", -410, recent)))

	get := func(query string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/budget/week?"+query, nil)
		req.Header.Set("HX-Request", "true")
		rec := httptest.NewRecorder()
		require.NoError(t, app.BudgetWeek(app.http.NewContext(req, rec)))
		return rec
	}

	t.Run("a given week is broken down and selected in the picker", func(t *testing.T) {
		start := isoWeekStart(recent).Format(time.DateOnly)
		rec := get("start=" + start)
		require.Equal(t, http.StatusOK, rec.Code)
		assert.Contains(t, rec.Body.String(), "LOAN INTEREST")
		assert.Contains(t, rec.Body.String(), `value="`+start+`" selected`)
	})

	t.Run("a mid-week date opens the week containing it", func(t *testing.T) {
		rec := get("start=" + recent.Format(time.DateOnly))
		assert.Contains(t, rec.Body.String(), "LOAN INTEREST")
	})

	t.Run("a malformed date is rejected", func(t *testing.T) {
		assert.Equal(t, http.StatusBadRequest, get("start=yesterday").Code)
	})
}

func TestBuildPerformanceSeries(t *testing.T) {
	weeks := []time.Time{
		time.Date(2026, 8, 3, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC),
	}
	in := func(week int) time.Time { return weeks[week].AddDate(0, 0, 2) }
	items := budgetItemsByCategory([]models.BudgetItem{
		{Category: "Supermarkets", Amount: 150, Frequency: "weekly"},
		{Category: "Local government", Amount: 3_500, Frequency: "yearly"},
		{Category: "Cafes"}, // saved without a target
		{Category: "Standing orders", Kind: models.BudgetKindSavings, Amount: 100, Frequency: "weekly"},
	})
	txs := []models.Transaction{
		spendTx("groceries", "EFTPOS", "Food", "Supermarkets", "", "", -120, in(0)),
		spendTx("rates", "DEBIT", "Housing", "Local government", "", "", -862, in(0)),
		spendTx("interest", "LOAN", "", "", "", "LOAN INTEREST", -410, in(0)),
		spendTx("coffee", "EFTPOS", "Lifestyle", "Cafes", "", "", -6, in(1)),
		spendTx("fund", "STANDING ORDER", "", "", "", "FISHER FUNDS", -385, in(1)),
		spendTx("before", "EFTPOS", "Food", "Supermarkets", "", "", -1_000, weeks[0].AddDate(0, 0, -3)),
	}

	totals, series := buildPerformanceSeries(weeks, txs, items)

	require.Len(t, series, 2)
	assert.Equal(t, budgetedSeries, series[0].Label, "budgeted sits at the base of the stack")
	assert.Equal(t, unbudgetedSeries, series[1].Label)

	assert.InDelta(t, 982, series[0].Data[0], 0.001, "a yearly target still makes its spend budgeted")
	assert.InDelta(t, 410, series[1].Data[0], 0.001, "a category with no saved item is unbudgeted")
	assert.InDelta(t, 6, series[1].Data[1], 0.001, "a saved item with no amount is unbudgeted")
	assert.InDelta(t, 6, totals[1], 0.001, "savings and weeks outside the chart are left out")

	for week := range weeks {
		assert.InDelta(t, totals[week], series[0].Data[week]+series[1].Data[week], 0.001, "each bar's segments add up to its total")
	}
}

func TestBuildPerformanceSeriesWithoutSpend(t *testing.T) {
	totals, series := buildPerformanceSeries(performanceWeeks(time.Now()), nil, nil)
	assert.Len(t, totals, 13)
	require.Len(t, series, 2, "both series are always sent so each keeps its colour")
	assert.Len(t, series[0].Data, 13)
}
