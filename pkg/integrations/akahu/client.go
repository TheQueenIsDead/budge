package akahu

import (
	"encoding/json"
	"fmt"
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
