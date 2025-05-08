package application

import (
	"github.com/TheQueenIsDead/budge/pkg/database/models"
	"github.com/labstack/echo/v4"
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
		return top[i].Total < top[j].Total
	})

	if len(top) >= n {
		return top[:n]
	}
	return top[:]
}

func (app *Application) Dashboard(c echo.Context) error {

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

	totalBalance, err := app.store.GetAccountsTotal()

	var transactions []models.Transaction
	transactions, err = app.store.ReadTransactions()
	if err != nil {
		c.Logger().Error(err)
		return err
	}

	top := topMerchants(transactions, 5)

	if accountCount == 0 && transactionCount == 0 && merchantCount == 0 {
		return c.Render(http.StatusOK, "empty", nil)
	}

	return c.Render(http.StatusOK, "dashboard", map[string]interface{}{
		"topMerchants": top,
		// TODO: Replace with sourced data
		"totalBalance":      totalBalance,
		"totalBalanceDelta": 0.035,
		"monthlySpend":      2156.42,
		"monthlySpendDelta": 0.021,
		"income":            4890.00,
		"incomeDelta":       0.012,
		"savings":           2733.58,
		"savingsDelta":      0.083,
	})
}
func (app *Application) _4XX(c echo.Context) error {
	return c.Render(http.StatusOK, "4XX", nil)
}
