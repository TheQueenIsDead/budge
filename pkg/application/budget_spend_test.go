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
