package application

import (
	"github.com/TheQueenIsDead/budge/pkg/database/models"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"math"
	"net/http"
	"strconv"
	"time"
)

func (app *Application) Inventory(c echo.Context) error {
	inventory, _ := app.store.ReadInventory()
	return c.Render(http.StatusOK, "inventory", map[string]interface{}{
		"inventory": inventory,
	})
}

func (app *Application) InventoryNew(c echo.Context) error {
	return c.Render(http.StatusOK, "inventory.new", map[string]interface{}{
		"today": time.Now().Format("2006-01-02"),
	})
}

func (app *Application) InventoryCreate(c echo.Context) error {

	name := c.Request().FormValue("name")
	costString := c.Request().FormValue("cost")
	cost, err := strconv.ParseFloat(costString, 64)
	if err != nil {
		return c.String(http.StatusBadRequest, err.Error())
	}
	quantityString := c.Request().FormValue("quantity")
	quantity, err := strconv.ParseInt(quantityString, 10, 64)
	if err != nil {
		return c.String(http.StatusBadRequest, err.Error())
	}
	if quantity < math.MinInt || quantity > math.MaxInt {
		return c.String(http.StatusBadRequest, "quantity out of bounds")
	}
	dateString := c.Request().FormValue("date")
	date, err := time.Parse("2006-01-02", dateString)
	if err != nil {
		return c.String(http.StatusBadRequest, err.Error())
	}
	description := c.Request().FormValue("description")

	item := models.Inventory{
		Id:          uuid.New().String(),
		CreatedAt:   time.Time{},
		Date:        date,
		Description: description,
		Cost:        cost,
		Category:    "",
		Name:        name,
		Quantity:    int(quantity),
	}

	err = app.store.CreateInventory(item)
	if err != nil {
		c.Logger().Error(err)
		return c.JSON(http.StatusInternalServerError, err.Error())
	}

	return c.Redirect(http.StatusFound, "/inventory")
}

func (app *Application) DeleteInventory(c echo.Context) error {

	id := c.Param("id")
	if id == "" {
		return c.String(http.StatusBadRequest, "id is required")
	}
	err := app.store.DeleteInventory([]byte(id))
	if err != nil {
		c.Logger().Error(err)
		return c.JSON(http.StatusInternalServerError, err.Error())
	}
	return c.NoContent(http.StatusOK)
}
