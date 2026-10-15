// Package enablenow is a small client for the enablenow PSD2 account
// information and categorization APIs (https://docs.enablenow.nl).
package enablenow

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// Client is everything the issuer needs from enablenow. It is an interface so
// the flow can be tested against a fake and run locally without credentials.
type Client interface {
	// ConsentURL returns the AppScreen URL the end-user is sent to in order to
	// connect a bank account. extraParams are forwarded back to redirectURI.
	ConsentURL(redirectURI string, extraParams url.Values) string
	GetConsent(ctx context.Context, consentId string) (*Consent, error)
	DeleteConsent(ctx context.Context, consentId string) error
	ListAccounts(ctx context.Context, consentId string) ([]Account, error)
	// ListTransactions returns all transactions of the consent booked in
	// [from, to), following pagination.
	ListTransactions(ctx context.Context, consentId string, from, to time.Time) ([]Transaction, error)
	ListCategorizationJobs(ctx context.Context, consentId string) ([]Job, error)
	CreateCategorizationJob(ctx context.Context, req JobRequest) (*Job, error)
	GetCategorizationJob(ctx context.Context, jobId string) (*Job, error)
}

type Config struct {
	ClientId     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
	// ApiUrl defaults to https://api.enablenow.nl
	ApiUrl string `json:"api_url,omitempty"`
	// AppUrl is the AppScreen base URL, defaults to https://app.enablenow.nl/
	AppUrl string `json:"app_url,omitempty"`
	// TokenUrl defaults to <api_url>/connect/token
	TokenUrl string `json:"token_url,omitempty"`
	// Scope defaults to customer-api
	Scope string `json:"scope,omitempty"`
}

const (
	defaultApiUrl = "https://api.enablenow.nl"
	defaultAppUrl = "https://app.enablenow.nl/"
	defaultScope  = "customer-api"
	// maxPageSize is the largest page size the transactions endpoint accepts.
	maxPageSize = 1000
	// maxPages bounds pagination so a misbehaving API can't loop us forever.
	maxPages = 100
	// maxResponseBytes caps how much of a response body is read.
	maxResponseBytes = 32 << 20
)

// ApiError is returned for non-2xx responses.
type ApiError struct {
	Method     string
	Path       string
	StatusCode int
	Body       string
}

func (e *ApiError) Error() string {
	return fmt.Sprintf("enablenow %s %s returned %d: %s", e.Method, e.Path, e.StatusCode, e.Body)
}

// IsNotFound reports whether err is an enablenow 404.
func IsNotFound(err error) bool {
	var apiErr *ApiError
	return errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound
}

type HttpClient struct {
	config     Config
	httpClient *http.Client

	tokenMutex  sync.Mutex
	accessToken string
	tokenExpiry time.Time
}

func NewHttpClient(config Config) (*HttpClient, error) {
	if config.ClientId == "" || config.ClientSecret == "" {
		return nil, errors.New("enablenow client_id and client_secret must be set")
	}
	if config.ApiUrl == "" {
		config.ApiUrl = defaultApiUrl
	}
	config.ApiUrl = strings.TrimRight(config.ApiUrl, "/")
	if config.AppUrl == "" {
		config.AppUrl = defaultAppUrl
	}
	if config.TokenUrl == "" {
		config.TokenUrl = config.ApiUrl + "/connect/token"
	}
	if config.Scope == "" {
		config.Scope = defaultScope
	}
	return &HttpClient{
		config:     config,
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}, nil
}

func (c *HttpClient) ConsentURL(redirectURI string, extraParams url.Values) string {
	params := url.Values{}
	maps.Copy(params, extraParams)
	params.Set("client", c.config.ClientId)
	params.Set("redirect_uri", redirectURI)
	return c.config.AppUrl + "?" + params.Encode()
}

type tokenResponse struct {
	AccessToken string `json:"access_token"`
	ExpiresIn   int    `json:"expires_in"`
}

// token returns a cached client-credentials access token, refreshing it a
// minute before it expires.
func (c *HttpClient) token(ctx context.Context) (string, error) {
	c.tokenMutex.Lock()
	defer c.tokenMutex.Unlock()

	if c.accessToken != "" && time.Now().Before(c.tokenExpiry) {
		return c.accessToken, nil
	}

	form := url.Values{
		"client_id":     {c.config.ClientId},
		"client_secret": {c.config.ClientSecret},
		"scope":         {c.config.Scope},
		"grant_type":    {"client_credentials"},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.config.TokenUrl, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to request enablenow access token: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", &ApiError{Method: http.MethodPost, Path: "/connect/token", StatusCode: resp.StatusCode, Body: truncate(string(body))}
	}

	var token tokenResponse
	if err := json.Unmarshal(body, &token); err != nil {
		return "", fmt.Errorf("failed to parse enablenow token response: %w", err)
	}
	if token.AccessToken == "" {
		return "", errors.New("enablenow token response contained no access_token")
	}
	expiresIn := time.Duration(token.ExpiresIn) * time.Second
	if expiresIn <= 0 {
		expiresIn = 60 * time.Minute
	}
	c.accessToken = token.AccessToken
	c.tokenExpiry = time.Now().Add(expiresIn - time.Minute)
	return c.accessToken, nil
}

// do performs an authenticated API call and decodes the response into out.
// Responses wrapped in {"data": ...} are unwrapped transparently.
func (c *HttpClient) do(ctx context.Context, method, path string, query url.Values, in, out any) error {
	respBody, err := c.request(ctx, method, path, query, in)
	if err != nil {
		return err
	}
	if out == nil || len(bytes.TrimSpace(respBody)) == 0 {
		return nil
	}
	return decodeData(respBody, out)
}

// request performs an authenticated API call and returns the raw body of a
// successful (2xx) response.
func (c *HttpClient) request(ctx context.Context, method, path string, query url.Values, in any) ([]byte, error) {
	token, err := c.token(ctx)
	if err != nil {
		return nil, err
	}

	var body io.Reader
	if in != nil {
		payload, err := json.Marshal(in)
		if err != nil {
			return nil, err
		}
		body = bytes.NewReader(payload)
	}

	fullUrl := c.config.ApiUrl + path
	if len(query) > 0 {
		fullUrl += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, fullUrl, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("enablenow %s %s failed: %w", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return nil, err
	}

	if resp.StatusCode == http.StatusUnauthorized {
		// force a fresh token on the next call
		c.tokenMutex.Lock()
		c.accessToken = ""
		c.tokenMutex.Unlock()
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &ApiError{Method: method, Path: path, StatusCode: resp.StatusCode, Body: truncate(string(respBody))}
	}
	return respBody, nil
}

// decodeData unmarshals body into out, unwrapping a top level "data" field
// when present. The enablenow APIs are not consistent about this envelope.
func decodeData(body []byte, out any) error {
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(body, &envelope); err == nil {
		if data, ok := envelope["data"]; ok {
			return json.Unmarshal(data, out)
		}
	}
	return json.Unmarshal(body, out)
}

func truncate(s string) string {
	const max = 500
	if len(s) > max {
		return s[:max] + "..."
	}
	return s
}

func (c *HttpClient) GetConsent(ctx context.Context, consentId string) (*Consent, error) {
	var consent Consent
	err := c.do(ctx, http.MethodGet, "/v2/consents/"+url.PathEscape(consentId), nil, nil, &consent)
	if err != nil {
		return nil, err
	}
	return &consent, nil
}

func (c *HttpClient) DeleteConsent(ctx context.Context, consentId string) error {
	return c.do(ctx, http.MethodDelete, "/v2/consents/"+url.PathEscape(consentId), nil, nil, nil)
}

func (c *HttpClient) ListAccounts(ctx context.Context, consentId string) ([]Account, error) {
	var accounts []Account
	err := c.do(ctx, http.MethodGet, "/Accounts", url.Values{"ConsentId": {consentId}}, nil, &accounts)
	if err != nil {
		return nil, err
	}
	return accounts, nil
}

type transactionsPage struct {
	Data          []Transaction `json:"data"`
	NextPageToken *string       `json:"nextPageToken"`
}

func (c *HttpClient) ListTransactions(ctx context.Context, consentId string, from, to time.Time) ([]Transaction, error) {
	var all []Transaction
	pageToken := ""
	for range maxPages {
		query := url.Values{
			"ConsentId": {consentId},
			"DateFrom":  {from.UTC().Format(time.RFC3339)},
			"DateTo":    {to.UTC().Format(time.RFC3339)},
			"PageSize":  {fmt.Sprint(maxPageSize)},
		}
		if pageToken != "" {
			query.Set("PageToken", pageToken)
		}
		body, err := c.request(ctx, http.MethodGet, "/Transactions", query, nil)
		if err != nil {
			return nil, err
		}
		var page transactionsPage
		if err := json.Unmarshal(body, &page); err != nil {
			return nil, fmt.Errorf("failed to parse transactions page: %w", err)
		}
		all = append(all, page.Data...)
		if page.NextPageToken == nil || *page.NextPageToken == "" {
			return all, nil
		}
		pageToken = *page.NextPageToken
	}
	return nil, fmt.Errorf("transactions for consent exceed %d pages", maxPages)
}

func (c *HttpClient) ListCategorizationJobs(ctx context.Context, consentId string) ([]Job, error) {
	var jobs []Job
	err := c.do(ctx, http.MethodGet, "/v2/categorization-jobs", url.Values{"consentId": {consentId}}, nil, &jobs)
	if err != nil {
		if IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	return jobs, nil
}

func (c *HttpClient) CreateCategorizationJob(ctx context.Context, req JobRequest) (*Job, error) {
	var job Job
	if err := c.do(ctx, http.MethodPost, "/v2/categorization-jobs", nil, req, &job); err != nil {
		return nil, err
	}
	return &job, nil
}

func (c *HttpClient) GetCategorizationJob(ctx context.Context, jobId string) (*Job, error) {
	var job Job
	if err := c.do(ctx, http.MethodGet, "/v2/categorization-jobs/"+url.PathEscape(jobId), nil, nil, &job); err != nil {
		return nil, err
	}
	return &job, nil
}
