package application

import (
	"net/http"
	"net/http/httptest"
	"net/url"
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

// TestBudgetCountsUncategorisedSpend is the regression test for weeks
// showing as over budget with nothing in the table to explain it: the
// weekly chart counted uncategorised debits that the table silently dropped.
func TestBudgetCountsUncategorisedSpend(t *testing.T) {
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

	rows := map[string]CategoryTargetRow{}
	for _, row := range rowsIn(t, setup, uncategorisedGroup) {
		rows[row.SubCategory] = row
	}
	assert.Equal(t, []string{"LOAN INTEREST"}, rows["Loan"].Merchants)
	assert.Equal(t, []string{"AP#23533701 TO FISHER FUNDS"}, rows["Standing orders"].Merchants)
	assert.True(t, rows["Loan"].NeedsTarget, "untargeted spend is flagged so it can be budgeted")

	assert.InDelta(t, 915, countedTotal(t, app), 0.001, "every debit is counted, but not income or transfers")
}

func TestNonSpendingRowsCarryNoTarget(t *testing.T) {
	for _, kind := range []string{models.BudgetKindSavings, models.BudgetKindIgnore} {
		t.Run(kind, func(t *testing.T) {
			item := models.BudgetItem{ID: "fund", Category: "Standing orders", Amount: 385, Frequency: "weekly", Kind: kind}
			row := buildSetupRowData(item, 400, 1_000)

			assert.False(t, row.IsSpending)
			assert.False(t, row.HasTarget)
			assert.False(t, row.IsOver, "a non-spending row can never be over budget")
			assert.False(t, row.NeedsTarget, "a non-spending row should not nag for a target")
			assert.Zero(t, row.PctOfIncome)

			html := renderTemplate(t, "budget.setup.row", row)
			assert.NotContains(t, html, `name="target_amount"`)
			assert.Contains(t, html, `hx-post="/budget/kind"`, "the row can still be switched back")
		})
	}
}

// countedTotal is the spending the budget counts across every category, each
// over its own window. Every fixture transaction is recent enough to fall in
// every window, so it adds back up to the raw amounts.
func countedTotal(t *testing.T, app *Application) float64 {
	t.Helper()
	setup, err := app.buildSetupData()
	require.NoError(t, err)
	var total float64
	for _, row := range setup.Rows {
		if row.IsSpending {
			total += row.ActualWeekly * float64(row.WindowWeeks)
		}
	}
	return total
}

// rowsIn returns the rows filed under a broad category, failing the test if
// there are none.
func rowsIn(t *testing.T, setup CategorySetupData, broad string) []CategoryTargetRow {
	t.Helper()
	var rows []CategoryTargetRow
	for _, row := range setup.Rows {
		if row.BroadCategory == broad {
			rows = append(rows, row)
		}
	}
	if len(rows) == 0 {
		t.Fatalf("no rows under %q", broad)
	}
	return rows
}

// countedWeekly totals the weekly spend that counts against the budget.
func countedWeekly(rows []CategoryTargetRow) float64 {
	var total float64
	for _, row := range rows {
		if row.IsSpending {
			total += row.ActualWeekly
		}
	}
	return total
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
	require.InDelta(t, 600, countedTotal(t, app), 0.001)

	saveKind := func(kind string) *httptest.ResponseRecorder {
		c, rec := postForm(t, app, url.Values{
			"sub_category":   {"Standing orders"},
			"broad_category": {uncategorisedGroup},
			"kind":           {kind},
		})
		require.NoError(t, app.BudgetSaveKind(c))
		return rec
	}

	t.Run("savings leave the counted total but are tracked", func(t *testing.T) {
		rec := saveKind(models.BudgetKindSavings)
		require.Equal(t, http.StatusOK, rec.Code)
		assert.Contains(t, rec.Body.String(), "going to savings categories", "the re-rendered table reports tracked savings")

		assert.InDelta(t, 400, countedTotal(t, app), 0.001)
		setup, err := app.buildSetupData()
		require.NoError(t, err)
		assert.InDelta(t, 400.0/untargetedWindowWeeks, countedWeekly(rowsIn(t, setup, uncategorisedGroup)), 0.001)
		assert.InDelta(t, 200.0/untargetedWindowWeeks, setup.TrackedSavingsWeekly, 0.001)
	})

	t.Run("ignored spend is dropped without counting as savings", func(t *testing.T) {
		saveKind(models.BudgetKindIgnore)

		assert.InDelta(t, 400, countedTotal(t, app), 0.001)
		setup, err := app.buildSetupData()
		require.NoError(t, err)
		assert.Zero(t, setup.TrackedSavingsWeekly)
	})

	t.Run("switching back to spending counts it again", func(t *testing.T) {
		saveKind(models.BudgetKindSpending)
		assert.InDelta(t, 600, countedTotal(t, app), 0.001)

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

func TestDashboardListsLeaveOutTransfers(t *testing.T) {
	now := time.Now()
	recent := []models.Transaction{
		spendTx("repay", models.TypeCountedTransfer, "General", "Mortgage", "Transfer", "AP#19781426 TO D J KING", -641, now),
		spendTx("cylinder", "PAYMENT", "", "", "", "PAY The Cylinder Guy Limited", -2455, now),
		spendTx("groceries", "EFTPOS", "Food", "Supermarkets", "PAK'nSAVE", "", -200, now),
	}

	var merchants []string
	for _, m := range BuildTopMerchants(nil, recent, 6) {
		merchants = append(merchants, m.Merchant)
	}
	assert.Equal(t, []string{"PAK'nSAVE"}, merchants, "a counted transfer is not a merchant")

	var biggest []string
	for _, tx := range BuildHighestOutgoingTransactions(recent, 6) {
		biggest = append(biggest, tx.Description)
	}
	assert.Equal(t, []string{"PAY The Cylinder Guy Limited", "PAK'nSAVE"}, biggest)
}

func TestDashboardMonthsAreWhole(t *testing.T) {
	now := time.Date(2026, 10, 23, 15, 4, 0, 0, time.UTC)
	start := dashboardMonthsStart(now)
	assert.Equal(t, time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC), start, "May to October, with May whole rather than its last week")

	months := AggregateMonthlyTransactions([]models.Transaction{
		spendTx("first", "EFTPOS", "Food", "Supermarkets", "", "", -10, start),
		spendTx("now", "EFTPOS", "Food", "Supermarkets", "", "", -10, now),
	}, time.UTC)
	assert.Len(t, months, 2)
	assert.Contains(t, months, "May 26")
}

func TestWithoutUncountedDropsSavingsAndIgnoredSpend(t *testing.T) {
	app := newTestApp(t)
	require.NoError(t, app.store.CreateBudgetItem(models.BudgetItem{ID: "loan", Category: "Loan", Kind: models.BudgetKindIgnore}))
	require.NoError(t, app.store.CreateBudgetItem(models.BudgetItem{ID: "fund", Category: "Savings", BroadCategory: "General", Kind: models.BudgetKindSavings}))
	now := time.Now()
	txs := []models.Transaction{
		spendTx("interest", "LOAN", "", "", "", "LOAN INTEREST", -410, now),
		spendTx("fund", models.TypeCountedTransfer, "General", "Savings", "", "AP#1 TO FISHER FUNDS", -100, now),
		spendTx("repay", models.TypeCountedTransfer, "Housing", "Mortgage", "", "AP#2 TO D J KING", -641, now),
		spendTx("groceries", "EFTPOS", "Food", "Supermarkets", "", "", -80, now),
		spendTx("pay", "CREDIT", "", "", "", "SALARY", 3000, now),
		spendTx("sweep", "TRANSFER", "Transfer", "Transfer", "", "TO SAVINGS", -500, now),
	}

	kept, err := app.withoutUncounted(txs)
	require.NoError(t, err)

	var ids []string
	for _, tx := range kept {
		ids = append(ids, tx.Id)
	}
	assert.Equal(t, []string{"repay", "groceries", "pay", "sweep"}, ids,
		"interest and fund contributions go; repayments, spending, income and plain transfers are left for each view to treat as it does")
}
