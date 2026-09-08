package application

import (
	"net/http"
	"time"

	"github.com/TheQueenIsDead/budge/pkg/database/models"
	"github.com/labstack/echo/v4"
)

// ScheduleView is the schedule as the settings page reads it: what is on, how
// often, and when it will next happen.
type ScheduleView struct {
	Settings       models.ScheduleSettings
	AkahuIntervals []models.ScheduleInterval
	AssetIntervals []models.ScheduleInterval

	// Next is zero when a job has never run, in which case it is due at the next
	// tick and the page says so rather than showing a date in the past.
	AkahuNext  time.Time
	AssetsNext time.Time
}

func buildScheduleView(schedule models.ScheduleSettings) ScheduleView {
	view := ScheduleView{
		Settings:       schedule,
		AkahuIntervals: models.AkahuIntervals,
		AssetIntervals: models.AssetIntervals,
	}
	if !schedule.AkahuLastRun.IsZero() {
		view.AkahuNext = schedule.AkahuLastRun.Add(schedule.AkahuEvery().Every)
	}
	if !schedule.AssetsLastRun.IsZero() {
		view.AssetsNext = schedule.AssetsLastRun.Add(schedule.AssetsEvery().Every)
	}
	return view
}

func (app *Application) Settings(c echo.Context) error {
	accounts, err := app.store.ReadAccounts()
	if err != nil {
		app.Toast(c, "Error", "Could not read accounts.")
		return c.NoContent(http.StatusInternalServerError)
	}

	akahuConfig, err := app.store.GetAkahuSettings()
	if err != nil {
		app.Toast(c, "Error", "Could not get Akahu settings.")
		return c.NoContent(http.StatusInternalServerError)
	}

	schedule, err := app.store.GetScheduleSettings()
	if err != nil {
		app.Toast(c, "Error", "Could not read the schedule.")
		return c.NoContent(http.StatusInternalServerError)
	}

	return c.Render(http.StatusOK, "settings", map[string]interface{}{
		"accounts":       accounts,
		"akahuAppToken":  akahuConfig.AppToken,
		"akahuUserToken": akahuConfig.UserToken,
		"akahuLastSync":  akahuConfig.LastSync,
		"schedule":       buildScheduleView(schedule),
	})
}

func (app *Application) SettingsDeleteSynced(c echo.Context) error {

	err := app.store.DeleteSynced()
	if err != nil {
		return err
	}
	_ = app.store.ResetAkahuLastSync()

	app.Toast(c, "Success", "All synced data removed.")
	return nil
}

func (app *Application) SyncAkahu(c echo.Context) error {

	akahuConfig, err := app.store.GetAkahuSettings()
	if err != nil {
		// Recorded as well as returned: once a timer drives this there is nobody
		// watching the response, and the note is the only trace left.
		app.Notify(models.NotificationError, SourceAkahuSync,
			"Sync could not start", "Akahu settings could not be read.")
		return err
	}

	err = app.integrations.SyncAkahu(c.Logger(), akahuConfig.LastSync)
	if err != nil {
		app.Notify(models.NotificationError, SourceAkahuSync,
			"Sync failed", err.Error())
		return err
	}

	// Re-retrieve the last sync time to use as a cache key
	akahuConfig, _ = app.store.GetAkahuSettings()
	c.SetCookie(&http.Cookie{
		Name:  "X-Cache-Key",
		Value: akahuConfig.LastSync.String(),
		Path:  "/",
	})

	app.Toast(c, "Success", "Akahu synced successfully!")
	_ = app.store.UpdateAkahuLastSync()

	return c.Render(http.StatusOK, "settings.integrations.last-sync", map[string]interface{}{
		"akahuLastSync": time.Now(),
	})
}

func (app *Application) PutAkahuSettings(c echo.Context) error {

	settings, err := app.store.GetAkahuSettings()
	if err != nil {
		return err
	}

	settings.AppToken = c.FormValue("akahuAppToken")
	settings.UserToken = c.FormValue("akahuUserToken")
	if err := settings.Validate(); err != nil {
		return err
	}

	err = app.integrations.PutAkahuSettings(settings)
	if err != nil {
		return err
	}
	app.Toast(c, "Success", "Akahu settings saved successfully!")
	return nil
}

// SettingsSaveSchedule stores what should run by itself. Last-run times are
// carried across rather than reset, so changing a cadence does not fire a job
// immediately as a side effect of saving.
func (app *Application) SettingsSaveSchedule(c echo.Context) error {

	schedule, err := app.store.GetScheduleSettings()
	if err != nil {
		app.Toast(c, "Error", "Could not read the schedule.")
		return c.NoContent(http.StatusInternalServerError)
	}

	schedule.AkahuEnabled = c.FormValue("akahu_enabled") != ""
	schedule.AssetsEnabled = c.FormValue("assets_enabled") != ""
	schedule.AkahuInterval = models.ResolveInterval(
		models.AkahuIntervals, c.FormValue("akahu_interval"), schedule.AkahuEvery()).Key
	schedule.AssetsInterval = models.ResolveInterval(
		models.AssetIntervals, c.FormValue("assets_interval"), schedule.AssetsEvery()).Key

	if err := app.store.SaveScheduleSettings(schedule); err != nil {
		app.Toast(c, "Error", "Could not save the schedule.")
		return c.NoContent(http.StatusInternalServerError)
	}

	app.Toast(c, "Success", "Schedule saved.")
	return c.Render(http.StatusOK, "settings.schedule", buildScheduleView(schedule))
}
