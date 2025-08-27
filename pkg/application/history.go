package application

import (
	"math"
	"net/http"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/TheQueenIsDead/budge/pkg/database/models"
	"github.com/labstack/echo/v4"
)

type MonthlyTransactions struct {
	Month          string
	Transactions   []models.Transaction
	MerchantTotals []models.MerchantTotal
	Total          float64
	Slug           string
}

type HistoryData struct {
	SpendTimeseries TimeseriesData
	//Transactions        []models.Transaction
	//MonthlyTransactions map[string][]models.Transaction
	History []MonthlyTransactions
}

func TransactionTotalByMerchant(transactions []models.Transaction) []models.MerchantTotal {

	// Aggregate spend across merchants by sum for past and recent
	merchantTotal := make(map[string]float64)
	for _, transaction := range transactions {
		merchantTotal[transaction.Merchant.Name] += transaction.Float()
	}

	// Place the recent merchant spend in a list of bespoke structs for ordering later on
	var merchantTotals []models.MerchantTotal
	for merchant, total := range merchantTotal {
		if merchant == "" {
			continue
		}
		merchantTotals = append(merchantTotals, models.MerchantTotal{
			Merchant: merchant,
			Total:    total,
		})
	}

	// Sort the list by merchant total to find the ones with the most spend
	sort.Slice(merchantTotals, func(i, j int) bool {
		return merchantTotals[i].Total < merchantTotals[j].Total
	})

	return merchantTotals
}

func (app *Application) History(c echo.Context) error {

	// Retrieve 6 months worth of transactions
	start := time.Now().AddDate(0, -6, 0)
	app.http.Logger.Print("start: ", start)
	start = time.Date(start.Year(), start.Month(), 1, 0, 0, 0, 0, start.Location())
	start = start.AddDate(0, 0, (start.Day()*-1)+1)
	app.http.Logger.Print("start--: ", start)

	transactions, transactionErr := app.store.ReadTransactionsByDate(start, time.Now())
	if err := transactionErr; err != nil {
		app.Toast(c, "Error", "Could not load dashboard data.")
		return c.NoContent(http.StatusInternalServerError)
	}

	// Filter out transfers
	var nonTransferTransactions []models.Transaction
	for _, tx := range transactions {
		if tx.Type != "TRANSFER" {
			nonTransferTransactions = append(nonTransferTransactions, tx)
		}
	}

	// Filter out transfers and categorise transactions into months
	monthlyTransactions := AggregateMonthlyTransactions(nonTransferTransactions)
	for key := range monthlyTransactions {
		slices.SortFunc(monthlyTransactions[key], func(a, b models.Transaction) int {
			if a.Amount < b.Amount {
				return -1
			} else if a.Amount > b.Amount {
				return 1
			}
			return 0
		})
	}

	var monthlyHistory []MonthlyTransactions
	for month, transactions := range monthlyTransactions {
		history := MonthlyTransactions{
			Transactions:   transactions,
			Total:          0.0,
			Month:          month,
			MerchantTotals: TransactionTotalByMerchant(transactions),
			Slug:           strings.ToLower(strings.ReplaceAll(month, " ", "")),
		}

		for _, tx := range transactions {
			history.Total += math.Min(0.0, tx.Amount)
		}
		monthlyHistory = append(monthlyHistory, history)
	}

	slices.SortFunc(monthlyHistory, func(a, b MonthlyTransactions) int {
		aDate, _ := time.Parse("Jan 06", a.Month)
		bDate, _ := time.Parse("Jan 06", b.Month)
		return aDate.Compare(bDate)
	})

	return c.Render(http.StatusOK, "history", HistoryData{
		SpendTimeseries: BuildTimeseriesData(monthlyTransactions),
		History:         monthlyHistory,
	})
}
