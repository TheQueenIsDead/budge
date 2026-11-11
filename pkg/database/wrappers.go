package database

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/TheQueenIsDead/budge/pkg/database/buckets"
	"github.com/TheQueenIsDead/budge/pkg/database/models"
	bolt "go.etcd.io/bbolt"
)

/* Accounts */

func (s *Store) CountAccount(ctx context.Context) (int, error) {
	return Count[models.Account](ctx, s.db)
}
func (s *Store) GetAccount(ctx context.Context, id []byte) (models.Account, error) {
	return Get[models.Account](ctx, s.db, id)
}
func (s *Store) GetAccountsTotal(ctx context.Context) (float64, error) {
	accounts, err := Read[models.Account](ctx, s.db)
	if err != nil {
		return 0, err
	}
	total := 0.0
	for _, account := range accounts {
		total += account.Balance.Current
	}
	return total, nil
}
func (s *Store) CreateAccount(ctx context.Context, account models.Account) error {
	return Create[models.Account](ctx, s.db, account)
}
func (s *Store) ReadAccounts(ctx context.Context) ([]models.Account, error) {
	return Read[models.Account](ctx, s.db)
}

/* Assets */

func (s *Store) GetAsset(ctx context.Context, id string) (models.Asset, error) {
	return Get[models.Asset](ctx, s.db, []byte(id))
}
func (s *Store) ReadAssets(ctx context.Context) ([]models.Asset, error) {
	return Read[models.Asset](ctx, s.db)
}
func (s *Store) CreateAsset(ctx context.Context, asset models.Asset) error {
	return Create[models.Asset](ctx, s.db, asset)
}
func (s *Store) UpdateAsset(ctx context.Context, asset models.Asset) error {
	return Update[models.Asset](ctx, s.db, asset)
}
func (s *Store) DeleteAsset(ctx context.Context, id string) error {
	return Delete[models.Asset](ctx, s.db, []byte(id))
}

// AddAssetValuation records a valuation, replacing whatever the asset already
// held for that day. An asset carries at most one valuation per day.
func (s *Store) AddAssetValuation(ctx context.Context, id string, valuation models.AssetValuation) error {
	asset, err := Get[models.Asset](ctx, s.db, []byte(id))
	if err != nil {
		return err
	}
	asset.Valuations = models.UpsertValuation(asset.Valuations, valuation)
	return Update[models.Asset](ctx, s.db, asset)
}
func (s *Store) DeleteAssetValuation(ctx context.Context, id, valuationID string) error {
	asset, err := Get[models.Asset](ctx, s.db, []byte(id))
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
	return Update[models.Asset](ctx, s.db, asset)
}

/* Memories */

func (s *Store) ReadMemories(ctx context.Context) ([]models.Memory, error) {
	return Read[models.Memory](ctx, s.db)
}

func (s *Store) GetMemory(ctx context.Context, id string) (models.Memory, error) {
	return Get[models.Memory](ctx, s.db, []byte(id))
}

// SaveMemory creates or updates a memory, taking its transactions away from
// any other memory that already held them. A purchase belongs to one memory: the same flight
// counted towards two trips would inflate both totals. A memory left with
// nothing in it is removed rather than kept as an empty card.
func (s *Store) SaveMemory(ctx context.Context, memory models.Memory) error {
	claimed := make(map[string]bool, len(memory.TransactionIDs))
	for _, id := range memory.TransactionIDs {
		claimed[id] = true
	}

	existing, err := Read[models.Memory](ctx, s.db)
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
			err = Delete[models.Memory](ctx, s.db, other.Key())
		case len(kept) != len(other.TransactionIDs):
			other.TransactionIDs = kept
			err = Update[models.Memory](ctx, s.db, other)
		}
		if err != nil {
			return err
		}
	}

	return Update[models.Memory](ctx, s.db, memory)
}

func (s *Store) DeleteMemory(ctx context.Context, id string) error {
	return Delete[models.Memory](ctx, s.db, []byte(id))
}

// GetTimelinePreferences returns what the timeline has been told to hide.
// Unset means nothing is hidden; preferences that cannot be read are an
// error, rather than nothing, which the next change would save over.
func (s *Store) GetTimelinePreferences(ctx context.Context) (models.TimelinePreferences, error) {
	prefs, _, err := GetOptional[models.TimelinePreferences](ctx, s.db, models.TimelinePreferences{}.Key())
	if err != nil {
		return models.TimelinePreferences{}, fmt.Errorf("reading timeline preferences: %w", err)
	}
	return prefs, nil
}

func (s *Store) SaveTimelinePreferences(ctx context.Context, prefs models.TimelinePreferences) error {
	return Update[models.TimelinePreferences](ctx, s.db, prefs)
}

/* Notifications */

// notificationRetention caps how many notifications are kept. They are a log of
// things that went wrong, not an archive, and an unbounded bucket would grow
// forever behind a page nobody scrolls to the bottom of.
const notificationRetention = 200

func (s *Store) ReadNotifications(ctx context.Context) ([]models.Notification, error) {
	return Read[models.Notification](ctx, s.db)
}

// GetNotificationLog reads when each notification was last raised.
func (s *Store) GetNotificationLog(ctx context.Context) (models.NotificationLog, error) {
	log, _, err := GetOptional[models.NotificationLog](ctx, s.db, models.NotificationLog{}.Key())
	if log.Raised == nil {
		log.Raised = map[string]time.Time{}
	}
	return log, err
}

func (s *Store) SaveNotificationLog(ctx context.Context, log models.NotificationLog) error {
	return Update[models.NotificationLog](ctx, s.db, log)
}

// CreateNotification records a notification and drops the oldest once the log
// is over its cap.
func (s *Store) CreateNotification(ctx context.Context, notification models.Notification) error {
	if err := Create[models.Notification](ctx, s.db, notification); err != nil {
		return err
	}
	return s.pruneNotifications(ctx)
}

func (s *Store) MarkNotificationsRead(ctx context.Context, at time.Time) error {
	notifications, err := Read[models.Notification](ctx, s.db)
	if err != nil {
		return err
	}
	for _, notification := range notifications {
		if !notification.Unread() {
			continue
		}
		notification.ReadAt = at
		if err := Update[models.Notification](ctx, s.db, notification); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) DeleteNotification(ctx context.Context, id string) error {
	return Delete[models.Notification](ctx, s.db, []byte(id))
}

func (s *Store) DeleteNotifications(ctx context.Context) error {
	notifications, err := Read[models.Notification](ctx, s.db)
	if err != nil {
		return err
	}
	for _, notification := range notifications {
		if err := Delete[models.Notification](ctx, s.db, notification.Key()); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) pruneNotifications(ctx context.Context) error {
	notifications, err := Read[models.Notification](ctx, s.db)
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
		if err := Delete[models.Notification](ctx, s.db, notification.Key()); err != nil {
			return err
		}
	}
	return nil
}

/* Backup */

// ExportBackup gathers everything set up by hand. Settings that were never
// saved are left out rather than exported as defaults, so importing a backup
// never pins a default the new instance might have chosen differently.
func (s *Store) ExportBackup(ctx context.Context) (models.Backup, error) {
	backup := models.Backup{Version: models.BackupVersion, ExportedAt: time.Now()}

	var err error
	if backup.Memories, err = Read[models.Memory](ctx, s.db); err != nil {
		return backup, err
	}
	if backup.Assets, err = Read[models.Asset](ctx, s.db); err != nil {
		return backup, err
	}
	if backup.BudgetItems, err = Read[models.BudgetItem](ctx, s.db); err != nil {
		return backup, err
	}
	if backup.Timeline, err = s.GetTimelinePreferences(ctx); err != nil {
		return backup, err
	}
	// A setting never saved is left out, but one that cannot be read fails
	// the export, rather than leaving a backup quietly without it.
	if backup.BudgetSalary, err = exported[models.BudgetSalary](ctx, s); err != nil {
		return backup, err
	}
	if backup.Schedule, err = exported[models.ScheduleSettings](ctx, s); err != nil {
		return backup, err
	}
	if backup.Schedule != nil {
		// When the jobs last ran belongs to this instance, not the next one.
		backup.Schedule.AkahuLastRun, backup.Schedule.AssetsLastRun = time.Time{}, time.Time{}
	}
	if backup.Categories, err = exported[models.CategorySettings](ctx, s); err != nil {
		return backup, err
	}
	if backup.BudgetCategories, err = exported[models.BudgetCategorySettings](ctx, s); err != nil {
		return backup, err
	}
	return backup, nil
}

// exported reads a setting for a backup: nil when it was never saved.
func exported[T Storable](ctx context.Context, s *Store) (*T, error) {
	var zero T
	setting, found, err := GetOptional[T](ctx, s.db, zero.Key())
	if err != nil || !found {
		return nil, err
	}
	return &setting, nil
}

// ImportBackup writes a backup into this instance in one commit, so a bad
// file changes nothing. It merges rather than replaces: anything here that the
// backup does not mention is kept, and anything with the same ID is
// overwritten. Hidden purchases and dismissed suggestions are combined.
func (s *Store) ImportBackup(ctx context.Context, backup models.Backup) error {
	prefs, err := s.GetTimelinePreferences(ctx)
	if err != nil {
		return err
	}
	prefs.HiddenTransactions = unionStrings(prefs.HiddenTransactions, backup.Timeline.HiddenTransactions)
	prefs.DismissedSuggestions = unionStrings(prefs.DismissedSuggestions, backup.Timeline.DismissedSuggestions)
	prefs.HiddenMerchants = unionStrings(prefs.HiddenMerchants, backup.Timeline.HiddenMerchants)

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

	var categories []models.CategorySettings
	if backup.Categories != nil {
		current, err := s.GetCategorySettings(ctx)
		if err != nil {
			return err
		}
		categories = append(categories, mergeCategorySettings(current, *backup.Categories))
	}

	// The category list is one whole, its order and merges depending on each
	// other, so a backup's replaces this one's rather than merging into it.
	var list []models.BudgetCategorySettings
	if backup.BudgetCategories != nil {
		list = append(list, *backup.BudgetCategories)
	}

	err = update(ctx, s.db, "backup", func(tx *bolt.Tx) error {
		return errors.Join(
			putAll(tx, backup.Memories),
			putAll(tx, backup.Assets),
			putAll(tx, backup.BudgetItems),
			putAll(tx, []models.TimelinePreferences{prefs}),
			putAll(tx, salary),
			putAll(tx, schedule),
			putAll(tx, categories),
			putAll(tx, list),
		)
	})
	if err == nil && len(list) > 0 {
		models.UseBudgetCategories(list[0])
	}
	return err
}

// mergeCategorySettings folds imported categories and rules into the current
// ones the way the rest of an import merges: same ID overwrites in place,
// anything new is added after what is already here, keeping rule order.
func mergeCategorySettings(current, imported models.CategorySettings) models.CategorySettings {
	categories := make(map[string]int, len(current.Categories))
	for i, category := range current.Categories {
		categories[category.ID] = i
	}
	for _, category := range imported.Categories {
		if i, ok := categories[category.ID]; ok {
			current.Categories[i] = category
		} else {
			current.Categories = append(current.Categories, category)
		}
	}
	rules := make(map[string]int, len(current.Rules))
	for i, rule := range current.Rules {
		rules[rule.ID] = i
	}
	for _, rule := range imported.Rules {
		if i, ok := rules[rule.ID]; ok {
			current.Rules[i] = rule
		} else {
			current.Rules = append(current.Rules, rule)
		}
	}
	overrides := make(map[string]int, len(current.Overrides))
	for i, o := range current.Overrides {
		overrides[o.TransactionID] = i
	}
	for _, o := range imported.Overrides {
		if i, ok := overrides[o.TransactionID]; ok {
			current.Overrides[i] = o
		} else {
			current.Overrides = append(current.Overrides, o)
		}
	}
	return current
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
func (s *Store) SaveSynced(ctx context.Context, accounts []models.Account, transactions []models.Transaction, merchants []models.Merchant) error {
	return update(ctx, s.db, "synced", func(tx *bolt.Tx) error {
		return errors.Join(
			putAll(tx, accounts),
			putAll(tx, transactions),
			putAll(tx, merchants),
		)
	})
}

// StoredTransfers reports which of the given transactions are stored as a
// transfer, reading them all in one transaction. One not stored is not.
func (s *Store) StoredTransfers(ctx context.Context, ids []string) (map[string]bool, error) {
	transfers := map[string]bool{}
	if len(ids) == 0 {
		return transfers, nil
	}
	err := view(ctx, s.db, bucketOf[models.Transaction](), func(tx *bolt.Tx) error {
		b := tx.Bucket(buckets.TransactionBucket)
		if b == nil {
			return nil
		}
		for _, id := range ids {
			buf := b.Get([]byte(id))
			if buf == nil {
				continue
			}
			var stored models.Transaction
			if err := json.Unmarshal(buf, &stored); err != nil {
				return err
			}
			if stored.Type == "TRANSFER" {
				transfers[id] = true
			}
		}
		return nil
	})
	return transfers, err
}

/* Merchants */

func (s *Store) CreateMerchant(ctx context.Context, merchant models.Merchant) error {
	return Create[models.Merchant](ctx, s.db, merchant)
}

/* Transactions */

func (s *Store) CountTransactions(ctx context.Context) (int, error) {
	return Count[models.Transaction](ctx, s.db)
}
func (s *Store) CreateTransaction(ctx context.Context, transaction models.Transaction) error {
	return Create[models.Transaction](ctx, s.db, transaction)
}

// Category rules are applied as transactions are read rather than written
// back, so they reach every transaction already synced and removing one
// restores Akahu's category without a resync.

func (s *Store) categorised(ctx context.Context) func([]models.Transaction, error) ([]models.Transaction, error) {
	return func(txs []models.Transaction, err error) ([]models.Transaction, error) {
		if err != nil {
			return nil, err
		}
		settings, err := s.GetCategorySettings(ctx)
		if err != nil {
			return nil, err
		}
		settings.Apply(txs)
		return txs, nil
	}
}

// GetTransaction reads one transaction, categorised like every other read.
func (s *Store) GetTransaction(ctx context.Context, id string) (models.Transaction, error) {
	tx, err := Get[models.Transaction](ctx, s.db, []byte(id))
	if err != nil {
		return tx, err
	}
	txs, err := s.categorised(ctx)([]models.Transaction{tx}, nil)
	if err != nil {
		return tx, err
	}
	return txs[0], nil
}

func (s *Store) ReadTransactions(ctx context.Context) ([]models.Transaction, error) {
	return s.categorised(ctx)(Read[models.Transaction](ctx, s.db))
}
func (s *Store) ReadTransactionsByAccount(ctx context.Context, account string) ([]models.Transaction, error) {
	return s.categorised(ctx)(ReadFilter[models.Transaction](ctx, s.db, func(transaction models.Transaction) bool {
		return transaction.Account == account
	}))
}
func (s *Store) ReadTransactionsByDate(ctx context.Context, start time.Time, end time.Time) ([]models.Transaction, error) {
	return s.categorised(ctx)(ReadFilter[models.Transaction](ctx, s.db, func(transaction models.Transaction) bool {
		return transaction.Date.After(start) && transaction.Date.Before(end)
	}))
}
func (s *Store) SearchTransactions(ctx context.Context, search string, account string) ([]models.Transaction, error) {
	settings, err := s.GetCategorySettings(ctx)
	if err != nil {
		return nil, err
	}
	return s.categorised(ctx)(ReadFilter[models.Transaction](ctx, s.db, func(transaction models.Transaction) bool {
		// Searching by a rule's category should find what it filed.
		one := []models.Transaction{transaction}
		settings.Apply(one)
		transaction = one[0]

		accountMatch := account == "" || transaction.Account == account

		metadata := strings.Builder{}
		metadata.WriteString(transaction.Description)
		metadata.WriteString(transaction.Merchant.Name)
		// Budge's categories are what is shown, but Akahu's still say what a
		// shop is, so searching "bakeries" finds bakeries.
		for _, c := range transaction.Categories() {
			metadata.WriteString(c)
		}
		metadata.WriteString(transaction.Category.Name)
		metadata.WriteString(transaction.Category.Groups.PersonalFinance.Name)

		reg := regexp.MustCompile(`[^a-zA-Z]+`)
		needle := strings.ToLower(search)
		haystack := strings.ToLower(metadata.String())

		needle = reg.ReplaceAllString(needle, "")
		haystack = reg.ReplaceAllString(haystack, "")

		searchMatch := strings.Contains(haystack, needle)

		return accountMatch && searchMatch
	}))
}

/* Budget Items */

func (s *Store) GetBudgetItem(ctx context.Context, id string) (models.BudgetItem, error) {
	return Get[models.BudgetItem](ctx, s.db, []byte(id))
}
func (s *Store) CreateBudgetItem(ctx context.Context, item models.BudgetItem) error {
	return Create[models.BudgetItem](ctx, s.db, item)
}
func (s *Store) ReadBudgetItems(ctx context.Context) ([]models.BudgetItem, error) {
	return Read[models.BudgetItem](ctx, s.db)
}
func (s *Store) UpdateBudgetItem(ctx context.Context, item models.BudgetItem) error {
	return Update[models.BudgetItem](ctx, s.db, item)
}

/* Budget Salary */

// GetBudgetSalary reads the income settings. Unset means the usual starting
// point; settings that cannot be read are an error, rather than that starting
// point, which the next change would save over.
func (s *Store) GetBudgetSalary(ctx context.Context) (models.BudgetSalary, error) {
	salary, found, err := GetOptional[models.BudgetSalary](ctx, s.db, models.BudgetSalary{}.Key())
	if err != nil {
		return models.BudgetSalary{}, fmt.Errorf("reading income settings: %w", err)
	}
	if !found {
		return models.BudgetSalary{
			SalaryFrequency: "yearly",
			IncludePAYE:     true,
			KiwiSaverRate:   3.5,
		}, nil
	}
	return salary, nil
}
func (s *Store) SaveBudgetSalary(ctx context.Context, salary models.BudgetSalary) error {
	return Update[models.BudgetSalary](ctx, s.db, salary)
}

/* Schedule */

// GetScheduleSettings returns what runs by itself. An unset schedule is off:
// nothing should start reaching out to a bank without being asked to. One
// that cannot be read is an error, rather than off, which the next change
// would save over.
func (s *Store) GetScheduleSettings(ctx context.Context) (models.ScheduleSettings, error) {
	schedule, found, err := GetOptional[models.ScheduleSettings](ctx, s.db, models.ScheduleSettings{}.Key())
	if err != nil {
		return models.ScheduleSettings{}, fmt.Errorf("reading the schedule: %w", err)
	}
	if !found {
		return models.ScheduleSettings{
			AkahuInterval:  models.AkahuIntervals[1].Key,
			AssetsInterval: models.AssetIntervals[0].Key,
		}, nil
	}
	return schedule, nil
}

func (s *Store) SaveScheduleSettings(ctx context.Context, schedule models.ScheduleSettings) error {
	return Update[models.ScheduleSettings](ctx, s.db, schedule)
}

/* Settings */

// GetAkahuSettings reads the tokens and how far the last sync reached.
// Nothing saved yet means none, as on a fresh instance; a saved document that
// cannot be read is an error.
func (s *Store) GetAkahuSettings(ctx context.Context) (models.IntegrationAkahuSettings, error) {
	settings, _, err := GetOptional[models.IntegrationAkahuSettings](ctx, s.db, models.IntegrationAkahuSettings{}.Key())
	return settings, err
}
func (s *Store) UpdateAkahuSettings(ctx context.Context, settings models.IntegrationAkahuSettings) error {
	return Update[models.IntegrationAkahuSettings](ctx, s.db, settings)
}

// RecordAkahuSync notes a successful sync, and how fresh Akahu's data was at
// the time, which is where the next sync starts reading.
func (s *Store) RecordAkahuSync(ctx context.Context, dataThrough time.Time) error {
	settings, err := s.GetAkahuSettings(ctx)
	if err != nil {
		return err
	}
	settings.LastSync = time.Now()
	settings.DataThrough = dataThrough
	return Update[models.IntegrationAkahuSettings](ctx, s.db, settings)
}

// ResetAkahuLastSync forgets how far the synced data reaches, so the next
// sync reads the whole history again.
func (s *Store) ResetAkahuLastSync(ctx context.Context) error {
	settings, err := s.GetAkahuSettings(ctx)
	if err != nil {
		return err
	}
	settings.LastSync = time.Time{}
	settings.DataThrough = time.Time{}
	return Update[models.IntegrationAkahuSettings](ctx, s.db, settings)
}
func (s *Store) DeleteSynced(ctx context.Context) error {
	err := update(ctx, s.db, "synced", func(tx *bolt.Tx) error {
		accountErr := tx.DeleteBucket(buckets.AccountBucket)
		merchantErr := tx.DeleteBucket(buckets.MerchantBucket)
		transactionErr := tx.DeleteBucket(buckets.TransactionBucket)
		return errors.Join(accountErr, merchantErr, transactionErr)
	})
	if err != nil {
		return err
	}
	return update(ctx, s.db, "synced", func(tx *bolt.Tx) error {
		_, accountErr := tx.CreateBucket(buckets.AccountBucket)
		_, merchantErr := tx.CreateBucket(buckets.MerchantBucket)
		_, transactionErr := tx.CreateBucket(buckets.TransactionBucket)
		return errors.Join(accountErr, merchantErr, transactionErr)
	})
}

/* Categories */

// GetCategorySettings reads the rules and corrections. Nothing saved yet
// means none; a saved document that cannot be read is an error, rather than
// none, which the next change would save over.
func (s *Store) GetCategorySettings(ctx context.Context) (models.CategorySettings, error) {
	settings, _, err := GetOptional[models.CategorySettings](ctx, s.db, models.CategorySettings{}.Key())
	if err != nil {
		return models.CategorySettings{}, fmt.Errorf("reading category rules: %w", err)
	}
	return settings, nil
}

func (s *Store) SaveCategorySettings(ctx context.Context, settings models.CategorySettings) error {
	return Update[models.CategorySettings](ctx, s.db, settings)
}

// GetBudgetCategorySettings reads the owner's category list. Nothing saved
// yet means budge's own list; a saved list that cannot be read is an error,
// rather than budge's own list, which the next edit would save over.
func (s *Store) GetBudgetCategorySettings(ctx context.Context) (models.BudgetCategorySettings, error) {
	settings, _, err := GetOptional[models.BudgetCategorySettings](ctx, s.db, models.BudgetCategorySettings{}.Key())
	if err != nil {
		return models.BudgetCategorySettings{}, fmt.Errorf("reading the category list: %w", err)
	}
	return settings, nil
}

// SaveBudgetCategorySettings saves the owner's category list and puts it in
// use straight away.
func (s *Store) SaveBudgetCategorySettings(ctx context.Context, settings models.BudgetCategorySettings) error {
	if err := Update[models.BudgetCategorySettings](ctx, s.db, settings); err != nil {
		return err
	}
	models.UseBudgetCategories(settings)
	return nil
}
