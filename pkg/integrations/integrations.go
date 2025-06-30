package integrations

import (
	"github.com/TheQueenIsDead/budge/pkg/database"
	"github.com/TheQueenIsDead/budge/pkg/database/models"
	"github.com/TheQueenIsDead/budge/pkg/integrations/akahu"
	"github.com/labstack/echo/v4"
	log "github.com/sirupsen/logrus"
	"math"
	"os"
	"strings"
	"time"
)

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

func (i *Integrations) SyncAkahu(c echo.Context, lastSync time.Time) error {

	accounts, err := i.AkahuAccounts()
	if err != nil {
		c.Logger().Error(err)
		return err
	}
	transactions, err := i.AkahuTransactions(lastSync)
	if err != nil {
		c.Logger().Error(err)
		return err
	}

	transfers := identifyTransfers(transactions)

	// Accounts hardly change, so just insert them and overwrite the key if need be.
	for _, account := range accounts {
		err := i.store.CreateAccount(models.Account(account))
		if err != nil {
			return err
		}
	}

	var merchants []models.Merchant

	for _, transaction := range transactions {

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
		err := i.store.CreateTransaction(tx)
		if err != nil {
			c.Logger().Error(err)
			return err
		}

		if transaction.Merchant.Id != "" {
			m := models.Merchant{
				Id:       tx.Merchant.Id,
				Name:     tx.Merchant.Name,
				Category: tx.Category.Groups.PersonalFinance.Name,
			}
			merchants = append(merchants, m)
		}

	}

	for _, merchant := range merchants {
		err = i.store.CreateMerchant(merchant)
		if err != nil {
			c.Logger().Error(err)
			return err
		}
	}

	return nil
}

func isMatchingTransfer(tx1, tx2 akahu.Transaction) bool {
	// Check if transactions are opposite (one positive, one negative)
	if (tx1.Amount > 0 && tx2.Amount > 0) || (tx1.Amount < 0 && tx2.Amount < 0) {
		return false
	}

	// Check if transactions are within 24 hours
	timeDiff := tx1.Date.Sub(tx2.Date)
	if math.Abs(timeDiff.Hours()) > 24 {
		return false
	}

	return true
}

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

		// Look for matching opposite transaction
		if matchingTxs, exists := potentialMatches[amount]; exists {
			for _, matchTx := range matchingTxs {
				// Check if transactions are within 24 hours of each other
				if isMatchingTransfer(tx, matchTx) {
					matchIds[matchTx.Id] = true
					matchIds[tx.Id] = true
				}
			}
		}

		// Add this transaction to potential matches
		potentialMatches[amount] = append(potentialMatches[amount], tx)
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

func (i *Integrations) AkahuTransactions(since time.Time) ([]akahu.Transaction, error) {
	return i.akahu.GetTransactions(since, true)
}
