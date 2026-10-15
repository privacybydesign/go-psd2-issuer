package main

import (
	"context"
	"hash/fnv"
	"log/slog"
	"psd2-issuer/credentials"
	"psd2-issuer/enablenow"
	"psd2-issuer/logging"
	"sync"
	"time"
)

const ErrorBankConnection = "error:bank-connection"
const ErrorCategorization = "error:categorization"
const ErrorNoTransactions = "error:no-transactions"
const ErrorTimeout = "error:timeout"
const ErrorEnablenow = "error:enablenow"

// FlowConfig tunes how a session moves from consent to computed credentials.
type FlowConfig struct {
	Engine        string
	Profile       string
	CountryCode   string
	Months        int
	Rounding      int
	MinConfidence float64
	// DeleteConsent removes the consent (and with it the bank data) from
	// enablenow once the credentials are computed.
	DeleteConsent bool
	// PollInterval is the minimum time between two enablenow calls for the
	// same session, however often the frontend polls.
	PollInterval time.Duration
	// StepTimeout fails a session that is stuck in one status for too long.
	StepTimeout time.Duration
	// MaxTransientErrors fails a session after this many consecutive failed
	// calls to enablenow.
	MaxTransientErrors int
}

// Flow advances sessions. It holds no state of its own besides a per-session
// lock: every step is driven by the frontend polling the status endpoint, so
// any instance can pick up any session and nothing runs in the background.
type Flow struct {
	config FlowConfig
	client enablenow.Client
	now    func() time.Time
	locks  [64]sync.Mutex
}

func NewFlow(config FlowConfig, client enablenow.Client) *Flow {
	return &Flow{config: config, client: client, now: time.Now}
}

// lock serializes Advance calls for one session within this instance, so two
// concurrent polls can't both create a categorization job. Sessions are hashed
// onto a fixed set of mutexes to keep memory bounded. Across instances the job
// lookup in startCategorization keeps duplicates unlikely.
func (f *Flow) lock(id string) func() {
	h := fnv.New32a()
	_, _ = h.Write([]byte(id))
	mutex := &f.locks[h.Sum32()%uint32(len(f.locks))]
	mutex.Lock()
	return mutex.Unlock
}

// Advance moves the session forward as far as currently possible and stores
// it. It returns the updated session.
func (f *Flow) Advance(ctx context.Context, storage SessionStorage, id string) (*Session, error) {
	unlock := f.lock(id)
	defer unlock()

	session, err := storage.Get(id)
	if err != nil {
		return nil, err
	}

	if session.Status != StatusAwaitingTransactions && session.Status != StatusCategorizing {
		return session, nil
	}

	now := f.now()
	if now.Sub(session.LastCheckedAt) < f.config.PollInterval {
		return session, nil
	}
	session.LastCheckedAt = now

	if now.Sub(session.StepStartedAt) > f.config.StepTimeout {
		slog.Warn("session timed out", "status", session.Status, "consent", logging.MaskIdentifier(session.ConsentId))
		session.fail(ErrorTimeout, now)
		f.cleanup(ctx, session)
		return session, storage.Put(session)
	}

	if err := f.step(ctx, session); err != nil {
		session.TransientErrors++
		slog.Warn("enablenow call failed", "error", err, "attempt", session.TransientErrors, "status", session.Status)
		if session.TransientErrors >= f.config.MaxTransientErrors {
			session.fail(ErrorEnablenow, now)
			f.cleanup(ctx, session)
		}
	} else {
		session.TransientErrors = 0
	}

	return session, storage.Put(session)
}

// step performs the enablenow calls for the current status. A returned error
// is treated as transient; definitive failures are recorded on the session.
func (f *Flow) step(ctx context.Context, session *Session) error {
	if session.Status == StatusAwaitingTransactions {
		consent, err := f.client.GetConsent(ctx, session.ConsentId)
		if err != nil {
			return err
		}
		if consent.IsFailed() {
			slog.Warn("consent failed", "consent_status", consent.Status, "consent", logging.MaskIdentifier(session.ConsentId))
			session.fail(ErrorBankConnection, f.now())
			f.cleanup(ctx, session)
			return nil
		}
		if !consent.HasTransactions() {
			return nil
		}
		session.ProviderKey = consent.ProviderKey
		if err := f.startCategorization(ctx, session); err != nil {
			return err
		}
		session.setStatus(StatusCategorizing, f.now())
	}

	if session.Status == StatusCategorizing {
		job, err := f.client.GetCategorizationJob(ctx, session.JobId)
		if err != nil {
			return err
		}
		switch job.Status {
		case enablenow.JobCompleted:
			return f.computeCredentials(ctx, session)
		case enablenow.JobFailed:
			slog.Warn("categorization job failed", "job", session.JobId)
			session.fail(ErrorCategorization, f.now())
			f.cleanup(ctx, session)
		}
	}
	return nil
}

// startCategorization reuses a job that was auto-started by enablenow for this
// consent, or creates one.
func (f *Flow) startCategorization(ctx context.Context, session *Session) error {
	jobs, err := f.client.ListCategorizationJobs(ctx, session.ConsentId)
	if err != nil {
		return err
	}
	for _, job := range jobs {
		if job.Engine == f.config.Engine && job.Profile == f.config.Profile && job.Status != enablenow.JobFailed {
			slog.Info("reusing categorization job", "job", job.Id, "job_status", job.Status)
			session.JobId = job.Id
			return nil
		}
	}

	job, err := f.client.CreateCategorizationJob(ctx, enablenow.JobRequest{
		Engine:      f.config.Engine,
		Profile:     f.config.Profile,
		CountryCode: f.config.CountryCode,
		ConsentId:   session.ConsentId,
	})
	if err != nil {
		return err
	}
	slog.Info("created categorization job", "job", job.Id)
	session.JobId = job.Id
	return nil
}

func (f *Flow) computeCredentials(ctx context.Context, session *Session) error {
	opts := credentials.Options{
		Months:        f.config.Months,
		Rounding:      f.config.Rounding,
		MinConfidence: f.config.MinConfidence,
		Now:           f.now(),
	}
	from, to := credentials.Window(opts)

	transactions, err := f.client.ListTransactions(ctx, session.ConsentId, from, to)
	if err != nil {
		return err
	}
	accounts, err := f.client.ListAccounts(ctx, session.ConsentId)
	if err != nil {
		return err
	}

	if len(transactions) == 0 {
		session.fail(ErrorNoTransactions, f.now())
		f.cleanup(ctx, session)
		return nil
	}

	session.Credentials = credentials.Compute(accounts, transactions, session.ProviderKey, opts)
	session.setStatus(StatusReady, f.now())
	slog.Info("computed credentials", "transactions", len(transactions), "accounts", len(accounts))
	f.cleanup(ctx, session)
	return nil
}

// cleanup deletes the consent at enablenow once it is no longer needed, so the
// bank data does not outlive the issuance flow.
func (f *Flow) cleanup(ctx context.Context, session *Session) {
	if !f.config.DeleteConsent || session.ConsentId == "" {
		return
	}
	if err := f.client.DeleteConsent(ctx, session.ConsentId); err != nil && !enablenow.IsNotFound(err) {
		slog.Error("failed to delete consent", "error", err, "consent", logging.MaskIdentifier(session.ConsentId))
		return
	}
	slog.Info("deleted consent", "consent", logging.MaskIdentifier(session.ConsentId))
}
