package application

import (
	"github.com/labstack/echo/v4"
	"net/http"
)

func (app *Application) Settings(c echo.Context) error {

	config := app.integrations.Config()
	return c.Render(http.StatusOK, "settings", config)
}
