package akahu

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

// requestTimeout bounds a single call to Akahu. Without one, a connection that
// stalls holds the sync open forever, and a scheduled sync never reports back.
const requestTimeout = 30 * time.Second

// maxBody caps how much of one response is read. A page of transactions is
// a few hundred kilobytes; anything near this is not one, and reading it
// whole would only use up memory.
const maxBody = 32 << 20

// maxPages caps how many pages one read of transactions follows. A full
// history is dozens; a cursor that never runs out is a fault on Akahu's side
// and must not keep a sync going forever.
const maxPages = 10000

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

func (a *AkahuClient) get(ctx context.Context, path string, query map[string]string) (*http.Response, error) {

	header := http.Header{}
	header.Set("Content-Type", "application/json")
	header.Set("Accept", "application/json")
	header.Set("Authorization", fmt.Sprintf("Bearer %s", a.UserToken))
	header.Set("X-Akahu-Id", a.AppToken)

	url, err := url.Parse(a.BaseURL + path)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header = header

	if len(query) > 0 {
		q := req.URL.Query()
		for k, v := range query {
			q.Add(k, v)
		}
		req.URL.RawQuery = q.Encode()
	}

	// Made for each call, so the trace it joins comes from the telemetry
	// running now rather than whatever was set up first.
	client := &http.Client{Timeout: requestTimeout, Transport: otelhttp.NewTransport(http.DefaultTransport)}
	res, err := client.Do(req)
	if err != nil {
		return nil, err
	}

	// An error from Akahu, such as an expired token or a rate limit, still
	// comes back as JSON. Decoded as a page of results it reads as an empty
	// one, and the sync reports success having fetched nothing.
	if res.StatusCode != http.StatusOK {
		defer func() { _ = res.Body.Close() }()
		body, _ := io.ReadAll(io.LimitReader(res.Body, 1024))
		var failure struct {
			Message string `json:"message"`
		}
		_ = json.Unmarshal(body, &failure)
		if failure.Message == "" {
			failure.Message = http.StatusText(res.StatusCode)
		}
		return nil, fmt.Errorf("akahu %s returned %d: %s", path, res.StatusCode, failure.Message)
	}
	return res, nil
}

// GetTransactions reads transactions since a time, a page at a time. Akahu
// pages are small, so a full history is dozens of requests in a row; onPage,
// when given, is told the running total after each one so a caller can show
// progress.
func (a *AkahuClient) GetTransactions(ctx context.Context, since time.Time, paginate bool, onPage func(fetched int)) (items []Transaction, err error) {

	var query = make(map[string]string)

	if !since.IsZero() {
		query["start"] = since.Format(time.RFC3339)
	}

	seen := map[string]bool{}
	for page := 0; ; page++ {
		if page == maxPages {
			return nil, fmt.Errorf("akahu /transactions: gave up after %d pages", maxPages)
		}

		var tr *TransactionsResponse
		if err := a.decode(ctx, "/transactions", query, &tr); err != nil {
			return nil, err
		}
		if tr == nil {
			return nil, fmt.Errorf("akahu /transactions: empty response")
		}

		items = append(items, tr.Items...)
		if onPage != nil {
			onPage(len(items))
		}

		next := tr.Cursor.Next
		if !paginate || next == "" {
			break
		}
		// A cursor that comes round again would read the same pages forever.
		if seen[next] {
			return nil, fmt.Errorf("akahu /transactions: cursor %q repeated", next)
		}
		seen[next] = true
		query["cursor"] = next
	}

	return items, nil
}

// TODO: get transactions for a specific account
// TODO: Add support for pagination
// TIP: To access subsequent pages, simply take the cursor.next value from each response and make a new request, supplying this value using the cursor query parameter. In response, you will receive the next page of results, along with a new cursor.next value.

func (a *AkahuClient) GetAccounts(ctx context.Context) ([]Account, error) {
	var accounts *AccountsResponse
	if err := a.decode(ctx, "/accounts", nil, &accounts); err != nil {
		return nil, err
	}
	if accounts == nil {
		return nil, fmt.Errorf("akahu /accounts: empty response")
	}
	return accounts.Items, nil
}

func (a *AkahuClient) Me(ctx context.Context) (*Me, error) {
	var me *MeResponse
	if err := a.decode(ctx, "/me", nil, &me); err != nil {
		return nil, err
	}
	if me == nil {
		return nil, fmt.Errorf("akahu /me: empty response")
	}
	return &me.Item, nil
}

// decode fetches a path and reads its JSON into v. The body is always
// closed, so the connection goes back to be reused rather than leaking, and
// is read no further than maxBody.
func (a *AkahuClient) decode(ctx context.Context, path string, query map[string]string, v any) error {
	res, err := a.get(ctx, path, query)
	if err != nil {
		return err
	}
	defer func() { _ = res.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(res.Body, maxBody+1))
	if err != nil {
		return fmt.Errorf("akahu %s: reading the response: %w", path, err)
	}
	if len(body) > maxBody {
		return fmt.Errorf("akahu %s: response larger than %d bytes", path, maxBody)
	}
	if err := json.Unmarshal(body, v); err != nil {
		return fmt.Errorf("akahu %s: %w", path, err)
	}
	return nil
}
