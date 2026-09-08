package application

import (
	"cmp"
	"maps"
	"math"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/TheQueenIsDead/budge/pkg/database/models"
	"github.com/labstack/echo/v4"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

type AccountTimeseriesData struct {
	Labels []string
	Data   []float64
}

type AccountStatistics struct {
	TotalInflow    float64
	TotalOutflow   float64
	NetChange      float64
	AverageBalance float64
	HighestBalance float64
	LowestBalance  float64
}

type AccountsPageProps struct {
	Account       models.Account
	GraphData     AccountTimeseriesData
	Statistics    AccountStatistics
	IsCurrentYear bool
	PrevYear      int
	NextYear      int
	Date          time.Time

	// HasData is whether the selected year holds anything to chart. The list
	// does not link accounts that have no history at all, but a bookmark or a
	// year with no activity can still land here, and an empty chart above six
	// zeroed statistics reads as a broken page rather than an empty one.
	HasData bool
}

// AccountSummary is a single account as it appears in the accounts list, annotated
// with how its balance has moved over the reporting window.
type AccountSummary struct {
	Account         models.Account
	TypeLabel       string
	Icon            string
	IsActive        bool
	IsLoan          bool
	Change          float64 // Net balance movement over the window
	PreviousBalance float64 // Balance at the start of the window
	Delta           float64 // Change as a proportion of the previous balance
	HasHistory      bool    // Whether any transactions fell within the window

	// HasTransactions is whether the account has any transactions at all, which
	// is a different question from HasHistory: an account can be quiet for the
	// last thirty days and still have a year of balance history worth opening.
	// Without any transactions the detail page has nothing to chart, so the row
	// does not link anywhere.
	HasTransactions bool
}

// AccountGroup collects every account held with a single connection.
type AccountGroup struct {
	Connection string
	Logo       string
	Accounts   []AccountSummary
	Total      float64
}

// Portfolio is the roll up of every account balance into a single net position.
type Portfolio struct {
	Assets      float64
	Liabilities float64
	NetWorth    float64
	Accounts    int
}

type AccountsListProps struct {
	Portfolio     Portfolio
	Groups        []AccountGroup
	LastRefreshed time.Time
	HasRefreshed  bool
}

// accountTypeLabels maps the account types reported by upstream providers onto
// labels that read well. Anything unmapped falls back to title casing the raw value.
var accountTypeLabels = map[string]string{
	"CREDITCARD":  "Credit Card",
	"TERMDEPOSIT": "Term Deposit",
	"KIWISAVER":   "KiwiSaver",
	"FOREIGN":     "Foreign Currency",
}

// accountTypeIcons gives each account type a mark, so a list of balances can be
// scanned by shape rather than read line by line.
var accountTypeIcons = map[string]string{
	"CHECKING":    "bi-wallet2",
	"SAVINGS":     "bi-piggy-bank",
	"LOAN":        "bi-house",
	"CREDITCARD":  "bi-credit-card",
	"TERMDEPOSIT": "bi-safe",
	"KIWISAVER":   "bi-umbrella",
	"FOREIGN":     "bi-currency-exchange",
	"INVESTMENT":  "bi-graph-up-arrow",
}

// AccountTypeIcon returns the icon for an account type, falling back to a
// generic one rather than rendering an empty box.
func AccountTypeIcon(accountType string) string {
	if icon, ok := accountTypeIcons[strings.ToUpper(accountType)]; ok {
		return icon
	}
	return "bi-bank2"
}

// AccountTypeLabel renders a provider account type for display.
func AccountTypeLabel(accountType string) string {
	if accountType == "" {
		return "Account"
	}
	if label, ok := accountTypeLabels[strings.ToUpper(accountType)]; ok {
		return label
	}
	return cases.Title(language.English).String(accountType)
}

// BuildPortfolio totals the current balance of every account into a net position.
// Accounts are split into assets and liabilities by the sign of their balance rather
// than by their type, so that the arithmetic holds regardless of how a provider
// chooses to sign debt.
func BuildPortfolio(accounts []models.Account) Portfolio {
	portfolio := Portfolio{Accounts: len(accounts)}
	for _, account := range accounts {
		balance := account.Balance.Current
		if balance < 0 {
			portfolio.Liabilities += balance
		} else {
			portfolio.Assets += balance
		}
		portfolio.NetWorth += balance
	}

	return portfolio
}

// BuildAccountGroups arranges accounts under their connection. transactions are
// those inside the reporting window and drive the movement shown against each
// balance; hasTransactions says which accounts have any history at all, which
// decides whether a row is worth opening.
func BuildAccountGroups(accounts []models.Account, transactions []models.Transaction, hasTransactions map[string]bool) []AccountGroup {

	// Bucket the balance movement by account in a single pass, rather than reading
	// transactions once per account.
	changes := make(map[string]float64)
	seen := make(map[string]bool)
	for _, tx := range transactions {
		changes[tx.Account] += tx.Amount
		seen[tx.Account] = true
	}

	grouped := make(map[string]*AccountGroup)
	for _, account := range accounts {
		connection := account.Connection.Name
		if connection == "" {
			connection = "Other"
		}

		summary := AccountSummary{
			Account:         account,
			TypeLabel:       AccountTypeLabel(account.Type),
			Icon:            AccountTypeIcon(account.Type),
			IsActive:        account.Status == "" || strings.EqualFold(account.Status, "ACTIVE"),
			IsLoan:          strings.EqualFold(account.Type, "LOAN"),
			Change:          changes[account.Id],
			HasHistory:      seen[account.Id],
			HasTransactions: hasTransactions[account.Id],
		}
		summary.PreviousBalance = account.Balance.Current - summary.Change
		if summary.PreviousBalance != 0 {
			summary.Delta = summary.Change / math.Abs(summary.PreviousBalance)
		}

		group, ok := grouped[connection]
		if !ok {
			group = &AccountGroup{Connection: connection, Logo: account.Connection.Logo}
			grouped[connection] = group
		}
		group.Accounts = append(group.Accounts, summary)
		group.Total += account.Balance.Current
	}

	groups := make([]AccountGroup, 0, len(grouped))
	for _, group := range grouped {
		slices.SortFunc(group.Accounts, func(a, b AccountSummary) int {
			if c := cmp.Compare(b.Account.Balance.Current, a.Account.Balance.Current); c != 0 {
				return c
			}
			return cmp.Compare(a.Account.Name, b.Account.Name)
		})
		groups = append(groups, *group)
	}
	slices.SortFunc(groups, func(a, b AccountGroup) int {
		if c := cmp.Compare(b.Total, a.Total); c != 0 {
			return c
		}
		return cmp.Compare(a.Connection, b.Connection)
	})

	return groups
}

// OldestRefresh returns the least recently refreshed balance across all accounts,
// which is the earliest point the displayed totals can be trusted from. The boolean
// reports whether any account has been refreshed at all.
func OldestRefresh(accounts []models.Account) (time.Time, bool) {
	var oldest time.Time
	for _, account := range accounts {
		refreshed := account.Refreshed.Balance
		if refreshed.IsZero() {
			continue
		}
		if oldest.IsZero() || refreshed.Before(oldest) {
			oldest = refreshed
		}
	}
	return oldest, !oldest.IsZero()
}

func (app *Application) Accounts(c echo.Context) error {

	accounts, err := app.store.ReadAccounts()
	if err != nil {
		c.Logger().Error(err)
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to fetch accounts")
	}

	// Transactions drive the per account movement shown alongside each balance. A
	// failure here costs us the deltas but not the balances, so carry on without them.
	// One read serves both questions: which accounts moved recently, and which have
	// any history at all.
	transactions, err := app.store.ReadTransactions()
	if err != nil {
		c.Logger().Error(err)
	}

	cutoff := time.Now().AddDate(0, 0, -30)
	recent := make([]models.Transaction, 0, len(transactions))
	hasTransactions := make(map[string]bool)
	for _, tx := range transactions {
		hasTransactions[tx.Account] = true
		if tx.Date.After(cutoff) {
			recent = append(recent, tx)
		}
	}

	refreshed, hasRefreshed := OldestRefresh(accounts)

	return c.Render(http.StatusOK, "accounts", AccountsListProps{
		Portfolio:     BuildPortfolio(accounts),
		Groups:        BuildAccountGroups(accounts, recent, hasTransactions),
		LastRefreshed: refreshed,
		HasRefreshed:  hasRefreshed,
	})
}

func (app *Application) Account(c echo.Context) error {

	accountId := c.Param("id")
	account, err := app.store.GetAccount([]byte(accountId))
	if err != nil {
		c.Logger().Error(err)
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to fetch accounts")
	}

	year := c.QueryParam("year")
	var viewDate time.Time
	if year != "" {
		y, err := strconv.Atoi(year)
		if err != nil {
			viewDate = time.Now()
		} else {
			viewDate = time.Date(y, time.January, 1, 0, 0, 0, 0, time.UTC)
		}
	} else {
		viewDate = time.Now()
	}

	graphData, statistics, err := app.accountBalance(c, account, viewDate)
	if err != nil {
		c.Logger().Error(err)
		// Continue, maybe graph is not essential
	}

	props := AccountsPageProps{
		Account:       account,
		GraphData:     graphData,
		Statistics:    statistics,
		HasData:       len(graphData.Data) > 0,
		Date:          viewDate,
		IsCurrentYear: viewDate.Year() == time.Now().Year(),
		PrevYear:      viewDate.AddDate(-1, 0, 0).Year(),
		NextYear:      viewDate.AddDate(1, 0, 0).Year(),
	}

	return c.Render(http.StatusOK, "account", props)
}

func (app *Application) accountBalance(c echo.Context, account models.Account, viewDate time.Time) (AccountTimeseriesData, AccountStatistics, error) {

	// Retrieve all transactions for an account
	transactions, err := app.store.ReadTransactionsByAccount(account.Id)
	if err != nil {
		c.Logger().Error(err)
		return AccountTimeseriesData{}, AccountStatistics{}, err
	}

	graphData, stats := BuildAccountBalanceHistory(account, transactions, viewDate)
	return graphData, stats, nil
}

// BuildAccountBalanceHistory walks an account's balance backwards through the
// selected year and summarises it. It is split from the handler so the walk can
// be tested without a store behind it.
func BuildAccountBalanceHistory(account models.Account, transactions []models.Transaction, viewDate time.Time) (AccountTimeseriesData, AccountStatistics) {

	// Filter transactions for the selected year
	year := viewDate.Year()
	startDate := time.Date(year, time.January, 1, 0, 0, 0, 0, time.UTC)
	endDate := startDate.AddDate(1, 0, 0)

	// Calculate the balance at the end of the selected year by rolling back future
	// transactions. Current is the account's actual balance and is what the
	// accounts list, the portfolio and net worth all report. Available means
	// something else entirely - on a revolving facility it is the credit left to
	// draw, and on KiwiSaver, investment and loan accounts the feed leaves it at
	// zero - so anchoring the history to it charts a balance the account never had.
	balanceAtEndDate := account.Balance.Current
	for _, t := range transactions {
		if t.Date.After(endDate) && (t.Date.Before(account.Refreshed.Balance) || t.Date.Equal(account.Refreshed.Balance)) {
			balanceAtEndDate -= t.Amount
		}
	}

	var recentTransactions []models.Transaction
	for _, t := range transactions {
		if (t.Date.After(startDate) || t.Date.Equal(startDate)) && t.Date.Before(endDate) {
			recentTransactions = append(recentTransactions, t)
		}
	}

	// Both extremes are seeded outside the range of any real balance. Leaving
	// HighestBalance at zero would report zero for an account that is always
	// overdrawn, since no negative balance ever beats it.
	stats := AccountStatistics{
		LowestBalance:  math.MaxFloat64,
		HighestBalance: -math.MaxFloat64,
	}
	for _, t := range recentTransactions {
		if t.Amount > 0 {
			stats.TotalInflow += t.Amount
		} else {
			stats.TotalOutflow += t.Amount
		}
	}
	stats.NetChange = stats.TotalInflow + stats.TotalOutflow

	// Calculate the balance delta per month
	deltas := make(map[string]float64)
	for _, t := range recentTransactions {
		deltas[t.Date.Format("2006-01")] += t.Amount
	}

	// Iterate all months between the first and last transaction, creating a backwards running balance
	balances := WalkAccount(balanceAtEndDate, deltas)

	var data []float64
	var labels []string
	keys := slices.Collect(maps.Keys(balances))
	slices.Sort(keys)
	var balanceSum float64
	for _, k := range keys {
		balance := balances[k]
		data = append(data, balance)
		labels = append(labels, k)

		// calculate balance stats
		balanceSum += balance
		if balance > stats.HighestBalance {
			stats.HighestBalance = balance
		}
		if balance < stats.LowestBalance {
			stats.LowestBalance = balance
		}
	}
	if len(data) > 0 {
		stats.AverageBalance = balanceSum / float64(len(data))
	} else {
		// Nothing was seen, so report zero rather than the sentinels.
		stats.LowestBalance = 0
		stats.HighestBalance = 0
	}

	graphData := AccountTimeseriesData{
		Labels: labels,
		Data:   data,
	}

	return graphData, stats
}

// WalkAccount takes a balance and list of changes in balance for a series of periods and calculates the balance at the preceding periods.
// It is assumed that the delta map is keyed with a format that sorts chronologically (e.g. "2006-01"), and that the balance given is for the most
// recent period in the deltas map.
func WalkAccount(balance float64, deltas map[string]float64) map[string]float64 {

	balances := make(map[string]float64)

	// Retrieve periods to iterate and ensure that they are sorted.
	periods := slices.Collect(maps.Keys(deltas))
	slices.Sort(periods)

	// Iterate through each period from most recent into the past.
	for i := len(periods) - 1; i >= 0; i-- {
		today := periods[i]
		if i+1 < len(periods) {
			// Derive today's balance by setting it to tomorrow's balance - tomorrow's delta (Inverted)
			tomorrow := periods[i+1]
			balances[today] = balances[tomorrow] + (deltas[tomorrow] * -1)
		} else {
			// Most recent period is assumed to have the starting balance
			balances[today] = balance
		}
	}

	return balances
}
