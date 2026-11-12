package application

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"
	"strconv"
	"sync"
	"time"

	"github.com/TheQueenIsDead/budge/pkg/database/models"
	"github.com/labstack/echo/v5"
	"go.opentelemetry.io/otel/codes"
)

// A sync runs in the background rather than inside the request that asked for
// it. A first sync reads the whole history from Akahu a page at a time, which
// takes a minute or more, and a button spinning for that long reads as broken
// even when the sync is fine. The button starts the sync and the page polls
// for progress instead.

// syncRunner allows one Akahu sync at a time and remembers how the last one
// went. Manual and scheduled syncs share it, so a scheduled run never starts
// on top of a manual one, and the settings page shows a sync in progress
// whichever started it.
type syncRunner struct {
	mu       sync.Mutex
	running  bool
	held     bool
	fetched  int
	started  time.Time
	finished time.Time
	err      error
}

// SyncStatus is the sync as the settings page shows it.
type SyncStatus struct {
	Running  bool
	Fetched  int
	Started  time.Time
	Finished time.Time
	Error    string
	LastSync time.Time
}

// StartedKey identifies the run being watched, so a poll can tell whether the
// finish it sees is the one it was waiting on.
func (s SyncStatus) StartedKey() string {
	return strconv.FormatInt(s.Started.UnixNano(), 10)
}

// begin claims the runner, reporting false when a sync is already going.
func (r *syncRunner) begin() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.running || r.held {
		return false
	}
	r.running, r.fetched, r.err = true, 0, nil
	r.started = time.Now()
	return true
}

// hold keeps any sync from starting until release, for work that must not
// run alongside one, such as deleting what syncs have stored. It reports
// false when a sync is already going or the runner is already held. Unlike
// begin, it leaves the last sync's status alone.
func (r *syncRunner) hold() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.running || r.held {
		return false
	}
	r.held = true
	return true
}

func (r *syncRunner) release() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.held = false
}

func (r *syncRunner) progress(fetched int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.fetched = fetched
}

func (r *syncRunner) end(err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.running, r.err = false, err
	r.finished = time.Now()
}

func (r *syncRunner) status() SyncStatus {
	r.mu.Lock()
	defer r.mu.Unlock()
	status := SyncStatus{Running: r.running, Fetched: r.fetched, Started: r.started, Finished: r.finished}
	if r.err != nil {
		status.Error = r.err.Error()
	}
	return status
}

// runAkahuSync syncs and records when it last succeeded. full forgets how
// far the last sync reached first, so the whole history is read again. The
// runner must already be claimed; it is released here, even if the sync
// panics, which is recorded as the sync failing.
func (app *Application) runAkahuSync(ctx context.Context, full bool) error {
	ctx, span := tracer().Start(ctx, "akahu sync")
	defer span.End()
	err := func() (err error) {
		defer func() {
			if r := recover(); r != nil {
				err = panicked(ctx, r)
			}
		}()
		if full {
			if err := app.store.ResetAkahuLastSync(ctx); err != nil {
				return err
			}
		}
		settings, err := app.store.GetAkahuSettings(ctx)
		if err != nil {
			return err
		}
		through, err := app.integrations.SyncAkahu(ctx, settings.DataThrough, app.akahuSync.progress)
		if err != nil {
			return err
		}
		return app.store.RecordAkahuSync(ctx, through)
	}()
	app.akahuSync.end(err)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	}
	if err == nil {
		// New spending has landed, so this is when a category tips over.
		app.notifyBudget(ctx, time.Now())
	}
	return err
}

// panicked logs a recovered panic with its stack, and makes an error of it.
func panicked(ctx context.Context, r any) error {
	slog.ErrorContext(ctx, "akahu sync panicked", "panic", r, "stack", string(debug.Stack()))
	return fmt.Errorf("sync panicked: %v", r)
}

// inBackground runs a sync that outlives the request that started it. Close
// waits for it, so the store is not closed underneath it, and a panic is
// logged rather than taking the whole process down.
func (app *Application) inBackground(ctx context.Context, run func(context.Context)) {
	// The sync keeps the request's trace but not its cancellation.
	ctx = context.WithoutCancel(ctx)
	app.background.Add(1)
	go func() {
		defer app.background.Done()
		defer func() {
			if r := recover(); r != nil {
				_ = panicked(ctx, r)
			}
		}()
		run(ctx)
	}()
}

// waitForBackground waits up to grace for syncs running in the background
// to finish, reporting whether they did.
func (app *Application) waitForBackground(grace time.Duration) bool {
	done := make(chan struct{})
	go func() {
		app.background.Wait()
		close(done)
	}()
	select {
	case <-done:
		return true
	case <-time.After(grace):
		return false
	}
}

// syncStatus is the runner's state alongside when the last sync landed.
func (app *Application) syncStatus(ctx context.Context) SyncStatus {
	status := app.akahuSync.status()
	if settings, err := app.store.GetAkahuSettings(ctx); err == nil {
		status.LastSync = settings.LastSync
	}
	return status
}

// SyncAkahu starts a sync in the background and answers at once with the
// progress view, which polls until the sync is done. Asking while one is
// already running joins it rather than starting another.
func (app *Application) SyncAkahu(c *echo.Context) error {
	ctx := c.Request().Context()
	if app.akahuSync.begin() {
		app.inBackground(ctx, func(ctx context.Context) {
			if err := app.runAkahuSync(ctx, false); err != nil {
				// Recorded as well as shown: the page that started this may be
				// long closed by the time it fails.
				app.Notify(ctx, models.NotificationError, SourceAkahuSync, "Sync failed", err.Error())
			}
		})
	}
	return c.Render(http.StatusOK, "settings.integrations.sync", app.syncStatus(ctx))
}

// ResyncAkahu re-reads the whole history rather than the days since the last
// sync. Every transaction is rewritten from Akahu, so fixes to how a sync
// classifies transactions reach the ones already stored. Nothing is deleted
// first, so memories and hidden purchases, which are keyed by Akahu's own
// transaction IDs, are untouched.
func (app *Application) ResyncAkahu(c *echo.Context) error {
	ctx := c.Request().Context()
	if app.akahuSync.begin() {
		app.inBackground(ctx, func(ctx context.Context) {
			if err := app.runAkahuSync(ctx, true); err != nil {
				app.Notify(ctx, models.NotificationError, SourceAkahuSync, "Full resync failed", err.Error())
			}
		})
	}
	return c.Render(http.StatusOK, "settings.integrations.sync", app.syncStatus(ctx))
}

// SyncAkahuStatus answers the progress view's poll. When the run it was
// watching has finished, it says how that went, once.
func (app *Application) SyncAkahuStatus(c *echo.Context) error {
	ctx := c.Request().Context()
	status := app.syncStatus(ctx)

	watching := c.QueryParam("started")
	if !status.Running && watching != "" && watching == status.StartedKey() {
		if status.Error != "" {
			app.Toast(c, "Error", "Sync failed: "+status.Error)
		} else {
			app.Toast(c, "Success", "Akahu synced successfully!")
		}
	}

	return c.Render(http.StatusOK, "settings.integrations.sync", status)
}
