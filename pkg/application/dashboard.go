package application

import (
	"cmp"
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

type DashboardData struct {
	BalanceCard CardData
	SpendCard   CardData
	IncomeCard  CardData
	SavingsCard CardData

	SpendTimeseries TimeseriesData
	SpendDoughnut   DoughnutData

	TopMerchants      []models.MerchantTotal
	FrequentMerchants []models.MerchantFrequency
}

type CardData struct {
	Total         float64
	PreviousTotal float64
	Delta         float64
}

type TimeseriesData struct {
	Labels []string
	Data   []int
}

type DoughnutData struct {
	Labels []string
	Data   []int
}

// AggregateMonthlyTransactions takes a list of transactions and returns a map with the
func AggregateMonthlyTransactions(transactions []models.Transaction) map[string][]models.Transaction {
	monthlyTransactions := make(map[string][]models.Transaction)
	for _, tx := range transactions {
		month := tx.Date.Format("Jan 06")
		if _, ok := monthlyTransactions[month]; !ok {
			monthlyTransactions[month] = []models.Transaction{tx}
		} else {
			monthlyTransactions[month] = append(monthlyTransactions[month], tx)
		}
	}
	return monthlyTransactions
}

// FilterRecentTransactions takes a list of transactions and returns two lists: past and recent.
// Recent transactions occurred in the last 30 days from the current date.
// Past transactions occurred in the 30 days prior to the recent transactions.
// Transactions older than 60 days are excluded from both lists.
func FilterRecentTransactions(transactions []models.Transaction) (past []models.Transaction, recent []models.Transaction) {
	now := time.Now()
	thirtyDaysAgo := now.AddDate(0, 0, -30)
	sixtyDaysAgo := now.AddDate(0, 0, -60)
	for _, tx := range transactions {
		if tx.Date.After(thirtyDaysAgo) {
			recent = append(recent, tx)
		} else if tx.Date.After(sixtyDaysAgo) {
			past = append(past, tx)
		}
	}
	return
}

func BuildCards(accounts []models.Account, last, current []models.Transaction) (balance, spend, income, savings CardData) {

	// Calculate the current balance across our current accounts
	balance.Total = func() float64 {
		total := 0.0
		for _, account := range accounts {
			total += account.Balance.Current
		}
		return total
	}()

	// Figure out the balance for last month by winding back transactions
	lastBalance := balance.Total
	for _, tx := range current {
		lastBalance -= tx.Amount
	}

	if lastBalance != 0 {
		balance.Delta = (balance.Total - lastBalance) / math.Abs(lastBalance)
	}
	balance.PreviousTotal = lastBalance

	calculateIncomingAndOutgoing := func(transactions []models.Transaction) (incoming, outgoing float64) {
		for _, transaction := range transactions {
			if transaction.Amount > 0 {
				incoming += transaction.Amount
			} else {
				outgoing += transaction.Amount
			}
		}
		return
	}

	lastIn, lastOut := calculateIncomingAndOutgoing(last)
	currentIn, currentOut := calculateIncomingAndOutgoing(current)

	spend.Total = currentOut
	spend.PreviousTotal = lastOut
	if lastOut != 0 {
		// Use positive values for calculation to make it intuitive
		// An increase in spending is a positive delta
		spend.Delta = ((-currentOut) - (-lastOut)) / (-lastOut)
	}

	income.Total = currentIn
	if lastIn != 0 {
		income.Delta = (currentIn - lastIn) / lastIn
	}
	income.PreviousTotal = lastIn

	savings.Total = currentIn + currentOut
	lastSavings := lastIn + lastOut
	if lastSavings != 0 {
		savings.Delta = (savings.Total - lastSavings) / lastSavings
	}
	savings.PreviousTotal = lastSavings

	return
}
func BuildTimeseriesData(monthlyTransactions map[string][]models.Transaction) TimeseriesData {

	// For every month, sum the spend
	timeseriesMap := make(map[string]int)
	sumSpend := func(transactions []models.Transaction) int {
		total := 0.0
		for _, tx := range transactions {
			// Only sum the spend
			if tx.Amount < 0 {
				total -= tx.Amount
			}
		}
		return int(total)
	}
	for month, transactions := range monthlyTransactions {
		timeseriesMap[month] = sumSpend(transactions)
	}

	// Sort the labels by date
	timeseriesLabels := slices.Collect(maps.Keys(timeseriesMap))
	sort.Slice(timeseriesLabels, func(i, j int) bool {
		id, _ := time.Parse("Jan 06", timeseriesLabels[i])
		jd, _ := time.Parse("Jan 06", timeseriesLabels[j])
		return id.Before(jd)
	})

	// Build an array of data based on the sorted labels
	timeseriesData := make([]int, len(timeseriesLabels))
	for i, label := range timeseriesLabels {
		timeseriesData[i] = timeseriesMap[label]
	}

	return TimeseriesData{timeseriesLabels, timeseriesData}
}
func BuildDoughnutData(transactions []models.Transaction) DoughnutData {

	totalSpend := 0.0
	categoryMap := map[string]float64{}
	for _, tx := range transactions {
		if tx.Amount >= 0 || tx.Category.Groups.PersonalFinance.Name == "" || tx.Type == "TRANSFER" {
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
	var categorySorting []struct {
		category string
		sum      float64
	}
	for k, v := range categoryMap {
		categorySorting = append(categorySorting, struct {
			category string
			sum      float64
		}{category: k, sum: v})
	}
	sort.Slice(categorySorting, func(i, j int) bool {
		return categorySorting[i].sum < categorySorting[j].sum
	})
	var categoryLabels []string
	var categoryData []int
	for _, category := range categorySorting {
		pct := category.sum / totalSpend * 100
		categoryLabels = append(categoryLabels, fmt.Sprintf("%s %.f%%", category.category, pct))
		categoryData = append(categoryData, int(pct))
	}

	return DoughnutData{categoryLabels, categoryData}
}
func BuildTopMerchants(last, current []models.Transaction, n int) []models.MerchantTotal {
	// Aggregate spend across merchants by sum for past and recent
	recentSpend := make(map[string]float64)
	for _, transaction := range current {
		recentSpend[transaction.Merchant.Name] += transaction.Float()
	}
	pastSpend := make(map[string]float64)
	for _, transaction := range last {
		pastSpend[transaction.Merchant.Name] += transaction.Float()
	}

	// Place the recent merchant spend in a list of bespoke structs for ordering later on
	var top []models.MerchantTotal
	for merchant, total := range recentSpend {
		if merchant == "" {
			continue
		}
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

	// For the N merchants, calculate the delta in spend from past to recent transactions
	for i, merchantTotal := range results {
		if pastTotal, ok := pastSpend[merchantTotal.Merchant]; ok {
			if pastTotal != 0 {
				merchantTotal.Delta = (merchantTotal.Total - pastTotal) / pastTotal
			}
			merchantTotal.PreviousTotal = pastTotal
			results[i] = merchantTotal
		}
	}

	return results
}
func BuildFrequentMerchants(transactions []models.Transaction, n int) []models.MerchantFrequency {

	// Count all merchant transactions
	merchantCount := make(map[string]int)
	for _, tx := range transactions {
		merchantCount[tx.Merchant.Name]++
	}

	// Parse the counts into objects
	var results []models.MerchantFrequency
	for merchant, count := range merchantCount {
		results = append(results, models.MerchantFrequency{
			Merchant: merchant,
			Count:    count,
		})
	}

	// Sort the list by merchant total to find the ones with the most spend
	sort.Slice(results, func(i, j int) bool {
		return results[i].Count > results[j].Count
	})

	// Limit the results to n elements
	if len(results) < n {
		return results
	}

	return results[:n]
}

func (app *Application) Dashboard(c echo.Context) error {

	// Retrieve accounts and 6 months worth of transactions
	accounts, accountErr := app.store.ReadAccounts()
	transactions, transactionErr := app.store.ReadTransactionsByDate(time.Now().AddDate(0, -6, 0), time.Now())
	if err := cmp.Or(accountErr, transactionErr); err != nil {
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
	pastTransactions, recentTransactions := FilterRecentTransactions(nonTransferTransactions)

	// Build cards based on differences between the last 30 days, and the 30 days prior to that
	balance, spend, income, savings := BuildCards(accounts, pastTransactions, recentTransactions)

	return c.Render(http.StatusOK, "dashboard", DashboardData{
		BalanceCard:       balance,
		SpendCard:         spend,
		IncomeCard:        income,
		SavingsCard:       savings,
		SpendTimeseries:   BuildTimeseriesData(monthlyTransactions),
		SpendDoughnut:     BuildDoughnutData(nonTransferTransactions),
		TopMerchants:      BuildTopMerchants(pastTransactions, recentTransactions, 10),
		FrequentMerchants: BuildFrequentMerchants(nonTransferTransactions, 10),
	})
}
func (app *Application) _4XX(c echo.Context) error {
	return c.Render(http.StatusOK, "4XX", nil)
}
