package database

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/TheQueenIsDead/budge/pkg/database/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// configured is an instance with something of everything set up by hand.
func configured(t *testing.T) *Store {
	store := tempStore(t)
	require.NoError(t, store.SaveMemory(models.Memory{Id: "japan", Title: "Japan", Kind: "trip", TransactionIDs: []string{"trans_1", "trans_2"}}))
	require.NoError(t, store.SaveTimelinePreferences(models.TimelinePreferences{
		HiddenTransactions: []string{"trans_9"}, HiddenMerchants: []string{"Fisher Funds"}, DismissedSuggestions: []string{"away-2026-04-02"},
	}))
	require.NoError(t, store.CreateAsset(models.Asset{Id: "house", Name: "House", PurchasePrice: 650000,
		Valuations: []models.AssetValuation{{ID: "v1", Value: 700000, Date: time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)}}}))
	require.NoError(t, store.CreateBudgetItem(models.BudgetItem{ID: "food", Name: "Groceries", Amount: 250, Frequency: "weekly"}))
	require.NoError(t, store.SaveBudgetSalary(models.BudgetSalary{Salary: 100000, SalaryFrequency: "yearly", SavingsGoal: 250}))
	require.NoError(t, store.SaveScheduleSettings(models.ScheduleSettings{AkahuEnabled: true, AkahuInterval: "6h", AkahuLastRun: time.Now()}))
	require.NoError(t, store.SaveCategorySettings(models.CategorySettings{Rules: []models.CategoryRule{{ID: "loan", Contains: "LOAN INTEREST", Broad: "General", Specific: "Housing"}}}))
	require.NoError(t, store.UpdateAkahuSettings(models.IntegrationAkahuSettings{AppToken: "app_x", UserToken: "user_y", LastSync: time.Now()}))
	return store
}

func TestBackupRoundTrip(t *testing.T) {
	backup, err := configured(t).ExportBackup()
	require.NoError(t, err)
	assert.Equal(t, models.BackupVersion, backup.Version)
	assert.True(t, backup.Schedule.AkahuLastRun.IsZero(), "when a job last ran stays with the old instance")

	fresh := tempStore(t)
	require.NoError(t, fresh.ImportBackup(backup))

	memory, err := fresh.GetMemory("japan")
	require.NoError(t, err)
	assert.Equal(t, []string{"trans_1", "trans_2"}, memory.TransactionIDs)

	prefs, err := fresh.GetTimelinePreferences()
	require.NoError(t, err)
	assert.Equal(t, []string{"Fisher Funds"}, prefs.HiddenMerchants)

	asset, err := fresh.GetAsset("house")
	require.NoError(t, err)
	assert.Len(t, asset.Valuations, 1)

	item, err := fresh.GetBudgetItem("food")
	require.NoError(t, err)
	assert.Equal(t, 250.0, item.Amount)

	salary, err := fresh.GetBudgetSalary()
	require.NoError(t, err)
	assert.Equal(t, 250.0, salary.SavingsGoal)

	schedule, err := fresh.GetScheduleSettings()
	require.NoError(t, err)
	assert.Equal(t, "6h", schedule.AkahuInterval)
	assert.True(t, schedule.AkahuEnabled)

	categories, err := fresh.GetCategorySettings()
	require.NoError(t, err)
	require.Len(t, categories.Rules, 1)
	assert.Equal(t, "Housing", categories.Rules[0].Specific)
}

func TestBackupLeavesTokensOut(t *testing.T) {
	backup, err := configured(t).ExportBackup()
	require.NoError(t, err)

	// Nowhere in the file, not just absent from a field.
	encoded, err := json.Marshal(backup)
	require.NoError(t, err)
	assert.NotContains(t, string(encoded), "user_y")
	assert.NotContains(t, string(encoded), "app_x")

	fresh := tempStore(t)
	require.NoError(t, fresh.UpdateAkahuSettings(models.IntegrationAkahuSettings{AppToken: "app_mine", UserToken: "user_mine"}))
	require.NoError(t, fresh.ImportBackup(backup))

	akahu, err := fresh.GetAkahuSettings()
	require.NoError(t, err)
	assert.Equal(t, "user_mine", akahu.UserToken, "tokens already here are untouched")
}

func TestBackupImportMerges(t *testing.T) {
	backup, err := configured(t).ExportBackup()
	require.NoError(t, err)

	target := tempStore(t)
	require.NoError(t, target.SaveMemory(models.Memory{Id: "local", Title: "Already here", TransactionIDs: []string{"trans_5"}}))
	require.NoError(t, target.SaveTimelinePreferences(models.TimelinePreferences{HiddenMerchants: []string{"NZTA"}}))
	require.NoError(t, target.ImportBackup(backup))

	memories, err := target.ReadMemories()
	require.NoError(t, err)
	assert.Len(t, memories, 2, "nothing already here is removed")

	prefs, err := target.GetTimelinePreferences()
	require.NoError(t, err)
	assert.Equal(t, []string{"NZTA", "Fisher Funds"}, prefs.HiddenMerchants)
}

func TestBackupLeavesOutSettingsNeverSaved(t *testing.T) {
	backup, err := tempStore(t).ExportBackup()
	require.NoError(t, err)
	assert.Nil(t, backup.BudgetSalary)
	assert.Nil(t, backup.Schedule)
}
