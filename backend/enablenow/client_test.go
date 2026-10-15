package enablenow

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func newTestClient(t *testing.T, handler http.HandlerFunc) (*HttpClient, *atomic.Int32) {
	t.Helper()
	var tokenRequests atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("POST /connect/token", func(w http.ResponseWriter, r *http.Request) {
		tokenRequests.Add(1)
		require.NoError(t, r.ParseForm())
		require.Equal(t, "client_credentials", r.PostForm.Get("grant_type"))
		require.Equal(t, "customer-api", r.PostForm.Get("scope"))
		require.Equal(t, "id", r.PostForm.Get("client_id"))
		require.Equal(t, "secret", r.PostForm.Get("client_secret"))
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "tok", "expires_in": 3600, "token_type": "Bearer"})
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "Bearer tok", r.Header.Get("Authorization"))
		handler(w, r)
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	client, err := NewHttpClient(Config{ClientId: "id", ClientSecret: "secret", ApiUrl: server.URL})
	require.NoError(t, err)
	return client, &tokenRequests
}

func TestConsentURL(t *testing.T) {
	client, err := NewHttpClient(Config{ClientId: "abc", ClientSecret: "s"})
	require.NoError(t, err)
	u, err := url.Parse(client.ConsentURL("https://issuer.example/callback", url.Values{"state": {"xyz"}}))
	require.NoError(t, err)
	require.Equal(t, "app.enablenow.nl", u.Host)
	require.Equal(t, "abc", u.Query().Get("client"))
	require.Equal(t, "https://issuer.example/callback", u.Query().Get("redirect_uri"))
	require.Equal(t, "xyz", u.Query().Get("state"))
}

func TestGetConsentUnwrapsDataAndCachesToken(t *testing.T) {
	client, tokenRequests := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/v2/consents/c1", r.URL.Path)
		_, _ = w.Write([]byte(`{"data":{"id":"c1","status":"UPDATED","providerKey":"ing","dateTransactionsUpdated":"2026-10-01T10:00:00Z"}}`))
	})

	for range 3 {
		consent, err := client.GetConsent(t.Context(), "c1")
		require.NoError(t, err)
		require.Equal(t, "ing", consent.ProviderKey)
		require.True(t, consent.HasTransactions())
		require.False(t, consent.IsFailed())
	}
	require.Equal(t, int32(1), tokenRequests.Load())
}

func TestListAccountsPlainArray(t *testing.T) {
	client, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/Accounts", r.URL.Path)
		require.Equal(t, "c1", r.URL.Query().Get("ConsentId"))
		_, _ = w.Write([]byte(`[{"id":"a1","iban":"NL91ABNA0417164300","accountHolder":"J. Jansen"}]`))
	})
	accounts, err := client.ListAccounts(t.Context(), "c1")
	require.NoError(t, err)
	require.Len(t, accounts, 1)
	require.Equal(t, "NL91ABNA0417164300", accounts[0].Iban)
}

func TestListTransactionsFollowsPages(t *testing.T) {
	client, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/Transactions", r.URL.Path)
		require.Equal(t, "c1", r.URL.Query().Get("ConsentId"))
		require.Equal(t, "2025-10-01T00:00:00Z", r.URL.Query().Get("DateFrom"))
		switch r.URL.Query().Get("PageToken") {
		case "":
			_, _ = w.Write([]byte(`{"data":[{"id":"t1","amount":-10.5,"bookDate":"2026-01-02","category":{"e1":{"category":"expense.essential.groceries","confidence":0.9,"flags":[]}}}],"nextPageToken":"p2"}`))
		case "p2":
			_, _ = w.Write([]byte(`{"data":[{"id":"t2","amount":100}],"nextPageToken":null}`))
		default:
			t.Fatalf("unexpected page token")
		}
	})
	from := time.Date(2025, 10, 1, 0, 0, 0, 0, time.UTC)
	txs, err := client.ListTransactions(t.Context(), "c1", from, from.AddDate(1, 0, 0))
	require.NoError(t, err)
	require.Len(t, txs, 2)
	require.Equal(t, "expense.essential.groceries", txs[0].E1())
	require.Equal(t, "", txs[1].E1())
}

func TestCreateAndGetJob(t *testing.T) {
	client, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v2/categorization-jobs":
			var req JobRequest
			require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
			require.Equal(t, JobRequest{Engine: "e1", Profile: "retail", CountryCode: "nl", ConsentId: "c1"}, req)
			// the real API answers with just the job id
			w.WriteHeader(http.StatusAccepted)
			_, _ = w.Write([]byte(`{"data":"j1"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/v2/categorization-jobs/j1":
			_, _ = w.Write([]byte(`{"data":{"id":"j1","status":"COMPLETED","engine":"e1"}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/v2/categorization-jobs":
			require.Equal(t, "c1", r.URL.Query().Get("consentId"))
			_, _ = w.Write([]byte(`{"data":[{"id":"j1","status":"COMPLETED","engine":"e1","profile":"retail"}]}`))
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	})
	job, err := client.CreateCategorizationJob(t.Context(), JobRequest{Engine: "e1", Profile: "retail", CountryCode: "nl", ConsentId: "c1"})
	require.NoError(t, err)
	require.Equal(t, "j1", job.Id)
	require.Equal(t, JobPending, job.Status)

	job, err = client.GetCategorizationJob(t.Context(), "j1")
	require.NoError(t, err)
	require.Equal(t, JobCompleted, job.Status)

	jobs, err := client.ListCategorizationJobs(t.Context(), "c1")
	require.NoError(t, err)
	require.Len(t, jobs, 1)
}

func TestApiErrors(t *testing.T) {
	client, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"title":"not found"}`))
	})
	_, err := client.GetConsent(t.Context(), "missing")
	require.Error(t, err)
	require.True(t, IsNotFound(err))

	// listing jobs for a consent without jobs is not an error
	jobs, err := client.ListCategorizationJobs(t.Context(), "c1")
	require.NoError(t, err)
	require.Empty(t, jobs)
}

func TestTransactionDate(t *testing.T) {
	ts := time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)
	for _, tc := range []struct {
		tx   Transaction
		want time.Time
		ok   bool
	}{
		{Transaction{BookDate: "2026-01-02"}, time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC), true},
		{Transaction{BookDate: "2026-01-02T00:00:00"}, time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC), true},
		{Transaction{BookDate: "garbage", TransactionDateTime: &Time{ts}}, ts, true},
		{Transaction{}, time.Time{}, false},
	} {
		got, ok := tc.tx.Date()
		require.Equal(t, tc.ok, ok)
		require.True(t, tc.want.Equal(got), "got %v want %v", got, tc.want)
	}
}

func TestConsentWithZonelessTimestamps(t *testing.T) {
	// enablenow returns timestamps without a time zone
	client, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":{"id":"c1","status":"UPDATED","providerKey":"ENSANDBOX","dateRequested":"2026-10-15T11:48:53.9247493","dateTransactionsUpdated":"2026-10-15T11:48:56.1","dateExpire":null}}`))
	})
	consent, err := client.GetConsent(t.Context(), "c1")
	require.NoError(t, err)
	require.True(t, consent.HasTransactions())
	require.Nil(t, consent.DateExpire)
	require.Equal(t, time.Date(2026, 10, 15, 11, 48, 53, 924749300, time.UTC), consent.DateRequested.Time)
}

func TestTimeUnmarshal(t *testing.T) {
	for in, want := range map[string]time.Time{
		`"2026-10-15T11:48:53.9247493"`:   time.Date(2026, 10, 15, 11, 48, 53, 924749300, time.UTC),
		`"2026-10-15T11:48:53"`:           time.Date(2026, 10, 15, 11, 48, 53, 0, time.UTC),
		`"2026-10-15T11:48:53Z"`:          time.Date(2026, 10, 15, 11, 48, 53, 0, time.UTC),
		`"2026-10-15T13:48:53+02:00"`:     time.Date(2026, 10, 15, 11, 48, 53, 0, time.UTC),
		`"2026-10-15"`:                    time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC),
		`"2019-08-24T14:15:22.123+00:00"`: time.Date(2019, 8, 24, 14, 15, 22, 123000000, time.UTC),
	} {
		var got Time
		require.NoError(t, json.Unmarshal([]byte(in), &got), in)
		require.True(t, want.Equal(got.Time), "%s: got %v", in, got.Time)
	}
	var bad Time
	require.Error(t, json.Unmarshal([]byte(`"yesterday"`), &bad))
}

func TestCreateJobAcceptsJobObject(t *testing.T) {
	client, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"data":{"id":"j2","status":"INPROGRESS","engine":"e1"}}`))
	})
	job, err := client.CreateCategorizationJob(t.Context(), JobRequest{Engine: "e1", Profile: "retail", ConsentId: "c1"})
	require.NoError(t, err)
	require.Equal(t, "j2", job.Id)
	require.Equal(t, JobInProgress, job.Status)
}
