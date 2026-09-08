package application

import (
	"strings"
	"testing"
	"time"

	"github.com/TheQueenIsDead/budge/pkg/database/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// house builds an asset with a purchase and the given valuations.
func house(purchase float64, purchased time.Time, valuations ...models.AssetValuation) models.Asset {
	return models.Asset{
		Id:            "asset-1",
		Name:          "12 Bealey Ave",
		Type:          "house",
		PurchasePrice: purchase,
		PurchaseDate:  purchased,
		Valuations:    valuations,
	}
}

func valuation(value float64, date time.Time) models.AssetValuation {
	return models.AssetValuation{ID: "v", Date: date, Value: value}
}

func TestAssetCurrentValue(t *testing.T) {
	bought := time.Date(2020, time.March, 1, 0, 0, 0, 0, time.UTC)

	t.Run("falls back to the purchase price before any valuation", func(t *testing.T) {
		assert.Equal(t, 620000.0, house(620000, bought).CurrentValue())
	})

	t.Run("uses the most recent valuation regardless of stored order", func(t *testing.T) {
		asset := house(620000, bought,
			valuation(910000, time.Date(2025, time.June, 1, 0, 0, 0, 0, time.UTC)),
			valuation(725000, time.Date(2022, time.June, 1, 0, 0, 0, 0, time.UTC)),
			valuation(880000, time.Date(2024, time.June, 1, 0, 0, 0, 0, time.UTC)),
		)
		assert.Equal(t, 910000.0, asset.CurrentValue())
	})

	t.Run("sorting does not disturb the stored slice", func(t *testing.T) {
		asset := house(620000, bought,
			valuation(910000, time.Date(2025, time.June, 1, 0, 0, 0, 0, time.UTC)),
			valuation(725000, time.Date(2022, time.June, 1, 0, 0, 0, 0, time.UTC)),
		)
		_ = asset.SortedValuations()
		assert.Equal(t, 910000.0, asset.Valuations[0].Value)
	})
}

func TestBuildAssetSummary(t *testing.T) {
	bought := time.Date(2020, time.March, 1, 0, 0, 0, 0, time.UTC)
	valued := time.Date(2025, time.June, 1, 0, 0, 0, 0, time.UTC)

	t.Run("measures the gain against what was paid", func(t *testing.T) {
		summary := BuildAssetSummary(house(620000, bought, valuation(910000, valued)))

		assert.Equal(t, 910000.0, summary.Current)
		assert.Equal(t, 290000.0, summary.Change)
		assert.InDelta(t, 290000.0/620000.0, summary.Delta, 0.0001)
		assert.True(t, summary.Valued)
		assert.Equal(t, valued, summary.LastDate)
	})

	t.Run("reports a loss when the value has fallen", func(t *testing.T) {
		summary := BuildAssetSummary(house(620000, bought, valuation(580000, valued)))
		assert.Equal(t, -40000.0, summary.Change)
		assert.Less(t, summary.Delta, 0.0)
	})

	t.Run("an unvalued asset sits at cost rather than claiming a flat result", func(t *testing.T) {
		summary := BuildAssetSummary(house(620000, bought))
		assert.False(t, summary.Valued)
		assert.Equal(t, 620000.0, summary.Current)
		assert.Equal(t, 0.0, summary.Change)
	})

	t.Run("a zero purchase price does not divide by zero", func(t *testing.T) {
		summary := BuildAssetSummary(house(0, bought, valuation(1000, valued)))
		assert.Equal(t, 1000.0, summary.Change)
		assert.Equal(t, 0.0, summary.Delta)
	})
}

func TestBuildAssetSeries(t *testing.T) {
	bought := time.Date(2020, time.March, 1, 0, 0, 0, 0, time.UTC)

	t.Run("starts the curve at the purchase", func(t *testing.T) {
		series := BuildAssetSeries(house(620000, bought,
			valuation(910000, time.Date(2025, time.June, 1, 0, 0, 0, 0, time.UTC)),
			valuation(725000, time.Date(2022, time.June, 1, 0, 0, 0, 0, time.UTC)),
		))

		require.Len(t, series.Data, 3)
		assert.Equal(t, []float64{620000, 725000, 910000}, series.Data)
		assert.Equal(t, []string{"Mar 20", "Jun 22", "Jun 25"}, series.Labels)
	})

	t.Run("omits a purchase with no date", func(t *testing.T) {
		asset := house(620000, time.Time{}, valuation(910000, time.Date(2025, time.June, 1, 0, 0, 0, 0, time.UTC)))
		series := BuildAssetSeries(asset)
		require.Len(t, series.Data, 1)
		assert.Equal(t, 910000.0, series.Data[0])
	})
}

func TestBuildAssetPortfolio(t *testing.T) {
	bought := time.Date(2020, time.March, 1, 0, 0, 0, 0, time.UTC)
	valued := time.Date(2025, time.June, 1, 0, 0, 0, 0, time.UTC)

	car := models.Asset{Id: "asset-2", Name: "Hilux", Type: "vehicle", PurchasePrice: 40000}

	summaries := []AssetSummary{
		BuildAssetSummary(house(620000, bought, valuation(910000, valued))),
		BuildAssetSummary(car),
	}

	portfolio := BuildAssetPortfolio(summaries)

	assert.Equal(t, 950000.0, portfolio.Total)
	assert.Equal(t, 660000.0, portfolio.Cost)
	assert.Equal(t, 290000.0, portfolio.Change)
	assert.Equal(t, 2, portfolio.Count)
	assert.True(t, portfolio.HasAsset)

	t.Run("an empty portfolio reports nothing held", func(t *testing.T) {
		empty := BuildAssetPortfolio(nil)
		assert.False(t, empty.HasAsset)
		assert.Equal(t, 0.0, empty.Delta)
	})
}

func TestAssetTypeLabelAndIcon(t *testing.T) {
	tests := []struct {
		input string
		label string
		icon  string
	}{
		{"house", "House", "bi-house"},
		{"vehicle", "Vehicle", "bi-car-front"},
		{"other", "Other", "bi-box-seam"},
		{"nonsense", "Asset", "bi-box-seam"},
	}

	for _, test := range tests {
		t.Run(test.input, func(t *testing.T) {
			assert.Equal(t, test.label, AssetTypeLabel(test.input))
			assert.Equal(t, test.icon, AssetTypeIcon(test.input))
		})
	}
}

func TestParseAssetInput(t *testing.T) {
	t.Run("an empty date falls back to today", func(t *testing.T) {
		assert.Equal(t, time.Now().Format("2006-01-02"), parseAssetDate("").Format("2006-01-02"))
	})

	t.Run("an unparseable date falls back to today", func(t *testing.T) {
		assert.Equal(t, time.Now().Format("2006-01-02"), parseAssetDate("not-a-date").Format("2006-01-02"))
	})

	t.Run("reads an ISO date", func(t *testing.T) {
		assert.Equal(t, "2024-07-19", parseAssetDate("2024-07-19").Format("2006-01-02"))
	})

	t.Run("reads an amount, tolerating whitespace", func(t *testing.T) {
		assert.Equal(t, 910000.50, parseAssetAmount(" 910000.50 "))
		assert.Equal(t, 0.0, parseAssetAmount(""))
		assert.Equal(t, 0.0, parseAssetAmount("abc"))
	})
}

func TestRenderAccountsWithAssets(t *testing.T) {
	bought := time.Date(2020, time.March, 1, 0, 0, 0, 0, time.UTC)
	valued := time.Date(2025, time.June, 1, 0, 0, 0, 0, time.UTC)

	accounts := []models.Account{account("everyday", "Everyday", "Kiwibank", "CHECKING", 1100)}
	assets := []AssetSummary{BuildAssetSummary(house(620000, bought, valuation(910000, valued)))}
	transactions := []models.Transaction{transaction("everyday", 100)}

	page := renderTemplate(t, "accounts", AccountsListProps{
		Portfolio:   BuildPortfolio(accounts, assets),
		Groups:      BuildAccountGroups(accounts, transactions, hasTransactions(transactions)),
		Assets:      assets,
		AssetTotals: BuildAssetPortfolio(assets),
		AssetTypes:  assetTypes,
		Today:       "2026-09-08",
	})

	t.Run("counts assets towards net worth", func(t *testing.T) {
		// 1100 in the bank plus a 910000 house.
		assert.Contains(t, page, "$911,100.00")
		assert.Contains(t, page, "Includes $910,000.00 of assets")
		assert.Contains(t, page, "and 1 asset")
	})

	t.Run("lists assets alongside the bank accounts", func(t *testing.T) {
		assert.Contains(t, page, "12 Bealey Ave")
		assert.Contains(t, page, `href="/assets/asset-1"`)
		assert.Contains(t, page, "Kiwibank")
	})

	t.Run("offers the add form", func(t *testing.T) {
		assert.Contains(t, page, `hx-post="/assets"`)
		for _, assetType := range assetTypes {
			assert.Contains(t, page, `value="`+assetType.Key+`"`)
		}
	})

	t.Run("renders with assets but no bank accounts", func(t *testing.T) {
		only := renderTemplate(t, "accounts", AccountsListProps{
			Portfolio:   BuildPortfolio(nil, assets),
			Assets:      assets,
			AssetTotals: BuildAssetPortfolio(assets),
			AssetTypes:  assetTypes,
			Today:       "2026-09-08",
		})
		assert.Contains(t, only, "12 Bealey Ave")
		assert.Contains(t, only, "$910,000.00")
	})
}

func TestRenderAsset(t *testing.T) {
	bought := time.Date(2020, time.March, 1, 0, 0, 0, 0, time.UTC)
	valued := time.Date(2025, time.June, 1, 0, 0, 0, 0, time.UTC)
	asset := house(620000, bought, valuation(910000, valued))

	page := renderTemplate(t, "asset", AssetPageProps{
		Summary: BuildAssetSummary(asset),
		Series:  BuildAssetSeries(asset),
		History: asset.SortedValuations(),
		Types:   assetTypes,
		Today:   "2026-09-08",
	})

	t.Run("charts the value once there is more than one point", func(t *testing.T) {
		assert.Contains(t, page, `id="assetChart"`)
		assert.Contains(t, page, "hx-preserve")
	})

	t.Run("offers a way to record another valuation", func(t *testing.T) {
		assert.Contains(t, page, `hx-post="/assets/asset-1/valuations"`)
	})

	t.Run("lists the valuation history", func(t *testing.T) {
		assert.Contains(t, page, "Valuation history")
		assert.Contains(t, page, "1 Jun 2025")
	})

	t.Run("says so when there is not enough to chart", func(t *testing.T) {
		bare := house(620000, time.Time{})
		flat := renderTemplate(t, "asset", AssetPageProps{
			Summary: BuildAssetSummary(bare),
			Series:  BuildAssetSeries(bare),
			Types:   assetTypes,
			Today:   "2026-09-08",
		})
		assert.Contains(t, flat, "Not enough to chart")
	})
}

func TestPortfolioSectionOrder(t *testing.T) {
	bought := time.Date(2020, time.March, 1, 0, 0, 0, 0, time.UTC)
	valued := time.Date(2025, time.June, 1, 0, 0, 0, 0, time.UTC)

	accounts := []models.Account{account("everyday", "Everyday", "Kiwibank", "CHECKING", 1100)}
	assets := []AssetSummary{BuildAssetSummary(house(620000, bought, valuation(910000, valued)))}
	transactions := []models.Transaction{transaction("everyday", 100)}

	page := renderTemplate(t, "accounts", AccountsListProps{
		Portfolio:   BuildPortfolio(accounts, assets),
		Groups:      BuildAccountGroups(accounts, transactions, hasTransactions(transactions)),
		Assets:      assets,
		AssetTotals: BuildAssetPortfolio(assets),
		AssetTypes:  assetTypes,
		Today:       "2026-09-09",
	})

	t.Run("assets come before the bank accounts", func(t *testing.T) {
		assetsAt := strings.Index(page, "12 Bealey Ave")
		accountsAt := strings.Index(page, "Kiwibank")
		require.NotEqual(t, -1, assetsAt)
		require.NotEqual(t, -1, accountsAt)
		assert.Less(t, assetsAt, accountsAt,
			"assets are the hand-maintained part and should not sit below a long list of accounts")
	})

	t.Run("the portfolio tiles still come first", func(t *testing.T) {
		assert.Less(t, strings.Index(page, "Net Worth"), strings.Index(page, "12 Bealey Ave"))
	})

	t.Run("the add form stays at the bottom", func(t *testing.T) {
		assert.Greater(t, strings.Index(page, "Add an asset"), strings.Index(page, "Kiwibank"))
	})
}
