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
		require.NoError(t, app.store.CreateTransaction(t.Context(), tx))
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
		rec := setCategory(t, app, "cylinder", url.Values{"category": {"trades"}})
		assert.Contains(t, rec.Body.String(), `id="txcats-d-cylinder" hx-swap-oob="true"`, "the row's badges update in place")
		assert.Contains(t, rec.Body.String(), "Trades &amp; repairs")

		tx, err := app.store.GetTransaction(t.Context(), "cylinder")
		require.NoError(t, err)
		assert.Equal(t, "trades", tx.Filed)
		assert.Equal(t, models.FiledByOwner, tx.FiledBy)
		assert.Equal(t, []string{"Maintenance", "Trades & repairs"}, tx.Categories())
		assert.Empty(t, tx.Category.Groups.PersonalFinance.Name, "Akahu's own category is kept as it came")
		other, err := app.store.GetTransaction(t.Context(), "cylinder-2")
		require.NoError(t, err)
		assert.Equal(t, models.CategoryUncategorised, other.Filed, "a one-off leaves its look-alikes alone")
	})

	t.Run("the form names where it is filed", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/?layout=d", nil)
		req.Header.Set("HX-Request", "true")
		rec := httptest.NewRecorder()
		c := app.http.NewContext(req, rec)
		c.SetParamNames("id")
		c.SetParamValues("cylinder")
		require.NoError(t, app.TransactionCategoryForm(c))
		body := rec.Body.String()
		assert.Contains(t, body, "Maintenance › Trades &amp; repairs")
		assert.Contains(t, body, "· from you")
		assert.Regexp(t, `<optgroup label="Transport">\s*<option value="transport">Transport \(general\)</option><option value="fuel">Fuel</option>`, body,
			"a category heads its subcategories, which are named plainly, after its general bucket")
		assert.Contains(t, body, `<option value="groceries">Groceries</option>`, "a category with nothing finer needs no heading")
		assert.NotContains(t, body, `<optgroup label="Groceries">`)
		assert.NotContains(t, body, "› Fuel")
		assert.NotContains(t, body, "Plumbing and gasfitting", "Akahu's categories are not shown")
	})

	t.Run("undo puts it back where Akahu's category goes", func(t *testing.T) {
		setCategory(t, app, "cylinder", url.Values{"clear": {"1"}})
		tx, err := app.store.GetTransaction(t.Context(), "cylinder")
		require.NoError(t, err)
		assert.Equal(t, models.CategoryUncategorised, tx.Filed)
	})

	t.Run("all like it adds a rule from its text", func(t *testing.T) {
		rec := setCategory(t, app, "cylinder", url.Values{"category": {"trades"}, "scope": {"like"}})
		assert.Equal(t, "true", rec.Header().Get("HX-Refresh"), "a rule can change many rows, so the page is redrawn")

		settings, err := app.store.GetCategorySettings(t.Context())
		require.NoError(t, err)
		require.Len(t, settings.Rules, 1)
		assert.Equal(t, "PAY The Cylinder Guy Limited", settings.Rules[0].Contains)
		assert.Equal(t, "trades", settings.Rules[0].Category)
		assert.Empty(t, settings.Overrides)

		other, err := app.store.GetTransaction(t.Context(), "cylinder-2")
		require.NoError(t, err)
		assert.Equal(t, "trades", other.Filed)
	})

	t.Run("an unknown category is refused", func(t *testing.T) {
		c, _ := postForm(t, app, url.Values{"category": {"Nowhere"}})
		c.SetParamNames("id")
		c.SetParamValues("plumber")
		err := app.TransactionSetCategory(c)
		require.Error(t, err)
	})
}

func TestMerchantTagsLeadToTheirTransactions(t *testing.T) {
	row := targetRow("uncategorised", 0, "", 105)
	row.Merchants = []string{"PAY The Cylinder Guy Limited"}
	html := renderTemplate(t, "budget.setup.row", row)
	assert.Contains(t, html, `href="/transactions?search=PAY%20The%20Cylinder%20Guy%20Limited"`)
	assert.NotContains(t, html, "/settings", "rules are made from the transactions page")
}

func TestTransactionsWarnAboutUncategorisedSpend(t *testing.T) {
	app := newTestApp(t)
	recent := time.Now().Add(-48 * time.Hour)
	for _, tx := range []models.Transaction{
		spendTx("cylinder", "PAYMENT", "", "", "", "PAY The Cylinder Guy Limited", -1200, recent),
		spendTx("old", "PAYMENT", "", "", "", "PAY Someone Long Ago", -500, time.Now().AddDate(0, -6, 0)),
		spendTx("groceries", "EFTPOS", "Food", "Supermarkets", "Countdown", "", -80, recent),
		spendTx("sweep", "TRANSFER", "Transfer", "Transfer", "", "TO SAVINGS", -900, recent),
		spendTx("kiwisaver", "CREDIT", "", "", "", "Employer Contributions", 300, recent),
		spendTx("cash", "ATM", "", "", "", "ATM W/D The Palms", -200, recent),
	} {
		require.NoError(t, app.store.CreateTransaction(t.Context(), tx))
	}

	get := func(query string) string {
		req := httptest.NewRequest(http.MethodGet, "/transactions?"+query, nil)
		req.Header.Set("HX-Request", "true")
		rec := httptest.NewRecorder()
		require.NoError(t, app.Transactions(app.http.NewContext(req, rec)))
		return rec.Body.String()
	}

	page := get("")
	assert.Regexp(t, `1 payment in the last 13 weeks\s+has no category</span>, \$1,200 in all`, page,
		"only recent money out with no category counts: not old spend, money in, transfers, cash or categorised spend")
	assert.Contains(t, page, `href="/transactions?uncategorised=1"`)

	assert.Regexp(t, `id="uncategorised" name="uncategorised" value="1"\s+autocomplete="off">`, page, "the toggle is off")
	assert.Contains(t, page, `change from:#uncategorised`, "and reloads the list when switched, with the other filters")

	filtered := get("uncategorised=1")
	assert.Contains(t, filtered, "Showing only spending with no category")
	assert.Contains(t, filtered, "PAY The Cylinder Guy Limited")
	assert.Contains(t, filtered, "PAY Someone Long Ago", "however old, so every one can be found and filed")
	for _, other := range []string{"Countdown", "TO SAVINGS", "Employer Contributions", "ATM W/D"} {
		assert.NotContains(t, filtered, other)
	}
	assert.Regexp(t, `id="uncategorised" name="uncategorised" value="1"\s+autocomplete="off" checked>`, filtered,
		"the toggle is on, and searching keeps it")

	setCategory(t, app, "cylinder", url.Values{"category": {"trades"}})
	assert.NotContains(t, get(""), "has no category", "once everything is filed the warning goes away")
}

func TestFilingTransactionsInBulk(t *testing.T) {
	app := newTestApp(t)
	recent := time.Now().Add(-48 * time.Hour)
	for _, tx := range []models.Transaction{
		spendTx("roof", "PAYMENT", "", "", "", "PAY Christchurch Roofing", -5465, recent),
		spendTx("cylinder", "PAYMENT", "", "", "", "PAY The Cylinder Guy Limited", -4909, recent),
		spendTx("pak", "EFTPOS", "Food", "Supermarkets and grocery stores", "PAK'nSAVE", "", -80, recent),
	} {
		require.NoError(t, app.store.CreateTransaction(t.Context(), tx))
	}
	require.NoError(t, app.store.SaveCategorySettings(t.Context(), models.CategorySettings{
		Overrides: []models.CategoryOverride{{TransactionID: "cylinder", Category: "hardware"}},
	}))

	c, rec := postForm(t, app, url.Values{"tx": {"roof", "cylinder"}, "category": {"trades"}})
	require.NoError(t, app.TransactionsSetCategory(c))
	assert.Equal(t, "true", rec.Header().Get("HX-Refresh"), "the rows can be anywhere, so the page is drawn again")
	assert.Contains(t, rec.Header().Get("Hx-Trigger"), "Filed 2 under Maintenance › Trades")

	for _, id := range []string{"roof", "cylinder"} {
		tx, err := app.store.GetTransaction(t.Context(), id)
		require.NoError(t, err)
		assert.Equal(t, "trades", tx.Filed, id)
		assert.Equal(t, models.FiledByOwner, tx.FiledBy, "as if filed by hand")
	}
	settings, err := app.store.GetCategorySettings(t.Context())
	require.NoError(t, err)
	assert.Len(t, settings.Overrides, 2, "an earlier correction is replaced, not added to")

	pak, err := app.store.GetTransaction(t.Context(), "pak")
	require.NoError(t, err)
	assert.Equal(t, "groceries", pak.Filed, "what was not ticked is left alone")

	t.Run("a category is needed", func(t *testing.T) {
		c, _ := postForm(t, app, url.Values{"tx": {"pak"}, "category": {"nowhere"}})
		assert.Error(t, app.TransactionsSetCategory(c))
	})

	t.Run("the selection bar offers it", func(t *testing.T) {
		days := GroupTransactionsByDay([]models.Transaction{spendTx("roof", "PAYMENT", "", "", "", "PAY Christchurch Roofing", -5465, recent)}, time.UTC)
		page := renderTemplate(t, "transactions", map[string]interface{}{
			"days": days, "categories": categoryPicker(), "in": 0.0, "out": -5465.0, "took": time.Millisecond, "total": 1, "showing": 1,
		})
		assert.Contains(t, page, `hx-post="/transactions/category" hx-trigger="change consume"`)
		assert.Contains(t, page, `<option value="trades">Trades &amp; repairs</option>`)
	})
}
