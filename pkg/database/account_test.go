package database

import (
	"testing"
	"time"

	"github.com/TheQueenIsDead/budge/pkg/database/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func synced(t *testing.T) *Store {
	t.Helper()
	store := tempStore(t)
	everyday, joint := models.Account{Id: "acc_everyday", Name: "Everyday"}, models.Account{Id: "acc_joint", Name: "Joint", FormattedAccount: "12-3456-0000001-00"}
	joint.Connection.Name = "ANZ"
	var transactions []models.Transaction
	for i, account := range []string{"acc_everyday", "acc_joint", "acc_joint", "acc_everyday", "acc_joint"} {
		transactions = append(transactions, models.Transaction{Id: "trans_" + string(rune('a'+i)), Account: account, Amount: -10})
	}
	require.NoError(t, store.SaveSynced([]models.Account{everyday, joint}, transactions, nil))
	require.NoError(t, store.UpdateAkahuSettings(models.IntegrationAkahuSettings{LastSync: time.Now(), DataThrough: time.Now()}))
	return store
}

func TestRemoveAccountDropsItAndItsTransactions(t *testing.T) {
	store := synced(t)
	require.NoError(t, store.RemoveAccount("acc_joint"))

	accounts, err := store.ReadAccounts()
	require.NoError(t, err)
	require.Len(t, accounts, 1)
	assert.Equal(t, "acc_everyday", accounts[0].Id)

	transactions, err := store.ReadTransactions()
	require.NoError(t, err)
	assert.Len(t, transactions, 2)
	for _, tx := range transactions {
		assert.Equal(t, "acc_everyday", tx.Account)
	}

	removed, err := store.GetRemovedAccounts()
	require.NoError(t, err)
	assert.Equal(t, []models.RemovedAccount{{Id: "acc_joint", Name: "Joint", Connection: "ANZ", FormattedAccount: "12-3456-0000001-00"}}, removed.Accounts,
		"what was removed is remembered by name, so it can be switched back on")
}

func TestRemoveAccountTwiceIsRememberedOnce(t *testing.T) {
	store := synced(t)
	require.NoError(t, store.RemoveAccount("acc_joint"))
	require.NoError(t, store.SaveSynced([]models.Account{{Id: "acc_joint", Name: "Joint"}}, nil, nil))
	require.NoError(t, store.RemoveAccount("acc_joint"))

	removed, err := store.GetRemovedAccounts()
	require.NoError(t, err)
	assert.Len(t, removed.Accounts, 1)
}

func TestRestoreAccountStaysListedUntilSynced(t *testing.T) {
	store := synced(t)
	require.NoError(t, store.RemoveAccount("acc_joint"))
	require.NoError(t, store.RestoreAccount("acc_joint"))

	removed, err := store.GetRemovedAccounts()
	require.NoError(t, err)
	require.Len(t, removed.Accounts, 1, "it does not vanish before the sync bringing it back lands")
	assert.True(t, removed.Restoring())
	assert.Empty(t, removed.IDs(), "and the sync no longer skips it")

	require.NoError(t, store.FinishRestoring())
	removed, err = store.GetRemovedAccounts()
	require.NoError(t, err)
	assert.Empty(t, removed.Accounts)
}

func TestRemoveAccountWhileRestoringIsListedOnce(t *testing.T) {
	store := synced(t)
	require.NoError(t, store.RemoveAccount("acc_joint"))
	require.NoError(t, store.RestoreAccount("acc_joint"))
	require.NoError(t, store.SaveSynced([]models.Account{{Id: "acc_joint", Name: "Joint"}}, nil, nil))
	require.NoError(t, store.RemoveAccount("acc_joint"))

	removed, err := store.GetRemovedAccounts()
	require.NoError(t, err)
	require.Len(t, removed.Accounts, 1)
	assert.False(t, removed.Restoring())
}

func TestBackupKeepsRemovedAccounts(t *testing.T) {
	store := synced(t)
	require.NoError(t, store.RemoveAccount("acc_joint"))
	backup, err := store.ExportBackup()
	require.NoError(t, err)

	fresh := tempStore(t)
	require.NoError(t, fresh.ImportBackup(backup))
	require.NoError(t, fresh.ImportBackup(backup))

	removed, err := fresh.GetRemovedAccounts()
	require.NoError(t, err)
	require.Len(t, removed.Accounts, 1, "importing twice does not list it twice")
	assert.Equal(t, "acc_joint", removed.Accounts[0].Id)
}

func TestBackupKeepsSpendRules(t *testing.T) {
	store := tempStore(t)
	rules := models.SpendRules{
		Groups: map[string]models.SpendRule{"groceries": {Keywords: []string{"veges"}, Merchants: []string{"Ooooby"}}},
		Custom: []models.CustomSpendGroup{{Key: "custom-rent", Label: "Rent", SpendRule: models.SpendRule{Keywords: []string{"rent"}}}},
	}
	require.NoError(t, store.SaveSpendRules(rules))
	backup, err := store.ExportBackup()
	require.NoError(t, err)

	fresh := tempStore(t)
	require.NoError(t, fresh.SaveSpendRules(models.SpendRules{Groups: map[string]models.SpendRule{"petrol": {Keywords: []string{"gas"}}}}))
	require.NoError(t, fresh.ImportBackup(backup))

	got, err := fresh.GetSpendRules()
	require.NoError(t, err)
	assert.Equal(t, rules.Groups["groceries"], got.Groups["groceries"])
	assert.Equal(t, []string{"gas"}, got.Groups["petrol"].Keywords, "a group the backup does not mention is kept")
	assert.Equal(t, rules.Custom, got.Custom, "the owner's own groups come across too")

	require.NoError(t, fresh.ImportBackup(backup))
	again, err := fresh.GetSpendRules()
	require.NoError(t, err)
	assert.Len(t, again.Custom, 1, "importing twice does not list a group twice")
}

func TestToggleInsightExclusion(t *testing.T) {
	store := tempStore(t)
	got, err := store.ToggleInsightExclusion("trans_a")
	require.NoError(t, err)
	assert.Equal(t, []string{"trans_a"}, got.TransactionIDs)

	backup, err := store.ExportBackup()
	require.NoError(t, err)
	fresh := tempStore(t)
	require.NoError(t, fresh.ImportBackup(backup))
	restored, err := fresh.GetInsightExclusions()
	require.NoError(t, err)
	assert.Equal(t, []string{"trans_a"}, restored.TransactionIDs, "exclusions travel in a backup")

	got, err = store.ToggleInsightExclusion("trans_a")
	require.NoError(t, err)
	assert.Empty(t, got.TransactionIDs, "toggling again puts it back")
}
