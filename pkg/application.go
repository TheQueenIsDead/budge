package pkg

import (
	"github.com/labstack/echo/v4"
	"gorm.io/gorm"
	"net/http"
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
