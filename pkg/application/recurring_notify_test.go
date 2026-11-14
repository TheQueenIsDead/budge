package application

import (
	"testing"
	"time"

	"github.com/TheQueenIsDead/budge/pkg/database/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRecurringNotifications(t *testing.T) {
	var txs []models.Transaction
	txs = append(txs, payments("Neon", "", 17.99, every(30, 5, days(-35))...)...)
	txs = append(txs, paid("Neon", "", 19.99, days(-5)))
	txs = append(txs, payments("CityFitness", "", 24.5, every(14, 3, days(-2))...)...)
	txs = append(txs, payments("AA Insurance", "Insurance", 1140, days(-365-361), days(-361))...)
	txs = append(txs, payments("Spark", "Internet services", 89.99, every(30, 8, days(-27))...)...)
	txs = append(txs, history())
	found := FindRecurring(txs, recurringNow)

	byTitle := map[string]recurringEvent{}
	for _, event := range recurringNotifications(found, recurringNow) {
		byTitle[event.notification.Title] = event
	}
	require.Len(t, byTitle, 3, "Spark has nothing to report: %v", byTitle)

	price := byTitle["Neon went up"]
	assert.Equal(t, models.NotificationWarning, price.notification.Level)
	assert.Equal(t, "It is $19.99 a month now, up from $17.99: $24 more a year.", price.notification.Message)
	assert.Equal(t, "recurring:price:neon:2026-06-10", price.key, "keyed by the payment that changed")

	fresh := byTitle["New recurring payment: CityFitness"]
	assert.Equal(t, models.NotificationInfo, fresh.notification.Level)
	assert.Equal(t, "Paid fortnightly, usually $24.50, which comes to $637 a year. If it is not recurring, mark it so on Insights.", fresh.notification.Message)
	assert.Equal(t, "recurring:new:cityfitness:2026-06-13", fresh.key)

	due := byTitle["AA Insurance is due Fri 19 Jun"]
	assert.Equal(t, models.NotificationInfo, due.notification.Level)
	assert.Equal(t, "About $1,140.00, expected on Friday 19 June.", due.notification.Message)
	assert.Equal(t, "recurring:due:aa insurance:2026-06-19", due.key)

	t.Run("nothing is raised for a payment marked either way", func(t *testing.T) {
		marked := applyRecurringMarks(found, map[string]string{
			"neon":         models.RecurringCancelled,
			"cityfitness":  models.RecurringNotRecurring,
			"aa insurance": models.RecurringNotRecurring,
		})
		assert.Empty(t, recurringNotifications(marked, recurringNow))
	})

	t.Run("a price change found long after is not raised late", func(t *testing.T) {
		later := recurringNow.AddDate(0, 0, 40)
		for _, event := range recurringNotifications(FindRecurring(txs, later), later) {
			assert.NotEqual(t, "Neon went up", event.notification.Title)
		}
	})

	t.Run("a price coming down says so", func(t *testing.T) {
		cheaper := append(payments("Neon", "", 19.99, every(30, 5, days(-35))...), paid("Neon", "", 14.99, days(-5)), history())
		events := recurringNotifications(FindRecurring(cheaper, recurringNow), recurringNow)
		require.Len(t, events, 1)
		assert.Equal(t, "Neon went down", events[0].notification.Title)
		assert.Equal(t, "It is $14.99 a month now, down from $19.99: $60 less a year.", events[0].notification.Message)
	})
}

func TestNotifyRecurringRaisesEachOnce(t *testing.T) {
	app := newTestApp(t)
	today := localDay(time.Now(), time.Local)
	txs := payments("AA Insurance", "Insurance", 1140, today.AddDate(0, 0, -365-361), today.AddDate(0, 0, -361))
	txs = append(txs, payments("Neon", "", 17.99, every(30, 5, today.AddDate(0, 0, -35))...)...)
	txs = append(txs, paid("Neon", "", 19.99, today.AddDate(0, 0, -5)))
	for _, tx := range txs {
		require.NoError(t, app.store.CreateTransaction(t.Context(), tx))
	}
	now := time.Now()
	read := func() []models.Notification {
		notes, err := app.store.ReadNotifications(t.Context())
		require.NoError(t, err)
		return notes
	}

	app.notifyRecurring(t.Context(), now)
	notes := read()
	require.Len(t, notes, 2, "the insurance due, and Neon's price")
	for _, n := range notes {
		assert.Equal(t, SourceRecurring, n.Source)
	}

	t.Run("not again at the next sync", func(t *testing.T) {
		app.notifyRecurring(t.Context(), now.Add(time.Hour))
		assert.Len(t, read(), 2)
	})

	t.Run("nor once dismissed", func(t *testing.T) {
		require.NoError(t, app.store.DeleteNotifications(t.Context()))
		app.notifyRecurring(t.Context(), now.Add(2*time.Hour))
		assert.Empty(t, read())
	})

	t.Run("nor for one marked since", func(t *testing.T) {
		require.NoError(t, app.store.SaveRecurringMarks(t.Context(), models.RecurringMarks{Marks: map[string]string{"neon": models.RecurringNotRecurring}}))
		// A new price for Neon would be news, but it is marked.
		require.NoError(t, app.store.CreateTransaction(t.Context(), paid("Neon", "", 24.99, today)))
		app.notifyRecurring(t.Context(), now.Add(3*time.Hour))
		assert.Empty(t, read())
	})

	t.Run("and what was raised is forgotten once it is long past", func(t *testing.T) {
		app.notifyRecurring(t.Context(), now.Add(recurringLogKeep+time.Hour))
		log, err := app.store.GetNotificationLog(t.Context())
		require.NoError(t, err)
		for key := range log.Raised {
			assert.NotContains(t, key, "aa insurance:", "the insurance's last due date is long gone")
		}
	})
}
