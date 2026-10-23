package application

import (
	"encoding/json"
	"html"
	"net/http"
	"net/http/httptest"
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

	req := httptest.NewRequest(http.MethodGet, "/budget/performance", nil)
	rec := httptest.NewRecorder()
	require.NoError(t, app.BudgetPerformance(app.http.NewContext(req, rec)))
	require.Equal(t, http.StatusOK, rec.Code)

	match := regexp.MustCompile(`data-actuals="([^"]*)"`).FindStringSubmatch(rec.Body.String())
	require.Len(t, match, 2)
	var actuals []float64
	require.NoError(t, json.Unmarshal([]byte(html.UnescapeString(match[1])), &actuals))
	var chartTotal float64
	for _, v := range actuals {
		chartTotal += v
	}

	assert.InDelta(t, 915, chartTotal, 0.001, "income and transfers are excluded")
	assert.InDelta(t, chartTotal, setupTotal, 0.001, "the chart and the table must count the same money")
}
