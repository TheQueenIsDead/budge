package database

import (
	"encoding/json"
	"errors"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/TheQueenIsDead/budge/pkg/database/buckets"
	"github.com/TheQueenIsDead/budge/pkg/database/models"
	bolt "go.etcd.io/bbolt"
)

/* Accounts */

func (s *Store) CountAccount() (int, error) {
	return Count[models.Account](s.db)
}
func (s *Store) GetAccount(id []byte) (models.Account, error) {
	return Get[models.Account](s.db, id)
}
func (s *Store) GetAccountsTotal() (float64, error) {
	accounts, err := Read[models.Account](s.db)
	if err != nil {
		return 0, err
	}
	total := 0.0
	for _, account := range accounts {
		total += account.Balance.Current
	}
	return total, nil
}
func (s *Store) CreateAccount(account models.Account) error {
	return Create[models.Account](s.db, account)
}
func (s *Store) ReadAccounts() ([]models.Account, error) {
	return Read[models.Account](s.db)
}

// GetRemovedAccounts returns the accounts taken out of Budge. Unset means
// none have been.
func (s *Store) GetRemovedAccounts() (models.RemovedAccounts, error) {
	removed, err := Get[models.RemovedAccounts](s.db, models.RemovedAccounts{}.Key())
	if err != nil {
		return models.RemovedAccounts{}, nil
	}
	return removed, nil
}

// RemoveAccount takes an account and its transactions out of Budge, and
// remembers it so later syncs leave it out. It all lands in one commit, so an
// account is never half removed: gone from the list but still counted in
// spending, or gone entirely but brought back by the next sync. Merchants are
// left alone, since other accounts may share them.
func (s *Store) RemoveAccount(id string) error {
	account, err := Get[models.Account](s.db, []byte(id))
	if err != nil {
		return err
	}
	removed, err := s.GetRemovedAccounts()
	if err != nil {
		return err
	}
	// Switching an account off again before it finished coming back replaces
	// its entry rather than listing it twice.
	removed.Accounts = slices.DeleteFunc(removed.Accounts, func(a models.RemovedAccount) bool { return a.Id == account.Id })
	removed.Accounts = append(removed.Accounts, models.RemovedAccount{
		Id:               account.Id,
		Name:             account.Name,
		Connection:       account.Connection.Name,
		FormattedAccount: account.FormattedAccount,
	})

	return s.db.Update(func(tx *bolt.Tx) error {
		transactions := tx.Bucket(buckets.TransactionBucket)
		var doomed [][]byte
		err := transactions.ForEach(func(k, v []byte) error {
			var t models.Transaction
			if err := json.Unmarshal(v, &t); err != nil {
				return err
			}
			if t.Account == account.Id {
				doomed = append(doomed, append([]byte{}, k...))
			}
			return nil
		})
		if err != nil {
			return err
		}
		// Deleting while ForEach walks the bucket skips keys, so collect first.
		for _, k := range doomed {
			if err := transactions.Delete(k); err != nil {
				return err
			}
		}
		if err := tx.Bucket(buckets.AccountBucket).Delete(account.Key()); err != nil {
			return err
		}
		return putAll(tx, []models.RemovedAccounts{removed})
	})
}

// RestoreAccount stops skipping an account. Its history went when it was
// removed, so the next sync reads the whole history again to get it back,
// and until that sync lands the account stays listed as restoring.
func (s *Store) RestoreAccount(id string) error {
	removed, err := s.GetRemovedAccounts()
	if err != nil {
		return err
	}
	for n := range removed.Accounts {
		if removed.Accounts[n].Id == id {
			removed.Accounts[n].Restoring = true
		}
	}
	return Update[models.RemovedAccounts](s.db, removed)
}

// FinishRestoring forgets the accounts a sync has just brought back.
func (s *Store) FinishRestoring() error {
	removed, err := s.GetRemovedAccounts()
	if err != nil || !removed.Restoring() {
		return err
	}
	removed.Accounts = slices.DeleteFunc(removed.Accounts, func(a models.RemovedAccount) bool { return a.Restoring })
	return Update[models.RemovedAccounts](s.db, removed)
}

/* Assets */

func (s *Store) GetAsset(id string) (models.Asset, error) {
	return Get[models.Asset](s.db, []byte(id))
}
func (s *Store) ReadAssets() ([]models.Asset, error) {
	return Read[models.Asset](s.db)
}
func (s *Store) CreateAsset(asset models.Asset) error {
	return Create[models.Asset](s.db, asset)
}
func (s *Store) UpdateAsset(asset models.Asset) error {
	return Update[models.Asset](s.db, asset)
}
func (s *Store) DeleteAsset(id string) error {
	return Delete[models.Asset](s.db, []byte(id))
}

// AddAssetValuation records a valuation, replacing whatever the asset already
// held for that day. An asset carries at most one valuation per day.
func (s *Store) AddAssetValuation(id string, valuation models.AssetValuation) error {
	asset, err := Get[models.Asset](s.db, []byte(id))
	if err != nil {
		return err
	}
	asset.Valuations = models.UpsertValuation(asset.Valuations, valuation)
	return Update[models.Asset](s.db, asset)
}
func (s *Store) DeleteAssetValuation(id, valuationID string) error {
	asset, err := Get[models.Asset](s.db, []byte(id))
	if err != nil {
		return err
	}
	remaining := asset.Valuations[:0]
	for _, v := range asset.Valuations {
		if v.ID != valuationID {
			remaining = append(remaining, v)
		}
	}
	asset.Valuations = remaining
	return Update[models.Asset](s.db, asset)
}

/* Memories */

func (s *Store) ReadMemories() ([]models.Memory, error) {
	return Read[models.Memory](s.db)
}

func (s *Store) GetMemory(id string) (models.Memory, error) {
	return Get[models.Memory](s.db, []byte(id))
}

// SaveMemory creates or updates a memory, taking its transactions away from
// any other memory that already held them. A purchase belongs to one memory: the same flight
// counted towards two trips would inflate both totals. A memory left with
// nothing in it is removed rather than kept as an empty card.
func (s *Store) SaveMemory(memory models.Memory) error {
	claimed := make(map[string]bool, len(memory.TransactionIDs))
	for _, id := range memory.TransactionIDs {
		claimed[id] = true
	}

	existing, err := Read[models.Memory](s.db)
	if err != nil {
		return err
	}
	for _, other := range existing {
		if other.Id == memory.Id {
			continue
		}
		kept := other.TransactionIDs[:0]
		for _, id := range other.TransactionIDs {
			if !claimed[id] {
				kept = append(kept, id)
			}
		}
		switch {
		case len(kept) == 0:
			err = Delete[models.Memory](s.db, other.Key())
		case len(kept) != len(other.TransactionIDs):
			other.TransactionIDs = kept
			err = Update[models.Memory](s.db, other)
		}
		if err != nil {
			return err
		}
	}

	return Update[models.Memory](s.db, memory)
}

func (s *Store) DeleteMemory(id string) error {
	return Delete[models.Memory](s.db, []byte(id))
}

// GetTimelinePreferences returns what the timeline has been told to hide. Unset
// means nothing is hidden.
func (s *Store) GetTimelinePreferences() (models.TimelinePreferences, error) {
	prefs, err := Get[models.TimelinePreferences](s.db, models.TimelinePreferences{}.Key())
	if err != nil {
		return models.TimelinePreferences{}, nil
	}
	return prefs, nil
}

func (s *Store) SaveTimelinePreferences(prefs models.TimelinePreferences) error {
	return Update[models.TimelinePreferences](s.db, prefs)
}

/* Spend rules */

// GetSpendRules returns the owner's own spend group rules. Unset means every
// group uses its built in rule.
func (s *Store) GetSpendRules() (models.SpendRules, error) {
	rules, err := Get[models.SpendRules](s.db, models.SpendRules{}.Key())
	if err != nil {
		return models.SpendRules{}, nil
	}
	return rules, nil
}

func (s *Store) SaveSpendRules(rules models.SpendRules) error {
	return Update[models.SpendRules](s.db, rules)
}

/* Notifications */

// notificationRetention caps how many notifications are kept. They are a log of
// things that went wrong, not an archive, and an unbounded bucket would grow
// forever behind a page nobody scrolls to the bottom of.
const notificationRetention = 200

func (s *Store) ReadNotifications() ([]models.Notification, error) {
	return Read[models.Notification](s.db)
}

// CreateNotification records a notification and drops the oldest once the log
// is over its cap.
func (s *Store) CreateNotification(notification models.Notification) error {
	if err := Create[models.Notification](s.db, notification); err != nil {
		return err
	}
	return s.pruneNotifications()
}

func (s *Store) MarkNotificationsRead(at time.Time) error {
	notifications, err := Read[models.Notification](s.db)
	if err != nil {
		return err
	}
	for _, notification := range notifications {
		if !notification.Unread() {
			continue
		}
		notification.ReadAt = at
		if err := Update[models.Notification](s.db, notification); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) DeleteNotification(id string) error {
	return Delete[models.Notification](s.db, []byte(id))
}

func (s *Store) DeleteNotifications() error {
	notifications, err := Read[models.Notification](s.db)
	if err != nil {
		return err
	}
	for _, notification := range notifications {
		if err := Delete[models.Notification](s.db, notification.Key()); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) pruneNotifications() error {
	notifications, err := Read[models.Notification](s.db)
	if err != nil {
		return err
	}
	if len(notifications) <= notificationRetention {
		return nil
	}

	sort.Slice(notifications, func(i, j int) bool {
		return notifications[i].CreatedAt.After(notifications[j].CreatedAt)
	})
	for _, notification := range notifications[notificationRetention:] {
		if err := Delete[models.Notification](s.db, notification.Key()); err != nil {
			return err
		}
	}
	return nil
}

/* Backup */

// ExportBackup gathers everything set up by hand. Settings that were never
// saved are left out rather than exported as defaults, so importing a backup
// never pins a default the new instance might have chosen differently.
func (s *Store) ExportBackup() (models.Backup, error) {
	backup := models.Backup{Version: models.BackupVersion, ExportedAt: time.Now()}

	var err error
	if backup.Memories, err = Read[models.Memory](s.db); err != nil {
		return backup, err
	}
	if backup.Assets, err = Read[models.Asset](s.db); err != nil {
		return backup, err
	}
	if backup.BudgetItems, err = Read[models.BudgetItem](s.db); err != nil {
		return backup, err
	}
	if backup.Timeline, err = s.GetTimelinePreferences(); err != nil {
		return backup, err
	}
	if salary, err := Get[models.BudgetSalary](s.db, models.BudgetSalary{}.Key()); err == nil {
		backup.BudgetSalary = &salary
	}
	if schedule, err := Get[models.ScheduleSettings](s.db, models.ScheduleSettings{}.Key()); err == nil {
		// When the jobs last ran belongs to this instance, not the next one.
		schedule.AkahuLastRun, schedule.AssetsLastRun = time.Time{}, time.Time{}
		backup.Schedule = &schedule
	}
	removed, err := s.GetRemovedAccounts()
	if err != nil {
		return backup, err
	}
	backup.RemovedAccounts = removed.Accounts
	if rules, err := s.GetSpendRules(); err == nil && len(rules.Groups) > 0 {
		backup.SpendRules = &rules
	}
	return backup, nil
}

// ImportBackup writes a backup into this instance in one commit, so a bad
// file changes nothing. It merges rather than replaces: anything here that the
// backup does not mention is kept, and anything with the same ID is
// overwritten. Hidden purchases and dismissed suggestions are combined.
func (s *Store) ImportBackup(backup models.Backup) error {
	prefs, err := s.GetTimelinePreferences()
	if err != nil {
		return err
	}
	prefs.HiddenTransactions = unionStrings(prefs.HiddenTransactions, backup.Timeline.HiddenTransactions)
	prefs.DismissedSuggestions = unionStrings(prefs.DismissedSuggestions, backup.Timeline.DismissedSuggestions)
	prefs.HiddenMerchants = unionStrings(prefs.HiddenMerchants, backup.Timeline.HiddenMerchants)

	removed, err := s.GetRemovedAccounts()
	if err != nil {
		return err
	}
	known := removed.IDs()
	for _, account := range backup.RemovedAccounts {
		if account.Id != "" && !known[account.Id] {
			known[account.Id] = true
			removed.Accounts = append(removed.Accounts, account)
		}
	}

	// A group the backup has a rule for takes the backup's; the rest are kept.
	rules, err := s.GetSpendRules()
	if err != nil {
		return err
	}
	if backup.SpendRules != nil {
		if rules.Groups == nil {
			rules.Groups = make(map[string]models.SpendRule)
		}
		for key, rule := range backup.SpendRules.Groups {
			rules.Groups[key] = rule
		}
	}

	var salary []models.BudgetSalary
	if backup.BudgetSalary != nil {
		salary = append(salary, *backup.BudgetSalary)
	}
	var schedule []models.ScheduleSettings
	if backup.Schedule != nil {
		next := *backup.Schedule
		next.AkahuLastRun, next.AssetsLastRun = time.Time{}, time.Time{}
		schedule = append(schedule, next)
	}

	return s.db.Update(func(tx *bolt.Tx) error {
		return errors.Join(
			putAll(tx, backup.Memories),
			putAll(tx, backup.Assets),
			putAll(tx, backup.BudgetItems),
			putAll(tx, []models.TimelinePreferences{prefs}),
			putAll(tx, salary),
			putAll(tx, schedule),
			putAll(tx, []models.RemovedAccounts{removed}),
			putAll(tx, []models.SpendRules{rules}),
		)
	})
}

// unionStrings combines two lists, keeping the order of the first and adding
// what only the second has.
func unionStrings(a, b []string) []string {
	seen := make(map[string]bool, len(a)+len(b))
	var union []string
	for _, value := range append(append([]string{}, a...), b...) {
		if value != "" && !seen[value] {
			seen[value] = true
			union = append(union, value)
		}
	}
	return union
}

/* Sync */

// SaveSynced stores everything a bank sync brought back in one commit. Each
// commit waits on the disk, so writing a full history of a few thousand
// transactions one at a time took minutes and outlasted the request that
// started it. One commit takes a moment, and a sync either lands whole or not
// at all.
func (s *Store) SaveSynced(accounts []models.Account, transactions []models.Transaction, merchants []models.Merchant) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		return errors.Join(
			putAll(tx, accounts),
			putAll(tx, transactions),
			putAll(tx, merchants),
		)
	})
}

/* Merchants */

func (s *Store) CreateMerchant(merchant models.Merchant) error {
	return Create[models.Merchant](s.db, merchant)
}

/* Transactions */

func (s *Store) CountTransactions() (int, error) {
	return Count[models.Transaction](s.db)
}
func (s *Store) CreateTransaction(transaction models.Transaction) error {
	return Create[models.Transaction](s.db, transaction)
}
func (s *Store) ReadTransactions() ([]models.Transaction, error) {
	return Read[models.Transaction](s.db)
}
func (s *Store) ReadTransactionsByAccount(account string) ([]models.Transaction, error) {
	return ReadFilter[models.Transaction](s.db, func(transaction models.Transaction) bool {
		return transaction.Account == account
	})
}
func (s *Store) ReadTransactionsByDate(start time.Time, end time.Time) ([]models.Transaction, error) {
	return ReadFilter[models.Transaction](s.db, func(transaction models.Transaction) bool {
		return transaction.Date.After(start) && transaction.Date.Before(end)
	})
}
func (s *Store) SearchTransactions(search string, account string) ([]models.Transaction, error) {
	return ReadFilter[models.Transaction](s.db, func(transaction models.Transaction) bool {

		accountMatch := account == "" || transaction.Account == account

		metadata := strings.Builder{}
		metadata.WriteString(transaction.Description)
		metadata.WriteString(transaction.Merchant.Name)
		for _, c := range transaction.Categories() {
			metadata.WriteString(c)
		}

		reg := regexp.MustCompile(`[^a-zA-Z]+`)
		needle := strings.ToLower(search)
		haystack := strings.ToLower(metadata.String())

		needle = reg.ReplaceAllString(needle, "")
		haystack = reg.ReplaceAllString(haystack, "")

		searchMatch := strings.Contains(haystack, needle)

		return accountMatch && searchMatch
	})
}

/* Budget Items */

func (s *Store) GetBudgetItem(id string) (models.BudgetItem, error) {
	return Get[models.BudgetItem](s.db, []byte(id))
}
func (s *Store) CreateBudgetItem(item models.BudgetItem) error {
	return Create[models.BudgetItem](s.db, item)
}
func (s *Store) ReadBudgetItems() ([]models.BudgetItem, error) {
	return Read[models.BudgetItem](s.db)
}
func (s *Store) UpdateBudgetItem(item models.BudgetItem) error {
	return Update[models.BudgetItem](s.db, item)
}
func (s *Store) AddBudgetSubItem(itemID string, sub models.BudgetSubItem) error {
	item, err := Get[models.BudgetItem](s.db, []byte(itemID))
	if err != nil {
		return err
	}
	item.SubItems = append(item.SubItems, sub)
	return Update[models.BudgetItem](s.db, item)
}
func (s *Store) UpdateBudgetSubItem(itemID string, sub models.BudgetSubItem) error {
	item, err := Get[models.BudgetItem](s.db, []byte(itemID))
	if err != nil {
		return err
	}
	for i, si := range item.SubItems {
		if si.ID == sub.ID {
			item.SubItems[i] = sub
			break
		}
	}
	return Update[models.BudgetItem](s.db, item)
}
func (s *Store) DeleteBudgetSubItem(itemID, subID string) error {
	item, err := Get[models.BudgetItem](s.db, []byte(itemID))
	if err != nil {
		return err
	}
	filtered := item.SubItems[:0]
	for _, si := range item.SubItems {
		if si.ID != subID {
			filtered = append(filtered, si)
		}
	}
	item.SubItems = filtered
	return Update[models.BudgetItem](s.db, item)
}

/* Budget Salary */

func (s *Store) GetBudgetSalary() (models.BudgetSalary, error) {
	salary, err := Get[models.BudgetSalary](s.db, models.BudgetSalary{}.Key())
	if err != nil {
		return models.BudgetSalary{
			SalaryFrequency: "yearly",
			IncludePAYE:     true,
			KiwiSaverRate:   3.5,
		}, nil
	}
	return salary, nil
}
func (s *Store) SaveBudgetSalary(salary models.BudgetSalary) error {
	return Update[models.BudgetSalary](s.db, salary)
}

/* Schedule */

// GetScheduleSettings returns what runs by itself. An unset schedule is off:
// nothing should start reaching out to a bank without being asked to.
func (s *Store) GetScheduleSettings() (models.ScheduleSettings, error) {
	schedule, err := Get[models.ScheduleSettings](s.db, models.ScheduleSettings{}.Key())
	if err != nil {
		return models.ScheduleSettings{
			AkahuInterval:  models.AkahuIntervals[1].Key,
			AssetsInterval: models.AssetIntervals[0].Key,
		}, nil
	}
	return schedule, nil
}

func (s *Store) SaveScheduleSettings(schedule models.ScheduleSettings) error {
	return Update[models.ScheduleSettings](s.db, schedule)
}

/* Settings */

func (s *Store) GetAkahuSettings() (models.IntegrationAkahuSettings, error) {
	return Get[models.IntegrationAkahuSettings](s.db, models.IntegrationAkahuSettings{}.Key())
}
func (s *Store) UpdateAkahuSettings(settings models.IntegrationAkahuSettings) error {
	return Update[models.IntegrationAkahuSettings](s.db, settings)
}

// RecordAkahuSync notes a successful sync, and how fresh Akahu's data was at
// the time, which is where the next sync starts reading.
func (s *Store) RecordAkahuSync(dataThrough time.Time) error {
	settings, err := Get[models.IntegrationAkahuSettings](s.db, models.IntegrationAkahuSettings{}.Key())
	if err != nil {
		return err
	}
	settings.LastSync = time.Now()
	settings.DataThrough = dataThrough
	return Update[models.IntegrationAkahuSettings](s.db, settings)
}

// ResetAkahuLastSync forgets how far the synced data reaches, so the next
// sync reads the whole history again.
func (s *Store) ResetAkahuLastSync() error {
	settings, err := Get[models.IntegrationAkahuSettings](s.db, models.IntegrationAkahuSettings{}.Key())
	if err != nil {
		return err
	}
	settings.LastSync = time.Time{}
	settings.DataThrough = time.Time{}
	return Update[models.IntegrationAkahuSettings](s.db, settings)
}
func (s *Store) DeleteSynced() error {
	err := s.db.Update(func(tx *bolt.Tx) error {
		accountErr := tx.DeleteBucket(buckets.AccountBucket)
		merchantErr := tx.DeleteBucket(buckets.MerchantBucket)
		transactionErr := tx.DeleteBucket(buckets.TransactionBucket)
		return errors.Join(accountErr, merchantErr, transactionErr)
	})
	if err != nil {
		return err
	}
	return s.db.Update(func(tx *bolt.Tx) error {
		_, accountErr := tx.CreateBucket(buckets.AccountBucket)
		_, merchantErr := tx.CreateBucket(buckets.MerchantBucket)
		_, transactionErr := tx.CreateBucket(buckets.TransactionBucket)
		return errors.Join(accountErr, merchantErr, transactionErr)
	})
}
