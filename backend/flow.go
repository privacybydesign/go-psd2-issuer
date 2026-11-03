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
	// DeleteConsent removes the enablenow user with its consents (and with
	// them the bank data) once the credentials are computed.
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
		// banks that are still fetching are given up on when others are
		// ready; otherwise the session fails
		if session.Status == StatusAwaitingTransactions && len(session.consentsWithStatus(ConsentReady)) > 0 {
			for i := range session.Consents {
				if session.Consents[i].Status == ConsentPending {
					slog.Warn("bank timed out, continuing without it", "consent", logging.MaskIdentifier(session.Consents[i].Id))
					session.Consents[i].Status = ConsentFailed
				}
			}
		} else {
			slog.Warn("session timed out", "status", session.Status)
			session.fail(ErrorTimeout, now)
			f.cleanup(ctx, session)
			return session, storage.Put(session)
		}
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
		if err := f.checkConsents(ctx, session); err != nil {
			return err
		}
		if len(session.consentsWithStatus(ConsentPending)) > 0 {
			return nil
		}
		if len(session.consentsWithStatus(ConsentReady)) == 0 {
			slog.Warn("no bank connection succeeded")
			session.fail(ErrorBankConnection, f.now())
			f.cleanup(ctx, session)
			return nil
		}
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

// checkConsents updates the status of every pending consent. A failed bank
// does not fail the session; the flow continues with the other banks.
func (f *Flow) checkConsents(ctx context.Context, session *Session) error {
	for i := range session.Consents {
		c := &session.Consents[i]
		if c.Status != ConsentPending {
			continue
		}
		consent, err := f.client.GetConsent(ctx, c.Id)
		if err != nil {
			return err
		}
		c.ProviderKey = consent.ProviderKey
		switch {
		case consent.IsFailed():
			slog.Warn("bank connection failed", "consent_status", consent.Status, "provider", consent.ProviderKey, "consent", logging.MaskIdentifier(c.Id))
			c.Status = ConsentFailed
			if consent.Status == enablenow.ConsentInvalidated {
				c.Status = ConsentReplaced
			}
		case consent.HasTransactions():
			c.Status = ConsentReady
		}
	}
	// enablenow revokes a consent when the same accounts are connected
	// again; that is only a failure when no later consent of the bank exists
	for i := range session.Consents {
		c := &session.Consents[i]
		if c.Status != ConsentReplaced {
			continue
		}
		c.Status = ConsentFailed
		for _, later := range session.Consents[i+1:] {
			if later.ProviderKey == c.ProviderKey && later.Status != ConsentFailed {
				c.Status = ConsentReplaced
			}
		}
	}
	return nil
}

// startCategorization starts one categorization job over all banks that are
// ready: per consent when there is one, so a job auto-started by enablenow
// can be reused, and per user otherwise.
func (f *Flow) startCategorization(ctx context.Context, session *Session) error {
	ready := session.consentsWithStatus(ConsentReady)

	internalIbans := []string{}
	for _, c := range ready {
		accounts, err := f.client.ListAccounts(ctx, c.Id)
		if err != nil {
			return err
		}
		for _, account := range accounts {
			if iban := credentials.NormalizeIban(account.Iban); iban != "" {
				internalIbans = append(internalIbans, iban)
			}
		}
	}

	req := enablenow.JobRequest{
		Engine:      f.config.Engine,
		Profile:     f.config.Profile,
		CountryCode: f.config.CountryCode,
		MetaData:    &enablenow.JobMetaData{InternalIbans: internalIbans},
	}
	var filter enablenow.JobFilter
	if len(ready) == 1 {
		req.ConsentId = ready[0].Id
		filter.ConsentId = ready[0].Id
	} else {
		req.UserId = session.UserId
		filter.ExternalUserId = session.ExternalUserId
	}

	jobs, err := f.client.ListCategorizationJobs(ctx, filter)
	if err != nil {
		return err
	}
	for _, job := range jobs {
		// a job of a single consent does not cover the other banks
		if req.UserId != "" && job.ConsentId != "" {
			continue
		}
		if job.Engine == f.config.Engine && job.Profile == f.config.Profile && job.Status != enablenow.JobFailed {
			slog.Info("reusing categorization job", "job", job.Id, "job_status", job.Status)
			session.JobId = job.Id
			return nil
		}
	}

	job, err := f.client.CreateCategorizationJob(ctx, req)
	if err != nil {
		return err
	}
	slog.Info("created categorization job", "job", job.Id, "banks", len(ready))
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

	var banks []credentials.Bank
	total := 0
	for _, c := range session.consentsWithStatus(ConsentReady) {
		transactions, err := f.client.ListTransactions(ctx, c.Id, from, to)
		if err != nil {
			return err
		}
		accounts, err := f.client.ListAccounts(ctx, c.Id)
		if err != nil {
			return err
		}
		if len(transactions) > 0 && !anyCategorized(transactions) {
			// the job may not have covered this bank
			slog.Warn("transactions of a bank were not categorized", "provider", c.ProviderKey, "transactions", len(transactions), "job", session.JobId)
		}
		total += len(transactions)
		banks = append(banks, credentials.Bank{ProviderKey: c.ProviderKey, Accounts: accounts, Transactions: transactions})
	}

	if total == 0 {
		session.fail(ErrorNoTransactions, f.now())
		f.cleanup(ctx, session)
		return nil
	}

	session.Credentials = credentials.Compute(banks, opts)
	session.setStatus(StatusReady, f.now())
	slog.Info("computed credentials", "banks", len(banks), "transactions", total)
	f.cleanup(ctx, session)
	return nil
}

func anyCategorized(transactions []enablenow.Transaction) bool {
	for _, tx := range transactions {
		if tx.E1() != "" {
			return true
		}
	}
	return false
}

// cleanup deletes the enablenow user, and with it every consent and all bank
// data, once it is no longer needed, so the bank data does not outlive the
// issuance flow.
func (f *Flow) cleanup(ctx context.Context, session *Session) {
	if !f.config.DeleteConsent || session.UserId == "" {
		return
	}
	if err := f.client.DeleteUser(ctx, session.UserId); err != nil && !enablenow.IsNotFound(err) {
		slog.Error("failed to delete enablenow user", "error", err, "user", logging.MaskIdentifier(session.UserId))
		return
	}
	slog.Info("deleted enablenow user", "user", logging.MaskIdentifier(session.UserId))
}
