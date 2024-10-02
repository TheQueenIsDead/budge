package integrations

import (
	"fmt"
	"github.com/TheQueenIsDead/budge/pkg/database"
	"github.com/TheQueenIsDead/budge/pkg/database/models"
	"github.com/TheQueenIsDead/budge/pkg/integrations/akahu"
	"github.com/labstack/echo/v4"
	"github.com/scylladb/go-set/strset"
	log "github.com/sirupsen/logrus"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
	"os"
	"regexp"
	"strings"
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

func sanitise(merchant string) string {

	// Filter  out words
	name := ""
	parts := strings.Split(merchant, " ")
	dropParts := strset.New("POS", "W/D", ";", "-", " ", "ATM", "TO", "FROM", "PAY")
	for _, part := range parts {
		if dropParts.Has(strings.ToUpper(part)) {
			continue
		}
		caser := cases.Title(language.English)
		// If the word is all caps, then preserve case, else Title it.
		if part != strings.ToUpper(part) {
			part = caser.String(part)
		}
		name = fmt.Sprintf("%s %s", name, part)
	}

	var re *regexp.Regexp
	expressions := []*regexp.Regexp{
		// Remove timecodes such as '-17:35'
		regexp.MustCompile(`-[0-9]{2}:[0-9]{2}`),
		// Remove autopay identifiers
		regexp.MustCompile(`(?i)AP#[0-9]{8}`),
		// Remove 'Direct Debit' information
		regexp.MustCompile(`(?i)Direct Debit`),
		regexp.MustCompile(`(?i)Transfer From`),
		regexp.MustCompile(`(?i)Bill Payment`),
		regexp.MustCompile(`(?i)Automatic Payment`),
	}
	for _, expression := range expressions {
		re = expression
		name = re.ReplaceAllString(name, "")
	}

	// Final tid-bits that might not have been removed via regex
	// TODO: Should this be non-alpha characters?
	name = strings.Replace(name, ";", "", -1)

	// Trim off whitespace
	name = strings.TrimSpace(name)

	if name != "" {
		return name
	}
	return merchant
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
		//tx.Description = sanitise(transaction.Description)
		err := i.store.CreateTransaction(tx)
		if err != nil {
			c.Logger().Error(err)
			return err
		}
		m := models.Merchant{
			//TODO: Using the TX id here isn't super smart, will lead to dupes
			Id:       tx.Id,
			Name:     tx.Description,
			Category: tx.Category.Groups.PersonalFinance.Name,
			Logo:     "",
			Website:  "",
			Aliases: []string{
				sanitise(tx.Description),
			},
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
