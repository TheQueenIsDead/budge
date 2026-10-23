package application

import (
	"net/url"
	"testing"
	"time"

	"github.com/TheQueenIsDead/budge/pkg/database/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAssetUpdatePurchase(t *testing.T) {
	app := newTestApp(t)
	bought := time.Date(2020, 3, 1, 0, 0, 0, 0, time.UTC)
	require.NoError(t, app.store.CreateAsset(models.Asset{Id: "car", Name: "Car", PurchasePrice: 1000, PurchaseDate: bought}))

	update := func(values url.Values) error {
		c, _ := postForm(t, app, values)
		c.Request().Header.Set("HX-Request", "true")
		c.SetParamNames("id")
		c.SetParamValues("car")
		return app.AssetUpdatePurchase(c)
	}

	t.Run("a corrected price and date are saved", func(t *testing.T) {
		require.NoError(t, update(url.Values{"purchase_price": {"18500"}, "purchase_date": {"2021-07-15"}}))
		asset, err := app.store.GetAsset("car")
		require.NoError(t, err)
		assert.InDelta(t, 18500, asset.PurchasePrice, 0.001)
		assert.Equal(t, "2021-07-15", asset.PurchaseDate.Format("2006-01-02"))
		assert.Equal(t, "Car", asset.Name, "nothing else about the asset changes")
	})

	t.Run("a missing price or date is refused and changes nothing", func(t *testing.T) {
		assert.Error(t, update(url.Values{"purchase_price": {"0"}, "purchase_date": {"2021-07-15"}}))
		assert.Error(t, update(url.Values{"purchase_price": {"200"}, "purchase_date": {""}}))
		asset, err := app.store.GetAsset("car")
		require.NoError(t, err)
		assert.InDelta(t, 18500, asset.PurchasePrice, 0.001)
	})
}

func TestAssetPageOffersPurchaseEdit(t *testing.T) {
	asset := models.Asset{Id: "car", Name: "Car", PurchasePrice: 18500, PurchaseDate: time.Date(2021, 7, 15, 0, 0, 0, 0, time.UTC)}
	html := renderTemplate(t, "asset", AssetPageProps{Summary: BuildAssetSummary(asset), Series: BuildAssetSeries(asset), Types: assetTypes, Today: "2026-10-23"})
	assert.Contains(t, html, `hx-post="/portfolio/assets/car/purchase"`)
	assert.Contains(t, html, `value="18500.00"`)
	assert.Contains(t, html, `value="2021-07-15"`)
}
