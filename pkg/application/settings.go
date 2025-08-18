package application

import (
	"github.com/labstack/echo/v4"
	"net/http"
	"time"
)

func (app *Application) Settings(c echo.Context) error {
	return c.Render(http.StatusOK, "settings", map[string]interface{}{
		"tab": "/settings/budge",
	})
}

func (app *Application) SettingsBudge(c echo.Context) error {

	accounts, err := app.store.ReadAccounts()
	if err != nil {
		app.Toast(c, "Error", "Could not read accounts.")
		return c.NoContent(http.StatusInternalServerError)
	}

	data := map[string]interface{}{
		"accounts": accounts,
		"tab":      "/settings/budge",
	}

	// If the request was initiated by HTMX, return only the tab
	if hx := c.Request().Header.Get("HX-Request"); hx != "" {
		return c.Render(http.StatusOK, "settings.budge", data)
	}

	// Else, render the settings page and instruct it to load a specific tab
	return c.Render(http.StatusOK, "settings", data)
}

func (app *Application) SettingsIntegrations(c echo.Context) error {
	akahuConfig, err := app.store.GetAkahuSettings()
	if err != nil {
		app.Toast(c, "Error", "Could not get Akahu settings.")
		return c.NoContent(http.StatusInternalServerError)
	}

	data := map[string]interface{}{
		"tab":            "/settings/integrations",
		"akahuAppToken":  akahuConfig.AppToken,
		"akahuUserToken": akahuConfig.UserToken,
		"akahuLastSync":  akahuConfig.LastSync,
	}

	// If the request was initiated by HTMX, return only the tab
	if hx := c.Request().Header.Get("HX-Request"); hx != "" {
		return c.Render(http.StatusOK, "settings.integrations", data)
	}

	// Else, render the settings page and instruct it to load a specific tab
	return c.Render(http.StatusOK, "settings", data)

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
		return err
	}

	err = app.integrations.SyncAkahu(c, akahuConfig.LastSync)
	if err != nil {
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
