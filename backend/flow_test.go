package main

import (
	"net/url"
	"testing"
	"time"

	"psd2-issuer/enablenow"

	"github.com/stretchr/testify/require"
)

type flowEnv struct {
	flow    *Flow
	storage *InMemorySessionStorage
	fake    *enablenow.FakeClient
	now     time.Time
	session *Session
	user    *enablenow.User
}

// newFlowEnv creates a session that connected one bank per provider key and
// finished connecting. The consents are still authenticating.
func newFlowEnv(t *testing.T, providers ...string) *flowEnv {
	t.Helper()
	fake := enablenow.NewFakeClient()
	env := &flowEnv{
		storage: NewInMemorySessionStorage(),
		fake:    fake,
		now:     time.Now(),
	}
	fake.Now = func() time.Time { return env.now }
	fake.JobDuration = 10 * time.Second
	env.storage.now = func() time.Time { return env.now }
	env.flow = NewFlow(FlowConfig{
		Engine: "e1", Profile: "retail", CountryCode: "nl", Months: 12, Rounding: 10,
		DeleteConsent: true, PollInterval: 2 * time.Second, StepTimeout: 5 * time.Minute, MaxTransientErrors: 3,
	}, fake)
	env.flow.now = func() time.Time { return env.now }

	user, err := fake.CreateUser(t.Context(), "ext-"+t.Name())
	require.NoError(t, err)
	env.user = user

	env.session = NewSession(env.now)
	env.session.UserId = user.Id
	env.session.ExternalUserId = user.ExternalUserId
	for _, provider := range providers {
		redirect, err := url.Parse(fake.ConsentURL("http://issuer.example/callback", url.Values{"user_id": {user.Id}, "provider": {provider}}))
		require.NoError(t, err)
		id := redirect.Query().Get("consent_id")
		fake.UpdateConsent(id, func(c *enablenow.Consent) {
			c.Status = enablenow.ConsentAuthenticating
			c.DateTransactionsUpdated = nil
		})
		env.session.Consents = append(env.session.Consents, SessionConsent{Id: id, Status: ConsentPending})
	}
	env.session.setStatus(StatusAwaitingTransactions, env.now)
	require.NoError(t, env.storage.Put(env.session))
	return env
}

func (e *flowEnv) consentId(i int) string {
	return e.session.Consents[i].Id
}

// fetched marks the consent's transactions as fetched by enablenow.
func (e *flowEnv) fetched(i int) {
	fetched := enablenow.Time{Time: e.now}
	e.fake.UpdateConsent(e.consentId(i), func(c *enablenow.Consent) {
		c.Status = enablenow.ConsentAuthenticated
		c.DateTransactionsUpdated = &fetched
	})
}

func (e *flowEnv) setConsentStatus(i int, status string) {
	e.fake.UpdateConsent(e.consentId(i), func(c *enablenow.Consent) { c.Status = status })
}

func (e *flowEnv) advance(t *testing.T, after time.Duration) *Session {
	t.Helper()
	e.now = e.now.Add(after)
	session, err := e.flow.Advance(t.Context(), e.storage, e.session.Id)
	require.NoError(t, err)
	return session
}

func bankAttributes(session *Session) map[string]string {
	banks := map[string]string{}
	for _, c := range session.Credentials {
		if c.Key == "bankaccount" {
			banks[c.Attributes["iban"]] = c.Attributes["bank"]
		}
	}
	return banks
}

func consentStatuses(session *Session) []string {
	var statuses []string
	for _, c := range session.Consents {
		statuses = append(statuses, c.Status)
	}
	return statuses
}

func TestFlowWaitsForTransactionsThenCategorizes(t *testing.T) {
	env := newFlowEnv(t, "RABONL2U")

	require.Equal(t, StatusAwaitingTransactions, env.advance(t, 3*time.Second).Status)

	env.fetched(0)
	session := env.advance(t, 3*time.Second)
	require.Equal(t, StatusCategorizing, session.Status)
	require.Equal(t, "RABONL2U", session.Consents[0].ProviderKey)
	require.NotEmpty(t, session.JobId)

	// a single bank is categorized per consent, with its accounts as internal
	require.Len(t, env.fake.JobRequests, 1)
	req := env.fake.JobRequests[0]
	require.Equal(t, env.consentId(0), req.ConsentId)
	require.Empty(t, req.UserId)
	require.Equal(t, []string{"NL91ABNA0417164300", "NL20INGB0001234567"}, req.MetaData.InternalIbans)

	// polling faster than the poll interval does not call enablenow
	require.Equal(t, StatusCategorizing, env.advance(t, time.Second).Status)

	session = env.advance(t, 15*time.Second)
	require.Equal(t, StatusReady, session.Status)
	require.NotEmpty(t, session.Credentials)
	require.Equal(t, map[string]string{"NL91ABNA0417164300": "RABONL2U", "NL20INGB0001234567": "RABONL2U"}, bankAttributes(session))

	// the enablenow user, and with it all bank data, is deleted
	require.Equal(t, []string{env.user.Id}, env.fake.DeletedUsers)

	// a ready session is left alone
	require.Equal(t, StatusReady, env.advance(t, 10*time.Minute).Status)
}

func TestFlowReusesAutoStartedJob(t *testing.T) {
	env := newFlowEnv(t, "RABONL2U")
	job, err := env.fake.CreateCategorizationJob(t.Context(), enablenow.JobRequest{Engine: "e1", Profile: "retail", ConsentId: env.consentId(0)})
	require.NoError(t, err)

	env.fetched(0)
	session := env.advance(t, 3*time.Second)
	require.Equal(t, job.Id, session.JobId)
	require.Len(t, env.fake.JobRequests, 1)
}

func TestFlowCombinesBanks(t *testing.T) {
	env := newFlowEnv(t, "ABNANL2A", "RABONL2U")
	env.fetched(0)
	env.fetched(1)

	session := env.advance(t, 3*time.Second)
	require.Equal(t, StatusCategorizing, session.Status)

	// one job over the user, with the accounts of both banks as internal
	require.Len(t, env.fake.JobRequests, 1)
	req := env.fake.JobRequests[0]
	require.Equal(t, env.user.Id, req.UserId)
	require.Empty(t, req.ConsentId)
	require.ElementsMatch(t, []string{"NL91ABNA0417164300", "NL20INGB0001234567", "NL02RABO0123456789"}, req.MetaData.InternalIbans)

	session = env.advance(t, 15*time.Second)
	require.Equal(t, StatusReady, session.Status, session.Error)
	require.Equal(t, map[string]string{
		"NL91ABNA0417164300": "ABNANL2A",
		"NL20INGB0001234567": "ABNANL2A",
		"NL02RABO0123456789": "RABONL2U",
	}, bankAttributes(session))

	creds := map[string]map[string]string{}
	for _, c := range session.Credentials {
		creds[c.Key] = c.Attributes
	}
	// salary from the first bank, side income from the second
	require.Equal(t, "3220", creds["income"]["salary"])
	require.Equal(t, "150", creds["income"]["other"])
	require.Equal(t, "-40", creds["lifestyle"]["sports"])
	// the transfer between the two banks is not a private transfer
	require.Equal(t, "0", creds["other"]["privatetransfer"])

	require.Equal(t, []string{env.user.Id}, env.fake.DeletedUsers)
}

func TestFlowReusesUserJob(t *testing.T) {
	env := newFlowEnv(t, "ABNANL2A", "RABONL2U")
	// an auto-started job of one consent only covers that bank
	_, err := env.fake.CreateCategorizationJob(t.Context(), enablenow.JobRequest{Engine: "e1", Profile: "retail", ConsentId: env.consentId(0)})
	require.NoError(t, err)
	userJob, err := env.fake.CreateCategorizationJob(t.Context(), enablenow.JobRequest{Engine: "e1", Profile: "retail", UserId: env.user.Id})
	require.NoError(t, err)

	env.fetched(0)
	env.fetched(1)
	session := env.advance(t, 3*time.Second)
	require.Equal(t, userJob.Id, session.JobId)
	require.Len(t, env.fake.JobRequests, 2)
}

func TestFlowWaitsForAllBanks(t *testing.T) {
	env := newFlowEnv(t, "ABNANL2A", "RABONL2U")
	env.fetched(0)

	session := env.advance(t, 3*time.Second)
	require.Equal(t, StatusAwaitingTransactions, session.Status)
	require.Equal(t, []string{ConsentReady, ConsentPending}, consentStatuses(session))
	require.Empty(t, env.fake.JobRequests)

	env.fetched(1)
	require.Equal(t, StatusCategorizing, env.advance(t, 3*time.Second).Status)
}

func TestFlowContinuesWhenOneBankFails(t *testing.T) {
	env := newFlowEnv(t, "ABNANL2A", "RABONL2U")
	env.fetched(0)
	env.setConsentStatus(1, enablenow.ConsentError)

	session := env.advance(t, 3*time.Second)
	require.Equal(t, StatusCategorizing, session.Status)
	require.Equal(t, []string{ConsentReady, ConsentFailed}, consentStatuses(session))
	require.Equal(t, "RABONL2U", session.Consents[1].ProviderKey)

	// only the remaining bank is categorized
	require.Equal(t, env.consentId(0), env.fake.JobRequests[0].ConsentId)

	session = env.advance(t, 15*time.Second)
	require.Equal(t, StatusReady, session.Status)
	require.Equal(t, map[string]string{"NL91ABNA0417164300": "ABNANL2A", "NL20INGB0001234567": "ABNANL2A"}, bankAttributes(session))
	require.Equal(t, []BankStatus{{Provider: "ABNANL2A", Status: ConsentReady}, {Provider: "RABONL2U", Status: ConsentFailed}}, statusResponse(session).Banks)
}

func TestFlowFailsWhenAllBanksFail(t *testing.T) {
	env := newFlowEnv(t, "ABNANL2A", "RABONL2U")
	env.setConsentStatus(0, enablenow.ConsentTimedOut)
	env.setConsentStatus(1, enablenow.ConsentError)

	session := env.advance(t, 3*time.Second)
	require.Equal(t, StatusFailed, session.Status)
	require.Equal(t, ErrorBankConnection, session.Error)
	require.Equal(t, []string{env.user.Id}, env.fake.DeletedUsers)
}

func TestFlowReplacedConsentIsNotAFailure(t *testing.T) {
	// connecting the same bank again revokes the earlier consent
	env := newFlowEnv(t, "RABONL2U", "ABNANL2A", "RABONL2U")
	env.setConsentStatus(0, enablenow.ConsentInvalidated)
	env.fetched(1)
	env.fetched(2)

	session := env.advance(t, 3*time.Second)
	require.Equal(t, StatusCategorizing, session.Status)
	require.Equal(t, []string{ConsentReplaced, ConsentReady, ConsentReady}, consentStatuses(session))
	require.Equal(t, []BankStatus{{Provider: "ABNANL2A", Status: ConsentReady}, {Provider: "RABONL2U", Status: ConsentReady}}, statusResponse(session).Banks)
	require.Equal(t, env.user.Id, env.fake.JobRequests[0].UserId)
}

func TestFlowTimeoutContinuesWithReadyBanks(t *testing.T) {
	env := newFlowEnv(t, "ABNANL2A", "RABONL2U")
	env.fetched(0)
	require.Equal(t, StatusAwaitingTransactions, env.advance(t, 3*time.Second).Status)

	// the second bank never finishes
	session := env.advance(t, 6*time.Minute)
	require.Equal(t, StatusCategorizing, session.Status)
	require.Equal(t, []string{ConsentReady, ConsentFailed}, consentStatuses(session))
	require.Equal(t, env.consentId(0), env.fake.JobRequests[0].ConsentId)
}

func TestFlowTimesOut(t *testing.T) {
	env := newFlowEnv(t, "RABONL2U")
	session := env.advance(t, 6*time.Minute)
	require.Equal(t, StatusFailed, session.Status)
	require.Equal(t, ErrorTimeout, session.Error)
	require.Equal(t, []string{env.user.Id}, env.fake.DeletedUsers)
}

func TestFlowKeepsUserWhenConfigured(t *testing.T) {
	env := newFlowEnv(t, "RABONL2U")
	env.flow.config.DeleteConsent = false
	env.fetched(0)
	env.advance(t, 3*time.Second)
	require.Equal(t, StatusReady, env.advance(t, 15*time.Second).Status)
	require.Empty(t, env.fake.DeletedUsers)
}

func TestFlowIgnoresSessionsWithoutConsent(t *testing.T) {
	env := newFlowEnv(t)
	fresh := NewSession(env.now)
	require.NoError(t, env.storage.Put(fresh))

	session, err := env.flow.Advance(t.Context(), env.storage, fresh.Id)
	require.NoError(t, err)
	require.Equal(t, StatusAwaitingConsent, session.Status)

	_, err = env.flow.Advance(t.Context(), env.storage, "missing")
	require.ErrorIs(t, err, ErrSessionNotFound)
}
