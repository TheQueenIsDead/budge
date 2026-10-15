package application

import (
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/TheQueenIsDead/budge/pkg/database/models"
	"github.com/labstack/echo/v4"
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
	if r.running {
		return false
	}
	r.running, r.fetched, r.err = true, 0, nil
	r.started = time.Now()
	return true
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

// runAkahuSync syncs and records when it last succeeded. The runner must
// already be claimed; it is released here.
func (app *Application) runAkahuSync() error {
	err := func() error {
		settings, err := app.store.GetAkahuSettings()
		if err != nil {
			return err
		}
		through, err := app.integrations.SyncAkahu(app.http.Logger, settings.DataThrough, app.akahuSync.progress)
		if err != nil {
			return err
		}
		return app.store.RecordAkahuSync(through)
	}()
	app.akahuSync.end(err)
	return err
}

// syncStatus is the runner's state alongside when the last sync landed.
func (app *Application) syncStatus() SyncStatus {
	status := app.akahuSync.status()
	if settings, err := app.store.GetAkahuSettings(); err == nil {
		status.LastSync = settings.LastSync
	}
	return status
}

// SyncAkahu starts a sync in the background and answers at once with the
// progress view, which polls until the sync is done. Asking while one is
// already running joins it rather than starting another.
func (app *Application) SyncAkahu(c echo.Context) error {
	if app.akahuSync.begin() {
		go func() {
			if err := app.runAkahuSync(); err != nil {
				// Recorded as well as shown: the page that started this may be
				// long closed by the time it fails.
				app.Notify(models.NotificationError, SourceAkahuSync, "Sync failed", err.Error())
			}
		}()
	}
	return c.Render(http.StatusOK, "settings.integrations.sync", app.syncStatus())
}

// ResyncAkahu re-reads the whole history rather than the days since the last
// sync. Every transaction is rewritten from Akahu, so fixes to how a sync
// classifies transactions reach the ones already stored. Nothing is deleted
// first, so memories and hidden purchases, which are keyed by Akahu's own
// transaction IDs, are untouched.
func (app *Application) ResyncAkahu(c echo.Context) error {
	if app.akahuSync.begin() {
		go func() {
			err := app.store.ResetAkahuLastSync()
			if err != nil {
				app.akahuSync.end(err)
			} else {
				err = app.runAkahuSync()
			}
			if err != nil {
				app.Notify(models.NotificationError, SourceAkahuSync, "Full resync failed", err.Error())
			}
		}()
	}
	return c.Render(http.StatusOK, "settings.integrations.sync", app.syncStatus())
}

// SyncAkahuStatus answers the progress view's poll. When the run it was
// watching has finished, it says how that went, once.
func (app *Application) SyncAkahuStatus(c echo.Context) error {
	status := app.syncStatus()

	watching := c.QueryParam("started")
	if !status.Running && watching != "" && watching == status.StartedKey() {
		if status.Error != "" {
			app.Toast(c, "Error", "Sync failed: "+status.Error)
		} else {
			app.Toast(c, "Success", "Akahu synced successfully!")
			c.SetCookie(&http.Cookie{
				Name:  "X-Cache-Key",
				Value: status.LastSync.String(),
				Path:  "/",
			})
		}
	}

	return c.Render(http.StatusOK, "settings.integrations.sync", status)
}
