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

func setCategory(t *testing.T, app *Application, id string, values url.Values) *httptest.ResponseRecorder {
	t.Helper()
	c, rec := postForm(t, app, values)
	c.Request().Header.Set("HX-Request", "true")
	c.SetParamNames("id")
	c.SetParamValues(id)
	require.NoError(t, app.TransactionSetCategory(c))
	return rec
}

func TestCategorisingOneTransaction(t *testing.T) {
	app := newTestApp(t)
	recent := time.Now().Add(-48 * time.Hour)
	for _, tx := range []models.Transaction{
		spendTx("cylinder", "PAYMENT", "", "", "", "PAY The Cylinder Guy Limited", -1200, recent),
		spendTx("cylinder-2", "PAYMENT", "", "", "", "PAY The Cylinder Guy Limited", -80, recent),
		spendTx("plumber", "EFTPOS", "Household", "Plumbing and gasfitting supplies and services", "Plumbing World", "", -40, recent),
	} {
		require.NoError(t, app.store.CreateTransaction(tx))
	}

	t.Run("the form says where the category came from", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/?layout=d", nil)
		req.Header.Set("HX-Request", "true")
		rec := httptest.NewRecorder()
		c := app.http.NewContext(req, rec)
		c.SetParamNames("id")
		c.SetParamValues("plumber")
		require.NoError(t, app.TransactionCategoryForm(c))
		assert.Contains(t, rec.Body.String(), "· from Akahu")
		assert.Contains(t, rec.Body.String(), "All transactions containing “Plumbing World”")
	})

	t.Run("just this one is filed by an override", func(t *testing.T) {
		rec := setCategory(t, app, "cylinder", url.Values{"category": {"Plumbing and gasfitting supplies and services"}})
		assert.Contains(t, rec.Body.String(), `id="txcats-d-cylinder" hx-swap-oob="true"`, "the row's badges update in place")
		assert.Contains(t, rec.Body.String(), "Plumbing and gasfitting supplies and services")

		tx, err := app.store.GetTransaction("cylinder")
		require.NoError(t, err)
		assert.Equal(t, "Household", tx.Category.Groups.PersonalFinance.Name)
		other, err := app.store.GetTransaction("cylinder-2")
		require.NoError(t, err)
		assert.Empty(t, other.Category.Name, "a one-off leaves its look-alikes alone")
	})

	t.Run("undo puts the original category back", func(t *testing.T) {
		setCategory(t, app, "cylinder", url.Values{"clear": {"1"}})
		tx, err := app.store.GetTransaction("cylinder")
		require.NoError(t, err)
		assert.Empty(t, tx.Category.Name)
	})

	t.Run("all like it adds a rule from its text", func(t *testing.T) {
		rec := setCategory(t, app, "cylinder", url.Values{"category": {"Plumbing and gasfitting supplies and services"}, "scope": {"like"}})
		assert.Equal(t, "true", rec.Header().Get("HX-Refresh"), "a rule can change many rows, so the page is redrawn")

		settings, err := app.store.GetCategorySettings()
		require.NoError(t, err)
		require.Len(t, settings.Rules, 1)
		assert.Equal(t, "PAY The Cylinder Guy Limited", settings.Rules[0].Contains)
		assert.Empty(t, settings.Overrides)

		other, err := app.store.GetTransaction("cylinder-2")
		require.NoError(t, err)
		assert.Equal(t, "Plumbing and gasfitting supplies and services", other.Category.Name)
	})

	t.Run("an unknown category is refused", func(t *testing.T) {
		c, _ := postForm(t, app, url.Values{"category": {"Nowhere"}})
		c.SetParamNames("id")
		c.SetParamValues("plumber")
		err := app.TransactionSetCategory(c)
		require.Error(t, err)
	})
}

func TestMerchantTagsLeadToTransactionsAndRules(t *testing.T) {
	row := buildSetupRowData(models.BudgetItem{Category: "Payments"}, 105, 1_000)
	row.Merchants = []string{"PAY The Cylinder Guy Limited"}
	html := renderTemplate(t, "budget.setup.row", row)
	assert.Contains(t, html, `href="/transactions?search=PAY%20The%20Cylinder%20Guy%20Limited"`)
	assert.Contains(t, html, `href="/settings?rule=PAY%20The%20Cylinder%20Guy%20Limited#rules"`)

	settings := renderTemplate(t, "settings.categories", CategoriesView{Prefill: "PAY The Cylinder Guy Limited"})
	assert.Contains(t, settings, `value="PAY The Cylinder Guy Limited" autofocus`)
	assert.Contains(t, settings, `hx-trigger="load, input changed`, "the preview shows straight away")
}
