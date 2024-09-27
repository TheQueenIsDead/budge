package models

import (
	"encoding/json"
	"fmt"
	"github.com/dustin/go-humanize"
	"github.com/scylladb/go-set/strset"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
	"regexp"
	"strings"
	"time"
)

type Account struct {
	Id          string `json:"_id"`
	Credentials string `json:"_credentials"`
	Connection  struct {
		Name string `json:"name"`
		Logo string `json:"logo"`
		Id   string `json:"_id"`
	} `json:"connection"`
	Name             string   `json:"name"`
	FormattedAccount string   `json:"formatted_account"`
	Status           string   `json:"status"`
	Type             string   `json:"type"`
	Attributes       []string `json:"attributes"`
	Balance          struct {
		Currency  string  `json:"currency"`
		Current   float64 `json:"current"`
		Available float64 `json:"available"`
		Overdrawn bool    `json:"overdrawn"`
	} `json:"balance"`
	Meta struct {
		Holder      string `json:"holder"`
		LoanDetails struct {
			Purpose  string `json:"purpose"`
			Type     string `json:"type"`
			Interest struct {
				Type string  `json:"type"`
				Rate float64 `json:"rate"`
			} `json:"interest"`
			IsInterestOnly bool `json:"is_interest_only"`
			Repayment      struct {
				Frequency  string    `json:"frequency"`
				NextAmount float64   `json:"next_amount"`
				NextDate   time.Time `json:"next_date,omitempty"`
			} `json:"repayment"`
		} `json:"loan_details,omitempty"`
	} `json:"meta"`
	Refreshed struct {
		Balance      time.Time `json:"balance"`
		Meta         time.Time `json:"meta"`
		Transactions time.Time `json:"transactions"`
		Party        time.Time `json:"party"`
	} `json:"refreshed"`
}

func (a *Account) Key() []byte {
	return []byte(a.Id)
}

func (a *Account) Value() ([]byte, error) {
	return json.Marshal(a)
}

type Merchant struct {
	Id      string `json:"_id"`
	Name    string `json:"name"`
	Logo    string `json:"logo"`
	Website string `json:"website"`
}

func (m *Merchant) Key() []byte {
	return []byte(m.Id)
}

func (m *Merchant) Value() ([]byte, error) {
	return json.Marshal(m)
}

type MerchantTotal struct {
	Merchant string
	Total    float64
}

type Transaction struct {
	Id         string `json:"_id"`
	Account    string `json:"_account"`
	Connection string `json:"_connection"`
	Category   struct {
		Id     string `json:"_id"`
		Name   string `json:"name"`
		Groups struct {
			PersonalFinance struct {
				Id   string `json:"_id"`
				Name string `json:"name"`
			} `json:"personal_finance"`
		} `json:"groups"`
	} `json:"category"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
	Date        time.Time `json:"date"`
	Description string    `json:"description"`
	Amount      float64   `json:"amount"`
	Balance     float64   `json:"balance"`
	Type        string    `json:"type"`
}

func (t *Transaction) Key() []byte {
	return []byte(t.Id)
}

func (t *Transaction) Value() ([]byte, error) {
	return json.Marshal(t)
}

func (t *Transaction) Merchant() string {

	// Try tidy up the memo into a passable name
	name := ""
	parts := strings.Split(t.Description, " ")
	dropParts := strset.New("POS", "W/D", ";")
	for _, part := range parts {
		if dropParts.Has(part) {
			continue
		}
		caser := cases.Title(language.English)
		name = fmt.Sprintf("%s %s", name, caser.String(part))
	}

	re := regexp.MustCompile(`-[0-9]{2}:[0-9]{2}`)
	name = re.ReplaceAllString(name, "")

	name = strings.Replace(name, ";", "", -1)

	if name != "" {
		return name
	}

	return t.Description

}

func (t *Transaction) String() string {
	return fmt.Sprintf("$%.2f", t.Amount)
}

func (t *Transaction) Float() float64 {
	return t.Amount
}

func (t *Transaction) Add(tx *Transaction) float64 {
	return t.Amount + tx.Amount
}

type Inventory struct {
	Id          string    `json:"_id"`
	CreatedAt   time.Time `json:"created_at"`
	Description string    `json:"description"`
	Cost        float64   `json:"amount"`
	Category    string    `json:"type"`
	Date        time.Time `json:"date"`
	Name        string    `json:"name"`
	Quantity    int       `json:"quantity"`

	// TODO: Upload media, like receipts
}

func (i *Inventory) Key() []byte {
	return []byte(i.Id)
}

func (i *Inventory) Value() ([]byte, error) {
	return json.Marshal(i)
}

func (i *Inventory) ISODateString() string {
	return i.Date.Format("2006-01-02")
}

func (i *Inventory) Purchased() string {
	return humanize.Time(i.Date)
}
