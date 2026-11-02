package models

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/TheQueenIsDead/budge/pkg/database/buckets"
	"github.com/dustin/go-humanize"
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

func (a Account) Key() []byte {
	return []byte(a.Id)
}

func (a Account) Bucket() []byte {
	return buckets.AccountBucket
}

func (a *Account) Value() ([]byte, error) {
	return json.Marshal(a)
}

type Merchant struct {
	Id       string   `json:"id"`
	Category string   `json:"category"`
	Name     string   `json:"name"`
	Aliases  []string `json:"aliases"`
}

func (m Merchant) Key() []byte {
	return []byte(m.Id)
}

func (m Merchant) Bucket() []byte {
	return buckets.MerchantBucket
}

func (m *Merchant) Value() ([]byte, error) {
	return json.Marshal(m)
}

type MerchantTotal struct {
	Merchant      string
	Total         float64
	PreviousTotal float64
	Delta         float64
}

type MerchantFrequency struct {
	Merchant string
	Count    int
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
	Merchant struct {
		Id      string `json:"_id"`
		Name    string `json:"name"`
		Website string `json:"website"`
	} `json:"merchant"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
	Date        time.Time `json:"date"`
	Description string    `json:"description"`
	Amount      float64   `json:"amount"`
	Balance     float64   `json:"balance"`
	Type        string    `json:"type"`
	Meta        struct {
		// Conversion is only present on purchases made in a foreign currency,
		// which makes it the most reliable sign of where money was spent.
		Conversion struct {
			Amount   float64 `json:"amount"`
			Currency string  `json:"currency"`
			Rate     float64 `json:"rate"`
		} `json:"conversion"`
	} `json:"meta"`

	// Filed is the budge category the transaction is filed under, and FiledBy
	// what filed it: FiledByOwner, FiledByRule, or empty for Akahu. Both are
	// worked out each time the transaction is read and never stored, so Akahu's
	// own category above is kept as it came, and changing a rule refiles
	// everything at once.
	Filed   string `json:"-"`
	FiledBy string `json:"-"`
}

func (t Transaction) Key() []byte {
	return []byte(t.Id)
}

func (t Transaction) Bucket() []byte {
	return buckets.TransactionBucket
}

func (t *Transaction) Value() ([]byte, error) {
	return json.Marshal(t)
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

// Categories names where the transaction is filed, for its badges: the
// category, then the subcategory when it has one. Money coming in and moving
// between the owner's accounts is not filed anywhere.
func (t *Transaction) Categories() (labels []string) {
	if t.Amount >= 0 || t.Type == "TRANSFER" {
		return nil
	}
	key := t.BudgetCategory()
	top := TopBudgetCategory(key)
	labels = append(labels, top.Label)
	if category, ok := BudgetCategoryByKey(key); ok && category.IsSub() {
		labels = append(labels, category.Label)
	}
	return labels
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

func (i Inventory) Key() []byte {
	return []byte(i.Id)
}

func (i Inventory) Bucket() []byte {
	return buckets.InventoryBucket
}

func (i *Inventory) Value() ([]byte, error) {
	return json.Marshal(i)
}

func (i *Inventory) ISODateString() string {
	return i.Date.Format("2006-01-02")
}

func (i *Inventory) Purchased() string {
	if i.Date.Format(time.DateOnly) == time.Now().Format(time.DateOnly) {
		return "today"
	}
	return humanize.Time(i.Date)
}
