package main

import (
	"bytes"
	"context"
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

	ids := map[string]string{}
	for _, key := range credentials.AllKeys {
		ids[key] = "irma-demo.enablenow." + key
	}
	jwtCreator, err := NewIrmaJwtCreator("../test-secrets/priv.pem", "psd2_issuer", ids, 90*24*time.Hour)
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
	return redirectParams(t, body)
}

// addBank asks for the AppScreen URL of another bank and returns the consent
// id and state the (fake) AppScreen redirects back with.
func (e *testEnv) addBank(t *testing.T) (consentId, state string) {
	t.Helper()
	resp, body := e.do(t, http.MethodPost, "/api/session/add-bank", map[string]string{})
	require.Equal(t, http.StatusOK, resp.StatusCode, string(body))
	return redirectParams(t, body)
}

func redirectParams(t *testing.T, body []byte) (consentId, state string) {
	t.Helper()
	var start StartResponse
	require.NoError(t, json.Unmarshal(body, &start))
	redirect, err := url.Parse(start.Url)
	require.NoError(t, err)
	require.Equal(t, "http://issuer.example/callback", redirect.Scheme+"://"+redirect.Host+redirect.Path)
	return redirect.Query().Get("consent_id"), redirect.Query().Get("state")
}

func (e *testEnv) consent(t *testing.T, consentId, state string) StatusResponse {
	t.Helper()
	resp, body := e.do(t, http.MethodPost, "/api/session/consent", ConsentPayload{ConsentId: consentId, State: state})
	require.Equal(t, http.StatusOK, resp.StatusCode, string(body))
	var status StatusResponse
	require.NoError(t, json.Unmarshal(body, &status))
	return status
}

func (e *testEnv) finish(t *testing.T) StatusResponse {
	t.Helper()
	resp, body := e.do(t, http.MethodPost, "/api/session/finish", map[string]string{})
	require.Equal(t, http.StatusOK, resp.StatusCode, string(body))
	var status StatusResponse
	require.NoError(t, json.Unmarshal(body, &status))
	return status
}

// session returns the stored session of the test client's cookie.
func (e *testEnv) session(t *testing.T) *Session {
	t.Helper()
	u, err := url.Parse(e.server.URL)
	require.NoError(t, err)
	for _, cookie := range e.client.Jar.Cookies(u) {
		if cookie.Name == sessionCookieName {
			session, err := e.storage.Get(cookie.Value)
			require.NoError(t, err)
			return session
		}
	}
	t.Fatal("no session cookie")
	return nil
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
	userId := env.session(t).UserId
	require.NotEmpty(t, userId)

	status := env.consent(t, consentId, state)
	// the user can connect another bank or continue
	require.Equal(t, StatusAwaitingConsent, status.Status)
	require.Equal(t, []BankStatus{{Provider: "fake-bank", Status: ConsentPending}}, status.Banks)

	// posting the same consent again (page reload) is fine
	status = env.consent(t, consentId, state)
	require.Len(t, status.Banks, 1)

	require.Equal(t, StatusAwaitingTransactions, env.finish(t).Status)
	// finishing twice (double click) is fine
	require.Equal(t, StatusAwaitingTransactions, env.finish(t).Status)

	status = env.status(t)
	require.Equal(t, StatusReady, status.Status, status.Error)
	require.Len(t, status.Credentials, len(credentials.AllKeys)+1) // two bank accounts
	require.Equal(t, []BankStatus{{Provider: "fake-bank", Status: ConsentReady}}, status.Banks)

	// the enablenow user and its bank data are deleted once the credentials
	// are computed
	require.Equal(t, []string{userId}, env.fake.DeletedUsers)

	resp, body := env.do(t, http.MethodPost, "/api/session/issue", IssuePayload{Credentials: []string{"income", "bankaccount", "unknown"}})
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

func TestDefinitions(t *testing.T) {
	env := newTestEnv(t)
	resp, body := env.do(t, http.MethodGet, "/api/definitions", nil)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	var defs credentials.Definitions
	require.NoError(t, json.Unmarshal(body, &defs))
	require.Equal(t, credentials.Defs, defs)
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

func TestConsentAfterFinishIsRejected(t *testing.T) {
	env := newTestEnv(t)
	consentId, state := env.start(t)
	env.consent(t, consentId, state)
	env.finish(t)

	resp, _ := env.do(t, http.MethodPost, "/api/session/consent", ConsentPayload{ConsentId: env.fake.CreateConsent(), State: state})
	require.Equal(t, http.StatusConflict, resp.StatusCode)

	// the known consent is still accepted (page reload)
	resp, _ = env.do(t, http.MethodPost, "/api/session/consent", ConsentPayload{ConsentId: consentId, State: state})
	require.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestMultiBankIssuanceFlow(t *testing.T) {
	env := newTestEnv(t)
	first, state := env.start(t)
	env.consent(t, first, state)

	second, state2 := env.addBank(t)
	require.Equal(t, state, state2)
	require.NotEqual(t, first, second)
	status := env.consent(t, second, state2)
	require.Equal(t, StatusAwaitingConsent, status.Status)
	require.Equal(t, []BankStatus{{Provider: "fake-bank", Status: ConsentPending}, {Provider: "fake-bank-2", Status: ConsentPending}}, status.Banks)

	env.finish(t)
	status = env.status(t)
	require.Equal(t, StatusReady, status.Status, status.Error)
	require.Len(t, status.Credentials, len(credentials.AllKeys)+2) // three bank accounts at two banks

	banks := map[string]string{}
	for _, c := range status.Credentials {
		if c.Key == credentials.BankAccount {
			banks[c.Attributes["iban"]] = c.Attributes["bank"]
		}
	}
	require.Equal(t, map[string]string{
		"NL91ABNA0417164300": "fake-bank",
		"NL20INGB0001234567": "fake-bank",
		"NL02RABO0123456789": "fake-bank-2",
	}, banks)

	// one categorization job over both banks
	require.Len(t, env.fake.JobRequests, 1)
	require.Equal(t, env.session(t).UserId, env.fake.JobRequests[0].UserId)
}

func TestOneFailedBankDoesNotFailTheSession(t *testing.T) {
	env := newTestEnv(t)
	first, state := env.start(t)
	env.consent(t, first, state)
	second, _ := env.addBank(t)
	env.consent(t, second, state)
	env.fake.UpdateConsent(second, func(c *enablenow.Consent) { c.Status = enablenow.ConsentError })

	env.finish(t)
	status := env.status(t)
	require.Equal(t, StatusReady, status.Status, status.Error)
	require.Equal(t, []BankStatus{{Provider: "fake-bank", Status: ConsentReady}, {Provider: "fake-bank-2", Status: ConsentFailed}}, status.Banks)
	require.Len(t, status.Credentials, len(credentials.AllKeys)+1) // two bank accounts at the remaining bank
}

func TestAddBankIsLimited(t *testing.T) {
	env := newTestEnv(t)
	consentId, state := env.start(t)
	env.consent(t, consentId, state)
	for range MaxBanks - 1 {
		id, _ := env.addBank(t)
		env.consent(t, id, state)
	}

	resp, body := env.do(t, http.MethodPost, "/api/session/add-bank", map[string]string{})
	require.Equal(t, http.StatusConflict, resp.StatusCode)
	require.Equal(t, ErrorTooManyBanks, string(body))

	resp, body = env.do(t, http.MethodPost, "/api/session/consent", ConsentPayload{ConsentId: env.fake.CreateConsent(), State: state})
	require.Equal(t, http.StatusConflict, resp.StatusCode)
	require.Equal(t, ErrorTooManyBanks, string(body))
}

func TestAddBankAfterFinishIsRejected(t *testing.T) {
	env := newTestEnv(t)
	consentId, state := env.start(t)
	env.consent(t, consentId, state)
	env.finish(t)

	resp, body := env.do(t, http.MethodPost, "/api/session/add-bank", map[string]string{})
	require.Equal(t, http.StatusConflict, resp.StatusCode)
	require.Equal(t, ErrorInvalidState, string(body))
}

func TestFinishWithoutConsent(t *testing.T) {
	env := newTestEnv(t)
	env.start(t)

	resp, body := env.do(t, http.MethodPost, "/api/session/finish", map[string]string{})
	require.Equal(t, http.StatusConflict, resp.StatusCode)
	require.Equal(t, ErrorNoConsent, string(body))
	require.Equal(t, StatusAwaitingConsent, env.status(t).Status)
}

// failingUserClient is the fake enablenow, except that users cannot be created.
type failingUserClient struct {
	*enablenow.FakeClient
}

func (failingUserClient) CreateUser(_ context.Context, _ string) (*enablenow.User, error) {
	return nil, &enablenow.ApiError{Method: "POST", Path: "/Users", StatusCode: 500}
}

func TestStartFailsWhenUserCannotBeCreated(t *testing.T) {
	env := newTestEnv(t)
	env.state.enablenow = failingUserClient{env.fake}

	resp, body := env.do(t, http.MethodPost, "/api/session/start", map[string]string{})
	require.Equal(t, http.StatusBadGateway, resp.StatusCode)
	require.Equal(t, ErrorEnablenow, string(body))
}

func TestNewStartDeletesPreviousUser(t *testing.T) {
	env := newTestEnv(t)
	consentId, state := env.start(t)
	env.consent(t, consentId, state)
	previous := env.session(t).UserId

	env.start(t)
	require.Equal(t, []string{previous}, env.fake.DeletedUsers)
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
	env.consent(t, consentId, state)
	env.finish(t)
	require.Equal(t, StatusReady, env.status(t).Status)

	resp, _ := env.do(t, http.MethodPost, "/api/session/issue", IssuePayload{Credentials: []string{"nope"}})
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestUnknownConsentFailsAfterRetries(t *testing.T) {
	env := newTestEnv(t)
	_, state := env.start(t)
	env.consent(t, "does-not-exist", state)
	env.finish(t)

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
	env.consent(t, consentId, state)
	userId := env.session(t).UserId

	resp, _ := env.do(t, http.MethodDelete, "/api/session", nil)
	require.Equal(t, http.StatusNoContent, resp.StatusCode)
	// the enablenow user with the in-progress consent is deleted as well
	require.Equal(t, []string{userId}, env.fake.DeletedUsers)

	resp, _ = env.do(t, http.MethodGet, "/api/session/status", nil)
	require.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestHealth(t *testing.T) {
	env := newTestEnv(t)
	resp, body := env.do(t, http.MethodGet, "/api/health", nil)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.JSONEq(t, `{"ok":true}`, string(body))
}

// appScreenClient builds real AppScreen URLs and fakes everything else.
type appScreenClient struct {
	*enablenow.FakeClient
	real *enablenow.HttpClient
}

func (c appScreenClient) ConsentURL(redirectURI string, params url.Values) string {
	return c.real.ConsentURL(redirectURI, params)
}

// TestStateTravelsInsideRedirectUri guards the AppScreen contract: only the
// query parameters of redirect_uri are forwarded back, so the state must be
// part of redirect_uri rather than a separate AppScreen parameter. The
// consent is linked to the enablenow user through user_id.
func TestStateTravelsInsideRedirectUri(t *testing.T) {
	env := newTestEnv(t)
	real, err := enablenow.NewHttpClient(enablenow.Config{ClientId: "client", ClientSecret: "secret"})
	require.NoError(t, err)
	env.state.enablenow = appScreenClient{FakeClient: env.fake, real: real}

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
	require.Equal(t, env.session(t).UserId, appScreen.Query().Get("user_id"))
}
