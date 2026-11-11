package integrations

import (
	"context"
	"log/slog"
	"math"
	"os"
	"strings"
	"time"

	"github.com/TheQueenIsDead/budge/pkg/database"
	"github.com/TheQueenIsDead/budge/pkg/database/models"
	"github.com/TheQueenIsDead/budge/pkg/integrations/akahu"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// syncOverlapDays is how far before Akahu's last known refresh each sync starts
// reading. A transaction can be dated some days before the bank posts it, so
// the window reaches back well past the refresh to catch those, and so both
// halves of a transfer made either side of it are seen together. Re-reading a
// fortnight costs a few hundred transactions, rewritten in one commit.
const syncOverlapDays = 14

// transferWindow is how far apart the two legs of a transfer can be dated.
const transferWindow = 24 * time.Hour

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
	ctx := context.Background()

	settings, err := i.store.GetAkahuSettings(ctx)
	if err != nil {
		slog.Warn("could not read the akahu settings, falling back to the environment", "error", err)
	}
	// Tokens from the environment stand in until some are saved, as on a
	// fresh instance, and are saved so settings shows them.
	if settings.UserToken == "" && settings.AppToken == "" {
		settings.UserToken = os.Getenv("AKAHU_USER_TOKEN")
		settings.AppToken = os.Getenv("AKAHU_APP_TOKEN")
		if settings.UserToken != "" || settings.AppToken != "" {
			if err := i.store.UpdateAkahuSettings(ctx, settings); err != nil {
				slog.Warn("could not save the akahu tokens from the environment", "error", err)
			}
		}
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
func (i *Integrations) SyncAkahu(ctx context.Context, dataThrough time.Time, progress func(fetched int)) (time.Time, error) {

	// Each phase is its own span, so a slow sync shows whether the time went
	// waiting on Akahu or writing it down.
	phase, span := tracer().Start(ctx, "fetch accounts")
	accounts, err := i.AkahuAccounts(phase)
	end(span, err, attribute.Int("akahu.accounts", len(accounts)))
	if err != nil {
		slog.ErrorContext(ctx, "akahu sync failed", "error", err)
		return dataThrough, err
	}

	since := dataThrough
	if !since.IsZero() {
		since = since.AddDate(0, 0, -syncOverlapDays)
	}
	phase, span = tracer().Start(ctx, "fetch transactions", trace.WithAttributes(attribute.String("akahu.since", since.Format(time.RFC3339))))
	transactions, err := i.akahu.GetTransactions(phase, since, true, progress)
	end(span, err, attribute.Int("akahu.transactions", len(transactions)))
	if err != nil {
		slog.ErrorContext(ctx, "akahu sync failed", "error", err)
		return dataThrough, err
	}

	_, span = tracer().Start(ctx, "classify transactions")

	transfers := identifyTransfers(transactions)
	if err := i.keepBoundaryTransfers(ctx, since, transactions, transfers); err != nil {
		end(span, err)
		slog.ErrorContext(ctx, "akahu sync failed", "error", err)
		return dataThrough, err
	}

	// Accounts hardly change, so just insert them and overwrite the key if need be.
	saved := make([]models.Account, len(accounts))
	for n, account := range accounts {
		saved[n] = models.Account(account)
	}

	var merchants []models.Merchant
	txs := make([]models.Transaction, 0, len(transactions))

	for _, transaction := range transactions {

		tx := fromAkahu(transaction)

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

	span.End()

	if err := i.store.SaveSynced(ctx, saved, txs, merchants); err != nil {
		slog.ErrorContext(ctx, "akahu sync failed", "error", err)
		return dataThrough, err
	}

	return freshness(accounts, transactions, dataThrough), nil
}

// tracer starts the sync's spans. It is fetched each time, so it follows
// telemetry being set up again.
func tracer() trace.Tracer {
	return otel.Tracer("github.com/TheQueenIsDead/budge/pkg/integrations")
}

// end finishes a phase's span with what it came to, and its error if any.
func end(span trace.Span, err error, attrs ...attribute.KeyValue) {
	span.SetAttributes(attrs...)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	}
	span.End()
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
	return math.Abs(timeDiff.Hours()) <= transferWindow.Hours()
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

// keepBoundaryTransfers stops an incremental sync downgrading a transfer it
// cannot see the whole of. The window starts at an arbitrary time of day, so
// one leg of a transfer can fall just before it and the other just after; the
// later leg then has no partner here and would be saved over as a debit or
// credit. A transaction dated within a day of the start of the window could
// have its partner outside it, so if it is stored as a transfer, it stays
// one. Feeding the stored transactions before the window into the pairing
// instead was the alternative, but a stored transfer has lost the type Akahu
// gave it, and is already paired with something that may not be loaded, so
// it could be paired a second time. A full resync sees both legs of
// everything, so it is still free to correct a transfer.
func (i *Integrations) keepBoundaryTransfers(ctx context.Context, since time.Time, transactions []akahu.Transaction, transfers map[string]bool) error {
	if since.IsZero() {
		return nil
	}
	boundary := since.Add(transferWindow)
	var unpaired []string
	for _, tx := range transactions {
		if !transfers[tx.Id] && tx.Date.Before(boundary) {
			unpaired = append(unpaired, tx.Id)
		}
	}
	stored, err := i.store.StoredTransfers(ctx, unpaired)
	if err != nil {
		return err
	}
	for id := range stored {
		transfers[id] = true
	}
	return nil
}

func (i *Integrations) PutAkahuSettings(ctx context.Context, settings models.IntegrationAkahuSettings) error {
	err := i.store.UpdateAkahuSettings(ctx, settings)
	if err != nil {
		return err
	}
	i.akahu.UserToken = settings.UserToken
	i.akahu.AppToken = settings.AppToken
	return nil
}

func (i *Integrations) AkahuAccounts(ctx context.Context) ([]akahu.Account, error) {
	return i.akahu.GetAccounts(ctx)
}

// fromAkahu copies a transaction as Akahu sent it. The model also carries the
// category budge files it under, worked out when it is read, so the two types
// no longer convert directly; their nested parts still do.
func fromAkahu(t akahu.Transaction) models.Transaction {
	return models.Transaction{
		Id:          t.Id,
		Account:     t.Account,
		Connection:  t.Connection,
		Category:    t.Category,
		Merchant:    t.Merchant,
		CreatedAt:   t.CreatedAt,
		UpdatedAt:   t.UpdatedAt,
		Date:        t.Date,
		Description: t.Description,
		Amount:      t.Amount,
		Balance:     t.Balance,
		Type:        t.Type,
		Meta:        t.Meta,
	}
}
