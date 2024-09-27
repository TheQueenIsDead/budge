package application

import (
	"github.com/TheQueenIsDead/budge/pkg/database/models"
	"github.com/labstack/echo/v4"
	"net/http"
)

func (app *Application) Settings(c echo.Context) error {

	config := app.integrations.Config()
	accounts, err := app.integrations.AkahuAccounts()
	if err != nil {
		return err
	}
	return c.Render(http.StatusOK, "settings", map[string]interface{}{
		"config":   config,
		"accounts": accounts,
	})
}

func (app *Application) SyncAkahu(c echo.Context) error {
	err := app.integrations.SyncAkahu(c)
	if err != nil {
		return err
	}

	app.Toast(c, "Success", "Akahu synced successfully!")

	return nil
}

func (app *Application) PutAkahuSettings(c echo.Context) error {

	settings := models.IntegrationAkahuSettings{
		AppToken:  c.FormValue("akahuAppToken"),
		UserToken: c.FormValue("akahuUserToken"),
	}

	if err := settings.Validate(); err != nil {
		return err
	}

	err := app.integrations.PutAkahuSettings(settings)
	if err != nil {
		return err
	}
	app.Toast(c, "Success", "Akahu settings saved successfully!")
	return nil
}
