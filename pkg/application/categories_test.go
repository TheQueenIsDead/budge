package application

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/TheQueenIsDead/budge/pkg/database/models"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCategoryRulesEndToEnd(t *testing.T) {
	app := newTestApp(t)
	recent := time.Now().Add(-48 * time.Hour)
	require.NoError(t, app.store.CreateTransaction(spendTx("interest", "LOAN", "", "", "", "LOAN INTEREST", -400, recent)))
	require.NoError(t, app.store.CreateTransaction(spendTx("rates", "DEBIT", "Housing", "Local government", "CCC", "", -860, recent)))

	post := func(handler echo.HandlerFunc, values url.Values) string {
		c, rec := postForm(t, app, values)
		require.NoError(t, handler(c))
		require.Equal(t, http.StatusOK, rec.Code)
		return rec.Body.String()
	}

	body := post(app.SettingsAddCategory, url.Values{"broad": {"General"}, "specific": {"Housing"}})
	assert.Contains(t, body, "General / <span class=\"fw-semibold\">Housing</span>")

	body = post(app.SettingsAddRule, url.Values{"contains": {"loan interest"}, "type": {"LOAN"}, "category": {"housing"}})
	assert.Contains(t, body, "· 1 transaction\n", "the rule reports what it matched")

	settings, err := app.store.GetCategorySettings()
	require.NoError(t, err)
	require.Len(t, settings.Rules, 1)
	assert.Equal(t, "General", settings.Rules[0].Broad, "the group comes from the chosen category")
	assert.Equal(t, "Housing", settings.Rules[0].Specific)

	setup, err := app.buildSetupData()
	require.NoError(t, err)
	general := groupNamed(t, setup, "General")
	require.Len(t, general.Rows, 1)
	assert.Equal(t, "Housing", general.Rows[0].SubCategory)
	assert.InDelta(t, 100, general.Rows[0].ActualWeekly, 0.001)
	for _, group := range setup.Groups {
		assert.NotEqual(t, uncategorisedGroup, group.Name, "the rule empties the uncategorised group")
	}

	t.Run("deleting the rule sends the spend back to uncategorised", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodDelete, "/", nil)
		rec := httptest.NewRecorder()
		c := app.http.NewContext(req, rec)
		c.SetParamNames("id")
		c.SetParamValues(settings.Rules[0].ID)
		require.NoError(t, app.SettingsDeleteRule(c))

		setup, err := app.buildSetupData()
		require.NoError(t, err)
		groupNamed(t, setup, uncategorisedGroup)
	})
}

func TestSettingsAddCategoryRefusals(t *testing.T) {
	app := newTestApp(t)
	require.NoError(t, app.store.CreateTransaction(spendTx("rates", "DEBIT", "Housing", "Local government", "CCC", "", -860, time.Now().Add(-time.Hour))))

	tests := []struct {
		name     string
		values   url.Values
		expected string
	}{
		{"both parts are required", url.Values{"broad": {"General"}}, "needs both a group and a name"},
		{"a name Akahu already uses is refused", url.Values{"broad": {"General"}, "specific": {"local government"}}, "already exists under Housing"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			c, rec := postForm(t, app, test.values)
			require.NoError(t, app.SettingsAddCategory(c))
			assert.Contains(t, rec.Body.String(), test.expected)

			settings, err := app.store.GetCategorySettings()
			require.NoError(t, err)
			assert.Empty(t, settings.Categories)
		})
	}
}

func TestSettingsAddRuleNeedsAKnownCategory(t *testing.T) {
	app := newTestApp(t)
	c, rec := postForm(t, app, url.Values{"contains": {"LOAN INTEREST"}, "category": {"Nowhere"}})
	require.NoError(t, app.SettingsAddRule(c))

	assert.Contains(t, rec.Body.String(), "Pick a category")
	settings, err := app.store.GetCategorySettings()
	require.NoError(t, err)
	assert.Empty(t, settings.Rules)
}

func TestSettingsCategoriesRenders(t *testing.T) {
	html := renderTemplate(t, "settings.categories", CategoriesView{
		Custom: []models.CustomCategory{{ID: "c1", Broad: "General", Specific: "Housing"}},
		Rules:  []RuleView{{CategoryRule: models.CategoryRule{ID: "r1", Contains: "LOAN INTEREST", Type: "LOAN", Broad: "General", Specific: "Housing"}}},
		Groups: []CategoryOptionGroup{{Broad: "General", Options: []CategoryOption{{Broad: "General", Specific: "Housing", Custom: true}}}},
		Types:  []string{"LOAN"},
		Error:  "Something was refused.",
	})

	assert.Contains(t, html, "Something was refused.")
	assert.Contains(t, html, `hx-delete="/settings/rules/r1"`)
	assert.Contains(t, html, `class="text-warning ms-1"`, "a rule matching nothing is flagged")
	assert.Contains(t, html, "Housing ★")
	assert.Contains(t, html, `hx-target="this"`, "the preview must not inherit the form's target and replace the card")
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

	assert.Equal(t, []string{"LOAN INTEREST", "Holland Suzuk", "Countdown", "AP#19781426 TO D J KING"}, got,
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
	assert.Contains(t, renderTemplate(t, "settings.rules.preview", preview), "· uncategorised")

	none := buildRulePreview(models.CategoryRule{Contains: "nothing like this"}, txs, settings)
	assert.Contains(t, renderTemplate(t, "settings.rules.preview", none), "Matches nothing yet")
}

func TestSettingsPreviewRule(t *testing.T) {
	app := newTestApp(t)
	require.NoError(t, app.store.CreateTransaction(spendTx("interest", "LOAN", "", "", "", "LOAN INTEREST", -410, time.Now().Add(-time.Hour))))

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

func TestTransferRuleBudgetsMortgageRepayments(t *testing.T) {
	app := newTestApp(t)
	recent := time.Now().Add(-48 * time.Hour)
	for _, tx := range []models.Transaction{
		spendTx("out", "TRANSFER", "Transfer", "Transfer", "", "AP#19781426 TO D J KING", -641, recent),
		spendTx("in", "TRANSFER", "Transfer", "Transfer", "", "AP#19781426 FROM D J KING", 641, recent),
		spendTx("interest", "LOAN", "", "", "", "LOAN INTEREST", -400, recent),
	} {
		require.NoError(t, app.store.CreateTransaction(tx))
	}
	require.NoError(t, app.store.CreateBudgetItem(models.BudgetItem{ID: "loan", Category: "Loan", Kind: models.BudgetKindIgnore}))
	require.NoError(t, app.store.SaveCategorySettings(models.CategorySettings{
		Categories: []models.CustomCategory{{ID: "m", Broad: "General", Specific: "Mortgage"}},
	}))

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

	c, _ := postForm(t, app, url.Values{"contains": {"AP#19781426"}, "category": {"Mortgage"}, "include_transfers": {"1"}})
	require.NoError(t, app.SettingsAddRule(c))

	setup, err := app.buildSetupData()
	require.NoError(t, err)
	general := groupNamed(t, setup, "General")
	require.Len(t, general.Rows, 1)
	assert.Equal(t, "Mortgage", general.Rows[0].SubCategory)
	assert.InDelta(t, 641.0/4, general.Rows[0].ActualWeekly, 0.001, "the repayment is counted once")
	assert.InDelta(t, 641, chartTotal(t, app), 0.001, "the ignored interest is gone and the repayment counted once")
}
