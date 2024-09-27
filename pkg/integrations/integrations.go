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

	for _, account := range accounts.Items {
		err := i.store.CreateAccount(account)
		if err != nil {
			return err
		}
	}

	var merchants []models.Merchant

	for _, transaction := range transactions.Items {

		tx := models.Transaction(transaction)
		err := i.store.CreateTransaction(transaction)
		if err != nil {
			c.Logger().Error(err)
			return err
		}
		m := models.Merchant{
			Id:      tx.Id,
			Name:    tx.Merchant(),
			Logo:    "",
			Website: "",
		}
		merchants = append(merchants, m)
	}

	for _, merchant := range merchants {
		err := i.store.CreateMerchant(merchant)
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

func (i *Integrations) AkahuAccounts() (*akahu.AkahuAccounts, error) {
	return i.akahu.GetAccounts()
}

func (i *Integrations) AkahuTransactions() (*akahu.AkahuTransactions, error) {
	return i.akahu.GetTransactions()
}
