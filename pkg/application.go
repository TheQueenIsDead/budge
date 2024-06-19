package pkg

import (
	"github.com/labstack/echo/v4"
	"gorm.io/gorm"
	"net/http"
	"strconv"
)

type Application struct {
	DB   *gorm.DB
	HTTP *echo.Echo
}

func (app *Application) Index(c echo.Context) error {
	return c.Render(http.StatusOK, "index", nil)
}

func (app *Application) Budget(c echo.Context) error {
	var budgetItems []BudgetItem
	app.DB.Find(&budgetItems)
	return c.Render(http.StatusOK, "budget", budgetItems)
}

func (app *Application) Merchant(c echo.Context) error {
	var merchants []Merchant
	app.DB.Find(&merchants)
	return c.Render(http.StatusOK, "merchant", merchants)
}

func (app *Application) EditMerchant(c echo.Context) error {

	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		return c.HTML(http.StatusBadRequest, "id is required")
	}

	var merchant Merchant
	if err = app.DB.First(&merchant, id).Error; err != nil {
		return c.HTML(http.StatusNotFound, "error finding merchant")
	}

	return c.Render(http.StatusOK, "merchant_edit", merchant)
}

func (app *Application) PutMerchant(c echo.Context) error {

	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		return c.HTML(http.StatusBadRequest, "id is required")
	}

	name := c.FormValue("name")
	description := c.FormValue("description")

	var merchant Merchant
	if err := app.DB.First(&merchant, id).Error; err != nil {
		return c.HTML(http.StatusNotFound, "error finding merchant")
	}

	if name != "" {
		merchant.Name = name
	}
	if description != "" {
		merchant.Description = description
	}

	if err = app.DB.Save(merchant).Error; err != nil {
		return c.HTML(http.StatusNotFound, "error saving merchant")
	}

	return c.Render(http.StatusOK, "merchant_row", merchant)
}

func (app *Application) GetMerchant(c echo.Context) error {

	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		return c.HTML(http.StatusBadRequest, "id is required")
	}

	var merchant Merchant
	if err := app.DB.First(&merchant, id).Error; err != nil {
		return c.HTML(http.StatusNotFound, "error finding merchant")
	}

	return c.Render(http.StatusOK, "merchant_row", merchant)
}

func (app *Application) Upload(c echo.Context) error {

	filepath, err := saveFile(c)
	if err != nil {
		c.Logger().Error(err)
		return c.HTML(http.StatusInternalServerError, err.Error())
	}

	transactions, err := parseFile(c, filepath)
	if err != nil {
		c.Logger().Error(err)
		return c.HTML(http.StatusInternalServerError, err.Error())
	}

	// Persist merchant if description is not unique
	for _, transaction := range transactions {
		var m Merchant
		app.DB.FirstOrCreate(&m, Merchant{Description: transaction.Description})
	}

	return c.Render(http.StatusOK, "partial_budget_items", transactions)

	//return c.HTML(http.StatusOK, strconv.Itoa(len(transactions)))
	//return c.HTML(http.StatusOK, fmt.Sprintf("<p>File %s uploaded successfully.</p>", file.Filename))
}

func (app *Application) Layout(c echo.Context) error {
	data := map[string]interface{}{
		"content": "huzzah, this is the beans",
	}
	return c.Render(http.StatusOK, "budget", data)
}
