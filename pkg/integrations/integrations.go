package integrations

import (
	"github.com/TheQueenIsDead/budge/pkg/database"
	"github.com/TheQueenIsDead/budge/pkg/database/models"
	"github.com/TheQueenIsDead/budge/pkg/integrations/akahu"
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
	i.akahu = akahu.NewClient(
		akahu.WithUserToken(os.Getenv("AKAHU_USER_TOKEN")),
		akahu.WithApptoken(os.Getenv("AKAHU_APP_TOKEN")),
	)
}

func (i *Integrations) Config() map[string]interface{} {
	return map[string]interface{}{
		"akahu": i.akahu.Config(),
	}
}

func (i *Integrations) SyncAkahu() error {

	accounts, err := i.AkahuAccounts()
	if err != nil {
		return err
	}
	transactions, err := i.AkahuTransactions()
	if err != nil {
		return err
	}

	for _, account := range accounts.Items {
		_, err := i.store.Accounts.Put(account)
		if err != nil {
			return err
		}
	}

	var merchants []models.Merchant

	for _, transaction := range transactions.Items {

		tx := models.Transaction(transaction)
		_, err := i.store.Transactions.Put(transaction)
		if err != nil {
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
		_, err := i.store.Merchants.Put(merchant)
		if err != nil {
			return err
		}
	}

	return nil

}

func (i *Integrations) AkahuAccounts() (*akahu.AkahuAccounts, error) {
	return i.akahu.GetAccounts()
}

func (i *Integrations) AkahuTransactions() (*akahu.AkahuTransactions, error) {
	return i.akahu.GetTransactions()
}
