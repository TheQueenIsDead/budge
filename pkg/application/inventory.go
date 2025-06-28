package application

import (
	"net/http"

	"github.com/TheQueenIsDead/budge/pkg/database/models"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
)

func (app *Application) Inventory(c echo.Context) error {

	var inventory []models.Inventory
	inventory, err := app.store.ReadInventory()
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	return c.Render(http.StatusOK, "inventory", map[string]interface{}{
		"inventory":        inventory,
		"totalAssets":      0.0,
		"totalLiabilities": 0.0,
		"netWorth":         0.0,
		"debtToAssetRatio": 0.0,
	})
}

func (app *Application) PutInventory(c echo.Context) error {
	inventory := &models.Inventory{}
	if err := c.Bind(inventory); err != nil {
		return err
	}

	// Generate an ID for the new object
	id := uuid.New().String()
	inventory.Id = id

	// Insert the new object into the database

	err := app.store.CreateInventory(*inventory)
	if err != nil {
		return err
	}

	return c.HTML(http.StatusCreated, "Created!")
}
