package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"psd2-issuer/credentials"
	"psd2-issuer/enablenow"
	rate "psd2-issuer/rate_limiter"

	"github.com/stretchr/testify/require"
)

type testEnv struct {
	server  *httptest.Server
	client  *http.Client
	fake    *enablenow.FakeClient
	storage *InMemorySessionStorage
	state   *ServerState
}

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	fake := enablenow.NewFakeClient()
	fake.JobDuration = 0

	jwtCreator, err := NewIrmaJwtCreator("../test-secrets/priv.pem", "psd2_issuer", map[string]string{
		credentials.Income:      "irma-demo.enablenow.income",
		credentials.Housing:     "irma-demo.enablenow.housing",
		credentials.Debts:       "irma-demo.enablenow.debts",
		credentials.Benefits:    "irma-demo.enablenow.benefits",
		credentials.BankAccount: "irma-demo.enablenow.bankaccount",
	}, 90*24*time.Hour)
	require.NoError(t, err)

	storage := NewInMemorySessionStorage()
	flow := NewFlow(FlowConfig{
		Engine:             "e1",
		Profile:            "retail",
		CountryCode:        "nl",
		Months:             12,
		Rounding:           10,
		DeleteConsent:      true,
		StepTimeout:        time.Minute,
		MaxTransientErrors: 3,
	}, fake)

	state := &ServerState{
		irmaServerURL:    "http://irma.example",
		publicURL:        "http://issuer.example",
		enablenow:        fake,
		flow:             flow,
		sessionStorage:   storage,
		jwtCreator:       jwtCreator,
		startRateLimiter: rate.NewInMemoryRateLimiter(rate.NewSystemClock(), rate.RateLimitingPolicy{Limit: 3, Window: time.Minute}),
		staticPath:       t.TempDir(),
	}
	srv, err := NewServer(state, ServerConfig{})
	require.NoError(t, err)

	server := httptest.NewServer(srv.server.Handler)
	t.Cleanup(server.Close)

	jar, err := cookiejar.New(nil)
	require.NoError(t, err)
	return &testEnv{server: server, client: &http.Client{Jar: jar}, fake: fake, storage: storage, state: state}
}

func (e *testEnv) do(t *testing.T, method, path string, body any) (*http.Response, []byte) {
	t.Helper()
	var reader io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		require.NoError(t, err)
		reader = bytes.NewReader(payload)
	}
	req, err := http.NewRequest(method, e.server.URL+path, reader)
	require.NoError(t, err)
	resp, err := e.client.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	content, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return resp, content
}

// start begins a session and returns the consent id and state the (fake)
// AppScreen redirects back with.
func (e *testEnv) start(t *testing.T) (consentId, state string) {
	t.Helper()
	resp, body := e.do(t, http.MethodPost, "/api/session/start", map[string]string{})
	require.Equal(t, http.StatusOK, resp.StatusCode, string(body))

	var start StartResponse
	require.NoError(t, json.Unmarshal(body, &start))
	redirect, err := url.Parse(start.Url)
	require.NoError(t, err)
	require.Equal(t, "http://issuer.example/callback", redirect.Scheme+"://"+redirect.Host+redirect.Path)
	return redirect.Query().Get("consent_id"), redirect.Query().Get("state")
}

func (e *testEnv) status(t *testing.T) StatusResponse {
	t.Helper()
	resp, body := e.do(t, http.MethodGet, "/api/session/status", nil)
	require.Equal(t, http.StatusOK, resp.StatusCode, string(body))
	var status StatusResponse
	require.NoError(t, json.Unmarshal(body, &status))
	return status
}

func TestFullIssuanceFlow(t *testing.T) {
	env := newTestEnv(t)
	consentId, state := env.start(t)
	require.NotEmpty(t, consentId)
	require.NotEmpty(t, state)

	require.Equal(t, StatusAwaitingConsent, env.status(t).Status)

	resp, body := env.do(t, http.MethodPost, "/api/session/consent", ConsentPayload{ConsentId: consentId, State: state})
	require.Equal(t, http.StatusOK, resp.StatusCode, string(body))

	// posting the same consent again (page reload) is fine
	resp, _ = env.do(t, http.MethodPost, "/api/session/consent", ConsentPayload{ConsentId: consentId, State: state})
	require.Equal(t, http.StatusOK, resp.StatusCode)

	status := env.status(t)
	require.Equal(t, StatusReady, status.Status, status.Error)
	require.Len(t, status.Credentials, 6)

	// the consent is deleted at enablenow once the credentials are computed
	require.Equal(t, []string{consentId}, env.fake.Deleted)

	resp, body = env.do(t, http.MethodPost, "/api/session/issue", IssuePayload{Credentials: []string{"income", "bankaccount", "unknown"}})
	require.Equal(t, http.StatusOK, resp.StatusCode, string(body))
	var issue IssueResponse
	require.NoError(t, json.Unmarshal(body, &issue))
	require.Equal(t, "http://irma.example", issue.IrmaServerURL)

	creds := issuedCredentials(t, issue.Jwt)
	require.Len(t, creds, 3) // income + two bank accounts
	require.Equal(t, "irma-demo.enablenow.income", creds[0].Credential)
	require.Equal(t, "3220", creds[0].Attributes["salary"])
	require.NotNil(t, creds[0].Validity)
	require.Equal(t, "irma-demo.enablenow.bankaccount", creds[1].Credential)
	require.Equal(t, "NL91ABNA0417164300", creds[1].Attributes["iban"])
	require.Equal(t, "irma-demo.enablenow.bankaccount", creds[2].Credential)
}

type issuedCredential struct {
	Credential string            `json:"credential"`
	Attributes map[string]string `json:"attributes"`
	Validity   *int64            `json:"validity"`
}

// issuedCredentials extracts the credential requests from a signed issuance
// request JWT, without verifying it.
func issuedCredentials(t *testing.T, jwt string) []issuedCredential {
	t.Helper()
	parts := strings.Split(jwt, ".")
	require.Len(t, parts, 3)
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	require.NoError(t, err)
	var claims struct {
		Iss       string `json:"iss"`
		IpRequest struct {
			Request struct {
				Credentials []issuedCredential `json:"credentials"`
			} `json:"request"`
		} `json:"iprequest"`
	}
	require.NoError(t, json.Unmarshal(payload, &claims))
	require.Equal(t, "psd2_issuer", claims.Iss)
	return claims.IpRequest.Request.Credentials
}

func TestConsentWithWrongStateIsRejected(t *testing.T) {
	env := newTestEnv(t)
	consentId, _ := env.start(t)

	resp, body := env.do(t, http.MethodPost, "/api/session/consent", ConsentPayload{ConsentId: consentId, State: "wrong"})
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
	require.Equal(t, ErrorInvalidState, string(body))
	require.Equal(t, StatusAwaitingConsent, env.status(t).Status)
}

func TestConsentCannotBeReplaced(t *testing.T) {
	env := newTestEnv(t)
	consentId, state := env.start(t)
	resp, _ := env.do(t, http.MethodPost, "/api/session/consent", ConsentPayload{ConsentId: consentId, State: state})
	require.Equal(t, http.StatusOK, resp.StatusCode)

	resp, _ = env.do(t, http.MethodPost, "/api/session/consent", ConsentPayload{ConsentId: env.fake.CreateConsent(), State: state})
	require.Equal(t, http.StatusConflict, resp.StatusCode)
}

func TestRequestsWithoutSession(t *testing.T) {
	env := newTestEnv(t)

	resp, body := env.do(t, http.MethodGet, "/api/session/status", nil)
	require.Equal(t, http.StatusNotFound, resp.StatusCode)
	require.Equal(t, ErrorSessionNotFound, string(body))

	resp, _ = env.do(t, http.MethodPost, "/api/session/consent", ConsentPayload{ConsentId: "x", State: "y"})
	require.Equal(t, http.StatusNotFound, resp.StatusCode)

	resp, _ = env.do(t, http.MethodPost, "/api/session/issue", IssuePayload{Credentials: []string{"income"}})
	require.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestIssueBeforeReadyIsRejected(t *testing.T) {
	env := newTestEnv(t)
	env.start(t)

	resp, body := env.do(t, http.MethodPost, "/api/session/issue", IssuePayload{Credentials: []string{"income"}})
	require.Equal(t, http.StatusConflict, resp.StatusCode)
	require.Equal(t, ErrorNotReady, string(body))
}

func TestIssueWithoutKnownCredentials(t *testing.T) {
	env := newTestEnv(t)
	consentId, state := env.start(t)
	env.do(t, http.MethodPost, "/api/session/consent", ConsentPayload{ConsentId: consentId, State: state})
	require.Equal(t, StatusReady, env.status(t).Status)

	resp, _ := env.do(t, http.MethodPost, "/api/session/issue", IssuePayload{Credentials: []string{"nope"}})
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestUnknownConsentFailsAfterRetries(t *testing.T) {
	env := newTestEnv(t)
	_, state := env.start(t)
	env.do(t, http.MethodPost, "/api/session/consent", ConsentPayload{ConsentId: "does-not-exist", State: state})

	// every poll fails at enablenow; after MaxTransientErrors the session fails
	require.Equal(t, StatusAwaitingTransactions, env.status(t).Status)
	require.Equal(t, StatusAwaitingTransactions, env.status(t).Status)
	status := env.status(t)
	require.Equal(t, StatusFailed, status.Status)
	require.Equal(t, ErrorEnablenow, status.Error)
}

func TestStartIsRateLimited(t *testing.T) {
	env := newTestEnv(t)
	for range 3 {
		env.start(t)
	}
	resp, body := env.do(t, http.MethodPost, "/api/session/start", map[string]string{})
	require.Equal(t, http.StatusTooManyRequests, resp.StatusCode)
	require.Equal(t, ErrorRateLimit, string(body))
	require.NotEmpty(t, resp.Header.Get("Retry-After"))
}

func TestDeleteSession(t *testing.T) {
	env := newTestEnv(t)
	consentId, state := env.start(t)
	env.do(t, http.MethodPost, "/api/session/consent", ConsentPayload{ConsentId: consentId, State: state})

	resp, _ := env.do(t, http.MethodDelete, "/api/session", nil)
	require.Equal(t, http.StatusNoContent, resp.StatusCode)
	// the in-progress consent is deleted at enablenow as well
	require.Equal(t, []string{consentId}, env.fake.Deleted)

	resp, _ = env.do(t, http.MethodGet, "/api/session/status", nil)
	require.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestHealth(t *testing.T) {
	env := newTestEnv(t)
	resp, body := env.do(t, http.MethodGet, "/api/health", nil)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.JSONEq(t, `{"ok":true}`, string(body))
}

// TestStateTravelsInsideRedirectUri guards the AppScreen contract: only the
// query parameters of redirect_uri are forwarded back, so the state must be
// part of redirect_uri rather than a separate AppScreen parameter.
func TestStateTravelsInsideRedirectUri(t *testing.T) {
	env := newTestEnv(t)
	real, err := enablenow.NewHttpClient(enablenow.Config{ClientId: "client", ClientSecret: "secret"})
	require.NoError(t, err)
	env.state.enablenow = real

	resp, body := env.do(t, http.MethodPost, "/api/session/start", map[string]string{})
	require.Equal(t, http.StatusOK, resp.StatusCode)
	var start StartResponse
	require.NoError(t, json.Unmarshal(body, &start))

	appScreen, err := url.Parse(start.Url)
	require.NoError(t, err)
	require.Empty(t, appScreen.Query().Get("state"))
	redirect, err := url.Parse(appScreen.Query().Get("redirect_uri"))
	require.NoError(t, err)
	require.Equal(t, "/callback", redirect.Path)
	require.NotEmpty(t, redirect.Query().Get("state"))
}
