package main

import (
	"context"
	"testing"
	"time"

	"psd2-issuer/enablenow"

	"github.com/stretchr/testify/require"
)

// scriptedClient wraps the fake client and lets a test override the consent.
type scriptedClient struct {
	*enablenow.FakeClient
	consent *enablenow.Consent
}

func (c *scriptedClient) GetConsent(_ context.Context, _ string) (*enablenow.Consent, error) {
	consent := *c.consent
	return &consent, nil
}

type flowEnv struct {
	flow    *Flow
	storage *InMemorySessionStorage
	client  *scriptedClient
	now     time.Time
	session *Session
}

func newFlowEnv(t *testing.T) *flowEnv {
	t.Helper()
	fake := enablenow.NewFakeClient()
	env := &flowEnv{
		storage: NewInMemorySessionStorage(),
		now:     time.Now(),
	}
	fake.Now = func() time.Time { return env.now }
	fake.JobDuration = 10 * time.Second
	env.storage.now = func() time.Time { return env.now }
	consentId := fake.CreateConsent()
	env.client = &scriptedClient{FakeClient: fake, consent: &enablenow.Consent{Id: consentId, Status: enablenow.ConsentAuthenticating, ProviderKey: "rabobank"}}
	env.flow = NewFlow(FlowConfig{
		Engine: "e1", Profile: "retail", CountryCode: "nl", Months: 12, Rounding: 10,
		DeleteConsent: true, PollInterval: 2 * time.Second, StepTimeout: 5 * time.Minute, MaxTransientErrors: 3,
	}, env.client)
	env.flow.now = func() time.Time { return env.now }

	env.session = NewSession(env.now)
	env.session.ConsentId = consentId
	env.session.setStatus(StatusAwaitingTransactions, env.now)
	require.NoError(t, env.storage.Put(env.session))
	return env
}

func (e *flowEnv) advance(t *testing.T, after time.Duration) *Session {
	t.Helper()
	e.now = e.now.Add(after)
	session, err := e.flow.Advance(t.Context(), e.storage, e.session.Id)
	require.NoError(t, err)
	return session
}

func TestFlowWaitsForTransactionsThenCategorizes(t *testing.T) {
	env := newFlowEnv(t)

	require.Equal(t, StatusAwaitingTransactions, env.advance(t, 3*time.Second).Status)

	// transactions fetched
	fetched := enablenow.Time{Time: env.now}
	env.client.consent.Status = enablenow.ConsentAuthenticated
	env.client.consent.DateTransactionsUpdated = &fetched

	session := env.advance(t, 3*time.Second)
	require.Equal(t, StatusCategorizing, session.Status)
	require.Equal(t, "rabobank", session.ProviderKey)
	require.NotEmpty(t, session.JobId)

	// polling faster than the poll interval does not call enablenow
	require.Equal(t, StatusCategorizing, env.advance(t, time.Second).Status)

	session = env.advance(t, 15*time.Second)
	require.Equal(t, StatusReady, session.Status)
	require.NotEmpty(t, session.Credentials)
	for _, c := range session.Credentials {
		if c.Key == "bankaccount" {
			require.Equal(t, "rabobank", c.Attributes["bank"])
		}
	}
	require.Equal(t, []string{env.session.ConsentId}, env.client.Deleted)

	// a ready session is left alone
	require.Equal(t, StatusReady, env.advance(t, 10*time.Minute).Status)
}

func TestFlowReusesAutoStartedJob(t *testing.T) {
	env := newFlowEnv(t)
	job, err := env.client.CreateCategorizationJob(t.Context(), enablenow.JobRequest{Engine: "e1", Profile: "retail", ConsentId: env.session.ConsentId})
	require.NoError(t, err)

	fetched := enablenow.Time{Time: env.now}
	env.client.consent.Status = enablenow.ConsentUpdated
	env.client.consent.DateTransactionsUpdated = &fetched

	session := env.advance(t, 3*time.Second)
	require.Equal(t, job.Id, session.JobId)
}

func TestFlowFailedConsent(t *testing.T) {
	env := newFlowEnv(t)
	env.client.consent.Status = enablenow.ConsentError

	session := env.advance(t, 3*time.Second)
	require.Equal(t, StatusFailed, session.Status)
	require.Equal(t, ErrorBankConnection, session.Error)
}

func TestFlowTimesOut(t *testing.T) {
	env := newFlowEnv(t)
	session := env.advance(t, 6*time.Minute)
	require.Equal(t, StatusFailed, session.Status)
	require.Equal(t, ErrorTimeout, session.Error)
	require.Equal(t, []string{env.session.ConsentId}, env.client.Deleted)
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
