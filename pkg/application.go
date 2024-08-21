package pkg

import (
	"github.com/labstack/echo/v4"
	bolt "go.etcd.io/bbolt"
	"net/http"
)

type Application struct {
	DB   *bolt.DB
	HTTP *echo.Echo
}

func (app *Application) Index(c echo.Context) error {

	var accountCount, transactionCount, merchantCount int64

	//app.DB.Model(&Account{}).Count(&accountCount)
	//app.DB.Model(&Transaction{}).Count(&transactionCount)
	//app.DB.Model(&Merchant{}).Count(&merchantCount)

	//var transactions []Transaction
	//tx := app.DB.Find(&transactions).Limit(1)
	//if err := tx.Error; err != nil {
	//	c.Logger().Error(err)
	//	return err
	//}

	//var in, out uint32
	//for _, transaction := range transactions {
	//	switch transaction.Type {
	//	case TransactionTypeDebit:
	//		in += transaction.Value
	//	case TransactionTypeCredit:
	//		out += transaction.Value
	//	}
	//}

	return c.Render(http.StatusOK, "home", map[string]interface{}{
		"accountCount":     accountCount,
		"transactionCount": transactionCount,
		"merchantCount":    merchantCount,
	})
}

//	func (app *Application) Budget(c echo.Context) error {
//		var budgetItems []BudgetItem
//		app.DB.Find(&budgetItems)
//		return c.Render(http.StatusOK, "budget", budgetItems)
//	}
//
//	func (app *Application) Merchant(c echo.Context) error {
//		var merchants []Merchant
//		app.DB.Find(&merchants)
//		return c.Render(http.StatusOK, "merchant.list", merchants)
//	}
func (app *Application) ListAccounts(c echo.Context) error {
	// TODO: Remove insert of dummy accounts
	err := PutAccount(app.DB, &Account{
		Number:       "6969",
		Transactions: nil,
		Bank:         Bank(Kiwibank),
	})
	if err != nil {
		return err
	}
	accounts := ListAccounts(app.DB)
	return c.Render(http.StatusOK, "account.list", accounts)
}

func (app *Application) ListMerchants(c echo.Context) error {
	merchants := ListMerchants(app.DB)
	return c.Render(http.StatusOK, "merchant.list", merchants)
}

func (app *Application) ListTransactions(c echo.Context) error {
	transactions := ListTransactions(app.DB)
	return c.Render(http.StatusOK, "transaction.list", transactions)
}

//func (app *Application) EditMerchant(c echo.Context) error {
//
//	id, err := strconv.Atoi(c.Param("id"))
//	if err != nil {
//		return c.HTML(http.StatusBadRequest, "id is required")
//	}
//
//	var merchant Merchant
//	if err = app.DB.First(&merchant, id).Error; err != nil {
//		return c.HTML(http.StatusNotFound, "error finding merchant")
//	}
//
//	return c.Render(http.StatusOK, "merchant_edit", merchant)
//}
//
//func (app *Application) PutMerchant(c echo.Context) error {
//
//	id, err := strconv.Atoi(c.Param("id"))
//	if err != nil {
//		return c.HTML(http.StatusBadRequest, "id is required")
//	}
//
//	name := c.FormValue("name")
//	description := c.FormValue("description")
//
//	var merchant Merchant
//	if err := app.DB.First(&merchant, id).Error; err != nil {
//		return c.HTML(http.StatusNotFound, "error finding merchant")
//	}
//
//	if name != "" {
//		merchant.Name = name
//	}
//	if description != "" {
//		merchant.Description = description
//	}
//
//	if err = app.DB.Save(merchant).Error; err != nil {
//		return c.HTML(http.StatusNotFound, "error saving merchant")
//	}
//
//	return c.Render(http.StatusOK, "merchant_row", merchant)
//}
//
//func (app *Application) GetMerchant(c echo.Context) error {
//
//	id, err := strconv.Atoi(c.Param("id"))
//	if err != nil {
//		return c.HTML(http.StatusBadRequest, "id is required")
//	}
//
//	var merchant Merchant
//	if err := app.DB.First(&merchant, id).Error; err != nil {
//		return c.HTML(http.StatusNotFound, "error finding merchant")
//	}
//
//	return c.Render(http.StatusOK, "merchant_row", merchant)
//}
//

func (app *Application) Upload(c echo.Context) error {

	filepath, err := saveFile(c)
	if err != nil {
		c.Logger().Error(err)
		return c.HTML(http.StatusInternalServerError, err.Error())
	}

	accounts, merchants, err := ParseCSV(c, filepath)
	if err != nil {
		c.Logger().Error(err)
		return c.HTML(http.StatusInternalServerError, err.Error())
	}

	err = ImportAccounts(app.DB, accounts)
	if err != nil {
		c.Logger().Error(err)
		return err
	}

	err = ImportMerchants(app.DB, merchants)
	if err != nil {
		c.Logger().Error(err)
		return err
	}

	// TODO: Redirect the user to a more pertinent page.
	return c.Render(http.StatusOK, "partial_budget_items", nil)

	//return c.HTML(http.StatusOK, strconv.Itoa(len(transactions)))
	//return c.HTML(http.StatusOK, fmt.Sprintf("<p>File %s uploaded successfully.</p>", file.Filename))
}

//
//func (app *Application) Layout(c echo.Context) error {
//	data := map[string]interface{}{
//		"content": "huzzah, this is the beans",
//	}
//	return c.Render(http.StatusOK, "budget", data)
//}
