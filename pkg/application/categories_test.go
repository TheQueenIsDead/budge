package application

import (
	"net/http"
	"net/http/httptest"
	"net/url"
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
}
