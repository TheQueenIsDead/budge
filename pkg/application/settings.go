package application

import (
	"net/http"
	"strings"
	"time"

	"github.com/TheQueenIsDead/budge/pkg/database/models"
	"github.com/labstack/echo/v5"
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

func (app *Application) Settings(c *echo.Context) error {
	ctx := c.Request().Context()
	accounts, err := app.store.ReadAccounts(ctx)
	if err != nil {
		app.Toast(c, "Error", "Could not read accounts.")
		return c.NoContent(http.StatusInternalServerError)
	}

	akahuConfig, err := app.store.GetAkahuSettings(ctx)
	if err != nil {
		app.Toast(c, "Error", "Could not get Akahu settings.")
		return c.NoContent(http.StatusInternalServerError)
	}

	schedule, err := app.store.GetScheduleSettings(ctx)
	if err != nil {
		app.Toast(c, "Error", "Could not read the schedule.")
		return c.NoContent(http.StatusInternalServerError)
	}

	categories, err := app.buildCategoriesView(ctx, "")
	if err != nil {
		app.Toast(c, "Error", "Could not read category rules.")
		return c.NoContent(http.StatusInternalServerError)
	}

	return c.Render(http.StatusOK, "settings", map[string]interface{}{
		"accounts":   accounts,
		"categories": categories,
		// Whether each token is saved, never the token itself: the page is
		// not somewhere a secret should be readable from.
		"akahuAppTokenSaved":  akahuConfig.AppToken != "",
		"akahuUserTokenSaved": akahuConfig.UserToken != "",
		"sync":                app.syncStatus(ctx),
		"schedule":            buildScheduleView(schedule),
		"admin":               app.telemetry != nil,
	})
}

func (app *Application) SettingsDeleteSynced(c *echo.Context) error {
	ctx := c.Request().Context()

	err := app.store.DeleteSynced(ctx)
	if err != nil {
		return err
	}
	_ = app.store.ResetAkahuLastSync(ctx)

	app.Toast(c, "Success", "All synced data removed.")
	return nil
}

func (app *Application) PutAkahuSettings(c *echo.Context) error {
	ctx := c.Request().Context()

	settings, err := app.store.GetAkahuSettings(ctx)
	if err != nil {
		return err
	}

	// The page never holds the saved tokens, so a field left blank keeps
	// what is saved rather than clearing it.
	if token := strings.TrimSpace(c.FormValue("akahuAppToken")); token != "" {
		settings.AppToken = token
	}
	if token := strings.TrimSpace(c.FormValue("akahuUserToken")); token != "" {
		settings.UserToken = token
	}
	// A token that does not look like one is the owner's to fix, not a fault.
	if err := settings.Validate(); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	err = app.integrations.PutAkahuSettings(ctx, settings)
	if err != nil {
		return err
	}
	app.Toast(c, "Success", "Akahu settings saved successfully!")
	return nil
}

// SettingsSaveSchedule stores what should run by itself. Last-run times are
// carried across rather than reset, so changing a cadence does not fire a job
// immediately as a side effect of saving.
func (app *Application) SettingsSaveSchedule(c *echo.Context) error {
	ctx := c.Request().Context()

	schedule, err := app.store.GetScheduleSettings(ctx)
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

	if err := app.store.SaveScheduleSettings(ctx, schedule); err != nil {
		app.Toast(c, "Error", "Could not save the schedule.")
		return c.NoContent(http.StatusInternalServerError)
	}

	app.Toast(c, "Success", "Schedule saved.")
	return c.Render(http.StatusOK, "settings.schedule", buildScheduleView(schedule))
}
