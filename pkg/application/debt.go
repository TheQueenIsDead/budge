package application

import (
	"math"
	"strings"
	"time"

	"github.com/TheQueenIsDead/budge/pkg/database/models"
	"github.com/labstack/echo/v4"
)

// repaymentFrequencies are how often a debt can be repaid, with how many
// repayments that makes a year.
var repaymentFrequencies = []struct {
	Key     string
	Label   string
	PerYear float64
}{
	{"weekly", "Weekly", 52},
	{"fortnightly", "Fortnightly", 26},
	{"monthly", "Monthly", 12},
}

func repaymentsPerYear(frequency string) float64 {
	for _, f := range repaymentFrequencies {
		if f.Key == frequency {
			return f.PerYear
		}
	}
	return 12
}

// repaymentStep moves a date on by one repayment.
func repaymentStep(t time.Time, frequency string, n int) time.Time {
	switch frequency {
	case "weekly":
		return t.AddDate(0, 0, 7*n)
	case "fortnightly":
		return t.AddDate(0, 0, 14*n)
	}
	return t.AddDate(0, n, 0)
}

// payoffLimit caps the projection. Fifty years of monthly repayments is
// longer than any debt worth tracking here, and a projection that long says
// the repayments are only just ahead of the interest.
const payoffLimit = 600

// DebtOutlook is where a debt is heading on its current terms.
type DebtOutlook struct {
	// Known is whether there are terms to project from: without a repayment
	// nothing can be said about when it ends.
	Known bool

	// Never is set when the repayment does not cover the interest, so the
	// debt grows however long it is paid.
	Never bool

	Repayments   int
	PaidOffBy    time.Time
	InterestLeft float64

	// InterestFirst is the interest in the next repayment period, the part of
	// the repayment that does not touch what is owed.
	InterestFirst float64
}

// BuildDebtOutlook projects a debt from what is owed on a date: interest
// charged on the balance each repayment period at the yearly rate divided
// across the year's repayments, then the repayment taken off, until nothing
// is owed. It is the plain arithmetic of the terms entered, not the lender's
// schedule, which may compound daily and round its own way.
func BuildDebtOutlook(owed, ratePercent, repayment float64, frequency string, from time.Time) DebtOutlook {
	if owed <= 0 || repayment <= 0 {
		return DebtOutlook{}
	}
	rate := ratePercent / 100 / repaymentsPerYear(frequency)
	outlook := DebtOutlook{Known: true, InterestFirst: owed * rate}
	if rate > 0 && repayment <= owed*rate {
		outlook.Never = true
		return outlook
	}

	balance := owed
	for balance > 0.005 && outlook.Repayments < payoffLimit {
		interest := balance * rate
		outlook.InterestLeft += interest
		balance += interest - repayment
		outlook.Repayments++
	}
	if balance > 0.005 {
		outlook.Never = true
		return outlook
	}
	outlook.InterestLeft = math.Round(outlook.InterestLeft*100) / 100
	outlook.PaidOffBy = repaymentStep(from, frequency, outlook.Repayments)
	return outlook
}

// debtOutlook projects one tracked debt from its latest balance.
func debtOutlook(asset models.Asset, owed float64, latest time.Time) DebtOutlook {
	from := latest
	if from.IsZero() {
		from = asset.PurchaseDate
	}
	return BuildDebtOutlook(owed, asset.InterestRate, asset.Repayment, asset.RepaymentFrequency, from)
}

// readDebtTerms takes a debt's terms from a form. A frequency it does not
// know falls back to monthly, the commonest.
func readDebtTerms(c echo.Context, asset *models.Asset) {
	asset.InterestRate = math.Max(parseAssetAmount(c.FormValue("interest_rate")), 0)
	asset.Repayment = math.Max(parseAssetAmount(c.FormValue("repayment")), 0)
	asset.RepaymentFrequency = "monthly"
	frequency := strings.TrimSpace(c.FormValue("repayment_frequency"))
	for _, f := range repaymentFrequencies {
		if f.Key == frequency {
			asset.RepaymentFrequency = frequency
		}
	}
}

// AssetSaveTerms updates a debt's interest rate and repayments.
func (app *Application) AssetSaveTerms(c echo.Context) error {
	asset, err := app.store.GetAsset(c.Param("id"))
	if err != nil || asset.Id == "" {
		return echo.NewHTTPError(404, "No such debt.")
	}
	readDebtTerms(c, &asset)
	if err := app.store.UpdateAsset(asset); err != nil {
		app.Toast(c, "Error", "Could not save the repayments.")
		return echo.NewHTTPError(500, err.Error())
	}
	app.Toast(c, "Saved", "Repayments saved.")
	return app.Asset(c)
}
