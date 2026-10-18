package integrations

import (
	"math"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/TheQueenIsDead/budge/pkg/database"
	"github.com/TheQueenIsDead/budge/pkg/database/models"
	"github.com/TheQueenIsDead/budge/pkg/integrations/akahu"
	"github.com/labstack/echo/v4"
	log "github.com/sirupsen/logrus"
)

// syncOverlapDays is how far before Akahu's last known refresh each sync starts
// reading. A transaction can be dated some days before the bank posts it, so
// the window reaches back well past the refresh to catch those, and so both
// halves of a transfer made either side of it are seen together. Re-reading a
// fortnight costs a few hundred transactions, rewritten in one commit.
const syncOverlapDays = 14

type Integrations struct {
	store *database.Store
	akahu *akahu.AkahuClient
}

type Integration interface {
	Config() map[string]string
	Sync() error
}

func NewIntegrations(store *database.Store) *Integrations {

	i := &Integrations{
		store: store,
	}
	i.RegisterAkahu()

	return i
}

func (i *Integrations) RegisterAkahu() {

	settings, err := i.store.GetAkahuSettings()
	if err != nil {
		log.Error("Could not retrieve akahu settings, falling back to ENV")
		settings.UserToken = os.Getenv("AKAHU_USER_TOKEN")
		settings.AppToken = os.Getenv("AKAHU_APP_TOKEN")
		_ = i.store.UpdateAkahuSettings(settings)
	}
	i.akahu = akahu.NewClient(
		akahu.WithUserToken(settings.UserToken),
		akahu.WithApptoken(settings.AppToken),
	)
}

func (i *Integrations) Config() map[string]interface{} {
	return map[string]interface{}{
		"akahu": i.akahu.Config(),
	}
}

// SyncAkahu pulls accounts and transactions. It takes a logger rather than a
// request context so a scheduled run, which has no request behind it, can call it.
// dataThrough is how fresh Akahu's data was at the last sync, and zero reads the
// whole history. progress, which may be nil, is told how many transactions have
// been fetched as each page arrives. It returns how fresh Akahu's data is now,
// for the next sync to start from.
func (i *Integrations) SyncAkahu(logger echo.Logger, dataThrough time.Time, progress func(fetched int)) (time.Time, error) {

	accounts, err := i.AkahuAccounts()
	if err != nil {
		logger.Error(err)
		return dataThrough, err
	}

	// An account switched back on lost its history when it was switched off,
	// so while one is on its way back the whole history is read again.
	removed, err := i.store.GetRemovedAccounts()
	if err != nil {
		logger.Error(err)
		return dataThrough, err
	}

	since := dataThrough
	if removed.Restoring() {
		since = time.Time{}
	}
	if !since.IsZero() {
		since = since.AddDate(0, 0, -syncOverlapDays)
	}
	transactions, err := i.akahu.GetTransactions(since, true, progress)
	if err != nil {
		logger.Error(err)
		return dataThrough, err
	}

	// Transfers are paired across every account before the untracked ones are
	// dropped, so moving money into an account the owner switched off still
	// reads as a transfer rather than spending.
	transfers := identifyTransfers(transactions)

	untracked := removed.IDs()
	accounts = slices.DeleteFunc(accounts, func(a akahu.Account) bool { return untracked[a.Id] })

	// Accounts hardly change, so just insert them and overwrite the key if need be.
	saved := make([]models.Account, len(accounts))
	for n, account := range accounts {
		saved[n] = models.Account(account)
	}

	var merchants []models.Merchant
	txs := make([]models.Transaction, 0, len(transactions))

	for _, transaction := range transactions {

		if untracked[transaction.Account] {
			continue
		}

		tx := models.Transaction(transaction)

		if _, ok := transfers[tx.Id]; ok {
			tx.Type = "TRANSFER"
		}

		// Attempt to detect salary before persisting
		if tx.Merchant.Id == "" {
			desc := strings.ToLower(tx.Description)
			if strings.Contains(desc, "salary") {
				tx.Merchant.Name = "Salary"
				tx.Category.Name = "Salary"
				tx.Category.Groups.PersonalFinance.Name = "Salary"
			}
		}

		if tx.Type == "TRANSFER" {
			tx.Merchant.Name = "Transfer"
			tx.Category.Name = "Transfer"
			tx.Category.Groups.PersonalFinance.Name = "Transfer"
		}

		// We get given a transaction id from Akahu, which helps us to maintain unique records, overwrite if need be.
		txs = append(txs, tx)

		if transaction.Merchant.Id != "" {
			m := models.Merchant{
				Id:       tx.Merchant.Id,
				Name:     tx.Merchant.Name,
				Category: tx.Category.Groups.PersonalFinance.Name,
			}
			merchants = append(merchants, m)
		}

	}

	if err := i.store.SaveSynced(saved, txs, merchants); err != nil {
		logger.Error(err)
		return dataThrough, err
	}
	if err := i.store.FinishRestoring(); err != nil {
		logger.Error(err)
		return dataThrough, err
	}

	return freshness(accounts, transactions, dataThrough), nil
}

// isMatchingTransfer reports whether two transactions look like the two legs
// of money moved between the owner's own accounts: equal and opposite, a day
// apart at most, and in different accounts. The last condition matters most.
// Topping up an everyday account from savings and then paying a tradesperson
// the same amount from it is an equal and opposite pair inside one account,
// and without it the payment for a new roof was filed away as a transfer.
// freshness is how far Akahu's data can be trusted to reach: the oldest of the
// times it last refreshed each open account's transactions from the bank. The
// oldest, because the next sync must reach back far enough for the account
// that is furthest behind. When Akahu reports no refresh times, the newest
// transaction date seen stands in, and failing that the previous mark stays;
// the wall clock is never used, since it says nothing about what Akahu has.
func freshness(accounts []akahu.Account, transactions []akahu.Transaction, previous time.Time) time.Time {
	var oldest time.Time
	for _, account := range accounts {
		refreshed := account.Refreshed.Transactions
		if account.Status == "INACTIVE" || refreshed.IsZero() {
			continue
		}
		if oldest.IsZero() || refreshed.Before(oldest) {
			oldest = refreshed
		}
	}
	if !oldest.IsZero() {
		return oldest
	}

	newest := previous
	for _, tx := range transactions {
		if tx.Date.After(newest) {
			newest = tx.Date
		}
	}
	return newest
}

func isMatchingTransfer(tx1, tx2 akahu.Transaction) bool {
	if tx1.Account == tx2.Account {
		return false
	}

	// Check if transactions are opposite (one positive, one negative)
	if (tx1.Amount > 0 && tx2.Amount > 0) || (tx1.Amount < 0 && tx2.Amount < 0) {
		return false
	}

	// Check if transactions are within 24 hours
	timeDiff := tx1.Date.Sub(tx2.Date)
	return math.Abs(timeDiff.Hours()) <= 24
}

// identifyTransfers pairs up transfers between the owner's accounts. Each
// transaction belongs to one pair at most: matching every candidate of the
// same amount let one real transfer sweep in a third, unrelated payment.
func identifyTransfers(transactions []akahu.Transaction) map[string]bool {

	// Create a map to track potential matching transfers by amount
	potentialMatches := make(map[float64][]akahu.Transaction)
	matchIds := map[string]bool{}

	for _, tx := range transactions {
		amount := math.Abs(tx.Amount) // Use absolute value for matching

		// Check if this might be a transfer based on description patterns
		tryClassify := tx.Type == "CREDIT" || tx.Type == "DEBIT" || tx.Type == "PAYMENT" || tx.Type == "STANDING ORDER"
		if !tryClassify {
			continue
		}

		// Look for an opposite transaction not already paired with another.
		matched := false
		for _, matchTx := range potentialMatches[amount] {
			if !matchIds[matchTx.Id] && isMatchingTransfer(tx, matchTx) {
				matchIds[matchTx.Id] = true
				matchIds[tx.Id] = true
				matched = true
				break
			}
		}

		// Add this transaction to potential matches
		if !matched {
			potentialMatches[amount] = append(potentialMatches[amount], tx)
		}
	}

	return matchIds

}

func (i *Integrations) PutAkahuSettings(settings models.IntegrationAkahuSettings) error {
	err := i.store.UpdateAkahuSettings(settings)
	if err != nil {
		return err
	}
	i.akahu.UserToken = settings.UserToken
	i.akahu.AppToken = settings.AppToken
	return nil
}

func (i *Integrations) AkahuAccounts() ([]akahu.Account, error) {
	return i.akahu.GetAccounts()
}
