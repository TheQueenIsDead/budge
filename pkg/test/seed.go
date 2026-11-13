// Package test holds what budge's tests share: a small household's worth of
// made-up data, written through the store the way a bank sync writes it.
package test

import (
	"context"
	"fmt"
	"time"

	"github.com/TheQueenIsDead/budge/pkg/database"
	"github.com/TheQueenIsDead/budge/pkg/database/models"
)

// The seeded records a test can open by ID.
const (
	EverydayAccount = "acc_seed_everyday"
	SavingsAccount  = "acc_seed_savings"
	CreditAccount   = "acc_seed_credit"
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

// Seed writes three accounts, the last three months of their transactions
// counted back from now, budget targets for groceries and transport, a salary,
// and a house. Given the same now, it writes the same
// data, and nothing in it points anywhere but budge, so a page drawn from it
// loads nothing from other sites.
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
