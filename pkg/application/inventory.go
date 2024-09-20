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

func (app *Application) InventoryNew(c echo.Context) error {
	return c.Render(http.StatusOK, "inventory.new", nil)
}

func (app *Application) InventoryCreate(c echo.Context) error {
	// TODO: Wire this up
	return c.JSON(http.StatusInternalServerError, "not implemented")
}
