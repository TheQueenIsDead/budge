package application

import (
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"fmt"

	"github.com/TheQueenIsDead/budge/pkg/database/models"
	"github.com/TheQueenIsDead/budge/pkg/integrations/property"
	"github.com/labstack/echo/v4"
)

// AssetType is a kind of asset the form offers.
type AssetType struct {
	Key   string
	Label string
	Icon  string
}

// assetTypes are the kinds of asset the form offers. Anything owned whose value
// drifts fits the same shape; a house is simply the one worth the most.
var assetTypes = []AssetType{
	{"house", "House", "bi-house"},
	{"vehicle", "Vehicle", "bi-car-front"},
	{"other", "Other", "bi-box-seam"},
}

// AssetTypeLabel names an asset type for display.
func AssetTypeLabel(assetType string) string {
	for _, t := range assetTypes {
		if t.Key == assetType {
			return t.Label
		}
	}
	return "Asset"
}

// AssetTypeIcon marks an asset type, so a list can be scanned by shape.
func AssetTypeIcon(assetType string) string {
	for _, t := range assetTypes {
		if t.Key == assetType {
			return t.Icon
		}
	}
	return "bi-box-seam"
}

// AssetSummary is an asset annotated with how its value has moved since it was
// bought. Gains are measured against the purchase price rather than the previous
// valuation, because that is the number an owner actually has in mind.
type AssetSummary struct {
	Asset models.Asset

	TypeLabel string
	Icon      string

	Current float64
	Change  float64
	Delta   float64

	// Valued is whether anything has been recorded since the purchase. Without
	// it the "change" is zero by definition and should not be dressed up as a
	// flat result.
	Valued   bool
	LastDate time.Time
}

// BuildAssetSummary annotates one asset for the list.
func BuildAssetSummary(asset models.Asset) AssetSummary {
	summary := AssetSummary{
		Asset:     asset,
		TypeLabel: AssetTypeLabel(asset.Type),
		Icon:      AssetTypeIcon(asset.Type),
		Current:   asset.CurrentValue(),
	}

	valuations := asset.SortedValuations()
	if len(valuations) > 0 {
		summary.Valued = true
		summary.LastDate = valuations[len(valuations)-1].Date
	}

	summary.Change = summary.Current - asset.PurchasePrice
	if asset.PurchasePrice != 0 {
		summary.Delta = summary.Change / math.Abs(asset.PurchasePrice)
	}

	return summary
}

// AssetPortfolio totals what is owned outside the bank feeds.
type AssetPortfolio struct {
	Total    float64
	Cost     float64
	Change   float64
	Delta    float64
	Count    int
	HasAsset bool
}

// BuildAssetPortfolio rolls every asset into one position.
func BuildAssetPortfolio(summaries []AssetSummary) AssetPortfolio {
	portfolio := AssetPortfolio{Count: len(summaries), HasAsset: len(summaries) > 0}
	for _, summary := range summaries {
		portfolio.Total += summary.Current
		portfolio.Cost += summary.Asset.PurchasePrice
	}
	portfolio.Change = portfolio.Total - portfolio.Cost
	if portfolio.Cost != 0 {
		portfolio.Delta = portfolio.Change / math.Abs(portfolio.Cost)
	}
	return portfolio
}

// AssetSeries is the value curve for one asset, ready for Chart.js.
type AssetSeries struct {
	Labels []string
	Data   []float64
}

type AssetPageProps struct {
	Summary AssetSummary
	Series  AssetSeries

	// History is newest first, which is the order the table reads in.
	History []models.AssetValuation
	Types   []AssetType
	Today   string
}

// BuildAssetSeries turns the purchase and every valuation into one curve. The
// purchase is the first point: without it a single valuation would draw a flat
// line saying nothing about whether the thing has gained.
func BuildAssetSeries(asset models.Asset) AssetSeries {
	type point struct {
		date  time.Time
		value float64
	}

	points := make([]point, 0, len(asset.Valuations)+1)
	if !asset.PurchaseDate.IsZero() {
		points = append(points, point{asset.PurchaseDate, asset.PurchasePrice})
	}
	for _, valuation := range asset.Valuations {
		points = append(points, point{valuation.Date, valuation.Value})
	}

	sort.Slice(points, func(i, j int) bool { return points[i].date.Before(points[j].date) })

	series := AssetSeries{
		Labels: make([]string, len(points)),
		Data:   make([]float64, len(points)),
	}
	for i, p := range points {
		series.Labels[i] = p.date.Format("Jan 06")
		series.Data[i] = p.value
	}
	return series
}

// parseAssetDate reads a date from the form, falling back to today. The input is
// a native date field, so anything unparseable means the field was left empty.
func parseAssetDate(raw string) time.Time {
	if raw == "" {
		return time.Now()
	}
	parsed, err := time.Parse("2006-01-02", raw)
	if err != nil {
		return time.Now()
	}
	return parsed
}

func parseAssetAmount(raw string) float64 {
	value, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	if err != nil {
		return 0
	}
	return value
}

// Assets used to be a page of its own. Everything owned now sits alongside the
// bank accounts so that net worth counts the house, which is the whole point of
// tracking it, so this survives only to keep existing links working.
func (app *Application) Assets(c echo.Context) error {
	return c.Redirect(http.StatusFound, "/accounts")
}

// buildAssetSummaries loads every asset, annotated and ordered by value.
func (app *Application) buildAssetSummaries() ([]AssetSummary, error) {
	assets, err := app.store.ReadAssets()
	if err != nil {
		return nil, err
	}

	summaries := make([]AssetSummary, 0, len(assets))
	for _, asset := range assets {
		summaries = append(summaries, BuildAssetSummary(asset))
	}
	// Most valuable first, with the name breaking ties so the order is stable
	// between renders rather than following the bucket's key order.
	sort.Slice(summaries, func(i, j int) bool {
		if summaries[i].Current == summaries[j].Current {
			return summaries[i].Asset.Name < summaries[j].Asset.Name
		}
		return summaries[i].Current > summaries[j].Current
	})
	return summaries, nil
}

func (app *Application) AssetCreate(c echo.Context) error {

	name := strings.TrimSpace(c.FormValue("name"))
	if name == "" {
		app.Toast(c, "Error", "Give the asset a name.")
		return echo.NewHTTPError(http.StatusBadRequest, "name is required")
	}

	assetType := c.FormValue("type")
	if AssetTypeLabel(assetType) == "Asset" {
		assetType = "other"
	}

	asset := models.Asset{
		Id:            newID(),
		Name:          name,
		Type:          assetType,
		Description:   strings.TrimSpace(c.FormValue("description")),
		PurchasePrice: parseAssetAmount(c.FormValue("purchase_price")),
		PurchaseDate:  parseAssetDate(c.FormValue("purchase_date")),
		CreatedAt:     time.Now(),

		HomesPropertyID: strings.TrimSpace(c.FormValue("homes_property_id")),
	}

	// A URL that is not a OneRoof property page is dropped rather than stored:
	// it is fetched server side later, so it must not point anywhere else.
	if raw := strings.TrimSpace(c.FormValue("oneroof_url")); property.ValidOneRoofURL(raw) {
		asset.OneRoofURL = raw
	}

	if err := app.store.CreateAsset(asset); err != nil {
		app.Toast(c, "Error", "Could not save the asset.")
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	return app.Accounts(c)
}

func (app *Application) Asset(c echo.Context) error {

	asset, err := app.store.GetAsset(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "asset not found")
	}

	// Newest first: the most recent valuation is the one being checked.
	history := asset.SortedValuations()
	for i, j := 0, len(history)-1; i < j; i, j = i+1, j-1 {
		history[i], history[j] = history[j], history[i]
	}

	return c.Render(http.StatusOK, "asset", AssetPageProps{
		Summary: BuildAssetSummary(asset),
		Series:  BuildAssetSeries(asset),
		History: history,
		Types:   assetTypes,
		Today:   time.Now().Format("2006-01-02"),
	})
}

func (app *Application) AssetAddValuation(c echo.Context) error {

	id := c.Param("id")
	value := parseAssetAmount(c.FormValue("value"))
	if value <= 0 {
		app.Toast(c, "Error", "Enter a value above zero.")
		return echo.NewHTTPError(http.StatusBadRequest, "a value is required")
	}

	valuation := models.AssetValuation{
		ID:    newID(),
		Date:  parseAssetDate(c.FormValue("date")),
		Value: value,
		Note:  strings.TrimSpace(c.FormValue("note")),
	}

	if err := app.store.AddAssetValuation(id, valuation); err != nil {
		app.Toast(c, "Error", "Could not save the valuation.")
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	return app.Asset(c)
}

func (app *Application) AssetDeleteValuation(c echo.Context) error {
	if err := app.store.DeleteAssetValuation(c.Param("id"), c.Param("valuationId")); err != nil {
		app.Toast(c, "Error", "Could not remove the valuation.")
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	return app.Asset(c)
}

func (app *Application) AssetDelete(c echo.Context) error {
	if err := app.store.DeleteAsset(c.Param("id")); err != nil {
		app.Toast(c, "Error", "Could not remove the asset.")
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	return app.Accounts(c)
}

// AddressSuggest proxies the address lookup so the browser never talks to the
// property site directly: that keeps the request off a cross origin path and
// leaves the user agent and rate limiting under this application's control.
//
// A lookup failure returns an empty list rather than an error. The address field
// is an ordinary text input, so losing autocomplete costs nothing but typing.
func (app *Application) AddressSuggest(c echo.Context) error {
	query := strings.TrimSpace(c.QueryParam("q"))
	if len(query) < 3 {
		return c.JSON(http.StatusOK, []property.Suggestion{})
	}

	suggestions, err := property.New().SuggestAddresses(c.Request().Context(), query, 6)
	if err != nil {
		c.Logger().Error(err)
		return c.JSON(http.StatusOK, []property.Suggestion{})
	}

	return c.JSON(http.StatusOK, suggestions)
}

// AssetRefreshEstimate asks the valuation sources what the property is worth now
// and records the average as a valuation. Sources that do not answer narrow the
// average rather than failing the request, and nothing is recorded if none do.
func (app *Application) AssetRefreshEstimate(c echo.Context) error {

	id := c.Param("id")
	asset, err := app.store.GetAsset(id)
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "asset not found")
	}

	if !asset.Trackable() {
		app.Toast(c, "Error", "Add an address or a OneRoof link to this asset first.")
		return echo.NewHTTPError(http.StatusBadRequest, "no valuation source configured")
	}

	result := property.New().Estimate(c.Request().Context(), asset.HomesPropertyID, asset.OneRoofURL)
	if len(result.Estimates) == 0 {
		app.Toast(c, "Error", "No source returned an estimate. Enter one by hand instead.")
		return app.Asset(c)
	}

	sources := make([]string, 0, len(result.Estimates))
	for _, estimate := range result.Estimates {
		sources = append(sources, fmt.Sprintf("%s %s", estimate.Source, formatShort(estimate.Value)))
	}

	valuation := models.AssetValuation{
		ID:    newID(),
		Date:  time.Now(),
		Value: result.Average,
		Note:  strings.Join(sources, " · "),
	}

	if err := app.store.AddAssetValuation(id, valuation); err != nil {
		app.Toast(c, "Error", "Could not save the estimate.")
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	if len(result.Failed) > 0 {
		app.Toast(c, "Warning", fmt.Sprintf("Averaged %d of %d sources. No answer from %s.",
			len(result.Estimates), len(result.Estimates)+len(result.Failed), strings.Join(result.Failed, ", ")))
	}

	return app.Asset(c)
}

// formatShort renders a valuation the way the property sites publish it, since
// that is the precision they actually offer.
func formatShort(value float64) string {
	if value >= 1_000_000 {
		return fmt.Sprintf("$%.2fM", value/1_000_000)
	}
	return fmt.Sprintf("$%.0fK", value/1_000)
}
