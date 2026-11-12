package application

import (
	"testing"
	"time"

	"github.com/TheQueenIsDead/budge/pkg/database/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBudgetNotifications(t *testing.T) {
	app := newTestApp(t)
	recent := lastWeek()
	require.NoError(t, app.store.CreateTransaction(t.Context(), spendTx("cafe", "EFTPOS", "Lifestyle", "Cafes and restaurants", "Valhalla", "", -916, recent)))
	require.NoError(t, app.store.CreateTransaction(t.Context(), spendTx("diy", "EFTPOS", "Household", "Building supplies", "Bunnings", "", -702, recent)))
	require.NoError(t, app.store.CreateTransaction(t.Context(), spendTx("pak", "EFTPOS", "Food", "Supermarkets and grocery stores", "", "", -400, recent)))
	require.NoError(t, app.store.CreateBudgetItem(t.Context(), models.BudgetItem{ID: "out", BudgetCategory: "eating-out", Amount: 120, Frequency: "weekly"}))
	require.NoError(t, app.store.CreateBudgetItem(t.Context(), models.BudgetItem{ID: "shop", BudgetCategory: "groceries", Amount: 150, Frequency: "weekly"}))

	now := time.Now()
	app.notifyBudget(t.Context(), now)

	notes, err := app.store.ReadNotifications(t.Context())
	require.NoError(t, err)
	byTitle := map[string]models.Notification{}
	for _, n := range notes {
		byTitle[n.Title] = n
	}
	require.Len(t, notes, 2)

	over := byTitle["Eating & drinking out is over target"]
	assert.Equal(t, models.NotificationWarning, over.Level)
	assert.Equal(t, SourceBudget, over.Source)
	assert.Contains(t, over.Message, "$109 a week over", "$229 a week over 4 weeks against $120")

	untargeted := byTitle["Spending with no target"]
	assert.Equal(t, models.NotificationInfo, untargeted.Level)
	assert.Contains(t, untargeted.Message, "Maintenance ($54)")

	t.Run("the same is not raised again within the week", func(t *testing.T) {
		app.notifyBudget(t.Context(), now.Add(time.Hour))
		notes, err := app.store.ReadNotifications(t.Context())
		require.NoError(t, err)
		assert.Len(t, notes, 2)
	})

	t.Run("nor once it has been dismissed", func(t *testing.T) {
		require.NoError(t, app.store.DeleteNotifications(t.Context()))
		app.notifyBudget(t.Context(), now.Add(2*time.Hour))
		notes, err := app.store.ReadNotifications(t.Context())
		require.NoError(t, err)
		assert.Empty(t, notes, "dismissing a notification does not make it due again")
	})

	t.Run("but is once a week has passed", func(t *testing.T) {
		app.notifyBudget(t.Context(), now.Add(budgetRenotifyAfter+time.Hour))
		notes, err := app.store.ReadNotifications(t.Context())
		require.NoError(t, err)
		assert.Len(t, notes, 2)
	})
}

func TestNoBudgetNotificationsWhenAllIsWell(t *testing.T) {
	assert.Empty(t, budgetNotifications(TrackingSummary{Actual: 100, Target: 150}))
}
