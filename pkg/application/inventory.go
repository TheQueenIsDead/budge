package application

import (
	"github.com/TheQueenIsDead/budge/pkg/database/models"
	"github.com/labstack/echo/v4"
	"net/http"
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
