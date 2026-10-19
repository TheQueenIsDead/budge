package application

import (
	"testing"
	"time"

	"github.com/TheQueenIsDead/budge/pkg/database/models"
	"github.com/stretchr/testify/assert"
)

func TestBuildDebtOutlook(t *testing.T) {
	from := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)

	// Avanti, from the money plan: about $6,400 at 9%, $234.11 a month.
	avanti := BuildDebtOutlook(6400, 9, 234.11, "monthly", from)
	assert.True(t, avanti.Known)
	assert.False(t, avanti.Never)
	assert.Equal(t, 31, avanti.Repayments)
	assert.InDelta(t, 820, avanti.InterestLeft, 60)
	assert.Equal(t, from.AddDate(0, 31, 0), avanti.PaidOffBy)
	assert.InDelta(t, 48, avanti.InterestFirst, 0.01, "9% a year is 0.75% a month")

	family := BuildDebtOutlook(1000, 0, 100, "fortnightly", from)
	assert.Equal(t, 10, family.Repayments, "no interest: what is owed over the repayment")
	assert.Zero(t, family.InterestLeft)
	assert.Equal(t, from.AddDate(0, 0, 140), family.PaidOffBy)

	stuck := BuildDebtOutlook(1000, 20, 3, "weekly", from)
	assert.True(t, stuck.Never, "a repayment smaller than the interest never clears it")

	assert.False(t, BuildDebtOutlook(1000, 9, 0, "monthly", from).Known, "nothing to project without a repayment")
	assert.False(t, BuildDebtOutlook(0, 9, 100, "monthly", from).Known, "nothing owed, nothing to project")
}

// on fixes the date debts are estimated to.
func on(t *testing.T, date time.Time) {
	t.Helper()
	today = func() time.Time { return date }
	t.Cleanup(func() { today = time.Now })
}

func TestDebtSummaryProjectsFromLatestBalance(t *testing.T) {
	on(t, time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC))
	debt := models.Asset{
		Id: "avanti", Name: "Avanti", Type: "debt", PurchasePrice: 8000,
		PurchaseDate: time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC),
		InterestRate: 9, Repayment: 234.11, RepaymentFrequency: "monthly",
		Valuations: []models.AssetValuation{{ID: "v", Value: 6400, Date: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)}},
	}
	summary := BuildAssetSummary(debt)
	assert.Equal(t, 31, summary.Outlook.Repayments, "from the $6,400 still owed, not the $8,000 borrowed")
	assert.Equal(t, time.Date(2029, 5, 1, 0, 0, 0, 0, time.UTC), summary.Outlook.PaidOffBy)

	owned := BuildAssetSummary(models.Asset{Type: "vehicle", PurchasePrice: 5000})
	assert.False(t, owned.Outlook.Known, "only debts have repayments")
}

func TestRenderDebt(t *testing.T) {
	on(t, time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC))
	debt := models.Asset{
		Id: "avanti", Name: "Avanti", Type: "debt", PurchasePrice: 8000,
		PurchaseDate: time.Date(2024, 5, 30, 0, 0, 0, 0, time.UTC),
		InterestRate: 9, Repayment: 234.11, RepaymentFrequency: "monthly",
		Valuations: []models.AssetValuation{{ID: "v", Value: 6400, Date: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)}},
	}
	page := renderTemplate(t, "asset", AssetPageProps{
		Summary: BuildAssetSummary(debt),
		Series:  BuildAssetSeries(debt),
		History: debt.SortedValuations(),
		Types:   assetTypes,
		Today:   "2026-10-19",
	})
	assert.Contains(t, page, "Paid off in <strong>31 repayments</strong>")
	assert.Contains(t, page, `b-chip b-accent-brick`, "a debt is marked red even while it is being paid off")
	assert.Contains(t, page, "May 2029")
	assert.Contains(t, page, `hx-post="/portfolio/assets/avanti/terms"`)
	assert.Contains(t, page, `value="234.11"`, "the form shows the saved terms")
	assert.Contains(t, page, `<option value="monthly" selected>`)

	unset := debt
	unset.Repayment = 0
	bare := renderTemplate(t, "asset", AssetPageProps{Summary: BuildAssetSummary(unset), Types: assetTypes, Today: "2026-10-19"})
	assert.Contains(t, bare, "Add the interest rate and what you repay")

	house := renderTemplate(t, "asset", AssetPageProps{Summary: BuildAssetSummary(models.Asset{Id: "h", Type: "house", PurchasePrice: 1}), Types: assetTypes, Today: "2026-10-19"})
	assert.NotContains(t, house, "Repayments", "only debts have repayments")

	debtType, _ := AssetTypeByKey("debt")
	wizard := renderTemplate(t, "asset_new", AssetNewProps{Types: assetTypes, Selected: debtType, Chosen: true, Today: "2026-10-19"})
	assert.Contains(t, wizard, `name="interest_rate"`, "a new debt can be given its terms")
	assert.Contains(t, wizard, "Amount borrowed")
	vehicle, _ := AssetTypeByKey("vehicle")
	car := renderTemplate(t, "asset_new", AssetNewProps{Types: assetTypes, Selected: vehicle, Chosen: true, Today: "2026-10-19"})
	assert.NotContains(t, car, `name="interest_rate"`)
}

func TestEstimateDebt(t *testing.T) {
	balance := time.Date(2026, 10, 19, 0, 0, 0, 0, time.UTC)

	none := EstimateDebt(6452.57, 9, 234.11, "monthly", balance, balance.AddDate(0, 0, 20))
	assert.Equal(t, 6452.57, none.Owed, "no repayment due yet")
	assert.Zero(t, none.Repayments)

	two := EstimateDebt(6452.57, 9, 234.11, "monthly", balance, balance.AddDate(0, 2, 3))
	assert.Equal(t, 2, two.Repayments)
	assert.InDelta(t, 6452.57*1.0075*1.0075-234.11*1.0075-234.11, two.Owed, 0.01, "interest on the balance, then the repayment off it")
	assert.Equal(t, balance.AddDate(0, 2, 0), two.Through)

	cleared := EstimateDebt(300, 0, 100, "weekly", balance, balance.AddDate(1, 0, 0))
	assert.Zero(t, cleared.Owed, "it stops at nothing owed")
	assert.Equal(t, 3, cleared.Repayments)

	assert.Equal(t, 500.0, EstimateDebt(500, 9, 0, "monthly", balance, balance.AddDate(1, 0, 0)).Owed, "no repayment, nothing applied")
}

func TestDebtSummaryEstimatesWhatIsOwed(t *testing.T) {
	avanti := models.Asset{
		Id: "avanti", Name: "Avanti", Type: "debt", PurchasePrice: 10941.30,
		PurchaseDate: time.Date(2024, 5, 30, 0, 0, 0, 0, time.UTC),
		InterestRate: 9, Repayment: 234.11, RepaymentFrequency: "monthly",
		Valuations: []models.AssetValuation{{ID: "v", Value: 6452.57, Date: time.Date(2026, 10, 19, 0, 0, 0, 0, time.UTC)}},
	}

	on(t, time.Date(2026, 12, 25, 0, 0, 0, 0, time.UTC))
	summary := BuildAssetSummary(avanti)
	assert.True(t, summary.Estimated)
	assert.Equal(t, 2, summary.Estimate.Repayments, "19 Nov and 19 Dec have been paid since the balance was entered")
	assert.Less(t, summary.Owed, 6452.57)
	assert.Equal(t, -summary.Owed, summary.Current)
	assert.Equal(t, 29, summary.Outlook.Repayments, "two fewer left")
	assert.Equal(t, time.Date(2029, 5, 19, 0, 0, 0, 0, time.UTC), summary.Outlook.PaidOffBy, "the payoff date does not move")

	never := avanti
	never.Valuations = nil
	from := BuildAssetSummary(never)
	assert.Equal(t, avanti.PurchaseDate, from.Estimate.From, "with no balance entered, it works forward from the loan itself")
	assert.Equal(t, 30, from.Estimate.Repayments)
}
