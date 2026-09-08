package application

import (
	"strings"
	"testing"
	"time"

	"github.com/TheQueenIsDead/budge/pkg/database/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func note(level models.NotificationLevel, title string, age time.Duration, read bool) models.Notification {
	n := models.Notification{
		Id:        title,
		Level:     level,
		Title:     title,
		Message:   "something went wrong",
		Source:    SourceAkahuSync,
		CreatedAt: time.Now().Add(-age),
	}
	if read {
		n.ReadAt = time.Now()
	}
	return n
}

func TestNotificationLevel(t *testing.T) {
	tests := []struct {
		level  models.NotificationLevel
		accent string
		icon   string
	}{
		{models.NotificationError, "brick", "bi-exclamation-octagon"},
		{models.NotificationWarning, "clay", "bi-exclamation-triangle"},
		{models.NotificationInfo, "slate", "bi-info-circle"},
		// An unrecognised level still renders as something.
		{models.NotificationLevel("shouting"), "slate", "bi-info-circle"},
	}

	for _, test := range tests {
		t.Run(string(test.level), func(t *testing.T) {
			assert.Equal(t, test.accent, test.level.Accent())
			assert.Equal(t, test.icon, test.level.Icon())
		})
	}
}

func TestNotificationUnread(t *testing.T) {
	assert.True(t, note(models.NotificationError, "a", 0, false).Unread())
	assert.False(t, note(models.NotificationError, "b", 0, true).Unread())
}

func TestRenderNotifications(t *testing.T) {

	notifications := []models.Notification{
		note(models.NotificationError, "Sync failed", time.Hour, false),
		note(models.NotificationWarning, "Partial sync", 26*time.Hour, true),
	}

	page := renderTemplate(t, "notifications", NotificationsProps{
		Notifications: notifications,
		Unread:        1,
	})

	t.Run("lists what happened", func(t *testing.T) {
		assert.Contains(t, page, "Sync failed")
		assert.Contains(t, page, "Partial sync")
		assert.Contains(t, page, SourceAkahuSync)
	})

	t.Run("colours each entry by its level", func(t *testing.T) {
		assert.Contains(t, page, "b-accent-brick")
		assert.Contains(t, page, "b-accent-clay")
	})

	t.Run("marks only the unread one", func(t *testing.T) {
		assert.Equal(t, 1, strings.Count(page, "b-note-unread"))
	})

	t.Run("marks read from a post, not from rendering", func(t *testing.T) {
		// Opening the list stays a plain GET, so a prefetch cannot clear the
		// badge without the owner having seen anything.
		assert.Contains(t, page, `hx-post="/notifications/read"`)
		assert.Contains(t, page, `hx-trigger="load"`)
	})

	t.Run("offers dismissal and a clear-all", func(t *testing.T) {
		assert.Contains(t, page, `hx-delete="/notifications/Sync failed"`)
		assert.Contains(t, page, `hx-delete="/notifications"`)
	})

	t.Run("tells the badge to recount after any change", func(t *testing.T) {
		assert.Contains(t, page, "htmx.trigger(document.body, 'notifications')")
	})

	t.Run("says so when there is nothing", func(t *testing.T) {
		empty := renderTemplate(t, "notifications", NotificationsProps{})
		assert.Contains(t, empty, "Nothing to report")
		// Nothing to clear, so no control for it.
		assert.NotContains(t, empty, "Clear all")
	})
}

func TestRenderNotificationBadge(t *testing.T) {
	tests := []struct {
		name   string
		unread int
		expect string
	}{
		{"nothing unread shows no badge", 0, ""},
		{"a count is shown", 3, "3"},
		{"large counts are capped", 12, "9+"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			badge := strings.TrimSpace(renderTemplate(t, "notifications.badge", test.unread))
			if test.expect == "" {
				assert.Empty(t, badge)
				return
			}
			assert.Contains(t, badge, test.expect)
			assert.Contains(t, badge, "b-bell-count")
		})
	}
}

func TestLayoutCarriesTheBell(t *testing.T) {
	page := renderTemplate(t, "layout", map[string]interface{}{"content": ""})

	t.Run("the bell is on every page", func(t *testing.T) {
		assert.Contains(t, page, `href="/notifications"`)
		assert.Contains(t, page, "bi-bell")
	})

	t.Run("the count fetches itself rather than the layout carrying data", func(t *testing.T) {
		assert.Contains(t, page, `hx-get="/notifications/badge"`)
		assert.Contains(t, page, `hx-trigger="load, notifications from:body"`)
	})

	t.Run("it opens a page rather than a popover", func(t *testing.T) {
		require.Contains(t, page, `data-nav href="/notifications"`)
	})
}

// TestBadgeDoesNotInheritTheNavTarget guards a bug that blanked every page a
// moment after it loaded. The badge sits inside the bell link, which targets
// #main, and hx-target is inherited: without pinning it to itself the badge
// swapped its own response over the page body.
func TestBadgeDoesNotInheritTheNavTarget(t *testing.T) {
	page := renderTemplate(t, "layout", map[string]interface{}{"content": ""})

	start := strings.Index(page, `id="notification-badge"`)
	require.NotEqual(t, -1, start, "badge not found")
	end := strings.Index(page[start:], ">") + start
	badge := page[start:end]

	assert.Contains(t, badge, `hx-target="this"`,
		"the badge must not inherit the nav's #main target")
	assert.Contains(t, badge, `hx-swap="innerHTML"`)
}
