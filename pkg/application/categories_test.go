package application

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/TheQueenIsDead/budge/pkg/database/models"
	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCategoryRulesEndToEnd(t *testing.T) {
	app := newTestApp(t)
	recent := time.Now().Add(-48 * time.Hour)
	require.NoError(t, app.store.CreateTransaction(t.Context(), spendTx("interest", "LOAN", "", "", "", "LOAN INTEREST", -400, recent)))
	require.NoError(t, app.store.CreateTransaction(t.Context(), spendTx("rates", "DEBIT", "Housing", "Local government", "CCC", "", -860, recent)))

	post := func(handler echo.HandlerFunc, values url.Values) string {
		c, rec := postForm(t, app, values)
		require.NoError(t, handler(c))
		require.Equal(t, http.StatusOK, rec.Code)
		return rec.Body.String()
	}

	body := post(app.SettingsAddRule, url.Values{"contains": {"loan interest"}, "type": {"LOAN"}, "category": {"mortgage"}})
	assert.Contains(t, body, "1 transaction\n", "the rule reports what it matched")
	assert.Regexp(t, `id="category-home" open`, body, "the category the rule went into stays open")

	settings, err := app.store.GetCategorySettings(t.Context())
	require.NoError(t, err)
	require.Len(t, settings.Rules, 1)
	assert.Equal(t, "mortgage", settings.Rules[0].Category)

	setup, err := app.buildSetupData(t.Context())
	require.NoError(t, err)
	assert.InDelta(t, 1260.0/untargetedWindowWeeks, rowByKey(t, setup, "home").ActualWeekly, 0.001, "the interest joins the rates under Home")
	assert.Zero(t, rowByKey(t, setup, "loan-interest").ActualWeekly)

	t.Run("deleting the rule puts it back where Akahu's category goes", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodDelete, "/", nil)
		rec := httptest.NewRecorder()
		c := app.http.NewContext(req, rec)
		c.SetPathValues(echo.PathValues{{Name: "id", Value: settings.Rules[0].ID}})
		require.NoError(t, app.SettingsDeleteRule(c))
		assert.Regexp(t, `id="category-home" open`, rec.Body.String())

		setup, err := app.buildSetupData(t.Context())
		require.NoError(t, err)
		assert.InDelta(t, 400.0/untargetedWindowWeeks, rowByKey(t, setup, "loan-interest").ActualWeekly, 0.001)
	})
}

func TestSettingsAddRuleRefusals(t *testing.T) {
	app := newTestApp(t)
	tests := []struct {
		name     string
		values   url.Values
		expected string
	}{
		{"a category is required", url.Values{"contains": {"LOAN INTEREST"}, "category": {"Nowhere"}}, "Pick a category"},
		{"text to match is required", url.Values{"contains": {"  "}, "category": {"fuel"}}, "needs some text"},
		{"the amounts must leave a range", url.Values{"contains": {"AA INSURANCE"}, "category": {"insurance"}, "at_least": {"2000"}, "at_most": {"1000"}}, "smallest amount is above the largest"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			c, rec := postForm(t, app, test.values)
			require.NoError(t, app.SettingsAddRule(c))
			assert.Contains(t, rec.Body.String(), test.expected)

			settings, err := app.store.GetCategorySettings(t.Context())
			require.NoError(t, err)
			assert.Empty(t, settings.Rules)
		})
	}
}

func TestSettingsCategoriesRenders(t *testing.T) {
	app := newTestApp(t)
	require.NoError(t, app.store.CreateTransaction(t.Context(), spendTx("bp", "EFTPOS", "Transport", "Fuel stations", "BP", "", -80, time.Now().Add(-time.Hour))))
	require.NoError(t, app.store.SaveCategorySettings(t.Context(), models.CategorySettings{Rules: []models.CategoryRule{
		{ID: "r1", Contains: "LOAN INTEREST", Type: "LOAN", Category: "mortgage"},
		{ID: "r2", Contains: "Holland Suzuk", Category: "servicing"},
	}}))
	view, err := app.buildCategoriesView(t.Context(), "transport")
	require.NoError(t, err)
	view.Error = "Something was refused."
	html := renderTemplate(t, "settings.categories", view)

	// A category's markup runs to the next category, or the end of the card.
	section := func(key string) string {
		t.Helper()
		start := indexOf(t, html, `id="category-`+key+`"`)
		rest := html[start+1:]
		if end := strings.Index(rest, `<details class="b-pv-cat`); end >= 0 {
			return html[start : start+1+end]
		}
		return html[start:]
	}

	assert.Contains(t, html, "Something was refused.")
	assert.True(t, strings.HasPrefix(strings.TrimSpace(html), `<div id="categories">`), "the card is its own swap target")
	assert.NotContains(t, html, `hx-target="#categories" hx-swap="innerHTML"`,
		"emptying the card before refilling it shortened the page, and the browser scrolled to the bottom")
	assert.Contains(t, section("home"), `hx-delete="/settings/rules/r1"`, "a rule is listed under the category it files into")
	assert.Contains(t, section("home"), "Mortgage", "and says which subcategory")
	assert.Contains(t, section("home"), "text-warning", "a rule matching nothing is flagged")
	assert.Contains(t, section("transport"), `hx-delete="/settings/rules/r2"`)
	assert.Contains(t, section("transport"), "1 transaction", "what is filed under it is counted")
	assert.Regexp(t, `id="category-transport" open`, html, "the category being worked on stays open")

	t.Run("each category adds rules into itself", func(t *testing.T) {
		transport := section("transport")
		assert.Contains(t, transport, `id="rule-contains-transport"`)
		assert.Contains(t, transport, `<option value="fuel">Fuel</option>`, "or one of its subcategories")
		assert.Contains(t, transport, `from:#rule-contains-transport`)
		assert.Contains(t, transport, `hx-target="this"`, "the preview must not inherit the form's target and replace the card")
		assert.Contains(t, section("groceries"), `<input type="hidden" name="category" value="groceries">`, "with nothing finer, no picker")
	})

	t.Run("uncategorised takes no rules of its own", func(t *testing.T) {
		assert.NotContains(t, section("uncategorised"), `hx-post="/settings/rules"`)
		assert.NotContains(t, section("uncategorised"), `hx-delete="/settings/budget-categories/uncategorised"`, "nor can it be deleted")
		assert.NotContains(t, section("uncategorised"), `id="cat-group-uncategorised"`, "or moved")
		assert.Contains(t, section("uncategorised"), "/transactions?uncategorised=1")
	})

	assert.NotContains(t, html, `/settings/categories`, "the old categories of your own are gone")

	t.Run("each category can be edited and deleted", func(t *testing.T) {
		transport := section("transport")
		assert.Contains(t, transport, `hx-post="/settings/budget-categories/transport" hx-trigger="change" hx-swap="none"`, "saved as it is changed")
		assert.Contains(t, transport, `x-model="label"`, "the heading follows the name as it is typed")
		assert.Contains(t, transport, `x-text="label"`)
		assert.Contains(t, transport, `$el.classList.add('b-accent-' + accent)`, "the colour is swapped, not added to")
		assert.NotContains(t, transport, `:class="'b-accent-' + accent"`)
		assert.Contains(t, transport, `x-model="subs[0]"`)
		assert.NotContains(t, transport, "Save details")
		assert.Contains(t, transport, `value="bi-car-front" checked`, "its icon is picked")
		assert.Contains(t, transport, `value="clay" checked`, "and its colour")
		assert.Contains(t, transport, `hx-delete="/settings/budget-categories/fuel"`, "subcategories can be deleted")
		assert.Contains(t, transport, `hx-delete="/settings/budget-categories/transport"`)
		assert.Contains(t, transport, `<input type="hidden" name="parent" value="transport">`, "and added to")
		assert.Equal(t, 1, strings.Count(html, `hx-post="/settings/budget-categories" hx-vals`), "one button adds a category")
		assert.Contains(t, html, `hx-swap="outerHTML show:details.b-pv-cat[open]:top"`, "and shows the new one, which opens above it")
		assert.Contains(t, html, `hx-vals='{"label": "New category"}'`, "named for you, to change after")
		assert.Contains(t, transport, `hx-vals='{"budget": "off", "render": "card"}'`, "each category can be taken off budget")
		assert.NotContains(t, section("uncategorised"), `"budget": "on"`, "but not Uncategorised")
	})
}

func TestRuleSuggestions(t *testing.T) {
	now := time.Now()
	txs := []models.Transaction{
		spendTx("1", "EFTPOS", "Food", "Supermarkets", "Countdown", "POS W/D COUNTDOWN", -50, now),
		spendTx("2", "EFTPOS", "Food", "Supermarkets", "Countdown", "", -60, now),
		spendTx("3", "EFTPOS", "", "", "", "POS W/D Holland Suzuk-13:04", -330, now),
		spendTx("4", "LOAN", "", "", "", "LOAN INTEREST", -410, now),
		spendTx("5", "LOAN", "", "", "", "LOAN INTEREST", -356, now),
		// Sync files both legs of a transfer under "Transfer".
		spendTx("6", "TRANSFER", "Transfer", "Transfer", "", "AP#19781426 TO D J KING", -641, now),
		spendTx("6b", "TRANSFER", "Transfer", "Transfer", "", "AP#19781426 FROM D J KING", 641, now),
		spendTx("7", "STANDING ORDER", "", "", "", "AP#1 TO FISHER FUNDS", -385, now),
	}
	settings := models.CategorySettings{Rules: []models.CategoryRule{{Contains: "fisher"}}}

	got := ruleSuggestions(txs, settings)

	assert.Equal(t, []string{"Holland Suzuk", "Countdown", "LOAN INTEREST", "AP#19781426 TO D J KING"}, got,
		"uncategorised first, then by how often; money arriving by transfer and already-ruled text are left out")
	for _, text := range got {
		matched := false
		for _, tx := range txs {
			if (models.CategoryRule{Contains: text, IncludeTransfers: true}).Matches(tx) {
				matched = true
			}
		}
		assert.True(t, matched, "%q must match the transaction it came from", text)
	}
}

func TestBuildRulePreview(t *testing.T) {
	base := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	var txs []models.Transaction
	for i := 0; i < 7; i++ {
		txs = append(txs, spendTx(string(rune('a'+i)), "LOAN", "", "", "", "LOAN INTEREST", -400, base.AddDate(0, 0, 14*i)))
	}
	txs = append(txs, spendTx("fee", "FEE", "", "", "", "LOAN ESTABLISHMENT FEE", -200, base))
	settings := models.CategorySettings{Rules: []models.CategoryRule{{Contains: "establishment"}}}

	preview := buildRulePreview(models.CategoryRule{Contains: "loan"}, txs, settings)

	assert.Equal(t, 7, preview.Matches)
	assert.Equal(t, 1, preview.Taken, "the fee is already filed by an earlier rule")
	require.Len(t, preview.Samples, rulePreviewSamples)
	assert.True(t, preview.Samples[0].Date.After(preview.Samples[1].Date), "most recent first")
	assert.Contains(t, renderTemplate(t, "settings.rules.preview", preview), "· Loan interest", "samples say where each is filed now")

	none := buildRulePreview(models.CategoryRule{Contains: "nothing like this"}, txs, settings)
	assert.Contains(t, renderTemplate(t, "settings.rules.preview", none), "Matches nothing yet")
}

func TestSettingsPreviewRule(t *testing.T) {
	app := newTestApp(t)
	require.NoError(t, app.store.CreateTransaction(t.Context(), spendTx("interest", "LOAN", "", "", "", "LOAN INTEREST", -410, time.Now().Add(-time.Hour))))

	get := func(query string) string {
		req := httptest.NewRequest(http.MethodGet, "/settings/rules/preview?"+query, nil)
		req.Header.Set("HX-Request", "true")
		rec := httptest.NewRecorder()
		require.NoError(t, app.SettingsPreviewRule(app.http.NewContext(req, rec)))
		return rec.Body.String()
	}

	assert.Contains(t, get("contains=loan+interest&type=LOAN"), "1 transaction")
	assert.Contains(t, get("contains=loan+interest&type=DEBIT"), "Matches nothing yet", "the type narrows the preview")
	assert.Empty(t, strings.TrimSpace(get("contains=+")), "nothing typed shows nothing")
}

func TestRulesTellPaymentsApartByAmount(t *testing.T) {
	app := newTestApp(t)
	recent := time.Now().Add(-48 * time.Hour)
	for _, tx := range []models.Transaction{
		spendTx("house", "DEBIT", "", "", "AA Insurance", "AA INSURANCE AUCKLAND", -2152.84, recent),
		spendTx("car", "DEBIT", "", "", "AA Insurance", "AA INSURANCE AUCKLAND", -493.18, recent),
		spendTx("monthly", "DIRECT DEBIT", "", "", "AA Insurance", "Direct Debit -AA INSURANCE LTD", -81.90, recent),
	} {
		require.NoError(t, app.store.CreateTransaction(t.Context(), tx))
	}
	preview := func(query string) string {
		req := httptest.NewRequest(http.MethodGet, "/settings/rules/preview?"+query, nil)
		req.Header.Set("HX-Request", "true")
		rec := httptest.NewRecorder()
		require.NoError(t, app.SettingsPreviewRule(app.http.NewContext(req, rec)))
		return rec.Body.String()
	}
	page := preview("contains=AA+INSURANCE+AUCKLAND&at_least=1500")
	assert.Contains(t, page, `Would file <span class="fw-semibold">1 transaction<`)
	assert.Contains(t, page, "1 more transaction matches the text, but its amount is outside the rule's",
		"the form says what the amounts leave out")

	add := func(values url.Values) string {
		c, rec := postForm(t, app, values)
		require.NoError(t, app.SettingsAddRule(c))
		return rec.Body.String()
	}
	body := add(url.Values{"contains": {"AA INSURANCE AUCKLAND"}, "category": {"insurance"}, "at_least": {"1500"}})
	assert.Contains(t, body, "$1,500.00 or more", "a rule says the amounts it matches")
	body = add(url.Values{"contains": {"AA INSURANCE AUCKLAND"}, "category": {"rego"}, "at_most": {"-1499.99"}})
	assert.Contains(t, body, "up to $1,499.99", "a sign is ignored, as an amount is a size")

	filed := map[string]string{}
	txs, err := app.store.ReadTransactions(t.Context())
	require.NoError(t, err)
	for _, tx := range txs {
		filed[tx.Id] = tx.BudgetCategory()
	}
	assert.Equal(t, "insurance", filed["house"])
	assert.Equal(t, "rego", filed["car"], "the same text, told apart by its amount")
	assert.NotEqual(t, "rego", filed["monthly"])
}

func TestTransferRuleBudgetsMortgageRepayments(t *testing.T) {
	app := newTestApp(t)
	recent := time.Now().Add(-48 * time.Hour)
	for _, tx := range []models.Transaction{
		spendTx("out", "TRANSFER", "Transfer", "Transfer", "", "AP#19781426 TO D J KING", -641, recent),
		spendTx("in", "TRANSFER", "Transfer", "Transfer", "", "AP#19781426 FROM D J KING", 641, recent),
		spendTx("interest", "LOAN", "", "", "", "LOAN INTEREST", -400, recent),
	} {
		require.NoError(t, app.store.CreateTransaction(t.Context(), tx))
	}
	preview := func(query string) string {
		req := httptest.NewRequest(http.MethodGet, "/settings/rules/preview?"+query, nil)
		req.Header.Set("HX-Request", "true")
		rec := httptest.NewRecorder()
		require.NoError(t, app.SettingsPreviewRule(app.http.NewContext(req, rec)))
		return rec.Body.String()
	}
	assert.Contains(t, preview("contains=AP%2319781426"), "1 transfer between your accounts also matches",
		"the form says why a repayment is missing")
	assert.Contains(t, preview("contains=AP%2319781426&include_transfers=1"), "Would file <span class=\"fw-semibold\">1 transaction<",
		"only the leg where money leaves is filed")

	c, _ := postForm(t, app, url.Values{"contains": {"AP#19781426"}, "category": {"mortgage"}, "include_transfers": {"1"}})
	require.NoError(t, app.SettingsAddRule(c))

	setup, err := app.buildSetupData(t.Context())
	require.NoError(t, err)
	assert.InDelta(t, 641.0/untargetedWindowWeeks, rowByKey(t, setup, "home").ActualWeekly, 0.001, "the repayment is counted once")
	assert.InDelta(t, 641, countedTotal(t, app), 0.001, "interest is not spending, and the repayment is counted once")
}

func TestEditingBudgetCategories(t *testing.T) {
	app := newTestApp(t)
	t.Cleanup(func() { models.UseBudgetCategories(models.BudgetCategorySettings{}) })
	recent := time.Now().Add(-48 * time.Hour)
	require.NoError(t, app.store.CreateTransaction(t.Context(), spendTx("cafe", "EFTPOS", "Lifestyle", "Cafes and restaurants", "Fat Sallys", "", -30, recent)))
	require.NoError(t, app.store.CreateTransaction(t.Context(), spendTx("pak", "EFTPOS", "Food", "Supermarkets and grocery stores", "PAK'nSAVE", "", -120, recent)))
	require.NoError(t, app.store.CreateBudgetItem(t.Context(), models.BudgetItem{ID: "out", BudgetCategory: "eating-out", Amount: 50, Frequency: "weekly"}))
	require.NoError(t, app.store.CreateBudgetItem(t.Context(), models.BudgetItem{ID: "shop", BudgetCategory: "groceries", Amount: 150, Frequency: "weekly"}))

	sendFor := func(method string, handler echo.HandlerFunc, key string, values url.Values) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, "/?"+values.Encode(), strings.NewReader(values.Encode()))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationForm)
		rec := httptest.NewRecorder()
		c := app.http.NewContext(req, rec)
		if key != "" {
			c.SetPathValues(echo.PathValues{{Name: "key", Value: key}})
		}
		require.NoError(t, handler(c))
		return rec
	}
	send := func(method, path string, handler echo.HandlerFunc, key string, values url.Values) string {
		t.Helper()
		rec := sendFor(method, handler, key, values)
		require.Equal(t, http.StatusOK, rec.Code)
		return rec.Body.String()
	}

	t.Run("renaming keeps its target and transactions", func(t *testing.T) {
		rec := sendFor(http.MethodPost, app.SettingsUpdateBudgetCategory, "groceries", url.Values{"label": {"Food shopping"}, "icon": {"bi-bag"}, "accent": {"plum"}})
		assert.Equal(t, http.StatusNoContent, rec.Code, "saved quietly: the page already shows it, and a redraw would take the field away")
		assert.Empty(t, rec.Body.String())

		setup, err := app.buildSetupData(t.Context())
		require.NoError(t, err)
		row := rowByKey(t, setup, "groceries")
		assert.Equal(t, "Food shopping", row.Label)
		assert.True(t, row.HasTarget)
		assert.InDelta(t, 120.0/4, row.ActualWeekly, 0.001)

		tile, ok := SpendGroupByKey("groceries")
		require.True(t, ok)
		assert.Equal(t, "Food shopping", tile.Label, "the dashboard tile follows the new name")
	})

	t.Run("a new category can be added and filed into", func(t *testing.T) {
		body := send(http.MethodPost, "/", app.SettingsAddBudgetCategory, "", url.Values{"label": {"New category"}})
		assert.Regexp(t, `id="category-new-category" open`, body, "drawn open, ready to be named")
		kids, _ := models.BudgetCategoryByKey("new-category")
		assert.True(t, kids.Budgeted(), "on budget, so its spending counts from the start")
		assert.Equal(t, http.StatusNoContent, sendFor(http.MethodPost, app.SettingsUpdateBudgetCategory, "new-category", url.Values{"label": {"Kids"}}).Code)
		kids, ok := models.BudgetCategoryByKey("new-category")
		require.True(t, ok)
		assert.Equal(t, "Kids", kids.Label)

		send(http.MethodPost, "/", app.SettingsAddRule, "", url.Values{"contains": {"Fat Sallys"}, "category": {"new-category"}})
		setup, err := app.buildSetupData(t.Context())
		require.NoError(t, err)
		assert.InDelta(t, 30.0/13, rowByKey(t, setup, "new-category").ActualWeekly, 0.001)
	})

	t.Run("taking it off budget redraws the card", func(t *testing.T) {
		body := send(http.MethodPost, "/", app.SettingsUpdateBudgetCategory, "groceries", url.Values{"label": {"Food shopping"}, "budget": {"off"}, "render": {"card"}})
		assert.Regexp(t, `id="category-groceries" open`, body)
		groceries, _ := models.BudgetCategoryByKey("groceries")
		assert.False(t, groceries.Budgeted())
		send(http.MethodPost, "/", app.SettingsUpdateBudgetCategory, "groceries", url.Values{"label": {"Food shopping"}, "budget": {"on"}, "render": {"card"}})
	})

	t.Run("a refused quiet save redraws the card to put the name back", func(t *testing.T) {
		rec := sendFor(http.MethodPost, app.SettingsUpdateBudgetCategory, "groceries", url.Values{"label": {"  "}})
		assert.Equal(t, "#categories", rec.Header().Get("HX-Retarget"))
		assert.Equal(t, "outerHTML", rec.Header().Get("HX-Reswap"))
		assert.Contains(t, rec.Body.String(), "A category needs a name.")
		assert.Contains(t, rec.Body.String(), `value="Food shopping"`)
	})

	t.Run("a refused edit says why and changes nothing", func(t *testing.T) {
		body := send(http.MethodPost, "/", app.SettingsAddBudgetCategory, "", url.Values{"label": {" "}, "group": {"everyday"}})
		assert.Contains(t, body, "A category needs a name.")
		body = send(http.MethodDelete, "/", app.SettingsDeleteBudgetCategory, "uncategorised", url.Values{"into": {"other"}})
		assert.Contains(t, body, "so it stays")
	})

	t.Run("deleting moves its transactions, and its target goes with it", func(t *testing.T) {
		send(http.MethodPost, "/", app.SettingsAddRule, "", url.Values{"contains": {"Fat Sallys"}, "category": {"eating-out"}})
		send(http.MethodDelete, "/", app.SettingsDeleteBudgetCategory, "eating-out", url.Values{"into": {"groceries"}})

		setup, err := app.buildSetupData(t.Context())
		require.NoError(t, err)
		for _, row := range setup.Rows {
			assert.NotEqual(t, "eating-out", row.Key)
		}
		groceries := rowByKey(t, setup, "groceries")
		assert.InDelta(t, 150, groceries.Target, 0.001, "the target it went into keeps its own, not the deleted one's")

		tx, err := app.store.GetTransaction(t.Context(), "cafe")
		require.NoError(t, err)
		assert.Equal(t, "new-category", tx.BudgetCategory(), "the first rule still wins")

		settings, err := app.store.GetCategorySettings(t.Context())
		require.NoError(t, err)
		assert.Equal(t, "groceries", settings.Rules[1].Target(), "a rule naming the deleted category follows its transactions")
	})
}
