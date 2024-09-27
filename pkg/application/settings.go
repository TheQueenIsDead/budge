package application

import (
	"encoding/json"
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

	// Set an HTMX Error even via headers
	event := map[string]interface{}{
		"toast": map[string]string{
			"level":   "Success",
			"message": "Akahu synced successfully!",
		},
	}
	buf, err := json.Marshal(event)
	if err != nil {
		c.Logger().Error(err)
	}
	c.Response().Header().Add("Hx-Trigger", string(buf))

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

	return nil
}
