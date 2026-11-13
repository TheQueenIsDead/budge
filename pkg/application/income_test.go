package application

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/TheQueenIsDead/budge/pkg/database/models"
	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFilingIncome(t *testing.T) {
	app := newTestApp(t)
	recent := time.Now().Add(-48 * time.Hour)
	for _, tx := range []models.Transaction{
		spendTx("pay", "CREDIT", "", "Salary", "Salary", "ACME LTD SALARY", 2500, recent),
		spendTx("sale", "CREDIT", "", "", "", "TRADEME LIMITED", 450, recent),
		spendTx("bought", "EFTPOS", "", "", "", "TRADEME LIMITED", -60, recent),
	} {
		require.NoError(t, app.store.CreateTransaction(t.Context(), tx))
	}
	get := func(id string) models.Transaction {
		tx, err := app.store.GetTransaction(t.Context(), id)
		require.NoError(t, err)
		return tx
	}
	categories := func(id string) []string {
		tx := get(id)
		return tx.Categories()
	}

	t.Run("pay is income as it syncs, and a sale is filed nowhere until it is", func(t *testing.T) {
		assert.Equal(t, []string{"Income"}, categories("pay"))
		assert.Empty(t, categories("sale"))
	})

	t.Run("money coming in is offered Income alone", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/?layout=d", nil)
		rec := httptest.NewRecorder()
		c := app.http.NewContext(req, rec)
		c.SetPathValues(echo.PathValues{{Name: "id", Value: "sale"}})
		require.NoError(t, app.TransactionCategoryForm(c))
		assert.Contains(t, rec.Body.String(), `<option value="income">Income</option>`)
		assert.NotContains(t, rec.Body.String(), `value="groceries"`)
	})

	t.Run("a sale filed as income is income", func(t *testing.T) {
		rec := setCategory(t, app, "sale", url.Values{"category": {"income"}})
		assert.Contains(t, rec.Body.String(), "Income")
		assert.True(t, get("sale").IsIncome())
		assert.Equal(t, []string{"Income"}, categories("sale"))
	})

	t.Run("each way is filed only where it belongs", func(t *testing.T) {
		for id, category := range map[string]string{"bought": "income", "sale": "groceries"} {
			c, _ := postForm(t, app, url.Values{"category": {category}})
			c.SetPathValues(echo.PathValues{{Name: "id", Value: id}})
			var refused *echo.HTTPError
			require.True(t, errors.As(app.TransactionSetCategory(c), &refused), "%s under %s", id, category)
			assert.Equal(t, http.StatusBadRequest, refused.Code)
		}
	})

	t.Run("a rule for income leaves what is bought from the same place as spending", func(t *testing.T) {
		rule := models.CategoryRule{Contains: "TRADEME", Category: models.CategoryIncome}
		assert.True(t, rule.Matches(get("sale")))
		assert.False(t, rule.Matches(get("bought")))
		spending := models.CategoryRule{Contains: "TRADEME", Category: "shopping"}
		assert.False(t, spending.Matches(get("sale")), "and a spending rule leaves money coming in alone")
		assert.True(t, spending.Matches(get("bought")))
	})

	t.Run("ticked both ways, only money coming in is filed as income", func(t *testing.T) {
		c, rec := postForm(t, app, url.Values{"tx": {"pay", "bought"}, "category": {"income"}})
		require.NoError(t, app.TransactionsSetCategory(c))
		message := toastMessage(t, rec.Header().Get("Hx-Trigger"))
		assert.Contains(t, message, "Filed 1 under Income.")
		assert.Contains(t, message, "1 left as they were")
		assert.Equal(t, models.FiledByOwner, get("pay").FiledBy)
		assert.NotEqual(t, models.CategoryIncome, get("bought").Filed)
	})

	t.Run("Income narrows the list to money earned", func(t *testing.T) {
		groups := transactionGroups([]string{"income"})
		require.Len(t, groups, 1)
		assert.True(t, groups[0].Matches(get("sale")))
		assert.True(t, groups[0].Matches(get("pay")))
		assert.False(t, groups[0].Matches(get("bought")))
	})
}

func TestIncomeIsBudgesOwnCategory(t *testing.T) {
	t.Run("it is not in the spending list, which the budget and insights walk", func(t *testing.T) {
		for _, category := range models.TopBudgetCategories() {
			assert.NotEqual(t, models.CategoryIncome, category.Key)
		}
		income, ok := models.BudgetCategoryByKey(models.CategoryIncome)
		require.True(t, ok)
		assert.False(t, income.Budgeted(), "it is off budget")
		assert.True(t, income.NotSpending)
	})

	t.Run("settings lists it off budget, with rules and nothing to rename or delete", func(t *testing.T) {
		app := newTestApp(t)
		view, err := app.buildCategoriesView(t.Context(), "")
		require.NoError(t, err)
		off := view.Groups[1]
		last := off.Categories[len(off.Categories)-1]
		assert.Equal(t, models.CategoryIncome, last.Key)
		assert.True(t, last.Income)
		assert.True(t, last.Fixed)
	})

	t.Run("a category named Income of the owner's own takes another key", func(t *testing.T) {
		next, key, err := models.BudgetCategorySettings{}.AddBudgetCategory("Income", "")
		require.NoError(t, err)
		assert.NotEqual(t, models.CategoryIncome, key)
		assert.NotEmpty(t, next.Categories)
	})
}
