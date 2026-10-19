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

func TestDebtSummaryProjectsFromLatestBalance(t *testing.T) {
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
