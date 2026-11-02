package application

import (
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
		name     string
		tx       models.Transaction
		expected string
		ok       bool
	}{
		{"a categorised debit goes where Akahu's category does", spendTx("1", "EFTPOS", "Food", "Supermarkets and grocery stores", "", "", -50, now), "groceries", true},
		{"a subcategory budgets under its category", spendTx("2", "EFTPOS", "Transport", "Fuel stations", "", "", -50, now), "transport", true},
		{"a repayment a rule counts is spending", spendTx("3", models.TypeCountedTransfer, "Housing", "Mortgage", "", "", -641, now), "home", true},
		{"anything unfiled is still spending", spendTx("4", "PAYMENT", "", "", "", "PAY ROOFER", -900, now), "uncategorised", true},
		{"loan interest is not spending", spendTx("5", "LOAN", "", "", "", "LOAN INTEREST", -400, now), "", false},
		{"savings are not spending", spendTx("6", models.TypeCountedTransfer, "General", "Savings", "", "", -385, now), "", false},
		{"income is not spending", spendTx("7", "CREDIT", "", "", "", "", 1000, now), "", false},
		{"transfers are not spending", spendTx("8", "TRANSFER", "", "", "", "", -500, now), "", false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			category, ok := spendCategory(test.tx)
			assert.Equal(t, test.ok, ok)
			assert.Equal(t, test.expected, category.Key)
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
		spendTx("groceries", "EFTPOS", "Food", "Supermarkets and grocery stores", "Countdown", "", -120, recent),
		spendTx("interest", "LOAN", "", "", "", "LOAN INTEREST", -410, recent),
		spendTx("fund", "STANDING ORDER", "", "", "", "AP#23533701 TO FISHER FUNDS", -385, recent),
		spendTx("salary", "CREDIT", "", "", "", "SALARY", 3000, recent),
		spendTx("sweep", "TRANSFER", "", "", "", "TO SAVINGS", -1000, recent),
	} {
		require.NoError(t, app.store.CreateTransaction(tx))
	}

	setup, err := app.buildSetupData()
	require.NoError(t, err)

	uncategorised := rowByKey(t, setup, "uncategorised")
	assert.Equal(t, []string{"AP#23533701 TO FISHER FUNDS"}, uncategorised.Merchants)
	assert.False(t, uncategorised.Budgeted, "what is not filed yet takes no target")
	assert.False(t, uncategorised.NeedsTarget)

	interest := rowByKey(t, setup, "loan-interest")
	assert.Equal(t, []string{"LOAN INTEREST"}, interest.Merchants, "interest is still shown, for information")

	assert.True(t, rowByKey(t, setup, "groceries").NeedsTarget, "untargeted spend is flagged so it can be budgeted")
	assert.InDelta(t, 505, countedTotal(t, app), 0.001, "every debit is spending but interest, income and transfers")
}

func TestEveryCategoryHasARow(t *testing.T) {
	app := newTestApp(t)
	require.NoError(t, app.store.CreateTransaction(spendTx("groceries", "EFTPOS", "Food", "Supermarkets and grocery stores", "", "", -50, time.Now())))

	setup, err := app.buildSetupData()
	require.NoError(t, err)
	assert.Len(t, setup.Rows, len(models.TopBudgetCategories()), "a target can be set before anything is spent")
	assert.Equal(t, "home", setup.Rows[0].Key, "rows follow the registry")
}

func TestUnbudgetedRowsCarryNoTarget(t *testing.T) {
	for _, key := range []string{"savings", "loan-interest", "uncategorised"} {
		t.Run(key, func(t *testing.T) {
			category, _ := models.BudgetCategoryByKey(key)
			item := models.BudgetItem{ID: "x", BudgetCategory: key, Amount: 385, Frequency: "weekly"}
			row := buildSetupRowData(category, item, 400, 1_000)

			assert.False(t, row.Budgeted)
			assert.False(t, row.HasTarget)
			assert.False(t, row.IsOver, "an unbudgeted row can never be over budget")
			assert.False(t, row.NeedsTarget, "an unbudgeted row should not nag for a target")
			assert.Zero(t, row.TargetWeekly, "nor count towards the targets")
			assert.Zero(t, row.PctOfIncome)

			html := renderTemplate(t, "budget.setup.row", row)
			assert.NotContains(t, html, `name="target_amount"`)
		})
	}
}

func TestSubcategorySpend(t *testing.T) {
	app := newTestApp(t)
	recent := time.Now().Add(-48 * time.Hour)
	for _, tx := range []models.Transaction{
		spendTx("power", "DIRECT DEBIT", "Utilities", "Electricity services", "Mercury", "", -130, recent),
		spendTx("net", "DIRECT DEBIT", "Utilities", "Internet services", "Voyager", "", -65, recent),
		spendTx("water", "DIRECT DEBIT", "Utilities", "Water", "Water Co", "", -26, recent),
	} {
		require.NoError(t, app.store.CreateTransaction(tx))
	}

	setup, err := app.buildSetupData()
	require.NoError(t, err)
	bills := rowByKey(t, setup, "bills")
	weeks := float64(bills.WindowWeeks)
	assert.InDelta(t, 221/weeks, bills.ActualWeekly, 0.001)
	assert.Equal(t, []SubcategorySpend{
		{Label: "Power", Weekly: 130 / weeks},
		{Label: "Internet & mobile", Weekly: 65 / weeks},
		{Label: "Other bills & subscriptions", Weekly: 26 / weeks},
	}, bills.Subs, "biggest first, with what no subcategory holds last")

	assert.Empty(t, rowByKey(t, setup, "groceries").Subs, "nothing finer to split by")
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
		if !row.NotSpending {
			total += row.ActualWeekly * float64(row.WindowWeeks)
		}
	}
	return total
}

// rowByKey returns a category's row, failing the test if there is none.
func rowByKey(t *testing.T, setup CategorySetupData, key string) CategoryTargetRow {
	t.Helper()
	for _, row := range setup.Rows {
		if row.Key == key {
			return row
		}
	}
	t.Fatalf("no row for %q", key)
	return CategoryTargetRow{}
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

func TestSpendingOnlyDropsSavingsAndInterest(t *testing.T) {
	now := time.Now()
	txs := []models.Transaction{
		spendTx("interest", "LOAN", "", "", "", "LOAN INTEREST", -410, now),
		spendTx("fund", models.TypeCountedTransfer, "General", "Savings", "", "AP#1 TO FISHER FUNDS", -100, now),
		spendTx("repay", models.TypeCountedTransfer, "Housing", "Mortgage", "", "AP#2 TO D J KING", -641, now),
		spendTx("groceries", "EFTPOS", "Food", "Supermarkets and grocery stores", "", "", -80, now),
		spendTx("pay", "CREDIT", "", "", "", "SALARY", 3000, now),
		spendTx("sweep", "TRANSFER", "Transfer", "Transfer", "", "TO SAVINGS", -500, now),
	}

	var ids []string
	for _, tx := range spendingOnly(txs) {
		ids = append(ids, tx.Id)
	}
	assert.Equal(t, []string{"repay", "groceries", "pay", "sweep"}, ids,
		"interest and fund contributions go; repayments, spending, income and plain transfers are left for each view to treat as it does")
}
