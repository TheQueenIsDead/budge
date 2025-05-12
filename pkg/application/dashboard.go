package application

import (
	"fmt"
	"github.com/TheQueenIsDead/budge/pkg/database/models"
	"github.com/labstack/echo/v4"
	"maps"
	"math"
	"net/http"
	"slices"
	"sort"
	"time"
)

func topMerchants(recent []models.Transaction, past []models.Transaction, n int) []models.MerchantTotal {

	// Aggregate spend across merchants by sum for past and recent
	recentSpend := make(map[string]float64)
	for _, transaction := range recent {
		recentSpend[transaction.Merchant.Name] += transaction.Float()
	}
	pastSpend := make(map[string]float64)
	for _, transaction := range past {
		pastSpend[transaction.Merchant.Name] += transaction.Float()
	}

	// Place the recent merchant spend in a list of bespoke structs for ordering later on
	var top []models.MerchantTotal
	for merchant, total := range recentSpend {
		top = append(top, models.MerchantTotal{
			Merchant: merchant,
			Total:    total,
		})
	}

	// Sort the list by merchant total to find the ones with the most spend
	sort.Slice(top, func(i, j int) bool {
		return top[i].Total < top[j].Total
	})

	// Filter the list of top merchants to a max of n elements
	results := make([]models.MerchantTotal, n)
	if len(top) >= n {
		results = top[:n]
	}

	// For the N merchants, calculate the delta in spend from past transactions
	for i, merchantTotal := range results {
		if pastTotal, ok := pastSpend[merchantTotal.Merchant]; ok {
			merchantTotal.Delta = (pastTotal / merchantTotal.Total) * 100
			results[i] = merchantTotal
		}
	}

	return results
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
		if transaction.Merchant.Name == "" {
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
		if transaction.Merchant.Name == "" {
			continue
		}
		if transaction.Amount > 0.0 {
			pastIncome += transaction.Amount
		} else {
			pastSpend += transaction.Amount
		}
	}

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

	top := topMerchants(recentTransactions, pastTransactions, 5)

	start = time.Now().AddDate(0, -6, 0)
	end = time.Now()
	halfYearTransactions, err := app.store.ReadTransactionsByDate(start, end)
	if err != nil {
		c.Logger().Error(err)
		return err
	}
	timeseriesMap := map[string]int{}
	for _, transaction := range halfYearTransactions {
		if transaction.Amount >= 0 {
			continue // Ignore income for the spend report
		}
		key := transaction.Date.Format("Jan 06")
		if _, ok := timeseriesMap[key]; !ok {
			timeseriesMap[key] = int(transaction.Amount) * -1
		} else {
			timeseriesMap[key] = timeseriesMap[key] + int(transaction.Amount)*-1
		}
	}

	timeseriesLabels := slices.Collect(maps.Keys(timeseriesMap))
	sort.Slice(timeseriesLabels, func(i, j int) bool {
		id, _ := time.Parse("Jan 06", timeseriesLabels[i])
		jd, _ := time.Parse("Jan 06", timeseriesLabels[j])
		return id.Before(jd)
	})
	timeseriesData := make([]int, len(timeseriesLabels))
	for i, label := range timeseriesLabels {
		timeseriesData[i] = timeseriesMap[label]
	}

	totalSpend := 0.0
	categoryMap := map[string]float64{}
	for _, tx := range halfYearTransactions {
		if tx.Amount >= 0 || tx.Category.Groups.PersonalFinance.Name == "" {
			continue
		}
		category := tx.Category.Groups.PersonalFinance.Name
		if _, ok := categoryMap[category]; !ok {
			categoryMap[category] = tx.Amount
		} else {
			categoryMap[category] += tx.Amount
		}
		totalSpend += tx.Amount
	}
	categorySorting := []struct {
		category string
		sum      float64
	}{}
	for k, v := range categoryMap {
		categorySorting = append(categorySorting, struct {
			category string
			sum      float64
		}{category: k, sum: v})
	}
	sort.Slice(categorySorting, func(i, j int) bool {
		return categorySorting[i].sum < categorySorting[j].sum
	})
	categoryLabels := []string{}
	categoryData := []int{}
	for _, category := range categorySorting {
		pct := category.sum / totalSpend * 100
		categoryLabels = append(categoryLabels, fmt.Sprintf("%s %.f%%", category.category, pct))
		categoryData = append(categoryData, int(pct))
	}

	return c.Render(http.StatusOK, "dashboard", map[string]interface{}{
		"topMerchants": top,
		// TODO: Replace with sourced data
		"totalBalance":      totalBalance,
		"totalBalanceDelta": 0.035,
		"monthlySpend":      analysis.MonthlySpend,
		"monthlySpendDelta": analysis.MonthlySpendDelta,
		"income":            analysis.Income,
		"incomeDelta":       analysis.IncomeDelta,
		"savings":           math.Abs(analysis.Income) - math.Abs(analysis.MonthlySpend),
		"savingsDelta":      0.083,
		"timeseriesData":    timeseriesData,
		"timeseriesLabels":  timeseriesLabels,
		"categoryLabels":    categoryLabels,
		"categoryData":      categoryData,
	})
}
func (app *Application) _4XX(c echo.Context) error {
	return c.Render(http.StatusOK, "4XX", nil)
}
