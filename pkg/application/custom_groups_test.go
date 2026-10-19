package application

import (
	"testing"
	"time"

	"github.com/TheQueenIsDead/budge/pkg/database/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func withCustomGroups(t *testing.T, custom ...models.CustomSpendGroup) {
	t.Helper()
	SetSpendRules(models.SpendRules{Custom: custom})
	t.Cleanup(func() { spendRules.Store(nil) })
}

func TestCustomGroups(t *testing.T) {
	withCustomGroups(t,
		models.CustomSpendGroup{Key: "custom-rent", Label: "Rent", SpendRule: models.SpendRule{Keywords: []string{"rent"}}},
		models.CustomSpendGroup{Key: "custom-lunches", Label: "Lunches", SpendRule: models.SpendRule{Merchants: []string{"Addington Fo"}}},
	)

	groups := SpendGroups()
	assert.Equal(t, "Rent", groups[len(groups)-2].Label, "the owner's groups follow the built in ones")
	group, ok := SpendGroupByKey("custom-lunches")
	require.True(t, ok)
	assert.Equal(t, "Lunches", group.Label)

	rent := spendTransaction("", "", -510, now)
	rent.Type = "STANDING ORDER"
	rent.Description = "Vvv Pm Rent 2/20 Rochest"
	key, ok := ClassifySpend(rent)
	assert.True(t, ok)
	assert.Equal(t, "custom-rent", key, "a standing order Akahu never categorised")

	lunch := spendTransaction("", "", -17.30, now)
	lunch.Type = "CREDIT CARD"
	lunch.Description = "Addington Fo"
	key, _ = ClassifySpend(lunch)
	assert.Equal(t, "custom-lunches", key)

	current := referenced("PAYMENT", "", "current acc", -20)
	_, ok = ClassifySpend(current)
	assert.False(t, ok, "rent matches the start of a word, so current does not")
}

func TestCustomGroupsTakePrecedence(t *testing.T) {
	withCustomGroups(t, models.CustomSpendGroup{Key: "custom-milkrun", Label: "Milkrun", SpendRule: models.SpendRule{Merchants: []string{"Milkrun"}}})

	key, ok := ClassifySpend(spendTransaction("Supermarkets and grocery stores", "Milkrun", -52.45, now))
	assert.True(t, ok)
	assert.Equal(t, "custom-milkrun", key, "the owner's narrower group wins over groceries")
}

func TestBudgetLine(t *testing.T) {
	withCustomGroups(t, models.CustomSpendGroup{Key: "custom-rent", Label: "Rent", SpendRule: models.SpendRule{Keywords: []string{"rent"}}})

	shop := spendTransaction("Supermarkets and grocery stores", "PAK'nSAVE", -120, now)
	shop.Category.Groups.PersonalFinance.Name = "Food"
	broad, specific, _, amount, ok := budgetLine(shop)
	assert.True(t, ok)
	assert.Equal(t, budgetGroupsBroad, broad, "a purchase a group matches is budgeted under the group")
	assert.Equal(t, "Groceries", specific)
	assert.Equal(t, 120.0, amount)

	repaid := referenced("PAYMENT", "", "groceries", 40)
	_, specific, _, amount, ok = budgetLine(repaid)
	assert.True(t, ok)
	assert.Equal(t, "Groceries", specific)
	assert.Equal(t, -40.0, amount, "being paid back takes from the group")

	rent := spendTransaction("", "", -510, now)
	rent.Type = "STANDING ORDER"
	rent.Description = "Flat Rent"
	_, specific, name, _, ok := budgetLine(rent)
	assert.True(t, ok)
	assert.Equal(t, "Rent", specific)
	assert.Equal(t, "Flat Rent", name, "with no merchant, the description names who was paid")

	haircut := spendTransaction("Haircuts and treatments", "Valentinos", -67, now)
	haircut.Category.Groups.PersonalFinance.Name = "Appearance"
	broad, specific, _, _, ok = budgetLine(haircut)
	assert.True(t, ok)
	assert.Equal(t, "Appearance", broad, "anything no group matches keeps its Akahu category")
	assert.Equal(t, "Haircuts and treatments", specific)

	_, _, _, _, ok = budgetLine(spendTransaction("", "", -30, now))
	assert.False(t, ok, "uncategorised spending no group matches has nowhere to go")
}

func TestCashGroup(t *testing.T) {
	atm := spendTransaction("", "", -1003.50, now)
	atm.Type = "ATM"
	atm.Description = "Acu Aap9480 Eastgate Sho"
	key, ok := ClassifySpend(atm)
	assert.True(t, ok)
	assert.Equal(t, "cash", key, "any ATM withdrawal, whatever the machine")
}

func TestMoneyIn(t *testing.T) {
	pay := spendTransaction("", "", 2950, now)
	pay.Type = "CREDIT"
	assert.Equal(t, 2950.0, moneyIn(pay, false), "pay is money in")

	sold := referenced("PAYMENT", "", "wheels", 280)
	assert.Equal(t, 280.0, moneyIn(sold, false), "so is someone paying you")

	repaid := referenced("PAYMENT", "groceries", "", 40)
	assert.Zero(t, moneyIn(repaid, false), "a repayment a spend group already nets off is not counted twice")

	own := spendTransaction("Transfer", "Transfer", 500, now)
	own.Type = "TRANSFER"
	assert.Zero(t, moneyIn(own, false), "moving money between tracked accounts brings nothing in")
	assert.Equal(t, 500.0, moneyIn(own, true), "money from an account Budge no longer tracks does")
	own.Amount = -235
	assert.Equal(t, -235.0, moneyIn(own, true), "and money sent back to it takes from what came in")

	spend := spendTransaction("Haircuts and treatments", "Valentinos", -67, now)
	assert.Zero(t, moneyIn(spend, false))
}

func TestExternalTransfers(t *testing.T) {
	leg := func(id, account string, amount float64, hours int) models.Transaction {
		tx := spendTransaction("Transfer", "Transfer", amount, now.Add(time.Duration(hours)*time.Hour))
		tx.Id, tx.Account, tx.Type = id, account, "TRANSFER"
		return tx
	}
	external := externalTransfers([]models.Transaction{
		leg("out", "weekly", -1750, 0),
		leg("in", "joint", 1750, 1),
		leg("business", "joint", 1010, 0),
	})
	assert.False(t, external["out"])
	assert.False(t, external["in"], "both legs are tracked")
	assert.True(t, external["business"], "the other leg went with the account switched off")
}
