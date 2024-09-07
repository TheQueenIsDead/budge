package akahu

import "time"

type AkahuTransactions struct {
	Success bool `json:"success"`
	Items   []struct {
		Id          string    `json:"_id"`
		Account     string    `json:"_account"`
		Connection  string    `json:"_connection"`
		CreatedAt   time.Time `json:"created_at"`
		UpdatedAt   time.Time `json:"updated_at"`
		Date        time.Time `json:"date"`
		Description string    `json:"description"`
		Amount      float64   `json:"amount"`
		Balance     int       `json:"balance"`
		Type        string    `json:"type"`
	} `json:"items"`
	Cursor struct {
		Next string `json:"next"`
	} `json:"cursor"`
}

type AkahuMe struct {
	Success bool `json:"success"`
	Item    struct {
		Id              string    `json:"_id"`
		AccessGrantedAt time.Time `json:"access_granted_at"`
		Email           string    `json:"email"`
	} `json:"item"`
}

type AkahuAccounts struct {
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