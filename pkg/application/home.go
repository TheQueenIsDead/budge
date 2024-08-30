package application

import (
	"github.com/TheQueenIsDead/budge/pkg/database/models"
	"github.com/labstack/echo/v4"
	"golang.org/x/text/language"
	"golang.org/x/text/message"
	"net/http"
	"sort"
)

func topMerchants(transactions []models.Transaction, n int) []models.MerchantTotal {

	merchants := make(map[string]float64)
	for _, tx := range transactions {
		if tx.Type == models.TransactionTypeCredit {
			merchants[tx.Merchant] += tx.Float()
		}
	}

	var top []models.MerchantTotal
	for merchant, total := range merchants {
		top = append(top, models.MerchantTotal{
			Merchant: merchant,
			Total:    total,
		})
	}

	sort.Slice(top, func(i, j int) bool {
		return top[i].Total > top[j].Total
	})

	return top[:n]
}

func (app *Application) Home(c echo.Context) error {

	var accountCount, transactionCount, merchantCount int
	var err error

	accountCount, err = app.store.Accounts.Count()
	transactionCount, err = app.store.Transactions.Count()
	merchantCount, err = app.store.Merchants.Count()

	var transactions []models.Transaction
	transactions, err = app.store.Transactions.List()
	if err != nil {
		c.Logger().Error(err)
		return err
	}

	top := topMerchants(transactions, 5)

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
		"topMerchants":     top,
	})
}

//
//func (app *Application) Layout(c echo.Context) error {
//	data := map[string]interface{}{
//		"content": "huzzah, this is the beans",
//	}
//	return c.Render(http.StatusOK, "budget", data)
//}