package application

import (
	"cmp"
	"fmt"
	"maps"
	"math"
	"net/http"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/TheQueenIsDead/budge/pkg/database/models"
	"github.com/labstack/echo/v4"
)

type DashboardData struct {
	BalanceCard CardData
	SpendCard   CardData
	IncomeCard  CardData
	SavingsCard CardData

	SpendTimeseries TimeseriesData
	SpendDoughnut   DoughnutData

	TopMerchants                []models.MerchantTotal
	HighestOutgoingTransactions []OutgoingTransaction
}

type OutgoingTransaction struct {
	Description string
	Amount      float64
	Date        time.Time
}

func BuildHighestOutgoingTransactions(current []models.Transaction, n int) []OutgoingTransaction {
	if n <= 0 || len(current) == 0 {
		return nil
	}

	var outgoing []OutgoingTransaction
	for _, tx := range current {
		// Exclude loan interest transactions for this report
		if strings.ToUpper(tx.Description) == "LOAN INTEREST" {
			continue
		}
		// Only consider outgoing transactions
		if tx.Amount < 0 {
			description := tx.Merchant.Name
			if description == "" {
				description = tx.Description
			}
			outgoing = append(outgoing, OutgoingTransaction{
				Description: description,
				Amount:      tx.Amount,
				Date:        tx.Date,
			})
		}
	}

	// Sort by absolute amount (highest outgoing first)
	sort.Slice(outgoing, func(i, j int) bool {
		return math.Abs(outgoing[i].Amount) > math.Abs(outgoing[j].Amount)
	})

	// Limit to n elements
	if len(outgoing) >= n {
		outgoing = outgoing[:n]
	}

	return outgoing
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

// AggregateMonthlyTransactions groups transactions by the month they fall in,
// read in the given location. Akahu stores dates in UTC, so a transaction late
// on the last of the month counts against the next one unless it is read
// locally first.
func AggregateMonthlyTransactions(transactions []models.Transaction, location *time.Location) map[string][]models.Transaction {
	if location == nil {
		location = time.Local
	}
	monthlyTransactions := make(map[string][]models.Transaction)
	for _, tx := range transactions {
		month := tx.Date.In(location).Format("Jan 06")
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

	if len(accounts) == 0 || len(last) == 0 || len(current) == 0 {
		return
	}

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

	if len(monthlyTransactions) == 0 {
		return TimeseriesData{}
	}

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

	if len(transactions) == 0 {
		return DoughnutData{}
	}

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

// BuildTopMerchants ranks merchants by what was spent with them in the current
// window, comparing each against the window before it. Totals are positive
// magnitudes, because a merchant list reads as "what I paid them".
func BuildTopMerchants(last, current []models.Transaction, n int) []models.MerchantTotal {

	if n <= 0 || len(current) == 0 {
		return nil
	}

	spendByMerchant := func(transactions []models.Transaction) map[string]float64 {
		totals := make(map[string]float64)
		for _, tx := range transactions {
			// Money coming back from a merchant is a refund, not a payment.
			if tx.Merchant.Name == "" || tx.Amount >= 0 {
				continue
			}
			totals[tx.Merchant.Name] += -tx.Amount
		}
		return totals
	}

	pastSpend := spendByMerchant(last)
	recentSpend := spendByMerchant(current)

	top := make([]models.MerchantTotal, 0, len(recentSpend))
	for merchant, total := range recentSpend {
		entry := models.MerchantTotal{Merchant: merchant, Total: total}
		// A merchant with no spend last window is new, which the template
		// distinguishes from unchanged by the zero PreviousTotal.
		if previous, ok := pastSpend[merchant]; ok && previous != 0 {
			entry.PreviousTotal = previous
			entry.Delta = (total - previous) / previous
		}
		top = append(top, entry)
	}

	// Highest spend first. The name breaks ties so that map iteration order
	// cannot reshuffle the list between renders.
	sort.Slice(top, func(i, j int) bool {
		if top[i].Total == top[j].Total {
			return top[i].Merchant < top[j].Merchant
		}
		return top[i].Total > top[j].Total
	})

	if len(top) > n {
		top = top[:n]
	}

	return top
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

	now := time.Now()

	accounts, accountErr := app.store.ReadAccounts()
	transactions, transactionErr := app.store.ReadTransactionsByDate(now.AddDate(0, -6, 0), now)
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

	monthlyTransactions := AggregateMonthlyTransactions(nonTransferTransactions, now.Location())
	pastTransactions, recentTransactions := FilterRecentTransactions(nonTransferTransactions)

	// Build cards based on differences between the last 30 days, and the 30 days prior to that
	balance, spend, income, savings := BuildCards(accounts, pastTransactions, recentTransactions)

	return c.Render(http.StatusOK, "dashboard", DashboardData{
		BalanceCard:     balance,
		SpendCard:       spend,
		IncomeCard:      income,
		SavingsCard:     savings,
		SpendTimeseries: BuildTimeseriesData(monthlyTransactions),
		SpendDoughnut:   BuildDoughnutData(nonTransferTransactions),
		TopMerchants:    BuildTopMerchants(pastTransactions, recentTransactions, 10),
		// Scoped to the same 30 days as the merchants beside it, so the two
		// lists describe the same period rather than silently differing.
		HighestOutgoingTransactions: BuildHighestOutgoingTransactions(recentTransactions, 10),
	})
}
func (app *Application) _4XX(c echo.Context) error {
	return c.Render(http.StatusOK, "4XX", nil)
}
