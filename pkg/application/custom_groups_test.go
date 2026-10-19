package application

import (
	"testing"

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
