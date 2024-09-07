package akahu

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
)

type AkahuClient struct {
	BaseURL   string `json: json:"baseURL"`
	UserToken string `json: json:"userToken"`
	AppToken  string `json: json:"appToken"`
}

type AkahuOption func(*AkahuClient)

func (a *AkahuClient) Config() map[string]interface{} {
	var config map[string]interface{}
	bytes, _ := json.Marshal(a)
	json.Unmarshal(bytes, &config)
	return config
}

func NewClient(options ...func(client *AkahuClient)) *AkahuClient {
	client := &AkahuClient{
		BaseURL: "https://api.akahu.io/v1",
	}
	for _, option := range options {
		option(client)
	}
	return client
}

func WithBaseURL(baseURL string) AkahuOption {
	return func(client *AkahuClient) {
		client.BaseURL = baseURL
	}
}

func WithUserToken(token string) AkahuOption {
	return func(client *AkahuClient) {
		client.UserToken = token
	}
}

func WithApptoken(token string) AkahuOption {
	return func(client *AkahuClient) {
		client.AppToken = token
	}
}

func (a *AkahuClient) Get(path string) (*http.Response, error) {

	header := http.Header{}
	header.Set("Content-Type", "application/json")
	header.Set("Accept", "application/json")
	header.Set("Authorization", fmt.Sprintf("Bearer %s", a.UserToken))
	header.Set("X-Akahu-Id", a.AppToken)

	url, err := url.Parse(a.BaseURL + path)
	if err != nil {
		return nil, err
	}

	client := http.Client{}
	return client.Do(&http.Request{
		Method: http.MethodGet,
		URL:    url,
		Header: header,
	})
}

// TODO: Paginate all transactions
func (a *AkahuClient) GetTransactions() *AkahuTransactions {
	res, err := a.Get("/transactions")
	if err != nil {
		return nil
	}

	body, _ := io.ReadAll(res.Body)
	var transactions *AkahuTransactions
	json.Unmarshal(body, &transactions)
	//fmt.Println(string(body))
	return transactions

}

// TODO: Get transactions for a specific account
// TODO: Add support for pagination
// TIP: To access subsequent pages, simply take the cursor.next value from each response and make a new request, supplying this value using the cursor query parameter. In response, you will receive the next page of results, along with a new cursor.next value.

func (a *AkahuClient) GetAccounts() *AkahuAccounts {
	res, err := a.Get("/accounts")
	if err != nil {
		return nil
	}

	body, _ := io.ReadAll(res.Body)
	var accounts *AkahuAccounts
	json.Unmarshal(body, &accounts)
	return accounts

}

func (a *AkahuClient) Me() {
	res, err := a.Get("/me")
	if err != nil {
		return
	}

	body, _ := io.ReadAll(res.Body)
	var me *AkahuMe
	json.Unmarshal(body, &me)
	fmt.Println(me) // TODO: Actually return something
}
