package application

import (
	"net/http"
	"sort"
	"time"

	"github.com/TheQueenIsDead/budge/pkg/database/models"
	"github.com/labstack/echo/v4"
)

// Sources a notification can come from. Naming them here rather than passing
// strings at each call site keeps a run of failures from one place grouped
// under one name.
const (
	SourceAkahuSync      = "Akahu sync"
	SourceAssetEstimates = "Asset estimates"
)

// NotificationsProps drives the list.
type NotificationsProps struct {
	Notifications []models.Notification
	Unread        int
}

// Notify records something worth telling the owner about later.
//
// It deliberately takes no echo.Context. The callers that matter most are
// background jobs with nobody watching - a scheduled sync failing overnight -
// and a toast needs somebody looking at a page when it fires. A failure to
// record is logged rather than returned: losing the note about a problem should
// not become a second problem for the caller to handle.
func (app *Application) Notify(level models.NotificationLevel, source, title, message string) {
	notification := models.Notification{
		Id:        newID(),
		Level:     level,
		Title:     title,
		Message:   message,
		Source:    source,
		CreatedAt: time.Now(),
	}

	if err := app.store.CreateNotification(notification); err != nil {
		app.http.Logger.Errorf("could not record notification %q: %v", title, err)
	}
}

// sortedNotifications returns the log newest first, with the count still unread.
func (app *Application) sortedNotifications() ([]models.Notification, int, error) {
	notifications, err := app.store.ReadNotifications()
	if err != nil {
		return nil, 0, err
	}

	sort.Slice(notifications, func(i, j int) bool {
		return notifications[i].CreatedAt.After(notifications[j].CreatedAt)
	})

	unread := 0
	for _, notification := range notifications {
		if notification.Unread() {
			unread++
		}
	}
	return notifications, unread, nil
}

func (app *Application) Notifications(c echo.Context) error {
	notifications, unread, err := app.sortedNotifications()
	if err != nil {
		app.Toast(c, "Error", "Could not load notifications.")
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	return c.Render(http.StatusOK, "notifications", NotificationsProps{
		Notifications: notifications,
		Unread:        unread,
	})
}

// NotificationBadge renders just the count on the bell. It is its own endpoint
// so the layout can stay a dumb shell: the badge asks for itself on load and
// again whenever something says the log changed.
func (app *Application) NotificationBadge(c echo.Context) error {
	_, unread, err := app.sortedNotifications()
	if err != nil {
		c.Logger().Error(err)
		// A badge that cannot be counted is not worth an error page.
		return c.Render(http.StatusOK, "notifications.badge", 0)
	}
	return c.Render(http.StatusOK, "notifications.badge", unread)
}

// NotificationsRead marks everything read. The list fires this on load rather
// than the page doing it while rendering, so opening the list stays a plain GET
// with no side effect of its own.
func (app *Application) NotificationsRead(c echo.Context) error {
	if err := app.store.MarkNotificationsRead(time.Now()); err != nil {
		c.Logger().Error(err)
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	return c.NoContent(http.StatusNoContent)
}

func (app *Application) NotificationDelete(c echo.Context) error {
	if err := app.store.DeleteNotification(c.Param("id")); err != nil {
		app.Toast(c, "Error", "Could not remove the notification.")
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	return app.Notifications(c)
}

func (app *Application) NotificationsClear(c echo.Context) error {
	if err := app.store.DeleteNotifications(); err != nil {
		app.Toast(c, "Error", "Could not clear notifications.")
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	return app.Notifications(c)
}
