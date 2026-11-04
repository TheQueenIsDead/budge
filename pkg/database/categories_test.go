package database

import (
	"errors"
	"testing"
	"time"

	"github.com/TheQueenIsDead/budge/pkg/database/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	bolt "go.etcd.io/bbolt"
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
		{"an amount at the bottom of the range matches", models.CategoryRule{Contains: "aa insurance", AtLeast: 100}, categorisedTx("6", "DEBIT", "AA INSURANCE AUCKLAND", ""), true},
		{"an amount under the range does not", models.CategoryRule{Contains: "aa insurance", AtLeast: 100.01}, categorisedTx("7", "DEBIT", "AA INSURANCE AUCKLAND", ""), false},
		{"an amount at the top of the range matches", models.CategoryRule{Contains: "aa insurance", AtMost: 100}, categorisedTx("8", "DEBIT", "AA INSURANCE AUCKLAND", ""), true},
		{"an amount over the range does not", models.CategoryRule{Contains: "aa insurance", AtLeast: 50, AtMost: 99.99}, categorisedTx("9", "DEBIT", "AA INSURANCE AUCKLAND", ""), false},
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
	groceries.Category.Name = "Supermarkets and grocery stores"
	groceries.Category.Groups.PersonalFinance.Name = "Food"
	require.NoError(t, store.CreateTransaction(t.Context(), akahu))
	require.NoError(t, store.CreateTransaction(t.Context(), groceries))

	require.NoError(t, store.SaveCategorySettings(t.Context(), models.CategorySettings{Rules: []models.CategoryRule{
		{ID: "first", Contains: "loan interest", Category: "mortgage"},
		{ID: "second", Contains: "interest", Category: "charity"},
	}}))

	byID := func(txs []models.Transaction) map[string]models.Transaction {
		m := map[string]models.Transaction{}
		for _, tx := range txs {
			m[tx.Id] = tx
		}
		return m
	}
	reads := map[string]func() ([]models.Transaction, error){
		"all": func() ([]models.Transaction, error) { return store.ReadTransactions(t.Context()) },
		"by date": func() ([]models.Transaction, error) {
			return store.ReadTransactionsByDate(t.Context(), time.Now().AddDate(0, 0, -1), time.Now())
		},
		"search": func() ([]models.Transaction, error) { return store.SearchTransactions(t.Context(), "mortgage", "") },
	}
	for name, read := range reads {
		t.Run(name, func(t *testing.T) {
			txs, err := read()
			require.NoError(t, err)
			tx := byID(txs)["interest"]
			assert.Equal(t, "mortgage", tx.Filed, "the first matching rule wins")
			assert.Equal(t, models.FiledByRule, tx.FiledBy)
			assert.Equal(t, []string{"Home", "Mortgage"}, tx.Categories())
			assert.Empty(t, tx.Category.Name, "Akahu's own category is left as it came")
		})
	}

	txs, err := store.ReadTransactions(t.Context())
	require.NoError(t, err)
	assert.Equal(t, "groceries", byID(txs)["groceries"].Filed, "unmatched transactions go where Akahu's category does")
	assert.Empty(t, byID(txs)["groceries"].FiledBy)

	t.Run("searching finds Akahu's categories too", func(t *testing.T) {
		txs, err := store.SearchTransactions(t.Context(), "supermarkets", "")
		require.NoError(t, err)
		assert.Contains(t, byID(txs), "groceries")
	})

	t.Run("deleting the rules puts Akahu's category back", func(t *testing.T) {
		require.NoError(t, store.SaveCategorySettings(t.Context(), models.CategorySettings{}))
		txs, err := store.ReadTransactions(t.Context())
		require.NoError(t, err)
		assert.Equal(t, "loan-interest", byID(txs)["interest"].Filed)
	})
}

func TestRulesFromBeforeBudgeCategories(t *testing.T) {
	// Rules and overrides saved before budge had its own categories name
	// Akahu's, or the owner's own; they land where those would.
	tests := []struct {
		broad, specific, want string
	}{
		{"Housing", "Mortgage", "mortgage"},
		{"General", "Savings", "savings"},
		{"Lifestyle", "Hotels, motels, and other temporary accommodation", "travel"},
		{"Transport", "Automotive repair and servicing", "servicing"},
		{"Household", "Pets and related supplies, accommodation, and services", "pet-care"},
		{"General", "Payments", "other"},
	}
	for _, tt := range tests {
		t.Run(tt.specific, func(t *testing.T) {
			assert.Equal(t, tt.want, models.CategoryRule{Broad: tt.broad, Specific: tt.specific}.Target())
			assert.Equal(t, tt.want, models.CategoryOverride{Broad: tt.broad, Specific: tt.specific}.Target())
		})
	}
	assert.Equal(t, "fuel", models.CategoryRule{Category: "fuel", Broad: "Housing", Specific: "Mortgage"}.Target(),
		"a budge category wins over an old one")
}

func TestCategorySettingsMergeOnImport(t *testing.T) {
	store := tempStore(t)
	require.NoError(t, store.SaveCategorySettings(t.Context(), models.CategorySettings{
		Categories: []models.CustomCategory{{ID: "c1", Broad: "General", Specific: "Housing"}},
		Rules:      []models.CategoryRule{{ID: "r1", Contains: "old"}, {ID: "r2", Contains: "keep"}},
	}))

	require.NoError(t, store.ImportBackup(t.Context(), models.Backup{Categories: &models.CategorySettings{
		Categories: []models.CustomCategory{{ID: "c2", Broad: "General", Specific: "Mortgage"}},
		Rules:      []models.CategoryRule{{ID: "r1", Contains: "new"}, {ID: "r3", Contains: "added"}},
	}}))

	settings, err := store.GetCategorySettings(t.Context())
	require.NoError(t, err)
	assert.Len(t, settings.Categories, 2)
	var patterns []string
	for _, rule := range settings.Rules {
		patterns = append(patterns, rule.Contains)
	}
	assert.Equal(t, []string{"new", "keep", "added"}, patterns, "same id overwrites in place and rule order is kept")
}

func TestTransferRules(t *testing.T) {
	out := categorisedTx("out", "TRANSFER", "AP#19781426 TO D J KING", "")
	out.Amount = -641
	in := categorisedTx("in", "TRANSFER", "AP#19781426 FROM D J KING", "")
	in.Amount = 641
	plain := models.CategoryRule{ID: "plain", Contains: "AP#19781426", Category: "mortgage"}
	counted := plain
	counted.IncludeTransfers = true

	assert.False(t, plain.Matches(out), "transfers are left alone unless the rule opts in")
	assert.True(t, counted.Matches(out), "an opted-in rule matches the money leaving")
	assert.False(t, counted.Matches(in), "the money arriving stays a transfer, so it is never counted twice")

	txs := []models.Transaction{out, in}
	models.CategorySettings{Rules: []models.CategoryRule{counted}}.Apply(txs)

	assert.Equal(t, models.TypeCountedTransfer, txs[0].Type)
	assert.Equal(t, "mortgage", txs[0].Filed)
	assert.True(t, txs[0].IsTransfer(), "it is still a transfer, just one that is counted")
	assert.True(t, counted.Matches(txs[0]), "a counted leg still matches its rule, so match counts stay right")
	assert.Equal(t, "TRANSFER", txs[1].Type, "the arriving leg is untouched")
	assert.Empty(t, txs[1].Categories(), "money arriving is not filed anywhere")
}

func TestOverridesWinOverRulesAndAkahu(t *testing.T) {
	cylinder := categorisedTx("cyl", "PAYMENT", "PAY The Cylinder Guy Limited", "")
	cylinder.Amount = -1200
	roofer := categorisedTx("roof", "PAYMENT", "PAY Christchurch Roofing", "")
	moved := categorisedTx("out", "TRANSFER", "TO D J KING", "")
	moved.Amount = -300
	settings := models.CategorySettings{
		Rules: []models.CategoryRule{{ID: "pay", Contains: "PAY ", Category: "trades"}},
		Overrides: []models.CategoryOverride{
			{TransactionID: "cyl", Category: "hardware"},
			{TransactionID: "out", Category: "mortgage"},
		},
	}
	txs := []models.Transaction{cylinder, roofer, moved}

	settings.Apply(txs)

	assert.Equal(t, "hardware", txs[0].Filed, "the override wins over the rule")
	assert.Equal(t, models.FiledByOwner, txs[0].FiledBy)
	assert.Equal(t, "trades", txs[1].Filed, "other transactions still follow the rule")
	assert.Equal(t, models.TypeCountedTransfer, txs[2].Type, "money leaving by transfer that is filed by hand is counted")

	_, ok := settings.Override("cyl")
	assert.True(t, ok)
	_, ok = settings.Override("roof")
	assert.False(t, ok)
}

func TestAkahuBudgetCategory(t *testing.T) {
	tests := []struct {
		broad, specific, txType, want string
	}{
		{"Food", "Supermarkets and grocery stores", "EFTPOS", "groceries"},
		{"Food", "Bakeries", "EFTPOS", "groceries"},
		{"Lifestyle", "Cafes and restaurants", "EFTPOS", "eating-out"},
		{"Lifestyle", "Liquor stores", "EFTPOS", "liquor"},
		{"Lifestyle", "Steampunk HQ", "EFTPOS", "entertainment"},
		{"Utilities", "Electricity services", "DIRECT DEBIT", "power"},
		{"Utilities", "Water", "DIRECT DEBIT", "bills"},
		{"Transport", "Fuel stations", "EFTPOS", "fuel"},
		{"Transport", "Car, truck, and motorcycle dealers", "PAYMENT", "vehicles"},
		{"Health", "Veterinary services", "EFTPOS", "vets"},
		{"Household", "Insurance", "DIRECT DEBIT", "insurance"},
		{"Professional Services", "Accounting", "PAYMENT", "other"},
		{"", "", "LOAN", "loan-interest"},
		{"", "", "ATM", "cash"},
		{"", "", "PAYMENT", "uncategorised"},
	}
	for _, tt := range tests {
		t.Run(tt.specific+tt.txType, func(t *testing.T) {
			assert.Equal(t, tt.want, models.AkahuBudgetCategory(tt.broad, tt.specific, tt.txType))
		})
	}
}

func TestBudgetCategoryRegistry(t *testing.T) {
	for _, top := range models.TopBudgetCategories() {
		assert.Equal(t, top, models.TopBudgetCategory(top.Key))
		for _, sub := range models.BudgetSubcategories(top.Key) {
			assert.Equal(t, top.Key, models.TopBudgetCategory(sub.Key).Key)
			assert.Equal(t, top.OffBudget, sub.OffBudget, "a subcategory is on or off budget with its parent")
			assert.Equal(t, top.Bill, sub.Bill)
			assert.Equal(t, top.Accent, sub.Accent)
			assert.NotEmpty(t, sub.Icon, "a subcategory without its own icon takes its parent's")
		}
	}
	for _, key := range []string{"power", "fuel", "groceries", "eating-out", "internet-mobile"} {
		_, ok := models.BudgetCategoryByKey(key)
		assert.True(t, ok, key)
	}
	assert.Equal(t, "uncategorised", models.TopBudgetCategory("nonsense").Key)

	savings, _ := models.BudgetCategoryByKey("savings")
	assert.True(t, savings.NotSpending)
	assert.False(t, savings.Budgeted())
	groceries, _ := models.BudgetCategoryByKey("groceries")
	assert.True(t, groceries.Budgeted())
	uncategorised, _ := models.BudgetCategoryByKey("uncategorised")
	assert.False(t, uncategorised.Budgeted(), "nothing is filed there on purpose, so it takes no target")
	assert.False(t, uncategorised.NotSpending, "but it is spending all the same")
	power, _ := models.BudgetCategoryByKey("power")
	assert.True(t, power.Bill, "a bill's subcategories are bills too")
	assert.False(t, groceries.Bill)
}

func TestCategoriesFromBeforeOnAndOffBudget(t *testing.T) {
	useDefaultCategories(t)
	// A list saved when categories sat under groups: Not budgeted is off
	// budget now, and Bills still marks a bill.
	models.UseBudgetCategories(models.BudgetCategorySettings{Categories: []models.BudgetCategory{
		{Key: "home", Label: "Home", Group: "bills"},
		{Key: "mortgage", Label: "Mortgage", Parent: "home"},
		{Key: "groceries", Label: "Groceries", Group: "everyday"},
		{Key: "savings", Label: "Savings", Group: "unbudgeted"},
		{Key: "uncategorised", Label: "Uncategorised", Group: "unbudgeted"},
	}})

	home, _ := models.BudgetCategoryByKey("home")
	assert.True(t, home.Bill)
	assert.True(t, home.Budgeted())
	mortgage, _ := models.BudgetCategoryByKey("mortgage")
	assert.True(t, mortgage.Bill)
	savings, _ := models.BudgetCategoryByKey("savings")
	assert.True(t, savings.OffBudget)
	assert.True(t, savings.NotSpending)
	groceries, _ := models.BudgetCategoryByKey("groceries")
	assert.True(t, groceries.Budgeted())

	t.Run("an edit saves it in the new shape", func(t *testing.T) {
		saved := models.BudgetCategorySettings{Categories: []models.BudgetCategory{{Key: "savings", Label: "Savings", Group: "unbudgeted"}}}
		next, err := saved.UpdateBudgetCategory("savings", models.BudgetCategoryChange{Label: "Savings & investments"})
		require.NoError(t, err)
		assert.True(t, next.Categories[0].OffBudget)
		assert.Empty(t, next.Categories[0].Group)
	})
}

// useDefaultCategories puts budge's own list back after a test changes it.
func useDefaultCategories(t *testing.T) {
	t.Cleanup(func() { models.UseBudgetCategories(models.BudgetCategorySettings{}) })
}

func keys(categories []models.BudgetCategory) []string {
	var out []string
	for _, category := range categories {
		out = append(out, category.Key)
	}
	return out
}

func TestAddBudgetCategory(t *testing.T) {
	useDefaultCategories(t)
	settings, key, err := models.BudgetCategorySettings{}.AddBudgetCategory("Kids & school", "")
	require.NoError(t, err)
	assert.Equal(t, "kids-school", key)
	models.UseBudgetCategories(settings)

	top := keys(models.TopBudgetCategories())
	assert.Equal(t, "kids-school", top[indexOfKey(top, "other")+1], "a new category goes after the last on budget")
	assert.Equal(t, "savings", top[indexOfKey(top, "kids-school")+1], "and before those off it")
	kids, ok := models.BudgetCategoryByKey("kids-school")
	require.True(t, ok)
	assert.True(t, kids.Budgeted(), "on budget, so its spending counts from the start")
	assert.False(t, kids.NotSpending)

	t.Run("a name already used gets a key of its own", func(t *testing.T) {
		_, key, err := settings.AddBudgetCategory("Kids & school", "")
		require.NoError(t, err)
		assert.Equal(t, "kids-school-2", key)
	})

	t.Run("a subcategory goes after its category's others", func(t *testing.T) {
		next, key, err := settings.AddBudgetCategory("Tolls", "transport")
		require.NoError(t, err)
		models.UseBudgetCategories(next)
		subs := keys(models.BudgetSubcategories("transport"))
		assert.Equal(t, key, subs[len(subs)-1])
		tolls, _ := models.BudgetCategoryByKey(key)
		assert.Equal(t, "clay", tolls.Accent, "it takes its category's colour")
		assert.True(t, tolls.Budgeted(), "and is on budget with it")
	})

	for name, add := range map[string][2]string{
		"a name is needed":          {"  ", ""},
		"the parent must exist":     {"Kids", "nowhere"},
		"subcategories go one deep": {"Kids", "fuel"},
	} {
		t.Run(name, func(t *testing.T) {
			_, _, err := settings.AddBudgetCategory(add[0], add[1])
			var refused models.BudgetCategoryError
			assert.ErrorAs(t, err, &refused)
		})
	}
}

func indexOfKey(keys []string, key string) int {
	for i, k := range keys {
		if k == key {
			return i
		}
	}
	return -1
}

func TestUpdateBudgetCategory(t *testing.T) {
	useDefaultCategories(t)
	settings, err := models.BudgetCategorySettings{}.UpdateBudgetCategory("groceries", models.BudgetCategoryChange{
		Label: "Food shopping", Blurb: "The weekly shop", Icon: "bi-bag", Accent: "plum",
	})
	require.NoError(t, err)
	models.UseBudgetCategories(settings)

	groceries, ok := models.BudgetCategoryByKey("groceries")
	require.True(t, ok, "the key stays, so rules and targets stay with it")
	assert.Equal(t, "Food shopping", groceries.Label)
	assert.Equal(t, "The weekly shop", groceries.Blurb)
	assert.Equal(t, "bi-bag", groceries.Icon)
	assert.Equal(t, "plum", groceries.Accent)
	assert.Equal(t, "groceries", models.AkahuBudgetCategory("Food", "Bakeries", "EFTPOS"), "Akahu's categories still land in it")

	t.Run("taking it off budget takes its subcategories along", func(t *testing.T) {
		off := true
		next, err := settings.UpdateBudgetCategory("transport", models.BudgetCategoryChange{Label: "Transport", OffBudget: &off})
		require.NoError(t, err)
		models.UseBudgetCategories(next)
		transport, _ := models.BudgetCategoryByKey("transport")
		assert.False(t, transport.Budgeted())
		fuel, _ := models.BudgetCategoryByKey("fuel")
		assert.False(t, fuel.Budgeted())
		top := keys(models.TopBudgetCategories())
		assert.Equal(t, "transport", top[indexOfKey(top, "groceries")+1], "it keeps its place in the list")
	})

	t.Run("Uncategorised cannot go on budget", func(t *testing.T) {
		on := false
		_, err := settings.UpdateBudgetCategory("uncategorised", models.BudgetCategoryChange{Label: "Unfiled", OffBudget: &on})
		var refused models.BudgetCategoryError
		assert.ErrorAs(t, err, &refused)
		_, err = settings.UpdateBudgetCategory("uncategorised", models.BudgetCategoryChange{Label: "Unfiled"})
		assert.NoError(t, err, "but can be renamed")
	})
}

func TestDeleteBudgetCategory(t *testing.T) {
	useDefaultCategories(t)
	settings, err := models.BudgetCategorySettings{}.DeleteBudgetCategory("fuel", "")
	require.NoError(t, err)
	models.UseBudgetCategories(settings)

	_, gone := models.BudgetCategoryByKey("fuel")
	assert.True(t, gone, "a deleted key is still found, as where it went")
	assert.Equal(t, "transport", models.ResolveBudgetCategory("fuel"), "a subcategory's transactions go to its category")
	assert.Equal(t, "transport", models.ResolveBudgetCategory(models.AkahuBudgetCategory("Transport", "Fuel stations", "EFTPOS")),
		"Akahu's category for it follows")
	assert.Equal(t, "transport", models.CategoryRule{Category: "fuel"}.Target())

	t.Run("deleting a category takes its subcategories with it", func(t *testing.T) {
		next, err := settings.DeleteBudgetCategory("transport", "other")
		require.NoError(t, err)
		models.UseBudgetCategories(next)
		assert.NotContains(t, keys(models.TopBudgetCategories()), "transport")
		for _, key := range []string{"transport", "fuel", "rego", "parking"} {
			assert.Equal(t, "other", models.ResolveBudgetCategory(key), key)
		}
	})

	t.Run("a new category never takes a deleted one's key", func(t *testing.T) {
		_, key, err := settings.AddBudgetCategory("Fuel", "")
		require.NoError(t, err)
		assert.Equal(t, "fuel-2", key)
	})

	for name, del := range map[string][2]string{
		"Uncategorised stays":        {"uncategorised", "other"},
		"it cannot go into itself":   {"transport", "transport"},
		"nor into its subcategories": {"transport", "rego"},
		"it must go somewhere real":  {"transport", "nowhere"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := settings.DeleteBudgetCategory(del[0], del[1])
			var refused models.BudgetCategoryError
			assert.ErrorAs(t, err, &refused)
		})
	}
}

func TestBudgetCategoriesAreSavedAndBackedUp(t *testing.T) {
	useDefaultCategories(t)
	store := tempStore(t)
	edited, err := models.BudgetCategorySettings{}.UpdateBudgetCategory("groceries", models.BudgetCategoryChange{Label: "Food shopping"})
	require.NoError(t, err)
	edited, err = edited.DeleteBudgetCategory("parking", "")
	require.NoError(t, err)
	require.NoError(t, store.SaveBudgetCategorySettings(t.Context(), edited))

	groceries, _ := models.BudgetCategoryByKey("groceries")
	assert.Equal(t, "Food shopping", groceries.Label, "a save is in use straight away")

	backup, err := store.ExportBackup(t.Context())
	require.NoError(t, err)
	require.NotNil(t, backup.BudgetCategories)

	models.UseBudgetCategories(models.BudgetCategorySettings{})
	other := tempStore(t)
	groceries, _ = models.BudgetCategoryByKey("groceries")
	require.Equal(t, "Groceries", groceries.Label, "a new instance starts with budge's own")

	require.NoError(t, other.ImportBackup(t.Context(), backup))
	groceries, _ = models.BudgetCategoryByKey("groceries")
	assert.Equal(t, "Food shopping", groceries.Label, "an import brings the list and puts it in use")
	assert.Equal(t, "transport", models.ResolveBudgetCategory("parking"))

	saved, err := other.GetBudgetCategorySettings(t.Context())
	require.NoError(t, err)
	assert.Equal(t, "transport", saved.Merged["parking"])
}

func TestUnreadableCategorySettingsAreAnError(t *testing.T) {
	useDefaultCategories(t)
	store := tempStore(t)

	_, err := store.GetBudgetCategorySettings(t.Context())
	assert.NoError(t, err, "nothing saved yet is budge's own list, not an error")
	_, err = store.GetCategorySettings(t.Context())
	assert.NoError(t, err)

	require.NoError(t, store.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(models.BudgetCategorySettings{}.Bucket())
		return errors.Join(
			b.Put(models.BudgetCategorySettings{}.Key(), []byte("{not json")),
			b.Put(models.CategorySettings{}.Key(), []byte("{not json")),
		)
	}))
	_, err = store.GetBudgetCategorySettings(t.Context())
	assert.Error(t, err, "a saved list that cannot be read is not quietly replaced by the default, for the next edit to save over")
	_, err = store.GetCategorySettings(t.Context())
	assert.Error(t, err)
}

func TestUnreadableSettingsAreAnError(t *testing.T) {
	store := tempStore(t)

	salary, err := store.GetBudgetSalary(t.Context())
	require.NoError(t, err, "nothing saved is the usual starting point")
	assert.Equal(t, "yearly", salary.SalaryFrequency)
	schedule, err := store.GetScheduleSettings(t.Context())
	require.NoError(t, err)
	assert.False(t, schedule.AkahuEnabled, "nothing saved is off")
	_, err = store.GetTimelinePreferences(t.Context())
	require.NoError(t, err)
	backup, err := store.ExportBackup(t.Context())
	require.NoError(t, err)
	assert.Nil(t, backup.BudgetSalary, "a setting never saved is left out of a backup")

	for _, key := range [][]byte{models.BudgetSalary{}.Key(), models.ScheduleSettings{}.Key(), models.TimelinePreferences{}.Key()} {
		require.NoError(t, store.db.Update(func(tx *bolt.Tx) error {
			return tx.Bucket(models.BudgetSalary{}.Bucket()).Put(key, []byte("{not json"))
		}))
	}
	_, err = store.GetBudgetSalary(t.Context())
	assert.Error(t, err, "settings that cannot be read are not quietly replaced, for the next change to save over")
	_, err = store.GetScheduleSettings(t.Context())
	assert.Error(t, err)
	_, err = store.GetTimelinePreferences(t.Context())
	assert.Error(t, err)
	_, err = store.ExportBackup(t.Context())
	assert.Error(t, err, "nor left out of a backup without a word")
}
