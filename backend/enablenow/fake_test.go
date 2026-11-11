package enablenow

import (
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// consentFromURL follows the fake AppScreen URL and returns the consent id it
// redirects back with.
func consentFromURL(t *testing.T, fake *FakeClient, params url.Values) string {
	t.Helper()
	u, err := url.Parse(fake.ConsentURL("https://issuer.example/callback?state=s", params))
	require.NoError(t, err)
	require.Equal(t, "s", u.Query().Get("state"))
	require.Empty(t, u.Query().Get("user_id"), "AppScreen parameters are not forwarded")
	return u.Query().Get("consent_id")
}

func TestFakeUserWithSeveralBanks(t *testing.T) {
	fake := NewFakeClient()
	ctx := t.Context()
	user, err := fake.CreateUser(ctx, "ext-1")
	require.NoError(t, err)
	require.NotEmpty(t, user.Id)

	first := consentFromURL(t, fake, url.Values{"user_id": {user.Id}})
	second := consentFromURL(t, fake, url.Values{"user_id": {user.Id}})

	c1, err := fake.GetConsent(ctx, first)
	require.NoError(t, err)
	c2, err := fake.GetConsent(ctx, second)
	require.NoError(t, err)
	require.Equal(t, "fake-bank", c1.ProviderKey)
	require.Equal(t, "fake-bank-2", c2.ProviderKey)

	// every bank has its own accounts
	a1, err := fake.ListAccounts(ctx, first)
	require.NoError(t, err)
	a2, err := fake.ListAccounts(ctx, second)
	require.NoError(t, err)
	require.Len(t, a1, 2)
	require.Len(t, a2, 1)
	require.NotEqual(t, a1[0].Iban, a2[0].Iban)

	from := time.Now().AddDate(-1, 0, 0)
	txs, err := fake.ListTransactions(ctx, second, from, time.Now())
	require.NoError(t, err)
	require.NotEmpty(t, txs)
	for _, tx := range txs {
		require.Equal(t, a2[0].Id, tx.AccountId)
	}

	// a user job is found by the external user id, a consent job by consent
	job, err := fake.CreateCategorizationJob(ctx, JobRequest{Engine: "e1", Profile: "retail", UserId: user.Id})
	require.NoError(t, err)
	jobs, err := fake.ListCategorizationJobs(ctx, JobFilter{ExternalUserId: "ext-1"})
	require.NoError(t, err)
	require.Len(t, jobs, 1)
	require.Equal(t, job.Id, jobs[0].Id)
	jobs, err = fake.ListCategorizationJobs(ctx, JobFilter{ConsentId: first})
	require.NoError(t, err)
	require.Empty(t, jobs)
	require.Equal(t, []JobRequest{{Engine: "e1", Profile: "retail", UserId: user.Id}}, fake.JobRequests)

	require.NoError(t, fake.DeleteUser(ctx, user.Id))
	require.Equal(t, []string{user.Id}, fake.DeletedUsers)
	_, err = fake.GetConsent(ctx, first)
	require.True(t, IsNotFound(err))
	require.True(t, IsNotFound(fake.DeleteUser(ctx, user.Id)))
}

func TestFakeProviderParameter(t *testing.T) {
	fake := NewFakeClient()
	id := consentFromURL(t, fake, url.Values{"provider": {"RABONL2U"}})
	consent, err := fake.GetConsent(t.Context(), id)
	require.NoError(t, err)
	require.Equal(t, "RABONL2U", consent.ProviderKey)
}

func TestFakeUpdateConsent(t *testing.T) {
	fake := NewFakeClient()
	id := fake.CreateConsent()
	fake.UpdateConsent(id, func(c *Consent) { c.Status = ConsentError })
	consent, err := fake.GetConsent(t.Context(), id)
	require.NoError(t, err)
	require.True(t, consent.IsFailed())
}

func TestFakeJobForUnknownUser(t *testing.T) {
	fake := NewFakeClient()
	_, err := fake.CreateCategorizationJob(t.Context(), JobRequest{Engine: "e1", UserId: "nope"})
	require.Error(t, err)
}
