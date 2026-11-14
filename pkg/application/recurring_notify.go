package application

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/TheQueenIsDead/budge/pkg/database/models"
)

// After a sync, the recurring payments raise what the owner would want to
// hear about without opening Insights: a subscription costing more, a new
// one, and a large payment due within the week. Each is raised once, for the
// payment it is about, so a later sync does not raise it again.

// SourceRecurring names notifications raised by the recurring payments.
const SourceRecurring = "Recurring"

// recurringLogPrefix starts the keys the recurring payments record raising
// under, apart from the budget's.
const recurringLogPrefix = "recurring:"

// recurringLogKeep is how long a raised event is remembered. Each is about a
// payment, and none can be raised once its payment is this far behind, so
// remembering them longer would only grow the log.
const recurringLogKeep = 90 * 24 * time.Hour

// recurringEvent is a notification about a recurring payment, with the key
// that says it was raised.
type recurringEvent struct {
	key          string
	notification models.Notification
}

// notifyRecurring raises a notification for each recurring payment with
// something to report that has not been raised before. Like notifyBudget, a
// failure is logged rather than returned.
func (app *Application) notifyRecurring(ctx context.Context, now time.Time) {
	found, marks, err := app.readRecurring(ctx, now)
	if err == nil {
		var raised models.NotificationLog
		raised, err = app.store.GetNotificationLog(ctx)
		if err == nil {
			changed := false
			for key, at := range raised.Raised {
				if strings.HasPrefix(key, recurringLogPrefix) && now.Sub(at) > recurringLogKeep {
					delete(raised.Raised, key)
					changed = true
				}
			}
			for _, event := range recurringNotifications(applyRecurringMarks(found, marks.Marks), now) {
				if _, ok := raised.Raised[event.key]; ok {
					continue
				}
				n := event.notification
				app.Notify(ctx, n.Level, SourceRecurring, n.Title, n.Message)
				raised.Raised[event.key] = now
				changed = true
			}
			if changed {
				err = app.store.SaveNotificationLog(ctx, raised)
			}
		}
	}
	if err != nil {
		slog.Error("could not check the recurring payments", "error", err)
	}
}

// recurringNotifications words what the recurring payments have to report,
// each keyed by the payment it is about. A payment the owner has marked
// either way, or that has stopped, reports nothing.
func recurringNotifications(found []RecurringPayment, now time.Time) []recurringEvent {
	today := localDay(now, now.Location())
	var events []recurringEvent
	for _, p := range found {
		if p.Mark != "" || p.Stopped {
			continue
		}
		every := "a " + p.Cadence.Noun
		// A price change is news for a month at most, so one found long
		// after it, as on the first sync, is not raised late.
		if p.PriceChanged && daysBetween(p.Last, today) <= recurringNewDays {
			direction, more, by := "up", "more", p.Latest-p.Previous
			if by < 0 {
				direction, more, by = "down", "less", -by
			}
			events = append(events, recurringEvent{
				key: recurringLogPrefix + "price:" + p.Key + ":" + p.Last.Format(time.DateOnly),
				notification: models.Notification{
					Level: models.NotificationWarning,
					Title: p.Name + " went " + direction,
					Message: fmt.Sprintf("It is %s %s now, %s from %s: %s %s a year.",
						fmtCurrency(p.Latest), every, direction, fmtCurrency(p.Previous), fmtWhole(by*p.Cadence.PerYear), more),
				},
			})
		}
		if p.New {
			events = append(events, recurringEvent{
				key: recurringLogPrefix + "new:" + p.Key + ":" + p.Recognised.Format(time.DateOnly),
				notification: models.Notification{
					Level: models.NotificationInfo,
					Title: "New recurring payment: " + p.Name,
					Message: fmt.Sprintf("Paid %s, usually %s, which comes to %s a year. If it is not recurring, mark it so on Insights.",
						strings.ToLower(p.Cadence.Label), fmtCurrency(p.Usual), fmtWhole(p.Yearly())),
				},
			})
		}
		if p.DueSoon {
			events = append(events, recurringEvent{
				key: recurringLogPrefix + "due:" + p.Key + ":" + p.Next.Format(time.DateOnly),
				notification: models.Notification{
					Level:   models.NotificationInfo,
					Title:   p.Name + " is due " + p.When(),
					Message: fmt.Sprintf("About %s, expected on %s.", fmtCurrency(p.Usual), p.Next.Format("Monday 2 January")),
				},
			})
		}
	}
	return events
}
