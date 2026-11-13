// Package test holds what budge's tests share: a small household's worth of
// made-up data, written through the store the way a bank sync writes it.
package test

import (
	"context"
	"fmt"
	"math"
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
// categories, the amount, and how many days apart it comes.
type spend struct {
	merchant, broad, specific string
	amount                    float64
	every                     int
}

var spends = []spend{
	{"Pak'nSave", "Food", "Supermarkets and grocery stores", 182.40, 7},
	{"Z Energy", "Transport", "Fuel stations", 94.10, 10},
	{"Mercury", "Utilities", "Electricity services", 165.00, 30},
	{"Spark", "Utilities", "Internet services", 89.99, 30},
	{"Little Bird Cafe", "Lifestyle", "Cafes and restaurants", 23.50, 5},
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
// counted back from now, two of them filed nowhere, and a home loan with a
// year and a half of its repayments and interest; budget targets for
// groceries and transport, a salary, and a house. Given the same now, it
// writes the same data, and nothing in it points anywhere but budge, so a
// page drawn from it loads nothing from other sites.
func Seed(ctx context.Context, store *database.Store, now time.Time) error {
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	from := today.AddDate(0, -3, 0)

	accounts := []models.Account{
		account(EverydayAccount, "Everyday", "CHECKING", 2_340.55, today),
		account(SavingsAccount, "Rainy Day", "SAVINGS", 12_800.00, today),
		account(CreditAccount, "Visa", "CREDITCARD", -640.20, today),
	}

	var transactions []models.Transaction
	merchants := map[string]models.Merchant{}
	for day := from; !day.After(today); day = day.AddDate(0, 0, 1) {
		n := int(day.Sub(from).Hours() / 24)
		for i, s := range spends {
			if n%s.every != i%s.every {
				continue
			}
			id := fmt.Sprintf("merchant_seed_%d", i)
			merchants[id] = models.Merchant{Id: id, Name: s.merchant, Category: s.specific}
			tx := transaction(fmt.Sprintf("trans_seed_%s_%d", day.Format("20060102"), i), EverydayAccount, day, -s.amount, "EFTPOS", s.merchant)
			tx.Merchant.Id, tx.Merchant.Name = id, s.merchant
			tx.Category.Name = s.specific
			tx.Category.Groups.PersonalFinance.Name = s.broad
			transactions = append(transactions, tx)
		}
		// Paid fortnightly, with a little put by each time.
		if n%14 == 0 {
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

// homeLoan is a mortgage being paid down: a repayment on the first of each
// month and interest on the 28th, for loanMonths back from today. It owes
// money, so, as Akahu signs it, its balance is negative and the repayments
// are credits. Each repayment is a transfer from the everyday account, whose
// leg is written only from from, where the everyday account's history starts.
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
