package application

import (
	"github.com/labstack/echo/v4"
	"net/http"
)

func (app *Application) Inventory(c echo.Context) error {
	inventory, _ := app.store.Inventory.List()
	return c.Render(http.StatusOK, "inventory", map[string]interface{}{
		"inventory": inventory,
	})
}
