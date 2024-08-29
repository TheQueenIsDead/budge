package application

import (
	"github.com/TheQueenIsDead/budge/pkg/database/models"
	"github.com/labstack/echo/v4"
	"golang.org/x/text/language"
	"golang.org/x/text/message"
	"net/http"
)

func (app *Application) Home(c echo.Context) error {

	var accountCount, transactionCount, merchantCount int
	var err error

	// TODO: reenable
	accountCount, err = app.store.Accounts.Count()
	transactionCount, err = app.store.Transactions.Count()
	merchantCount, err = app.store.Merchants.Count()

	var transactions []models.Transaction
	transactions, err = app.store.Transactions.List()
	if err != nil {
		c.Logger().Error(err)
		return err
	}

	var in, out float64
	for _, transaction := range transactions {
		switch transaction.Type {
		case models.TransactionTypeDebit:
			in += transaction.Float()
		case models.TransactionTypeCredit:
			out += transaction.Float()
		}
	}

	var incomingString, outgoingString string
	p := message.NewPrinter(language.English)
	incomingString = p.Sprintf("%.2f", in)
	outgoingString = p.Sprintf("%.2f", out)

	return c.Render(http.StatusOK, "home", map[string]interface{}{
		"accountCount":     accountCount,
		"transactionCount": transactionCount,
		"merchantCount":    merchantCount,
		"incoming":         in,
		"incomingString":   incomingString,
		"outgoing":         out,
		"outgoingString":   outgoingString,
	})
}

//
//func (app *Application) Layout(c echo.Context) error {
//	data := map[string]interface{}{
//		"content": "huzzah, this is the beans",
//	}
//	return c.Render(http.StatusOK, "budget", data)
//}
