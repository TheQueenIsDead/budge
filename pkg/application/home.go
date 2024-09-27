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
		merchants[tx.Description] += tx.Float()
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

	if len(top) >= n {
		return top[:n]
	}
	return top[:]
}

func (app *Application) Home(c echo.Context) error {

	var accountCount, transactionCount, merchantCount int
	var err error

	accountCount, err = app.store.CountAccount()
	if err != nil {
		return err
	}
	transactionCount, err = app.store.CountTransactions()
	if err != nil {
		return err
	}
	merchantCount, err = app.store.CountMerchant()
	if err != nil {
		return err
	}

	var transactions []models.Transaction
	transactions, err = app.store.ReadTransactions()
	if err != nil {
		c.Logger().Error(err)
		return err
	}

	top := topMerchants(transactions, 5)

	categories := make(map[string]float64)
	var in, out float64
	for _, transaction := range transactions {
		if transaction.Amount < 0 {
			out += transaction.Float()
			category := transaction.Category.Groups.PersonalFinance.Name
			if category != "" {
				categories[category] += transaction.Amount
			}
		} else {
			in += transaction.Float()
		}
	}

	var categoryLabels []string
	var categoryData []float64
	for k, v := range categories {
		categoryLabels = append(categoryLabels, k)
		categoryData = append(categoryData, v)
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
		"category_labels":  categoryLabels,
		"category_data":    categoryData,
	})
}
func (app *Application) _4XX(c echo.Context) error {
	return c.Render(http.StatusOK, "4XX", nil)
}
