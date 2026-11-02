package application

import (
	"fmt"
	"strings"
	"time"

	"github.com/TheQueenIsDead/budge/pkg/database/models"
)

// The budget page says how spending is tracking; the bell says what to do
// about it. After a sync brings in new spending, each category over its
// target gets a notification of its own, and spending with no target gets
// one between them, so the page stays a page to read rather than a list of
// warnings.

// SourceBudget names notifications raised by the budget.
const SourceBudget = "Budget"

// budgetRenotifyAfter is how long a budget notification is left before the
// same one is raised again, so a category that stays over is not reported
// after every sync.
const budgetRenotifyAfter = 7 * 24 * time.Hour

// notifyBudget raises a notification for each category over its target, and
// one for spending with no target, unless the same was raised in the last
// week. When each was raised is recorded apart from the notifications, so
// dismissing one does not bring it back at the next sync. Like Notify, a
// failure is logged rather than returned: a sync that worked should not
// report failing because its follow-up did not.
func (app *Application) notifyBudget(now time.Time) {
	setup, err := app.buildSetupData()
	if err == nil {
		var raised models.NotificationLog
		raised, err = app.store.GetNotificationLog()
		if err == nil {
			changed := false
			for _, n := range budgetNotifications(setup.Tracking) {
				if last, ok := raised.Raised[n.Title]; ok && now.Sub(last) < budgetRenotifyAfter {
					continue
				}
				app.Notify(n.Level, SourceBudget, n.Title, n.Message)
				raised.Raised[n.Title] = now
				changed = true
			}
			if changed {
				err = app.store.SaveNotificationLog(raised)
			}
		}
	}
	if err != nil {
		app.http.Logger.Errorf("could not check the budget: %v", err)
	}
}

// budgetNotifications words what the budget has to report.
func budgetNotifications(tracking TrackingSummary) []models.Notification {
	var notes []models.Notification
	for _, over := range tracking.Over {
		notes = append(notes, models.Notification{
			Level: models.NotificationWarning,
			Title: over.Label + " is over target",
			Message: fmt.Sprintf("Running %s a week over its target. Lower what goes there, or raise the target on the budget page.",
				fmtWhole(over.Weekly)),
		})
	}
	if tracking.UntargetedCount > 0 {
		var names []string
		for _, item := range tracking.Untargeted {
			names = append(names, fmt.Sprintf("%s (%s)", item.Label, fmtWhole(item.Weekly)))
		}
		list := strings.Join(names, ", ")
		if tracking.UntargetedMore > 0 {
			list += fmt.Sprintf(" and %d more", tracking.UntargetedMore)
		}
		notes = append(notes, models.Notification{
			Level: models.NotificationInfo,
			Title: "Spending with no target",
			Message: fmt.Sprintf("%s a week goes to categories with no target: %s. Set one on the budget page so it counts.",
				fmtWhole(tracking.UntargetedWeekly), list),
		})
	}
	return notes
}
