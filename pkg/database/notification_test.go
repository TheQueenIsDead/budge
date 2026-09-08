package database

import (
	"fmt"
	"testing"
	"time"

	"github.com/TheQueenIsDead/budge/pkg/database/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// tempStore opens a store against a throwaway database, so these exercise real
// bolt behaviour without going near anybody's actual data.
func tempStore(t *testing.T) *Store {
	t.Helper()
	t.Setenv("BUDGE_BOLT_PATH", t.TempDir())

	store, err := NewStore()
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func notification(id string, age time.Duration) models.Notification {
	return models.Notification{
		Id:        id,
		Level:     models.NotificationError,
		Title:     "Sync failed",
		Source:    "Akahu sync",
		CreatedAt: time.Now().Add(-age),
	}
}

func TestNotificationRoundTrip(t *testing.T) {
	store := tempStore(t)

	require.NoError(t, store.CreateNotification(notification("a", time.Hour)))
	require.NoError(t, store.CreateNotification(notification("b", time.Minute)))

	got, err := store.ReadNotifications()
	require.NoError(t, err)
	require.Len(t, got, 2)
	for _, n := range got {
		assert.True(t, n.Unread(), "a new notification has not been read")
	}
}

func TestMarkNotificationsRead(t *testing.T) {
	store := tempStore(t)
	require.NoError(t, store.CreateNotification(notification("a", time.Hour)))
	require.NoError(t, store.CreateNotification(notification("b", time.Minute)))

	at := time.Now()
	require.NoError(t, store.MarkNotificationsRead(at))

	got, err := store.ReadNotifications()
	require.NoError(t, err)
	for _, n := range got {
		assert.False(t, n.Unread())
	}

	t.Run("a later read does not disturb the first", func(t *testing.T) {
		before, err := store.ReadNotifications()
		require.NoError(t, err)
		require.NoError(t, store.MarkNotificationsRead(time.Now().Add(time.Hour)))

		after, err := store.ReadNotifications()
		require.NoError(t, err)
		assert.Equal(t, before[0].ReadAt, after[0].ReadAt,
			"already-read entries keep the time they were first read")
	})
}

func TestNotificationRetention(t *testing.T) {
	store := tempStore(t)

	// Oldest first, so the ones dropped are the ones at the far end.
	total := notificationRetention + 25
	for i := 0; i < total; i++ {
		age := time.Duration(total-i) * time.Minute
		require.NoError(t, store.CreateNotification(notification(fmt.Sprintf("n%03d", i), age)))
	}

	got, err := store.ReadNotifications()
	require.NoError(t, err)
	assert.Len(t, got, notificationRetention, "the log is capped, not an archive")

	kept := make(map[string]bool, len(got))
	for _, n := range got {
		kept[n.Id] = true
	}
	assert.False(t, kept["n000"], "the oldest is dropped first")
	assert.True(t, kept[fmt.Sprintf("n%03d", total-1)], "the newest is always kept")
}

func TestDeleteNotifications(t *testing.T) {
	store := tempStore(t)
	require.NoError(t, store.CreateNotification(notification("a", time.Hour)))
	require.NoError(t, store.CreateNotification(notification("b", time.Minute)))

	t.Run("one at a time", func(t *testing.T) {
		require.NoError(t, store.DeleteNotification("a"))
		got, err := store.ReadNotifications()
		require.NoError(t, err)
		require.Len(t, got, 1)
		assert.Equal(t, "b", got[0].Id)
	})

	t.Run("all at once", func(t *testing.T) {
		require.NoError(t, store.DeleteNotifications())
		got, err := store.ReadNotifications()
		require.NoError(t, err)
		assert.Empty(t, got)
	})
}
