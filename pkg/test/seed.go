// Package test holds what budge's tests share: a small household's worth of
// made-up data, written through the store the way a bank sync writes it.
package test

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/TheQueenIsDead/budge/pkg/database"
	"github.com/TheQueenIsDead/budge/pkg/database/models"
)

// The seeded records a test can open by ID.
const (
	EverydayAccount = "acc_seed_everyday"
	SavingsAccount  = "acc_seed_savings"
	CreditAccount   = "acc_seed_credit"
	HomeLoan        = "acc_seed_homeloan"
	House           = "asset_seed_house"
)

const connection = "conn_seed_bank"

// spend is a purchase that repeats: the merchant, Akahu's broad and specific
// categories, the amount, and how many days apart it comes. again is a day
// into each period it comes a second time, if it does. A bill runs back
// further than the rest, months back, so it has been paid long enough to be
// a recurring payment rather than a new one; and power is seasonal, costing
// more in winter, so it is a bill that varies.
type spend struct {
	merchant, broad, specific string
	amount                    float64
	every                     int
	again                     int
	months                    int
	seasonal                  bool
}

var spends = []spend{
	// Twice a week, as a household shops, rather than on one day each week
	// for the same amount, which only a subscription is.
	{merchant: "Pak'nSave", broad: "Food", specific: "Supermarkets and grocery stores", amount: 91.20, every: 7, again: 3},
	{merchant: "Z Energy", broad: "Transport", specific: "Fuel stations", amount: 94.10, every: 10},
	{merchant: "Mercury", broad: "Utilities", specific: "Electricity services", amount: 165.00, every: 30, months: 13, seasonal: true},
	{merchant: "Spark", broad: "Utilities", specific: "Internet services", amount: 89.99, every: 30, months: 13},
	{merchant: "Little Bird Cafe", broad: "Lifestyle", specific: "Cafes and restaurants", amount: 23.50, every: 5},
}

// winter scales the power bill by the month it is paid in: most in July,
// least in summer.
var winter = map[time.Month]float64{
	time.January: 0.80, time.February: 0.78, time.March: 0.85, time.April: 0.95,
	time.May: 1.10, time.June: 1.30, time.July: 1.40, time.August: 1.35,
	time.September: 1.15, time.October: 1.00, time.November: 0.90, time.December: 0.82,
}

// on reports whether the spend comes on day n of the history, counting
// from its start, which may be before it for a bill that runs further back.
func (s spend) on(n, i int) bool {
	at := ((n-i)%s.every + s.every) % s.every
	return at == 0 || (s.again > 0 && at == s.again)
}

// costOn is what the spend costs on day.
func (s spend) costOn(day time.Time) float64 {
	if !s.seasonal {
		return s.amount
	}
	return math.Round(s.amount*winter[day.Month()]*100) / 100
}

// The home loan's terms. It starts the seeded history owing loanOwed, and is
// paid loanRepayment on the first of each month from the everyday account,
// with interest charged on the 28th at loanRate a year.
const (
	loanMonths    = 18
	loanOwed      = 420_000.00
	loanRepayment = 3_400.00
	loanRate      = 5.89
)

// Seed writes three accounts, the last three months of their transactions
// counted back from now, two of them filed nowhere, with the bills and
// subscriptions going back further on the Visa, as recurring payments do; a
// home loan
// with a year and a half of its repayments and interest; budget targets for
// groceries and transport, a salary, and a house. Given the same now, it
// writes the same data, and nothing in it points anywhere but budge, so a
// page drawn from it loads nothing from other sites.
func Seed(ctx context.Context, store *database.Store, now time.Time) error {
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	from := today.AddDate(0, -3, 0)
	earliest := from

	accounts := []models.Account{
		account(EverydayAccount, "Everyday", "CHECKING", 2_340.55, today),
		account(SavingsAccount, "Rainy Day", "SAVINGS", 12_800.00, today),
		account(CreditAccount, "Visa", "CREDITCARD", -640.20, today),
	}

	var transactions []models.Transaction
	merchants := map[string]models.Merchant{}
	for _, s := range spends {
		if s.months > 3 {
			from := today.AddDate(0, -s.months, 0)
			if from.Before(earliest) {
				earliest = from
			}
		}
	}
	for day := earliest; !day.After(today); day = day.AddDate(0, 0, 1) {
		n := int(math.Round(day.Sub(from).Hours() / 24))
		for i, s := range spends {
			if !s.on(n, i) || day.Before(today.AddDate(0, -max(s.months, 3), 0)) {
				continue
			}
			id := fmt.Sprintf("merchant_seed_%d", i)
			merchants[id] = models.Merchant{Id: id, Name: s.merchant, Category: s.specific}
			tx := transaction(fmt.Sprintf("trans_seed_%s_%d", day.Format("20060102"), i), payingAccount(day, from), day, -s.costOn(day), "EFTPOS", s.merchant)
			tx.Merchant.Id, tx.Merchant.Name = id, s.merchant
			tx.Category.Name = s.specific
			tx.Category.Groups.PersonalFinance.Name = s.broad
			transactions = append(transactions, tx)
		}
		// Paid fortnightly, with a little put by each time.
		if !day.Before(from) && n%14 == 0 {
			date := day.Format("20060102")
			transactions = append(transactions,
				transaction("trans_seed_"+date+"_salary", EverydayAccount, day, 3_250.00, "DIRECT CREDIT", "ACME LTD SALARY"),
				transaction("trans_seed_"+date+"_saving", EverydayAccount, day, -250.00, "TRANSFER", "TRANSFER TO RAINY DAY"),
				transaction("trans_seed_"+date+"_saved", SavingsAccount, day, 250.00, "TRANSFER", "TRANSFER FROM EVERYDAY"),
			)
		}
	}

	// A window cleaner paid by bank transfer, which nothing files, so the
	// transactions page has uncategorised spending to call out and filter to.
	for _, back := range []int{6, 20} {
		day := today.AddDate(0, 0, -back)
		transactions = append(transactions,
			transaction("trans_seed_"+day.Format("20060102")+"_windows", EverydayAccount, day, -22.00, "PAYMENT", "PAY K TAYLOR WINDOWS"))
	}

	loan, loanTransactions := homeLoan(today, from)
	accounts = append(accounts, loan)
	transactions = append(transactions, loanTransactions...)

	var merchantList []models.Merchant
	for i := range spends {
		merchantList = append(merchantList, merchants[fmt.Sprintf("merchant_seed_%d", i)])
	}

	recurring, recurringMerchants := subscriptions(today, from)
	transactions = append(transactions, recurring...)
	merchantList = append(merchantList, recurringMerchants...)
	if err := store.SaveSynced(ctx, accounts, transactions, merchantList); err != nil {
		return err
	}

	// Targets for two categories, so the budget has periods to fill, and a
	// salary to weigh them against. Transport's is well above what the fuel
	// comes to, more than twice it, so the budget has a target to suggest
	// lowering whichever weeks the windows catch.
	for _, target := range []struct {
		key       string
		amount    float64
		frequency string
	}{{"groceries", 200, "weekly"}, {"transport", 800, "monthly"}} {
		category, _ := models.BudgetCategoryByKey(target.key)
		if err := store.CreateBudgetItem(ctx, models.BudgetItem{
			ID: "budget_seed_" + target.key, Name: category.Label, BudgetCategory: target.key,
			Amount: target.amount, Frequency: target.frequency,
		}); err != nil {
			return err
		}
	}
	if err := store.SaveBudgetSalary(ctx, models.BudgetSalary{
		Salary: 84_500, SalaryFrequency: "yearly", IncludePAYE: true, KiwiSaverRate: 3,
	}); err != nil {
		return err
	}

	return store.CreateAsset(ctx, models.Asset{
		Id:            House,
		Name:          "12 Example Street",
		Type:          "house",
		CreatedAt:     today,
		PurchasePrice: 650_000,
		PurchaseDate:  today.AddDate(-4, 0, 0),
		Valuations: []models.AssetValuation{
			{ID: "valuation_seed_1", Date: today.AddDate(-1, 0, 0), Value: 710_000, Note: "Bank valuation"},
		},
	})
}

// subscriptions are the recurring payments that are more than a bill paid
// month after month: a streaming service whose price has just gone up,
// yearly insurance due within the week, a gym joined a month ago and so
// newly recurring, and a streaming service cancelled months ago.
func subscriptions(today, from time.Time) ([]models.Transaction, []models.Merchant) {
	type payment struct {
		day    time.Time
		amount float64
	}
	var transactions []models.Transaction
	var merchants []models.Merchant
	add := func(id, name, broad, specific, kind string, payments []payment) {
		merchants = append(merchants, models.Merchant{Id: "merchant_seed_" + id, Name: name, Category: specific})
		for _, p := range payments {
			tx := transaction("trans_seed_"+p.day.Format("20060102")+"_"+id, payingAccount(p.day, from), p.day, -p.amount, kind, strings.ToUpper(name))
			tx.Merchant.Id, tx.Merchant.Name = "merchant_seed_"+id, name
			tx.Category.Name = specific
			tx.Category.Groups.PersonalFinance.Name = broad
			transactions = append(transactions, tx)
		}
	}
	// monthly is a payment on the given day of each month, back the given
	// number of months to the given number of months ago.
	monthly := func(day, from, to int, amount float64) []payment {
		var payments []payment
		for back := from; back >= to; back-- {
			date := time.Date(today.Year(), today.Month()-time.Month(back), day, 0, 0, 0, 0, time.UTC)
			if !date.After(today) {
				payments = append(payments, payment{date, amount})
			}
		}
		return payments
	}

	neon := monthly(20, 12, 0, 17.99)
	neon[len(neon)-1].amount = 19.99
	add("neon", "Neon", "Utilities", "Business software and cloud services", "EFTPOS", neon)

	renewal := today.AddDate(0, 0, 4)
	add("insurance", "AA Insurance", "Professional services", "Insurance", "DIRECT DEBIT", []payment{
		{renewal.AddDate(-2, 0, 0), 1_140.00}, {renewal.AddDate(-1, 0, 0), 1_140.00},
	})

	add("gym", "CityFitness", "Health", "Gyms, fitness, aquatic facilities, yoga, pilates", "DIRECT DEBIT", []payment{
		{today.AddDate(0, 0, -30), 24.50}, {today.AddDate(0, 0, -16), 24.50}, {today.AddDate(0, 0, -2), 24.50},
	})

	add("lightbox", "Lightbox", "Utilities", "Business software and cloud services", "EFTPOS", monthly(8, 12, 5, 13.99))

	return transactions, merchants
}

// payingAccount is the account a payment on day came from. The everyday
// account's history starts at from, as the salary paid into it does, so the
// bills paid before then were paid from the Visa: paid from the everyday
// account, they would walk its balance back into the red.
func payingAccount(day, from time.Time) string {
	if day.Before(from) {
		return CreditAccount
	}
	return EverydayAccount
}

// homeLoan is a mortgage being paid down: a repayment on the first of each
// month and interest on the 28th, for loanMonths back from today. It owes
// money, so, as Akahu signs it, its balance is negative and the repayments
// are credits. Each repayment is a transfer from the everyday account, whose
// leg is written only from from, where the everyday account's day-to-day history starts.
// Both legs are transfers, which nothing counts as spending or income.
func homeLoan(today, from time.Time) (models.Account, []models.Transaction) {
	var transactions []models.Transaction
	balance := -loanOwed
	for day := today.AddDate(0, -loanMonths, 0); !day.After(today); day = day.AddDate(0, 0, 1) {
		date := day.Format("20060102")
		switch day.Day() {
		case 1:
			balance += loanRepayment
			transactions = append(transactions,
				transaction("trans_seed_"+date+"_repaid", HomeLoan, day, loanRepayment, "TRANSFER", "TRANSFER FROM EVERYDAY"))
			if !day.Before(from) {
				transactions = append(transactions,
					transaction("trans_seed_"+date+"_repay", EverydayAccount, day, -loanRepayment, "TRANSFER", "TRANSFER TO HOME LOAN"))
			}
		case 28:
			interest := math.Round(-balance*loanRate/100/12*100) / 100
			balance -= interest
			transactions = append(transactions,
				transaction("trans_seed_"+date+"_interest", HomeLoan, day, -interest, "LOAN", "LOAN INTEREST"))
		}
	}

	loan := account(HomeLoan, "Home Loan", "LOAN", math.Round(balance*100)/100, today)
	details := &loan.Meta.LoanDetails
	details.Purpose, details.Type = "HOME", "TABLE"
	details.Interest.Type, details.Interest.Rate = "FIXED", loanRate
	details.Repayment.Frequency, details.Repayment.NextAmount = "MONTHLY", loanRepayment
	details.Repayment.NextDate = time.Date(today.Year(), today.Month()+1, 1, 0, 0, 0, 0, time.UTC)
	return loan, transactions
}

func account(id, name, kind string, balance float64, refreshed time.Time) models.Account {
	a := models.Account{Id: id, Name: name, Type: kind, Status: "ACTIVE", FormattedAccount: "12-3456-0" + id[len(id)-4:] + "-00"}
	a.Connection.Id, a.Connection.Name = connection, "Example Bank"
	a.Balance.Currency, a.Balance.Current, a.Balance.Available = "NZD", balance, balance
	a.Refreshed.Balance, a.Refreshed.Transactions = refreshed, refreshed
	return a
}

func transaction(id, account string, date time.Time, amount float64, kind, description string) models.Transaction {
	return models.Transaction{
		Id: id, Account: account, Connection: connection,
		CreatedAt: date, UpdatedAt: date, Date: date,
		Description: description, Amount: amount, Type: kind,
	}
}
