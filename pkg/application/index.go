package application

import (
	"github.com/labstack/echo/v4"
	"net/http"
)

func (app *Application) Index(c echo.Context) error {

	var accountCount, transactionCount, merchantCount int

	// TODO: reenable
	//accountCount = GetAccountCount(app.DB)
	//transactionCount = GetTransactionCount(app.DB)
	//merchantCount = GetMerchantCount(app.DB)

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

//
//func (app *Application) Layout(c echo.Context) error {
//	data := map[string]interface{}{
//		"content": "huzzah, this is the beans",
//	}
//	return c.Render(http.StatusOK, "budget", data)
//}
