package akahu

import (
	"encoding/json"
	"io"
	"time"
)

type Accounts struct {
	Success bool `json:"success"`
	Items   []struct {
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
	} `json:"items"`
}

func (a *AkahuClient) Accounts() *Accounts {
	res, err := a.Get("/accounts")
	if err != nil {
		return nil
	}

	body, _ := io.ReadAll(res.Body)
	var accounts *Accounts
	json.Unmarshal(body, &accounts)
	return accounts

}
