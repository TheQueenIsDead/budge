package application

import (
	"github.com/TheQueenIsDead/budge/pkg/database/models"
	"github.com/labstack/echo/v4"
	"net/http"
	"slices"
	"time"
)

func (app *Application) ListTransactions(c echo.Context) error {

	account := c.QueryParam("account")
	var transactions []models.Transaction
	var err error
	if account == "" {
		transactions, err = app.store.ReadTransactions()
	} else {
		transactions, err = app.store.ReadTransactionsByAccount(account)
	}
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	slices.SortFunc(transactions, func(a, b models.Transaction) int {
		return b.Date.Compare(a.Date)
	})

	accounts, err := app.store.ReadAccounts()
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	return c.Render(http.StatusOK, "transactions", map[string]interface{}{
		"accounts":     accounts,
		"transactions": transactions,
	})
}

func FindTransactionRange(transactions []models.Transaction) (models.Transaction, models.Transaction) {
	first := models.Transaction{Date: time.Now()}
	last := models.Transaction{Date: time.Unix(0, 0)}

	for _, t := range transactions {
		if t.Date.Before(first.Date) {
			first = t
		}
		if t.Date.After(last.Date) {
			last = t
		}
	}

	return first, last
}
