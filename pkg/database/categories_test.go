package database

import (
	"testing"
	"time"

	"github.com/TheQueenIsDead/budge/pkg/database/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func categorisedTx(id, txType, description, merchant string) models.Transaction {
	tx := models.Transaction{Id: id, Type: txType, Description: description, Amount: -100, Date: time.Now().Add(-time.Hour)}
	tx.Merchant.Name = merchant
	return tx
}

func TestCategoryRuleMatches(t *testing.T) {
	interest := models.CategoryRule{Contains: "loan interest", Type: "LOAN"}
	tests := []struct {
		name     string
		rule     models.CategoryRule
		tx       models.Transaction
		expected bool
	}{
		{"description matches case-insensitively", interest, categorisedTx("1", "LOAN", "LOAN INTEREST", ""), true},
		{"the type narrows the rule", interest, categorisedTx("2", "DEBIT", "LOAN INTEREST", ""), false},
		{"merchant name matches", models.CategoryRule{Contains: "fisher"}, categorisedTx("3", "EFTPOS", "AP#1", "Fisher Funds"), true},
		{"an empty pattern matches nothing", models.CategoryRule{}, categorisedTx("4", "DEBIT", "anything", ""), false},
		{"transfers are never recategorised", models.CategoryRule{Contains: "loan"}, categorisedTx("5", "TRANSFER", "LOAN REPAYMENT", ""), false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, test.expected, test.rule.Matches(test.tx))
		})
	}
}

func TestCategoryRulesApplyOnRead(t *testing.T) {
	store := tempStore(t)
	akahu := categorisedTx("interest", "LOAN", "LOAN INTEREST", "")
	groceries := categorisedTx("groceries", "EFTPOS", "COUNTDOWN", "Countdown")
	groceries.Category.Name = "Supermarkets"
	groceries.Category.Groups.PersonalFinance.Name = "Food"
	require.NoError(t, store.CreateTransaction(akahu))
	require.NoError(t, store.CreateTransaction(groceries))

	require.NoError(t, store.SaveCategorySettings(models.CategorySettings{Rules: []models.CategoryRule{
		{ID: "first", Contains: "loan interest", Broad: "General", Specific: "Housing"},
		{ID: "second", Contains: "interest", Broad: "Ignored", Specific: "Never"},
	}}))

	byID := func(txs []models.Transaction) map[string]models.Transaction {
		m := map[string]models.Transaction{}
		for _, tx := range txs {
			m[tx.Id] = tx
		}
		return m
	}
	reads := map[string]func() ([]models.Transaction, error){
		"all":     store.ReadTransactions,
		"by date": func() ([]models.Transaction, error) { return store.ReadTransactionsByDate(time.Now().AddDate(0, 0, -1), time.Now()) },
		"search":  func() ([]models.Transaction, error) { return store.SearchTransactions("housing", "") },
	}
	for name, read := range reads {
		t.Run(name, func(t *testing.T) {
			txs, err := read()
			require.NoError(t, err)
			tx := byID(txs)["interest"]
			assert.Equal(t, "Housing", tx.Category.Name, "the first matching rule wins")
			assert.Equal(t, "General", tx.Category.Groups.PersonalFinance.Name)
			assert.Equal(t, []string{"General", "Housing"}, tx.Categories())
		})
	}

	txs, err := store.ReadTransactions()
	require.NoError(t, err)
	assert.Equal(t, "Supermarkets", byID(txs)["groceries"].Category.Name, "unmatched transactions keep Akahu's category")

	t.Run("deleting the rules restores Akahu's category", func(t *testing.T) {
		require.NoError(t, store.SaveCategorySettings(models.CategorySettings{}))
		txs, err := store.ReadTransactions()
		require.NoError(t, err)
		assert.Empty(t, byID(txs)["interest"].Category.Name)
	})
}

func TestCategorySettingsMergeOnImport(t *testing.T) {
	store := tempStore(t)
	require.NoError(t, store.SaveCategorySettings(models.CategorySettings{
		Categories: []models.CustomCategory{{ID: "c1", Broad: "General", Specific: "Housing"}},
		Rules:      []models.CategoryRule{{ID: "r1", Contains: "old"}, {ID: "r2", Contains: "keep"}},
	}))

	require.NoError(t, store.ImportBackup(models.Backup{Categories: &models.CategorySettings{
		Categories: []models.CustomCategory{{ID: "c2", Broad: "General", Specific: "Mortgage"}},
		Rules:      []models.CategoryRule{{ID: "r1", Contains: "new"}, {ID: "r3", Contains: "added"}},
	}}))

	settings, err := store.GetCategorySettings()
	require.NoError(t, err)
	assert.Len(t, settings.Categories, 2)
	var patterns []string
	for _, rule := range settings.Rules {
		patterns = append(patterns, rule.Contains)
	}
	assert.Equal(t, []string{"new", "keep", "added"}, patterns, "same id overwrites in place and rule order is kept")
}
