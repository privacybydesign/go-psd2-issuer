package enablenow

import (
	"context"
	"crypto/rand"
	"fmt"
	"maps"
	"net/url"
	"strings"
	"sync"
	"time"
)

// FakeClient is an in-memory stand-in for enablenow. Its AppScreen URL skips
// the bank entirely and redirects straight back with a new consent id, and
// every consent holds a year of synthetic, already categorized transactions.
// It is used for local development and tests; never in production.
type FakeClient struct {
	mutex    sync.Mutex
	consents map[string]*Consent
	jobs     map[string]*Job
	// JobDuration is how long a categorization job stays in progress.
	JobDuration time.Duration
	jobStarted  map[string]time.Time
	Now         func() time.Time
	// Deleted records the consents that were deleted.
	Deleted []string
}

func NewFakeClient() *FakeClient {
	return &FakeClient{
		consents:    map[string]*Consent{},
		jobs:        map[string]*Job{},
		jobStarted:  map[string]time.Time{},
		JobDuration: 2 * time.Second,
		Now:         time.Now,
	}
}

func newId() string {
	return rand.Text()
}

// CreateConsent registers a new, fully synced consent and returns its id.
func (f *FakeClient) CreateConsent() string {
	f.mutex.Lock()
	defer f.mutex.Unlock()
	id := newId()
	now := Time{f.Now()}
	f.consents[id] = &Consent{
		Id:                      id,
		Status:                  ConsentAuthenticated,
		ProviderKey:             "fake-bank",
		DateRequested:           &now,
		DateTransactionsUpdated: &now,
	}
	return id
}

func (f *FakeClient) ConsentURL(redirectURI string, extraParams url.Values) string {
	params := url.Values{}
	maps.Copy(params, extraParams)
	params.Set("consent_id", f.CreateConsent())
	separator := "?"
	if strings.Contains(redirectURI, "?") {
		separator = "&"
	}
	return redirectURI + separator + params.Encode()
}

func (f *FakeClient) GetConsent(_ context.Context, consentId string) (*Consent, error) {
	f.mutex.Lock()
	defer f.mutex.Unlock()
	consent, ok := f.consents[consentId]
	if !ok {
		return nil, &ApiError{Method: "GET", Path: "/v2/consents/" + consentId, StatusCode: 404}
	}
	c := *consent
	return &c, nil
}

func (f *FakeClient) DeleteConsent(_ context.Context, consentId string) error {
	f.mutex.Lock()
	defer f.mutex.Unlock()
	if _, ok := f.consents[consentId]; !ok {
		return &ApiError{Method: "DELETE", Path: "/v2/consents/" + consentId, StatusCode: 404}
	}
	delete(f.consents, consentId)
	f.Deleted = append(f.Deleted, consentId)
	return nil
}

func (f *FakeClient) ListAccounts(ctx context.Context, consentId string) ([]Account, error) {
	if _, err := f.GetConsent(ctx, consentId); err != nil {
		return nil, err
	}
	return []Account{
		{Id: "acc-1", Iban: "NL91ABNA0417164300", AccountHolder: "J. Jansen", Currency: "EUR"},
		{Id: "acc-2", Iban: "NL20INGB0001234567", AccountHolder: "J. Jansen", Currency: "EUR"},
	}, nil
}

type fakeMonthly struct {
	day      int
	amount   float64
	category string
	account  string
}

// fakeMonthlyTransactions is one month of a fictional household.
var fakeMonthlyTransactions = []fakeMonthly{
	{25, 3215.40, "income.salary", "acc-1"},
	{20, 154.00, "subsidy.health", "acc-1"},
	{20, 95.00, "subsidy.rent", "acc-1"},
	{1, -1050.00, "housing.rent", "acc-1"},
	{3, -138.50, "insurance.health", "acc-1"},
	{5, -110.00, "expense.essential.utilities.energy", "acc-1"},
	{5, -22.75, "expense.essential.utilities.water", "acc-1"},
	{8, -45.00, "expense.essential.telecom", "acc-1"},
	{10, -31.00, "expense.essential.municipal", "acc-1"},
	{12, -125.00, "loan.student", "acc-1"},
	{14, -62.40, "expense.essential.groceries", "acc-1"},
	{21, -88.15, "expense.essential.groceries", "acc-1"},
	{16, -12.99, "expense.lifestyle.streaming", "acc-1"},
	{18, -42.00, "expense.lifestyle.restaurants_bars", "acc-1"},
	{26, -400.00, "internal.other", "acc-1"},
	{26, 400.00, "internal.other", "acc-2"},
}

// transactions returns the synthetic transactions booked in [from, to).
func (f *FakeClient) transactions(from, to time.Time) []Transaction {
	var txs []Transaction
	start := time.Date(from.Year(), from.Month(), 1, 0, 0, 0, 0, time.UTC)
	for month := start; month.Before(to); month = month.AddDate(0, 1, 0) {
		for i, m := range fakeMonthlyTransactions {
			date := time.Date(month.Year(), month.Month(), m.day, 0, 0, 0, 0, time.UTC)
			if date.Before(from) || !date.Before(to) {
				continue
			}
			txs = append(txs, Transaction{
				Id:        fmt.Sprintf("%s-%d", date.Format("2006-01"), i),
				AccountId: m.account,
				BookDate:  date.Format("2006-01-02"),
				Amount:    m.amount,
				Currency:  "EUR",
				Category:  &TransactionCategory{E1: &E1Category{Category: m.category, Confidence: 0.97}},
			})
		}
	}
	return txs
}

func (f *FakeClient) ListTransactions(ctx context.Context, consentId string, from, to time.Time) ([]Transaction, error) {
	if _, err := f.GetConsent(ctx, consentId); err != nil {
		return nil, err
	}
	return f.transactions(from, to), nil
}

func (f *FakeClient) ListCategorizationJobs(_ context.Context, consentId string) ([]Job, error) {
	f.mutex.Lock()
	defer f.mutex.Unlock()
	var jobs []Job
	for _, job := range f.jobs {
		if job.ConsentId == consentId {
			jobs = append(jobs, *f.progress(job))
		}
	}
	return jobs, nil
}

func (f *FakeClient) CreateCategorizationJob(_ context.Context, req JobRequest) (*Job, error) {
	f.mutex.Lock()
	defer f.mutex.Unlock()
	if _, ok := f.consents[req.ConsentId]; !ok {
		return nil, &ApiError{Method: "POST", Path: "/v2/categorization-jobs", StatusCode: 400, Body: "unknown consent"}
	}
	job := &Job{
		Id:          newId(),
		Status:      JobPending,
		Engine:      req.Engine,
		Profile:     req.Profile,
		CountryCode: req.CountryCode,
		ConsentId:   req.ConsentId,
	}
	f.jobs[job.Id] = job
	f.jobStarted[job.Id] = f.Now()
	j := *job
	return &j, nil
}

func (f *FakeClient) GetCategorizationJob(_ context.Context, jobId string) (*Job, error) {
	f.mutex.Lock()
	defer f.mutex.Unlock()
	job, ok := f.jobs[jobId]
	if !ok {
		return nil, &ApiError{Method: "GET", Path: "/v2/categorization-jobs/" + jobId, StatusCode: 404}
	}
	return f.progress(job), nil
}

// progress advances a job's status based on how long ago it was started.
func (f *FakeClient) progress(job *Job) *Job {
	if job.Status == JobPending || job.Status == JobInProgress {
		if f.Now().Sub(f.jobStarted[job.Id]) >= f.JobDuration {
			job.Status = JobCompleted
		} else {
			job.Status = JobInProgress
		}
	}
	j := *job
	return &j
}
