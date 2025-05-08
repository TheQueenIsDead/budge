package application

import (
	"fmt"
	"github.com/TheQueenIsDead/budge/pkg/database/models"
	"github.com/labstack/echo/v4"
	"net/http"
	"sort"
	"time"
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

type DashboardAnalysis struct {
	MonthlySpend      float64
	MonthlySpendDelta float64
	Income            float64
	IncomeDelta       float64
}

func analyseTransactions(recent []models.Transaction, past []models.Transaction) DashboardAnalysis {

	recentSpend := 0.0
	recentIncome := 0.0
	for _, transaction := range recent {
		if transaction.Type == "TRANSFER" {
			continue
		}
		if transaction.Amount > 0.0 {
			recentIncome += transaction.Amount
		} else {
			recentSpend += transaction.Amount
		}
	}

	pastSpend := 0.0
	pastIncome := 0.0
	for _, transaction := range past {
		// TODO: Filter transactions out if they went between accounts.
		if transaction.Type == "TRANSFER" {
			continue
		}
		if transaction.Amount > 0.0 {
			pastIncome += transaction.Amount
		} else {
			pastSpend += transaction.Amount
		}
	}

	fmt.Println(pastSpend, recentSpend, pastIncome, recentIncome)

	return DashboardAnalysis{
		MonthlySpend:      recentSpend,
		MonthlySpendDelta: (pastSpend / recentSpend),
		Income:            recentIncome,
		IncomeDelta:       pastIncome / recentIncome,
	}
}

func (app *Application) Dashboard(c echo.Context) error {

	var err error

	totalBalance, err := app.store.GetAccountsTotal()

	start := time.Now().AddDate(0, 0, -60)
	end := time.Now().AddDate(0, 0, -30)
	pastTransactions, err := app.store.ReadTransactionsByDate(start, end)
	if err != nil {
		c.Logger().Error(err)
		return err
	}

	start = time.Now().AddDate(0, 0, -30)
	end = time.Now()
	recentTransactions, err := app.store.ReadTransactionsByDate(start, end)
	if err != nil {
		c.Logger().Error(err)
		return err
	}

	analysis := analyseTransactions(recentTransactions, pastTransactions)
	fmt.Print(analysis)

	top := topMerchants(recentTransactions, 5)

	return c.Render(http.StatusOK, "dashboard", map[string]interface{}{
		"topMerchants": top,
		// TODO: Replace with sourced data
		"totalBalance":      totalBalance,
		"totalBalanceDelta": 0.035,
		"monthlySpend":      analysis.MonthlySpend,
		"monthlySpendDelta": analysis.MonthlySpendDelta,
		"income":            analysis.Income,
		"incomeDelta":       analysis.IncomeDelta,
		"savings":           2733.58,
		"savingsDelta":      0.083,
	})
}
func (app *Application) _4XX(c echo.Context) error {
	return c.Render(http.StatusOK, "4XX", nil)
}
