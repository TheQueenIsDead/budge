package akahu

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

type AkahuClient struct {
	BaseURL   string `json:"baseURL"`
	UserToken string `json:"userToken"`
	AppToken  string `json:"appToken"`
}

type AkahuOption func(*AkahuClient)

func (a *AkahuClient) Config() map[string]interface{} {
	var config map[string]interface{}
	bytes, _ := json.Marshal(a)
	err := json.Unmarshal(bytes, &config)
	if err != nil {
		return nil
	}
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

func (a *AkahuClient) get(path string, query map[string]string) (*http.Response, error) {

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
	req := &http.Request{
		Method: http.MethodGet,
		URL:    url,
		Header: header,
	}

	if len(query) > 0 {
		q := req.URL.Query()
		for k, v := range query {
			q.Add(k, v)
		}
		req.URL.RawQuery = q.Encode()
	}

	return client.Do(req)
}

func (a *AkahuClient) GetTransactions(since time.Time, paginate bool) (items []Transaction, err error) {

	var query = make(map[string]string)

	if !since.IsZero() {
		query["start"] = since.Format(time.RFC3339)
	}

	for {
		res, httpErr := a.get("/transactions", query)

		if httpErr != nil {
			err = httpErr
			break
		}

		body, _ := io.ReadAll(res.Body)
		var tr *TransactionsResponse
		err = json.Unmarshal(body, &tr)
		if err != nil {
			return nil, err
		}

		items = append(items, tr.Items...)

		if paginate && tr.Cursor.Next != "" {
			query["cursor"] = tr.Cursor.Next
		} else {
			break
		}
	}

	return
}

// TODO: get transactions for a specific account
// TODO: Add support for pagination
// TIP: To access subsequent pages, simply take the cursor.next value from each response and make a new request, supplying this value using the cursor query parameter. In response, you will receive the next page of results, along with a new cursor.next value.

func (a *AkahuClient) GetAccounts() ([]Account, error) {
	res, err := a.get("/accounts", nil)
	if err != nil {
		return nil, err
	}

	body, _ := io.ReadAll(res.Body)
	var accounts *AccountsResponse
	err = json.Unmarshal(body, &accounts)
	if err != nil {
		return nil, err
	}
	return accounts.Items, nil

}

func (a *AkahuClient) Me() (*Me, error) {
	res, err := a.get("/me", nil)
	if err != nil {
		return nil, err
	}

	body, _ := io.ReadAll(res.Body)
	var me *MeResponse
	err = json.Unmarshal(body, &me)
	if err != nil {
		return nil, err
	}

	return &me.Item, nil
}
