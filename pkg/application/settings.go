package application

import (
	"github.com/labstack/echo/v4"
	"net/http"
)

func (app *Application) Settings(c echo.Context) error {

	akahuConfig, err := app.store.GetAkahuSettings()
	if err != nil {
		app.Toast(c, "Error", "Could not get Akahu settings.")
		return c.NoContent(http.StatusInternalServerError)
	}
	return c.Render(http.StatusOK, "settings", map[string]interface{}{
		"akahuAppToken":  akahuConfig.AppToken,
		"akahuUserToken": akahuConfig.UserToken,
		"akahuLastSync":  akahuConfig.LastSync,
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

	return nil
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

func (app *Application) ListAkahuAccounts(c echo.Context) error {
	accounts, err := app.integrations.AkahuAccounts()
	if err != nil {
		return err
	}
	return c.Render(http.StatusOK, "akahu.accounts", map[string]interface{}{
		"accounts": accounts,
	})
}
