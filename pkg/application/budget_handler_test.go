package application

import (
	"html/template"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/TheQueenIsDead/budge/pkg/database"
	"github.com/TheQueenIsDead/budge/pkg/database/models"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestApp returns an Application backed by a bolt database in a temp
// directory, so handler tests never touch a real budge.bolt.db.
func newTestApp(t *testing.T) *Application {
	t.Helper()

	t.Setenv("BUDGE_BOLT_PATH", t.TempDir())
	store, err := database.NewStore()
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	tpl, err := template.New("").Funcs(templateFuncs()).ParseGlob("../../web/templates/*.gohtml")
	require.NoError(t, err)

	e := echo.New()
	e.Renderer = &Template{templates: tpl}

	return &Application{store: store, http: e}
}

// postForm builds an echo context for a form POST, plus the recorder holding
// whatever the handler writes.
func postForm(t *testing.T, app *Application, values url.Values) (echo.Context, *httptest.ResponseRecorder) {
	t.Helper()

	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(values.Encode()))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationForm)
	rec := httptest.NewRecorder()

	return app.http.NewContext(req, rec), rec
}

// TestBudgetSaveSalaryPreservesSavingsGoal is the regression test for the bug
// where BudgetSaveSalary rebuilt BudgetSalary from scratch, silently zeroing
// the savings goal. The income card saves on every "change" event, so this
// fired whenever a user touched any income field.
func TestBudgetSaveSalaryPreservesSavingsGoal(t *testing.T) {
	app := newTestApp(t)

	// A user sets a savings goal first.
	goalCtx, _ := postForm(t, app, url.Values{
		"savings_goal":           {"250"},
		"savings_goal_frequency": {"fortnightly"},
	})
	require.NoError(t, app.BudgetSaveSavingsGoal(goalCtx))

	saved, err := app.store.GetBudgetSalary()
	require.NoError(t, err)
	require.Equal(t, 250.0, saved.SavingsGoal)

	// They then edit their income, which posts the whole income card.
	salaryCtx, _ := postForm(t, app, url.Values{
		"salary":           {"100000"},
		"salary_frequency": {"yearly"},
		"kiwisaver_rate":   {"3"},
		"include_paye":     {"on"},
	})
	require.NoError(t, app.BudgetSaveSalary(salaryCtx))

	after, err := app.store.GetBudgetSalary()
	require.NoError(t, err)

	assert.Equal(t, 250.0, after.SavingsGoal, "savings goal must survive an income edit")
	assert.Equal(t, "fortnightly", after.SavingsGoalFrequency, "goal frequency must survive too")

	// ...and the income fields themselves are actually saved.
	assert.Equal(t, 100_000.0, after.Salary)
	assert.Equal(t, "yearly", after.SalaryFrequency)
	assert.Equal(t, 3.0, after.KiwiSaverRate)
	assert.True(t, after.IncludePAYE)
	assert.False(t, after.StudentLoan, "an unchecked box posts nothing and must clear the flag")
}

// The reverse direction: saving a goal must not clobber the income config.
func TestBudgetSaveSavingsGoalPreservesSalary(t *testing.T) {
	app := newTestApp(t)

	salaryCtx, _ := postForm(t, app, url.Values{
		"salary":           {"85000"},
		"salary_frequency": {"yearly"},
		"student_loan":     {"on"},
	})
	require.NoError(t, app.BudgetSaveSalary(salaryCtx))

	goalCtx, _ := postForm(t, app, url.Values{"savings_goal": {"100"}})
	require.NoError(t, app.BudgetSaveSavingsGoal(goalCtx))

	after, err := app.store.GetBudgetSalary()
	require.NoError(t, err)

	assert.Equal(t, 85_000.0, after.Salary, "salary must survive a savings goal edit")
	assert.True(t, after.StudentLoan)
	assert.Equal(t, 100.0, after.SavingsGoal)
	assert.Equal(t, "weekly", after.SavingsGoalFrequency, "empty frequency defaults to weekly")
}

func TestGetBudgetSalaryDefaults(t *testing.T) {
	app := newTestApp(t)

	salary, err := app.store.GetBudgetSalary()
	require.NoError(t, err)

	assert.Equal(t, "yearly", salary.SalaryFrequency)
	assert.True(t, salary.IncludePAYE)
	assert.Equal(t, 3.5, salary.KiwiSaverRate)
	assert.Zero(t, salary.Salary)
}

// TestBudgetRemoveKeyword covers the routing fix: keywords containing "/" and
// "&" used to be interpolated into a path segment, producing a URL that could
// not match the route at all.
func TestBudgetRemoveKeyword(t *testing.T) {
	tests := []struct {
		name    string
		keyword string
	}{
		{"plain name", "New World"},
		{"contains a slash", "Z/Caltex"},
		{"contains an ampersand", "Mitre 10 & More"},
		{"contains a hash", "Store #42"},
		{"contains a percent", "50% Off Ltd"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			app := newTestApp(t)

			item := models.BudgetItem{
				ID:       "item-1",
				Category: "Groceries",
				Keywords: []string{test.keyword, "Keep Me"},
			}
			require.NoError(t, app.store.CreateBudgetItem(item))

			// Route the request the way echo will, so a keyword that breaks
			// path matching fails here rather than silently passing.
			e := app.http
			e.DELETE("/budget/items/:id/keywords", app.BudgetRemoveKeyword)

			target := "/budget/items/item-1/keywords?keyword=" + url.QueryEscape(test.keyword)
			req := httptest.NewRequest(http.MethodDelete, target, nil)
			rec := httptest.NewRecorder()
			e.ServeHTTP(rec, req)

			require.Equal(t, http.StatusOK, rec.Code, "route must match for %q", test.keyword)

			after, err := app.store.GetBudgetItem("item-1")
			require.NoError(t, err)
			assert.Equal(t, []string{"Keep Me"}, after.Keywords,
				"only the targeted keyword should be removed")
		})
	}
}

func TestBudgetRemoveKeywordRejectsEmpty(t *testing.T) {
	app := newTestApp(t)
	require.NoError(t, app.store.CreateBudgetItem(models.BudgetItem{ID: "item-1", Keywords: []string{"a"}}))

	e := app.http
	e.DELETE("/budget/items/:id/keywords", app.BudgetRemoveKeyword)

	req := httptest.NewRequest(http.MethodDelete, "/budget/items/item-1/keywords", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}
