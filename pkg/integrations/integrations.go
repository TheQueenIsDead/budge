package integrations

import (
	"github.com/TheQueenIsDead/budge/pkg/database"
	"github.com/TheQueenIsDead/budge/pkg/database/models"
	"github.com/TheQueenIsDead/budge/pkg/integrations/akahu"
	"github.com/labstack/echo/v4"
	log "github.com/sirupsen/logrus"
	"os"
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

func (i *Integrations) SyncAkahu(c echo.Context) error {

	accounts, err := i.AkahuAccounts()
	if err != nil {
		c.Logger().Error(err)
		return err
	}
	transactions, err := i.AkahuTransactions()
	if err != nil {
		c.Logger().Error(err)
		return err
	}

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

func (i *Integrations) AkahuTransactions() ([]akahu.Transaction, error) {
	return i.akahu.GetTransactions(true)
}
