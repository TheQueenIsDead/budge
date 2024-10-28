package akahu

type Response struct {
	Success bool `json:"success"`
	Cursor  struct {
		Next string `json:"next"`
	} `json:"cursor"`
}

type AccountsResponse struct {
	Response
	Items []Account `json:"items"`
}

type MeResponse struct {
	Response
	Item Me `json:"item"`
}

type TransactionsResponse struct {
	Response
	Items []Transaction `json:"items"`
}
