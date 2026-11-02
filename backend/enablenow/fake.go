package enablenow

import (
	"context"
	"crypto/rand"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"
)

// FakeClient is an in-memory stand-in for enablenow. Its AppScreen URL skips
// the bank entirely and redirects straight back with a new consent id, and
// every consent holds a year of synthetic, already categorized transactions.
// The first bank of a user is a household's main bank; every further bank
// holds a side income and a transfer from the main bank.
// It is used for local development and tests; never in production.
type FakeClient struct {
	mutex    sync.Mutex
	consents map[string]*fakeConsent
	users    map[string]*User
	jobs     map[string]*Job
	// JobDuration is how long a categorization job stays in progress.
	JobDuration time.Duration
	jobStarted  map[string]time.Time
	Now         func() time.Time
	// Deleted records the consents that were deleted.
	Deleted []string
	// DeletedUsers records the users that were deleted.
	DeletedUsers []string
	// JobRequests records the categorization jobs that were created.
	JobRequests []JobRequest
}

type fakeConsent struct {
	Consent
	userId string
	// bank is the position of the consent among the user's consents; it
	// selects the synthetic data set.
	bank int
}

func NewFakeClient() *FakeClient {
	return &FakeClient{
		consents:    map[string]*fakeConsent{},
		users:       map[string]*User{},
		jobs:        map[string]*Job{},
		jobStarted:  map[string]time.Time{},
		JobDuration: 2 * time.Second,
		Now:         time.Now,
	}
}

func newId() string {
	return rand.Text()
}

func (f *FakeClient) CreateUser(_ context.Context, externalUserId string) (*User, error) {
	f.mutex.Lock()
	defer f.mutex.Unlock()
	user := &User{Id: newId(), ExternalUserId: externalUserId}
	f.users[user.Id] = user
	u := *user
	return &u, nil
}

func (f *FakeClient) DeleteUser(_ context.Context, userId string) error {
	f.mutex.Lock()
	defer f.mutex.Unlock()
	if _, ok := f.users[userId]; !ok {
		return &ApiError{Method: "DELETE", Path: "/Users", StatusCode: 404}
	}
	delete(f.users, userId)
	for id, consent := range f.consents {
		if consent.userId == userId {
			delete(f.consents, id)
		}
	}
	f.DeletedUsers = append(f.DeletedUsers, userId)
	return nil
}

// CreateConsent registers a new, fully synced consent without a user and
// returns its id.
func (f *FakeClient) CreateConsent() string {
	f.mutex.Lock()
	defer f.mutex.Unlock()
	return f.createConsent("", "")
}

func (f *FakeClient) createConsent(userId, providerKey string) string {
	bank := 0
	for _, c := range f.consents {
		if userId != "" && c.userId == userId {
			bank++
		}
	}
	if providerKey == "" {
		providerKey = "fake-bank"
		if bank > 0 {
			providerKey = fmt.Sprintf("fake-bank-%d", bank+1)
		}
	}
	id := newId()
	now := Time{f.Now()}
	f.consents[id] = &fakeConsent{
		Id:                      id,
		Status:                  ConsentAuthenticated,
		ProviderKey:             providerKey,
		DateRequested:           &now,
		DateTransactionsUpdated: &now,
		userId:                  userId,
		bank:                    bank,
	}
	return id
}

// UpdateConsent changes a consent, e.g. to let it fail.
func (f *FakeClient) UpdateConsent(consentId string, update func(*Consent)) {
	f.mutex.Lock()
	defer f.mutex.Unlock()
	if consent, ok := f.consents[consentId]; ok {
		update(&consent.Consent)
	}
}

// ConsentURL creates the consent right away and returns the redirect back.
// The AppScreen parameters user_id and provider are honoured.
func (f *FakeClient) ConsentURL(redirectURI string, params url.Values) string {
	f.mutex.Lock()
	id := f.createConsent(params.Get("user_id"), params.Get("provider"))
	f.mutex.Unlock()
	separator := "?"
	if strings.Contains(redirectURI, "?") {
		separator = "&"
	}
	return redirectURI + separator + url.Values{"consent_id": {id}}.Encode()
}

func (f *FakeClient) consent(consentId string) (*fakeConsent, error) {
	f.mutex.Lock()
	defer f.mutex.Unlock()
	consent, ok := f.consents[consentId]
	if !ok {
		return nil, &ApiError{Method: "GET", Path: "/v2/consents/" + consentId, StatusCode: 404}
	}
	c := *consent
	return &c, nil
}

func (f *FakeClient) GetConsent(_ context.Context, consentId string) (*Consent, error) {
	consent, err := f.consent(consentId)
	if err != nil {
		return nil, err
	}
	return &consent.Consent, nil
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

func (f *FakeClient) ListAccounts(_ context.Context, consentId string) ([]Account, error) {
	consent, err := f.consent(consentId)
	if err != nil {
		return nil, err
	}
	if consent.bank == 0 {
		return []Account{
			{Id: "acc-1", Iban: "NL91ABNA0417164300", AccountHolder: "J. Jansen", Currency: "EUR"},
			{Id: "acc-2", Iban: "NL20INGB0001234567", AccountHolder: "J. Jansen", Currency: "EUR"},
		}, nil
	}
	return []Account{{Id: sideAccountId(consent.bank), Iban: sideIban(consent.bank), AccountHolder: "J. Jansen", Currency: "EUR"}}, nil
}

func sideAccountId(bank int) string {
	return fmt.Sprintf("acc-%d-1", bank+1)
}

func sideIban(bank int) string {
	if bank == 1 {
		return "NL02RABO0123456789"
	}
	return fmt.Sprintf("NL%02dFAKE%010d", bank, bank)
}

type fakeMonthly struct {
	day         int
	amount      float64
	category    string
	account     string
	counterpart string
}

// fakeMonthlyTransactions is one month of a fictional household.
var fakeMonthlyTransactions = []fakeMonthly{
	{25, 3215.40, "income.salary", "acc-1", ""},
	{20, 154.00, "subsidy.health", "acc-1", ""},
	{20, 95.00, "subsidy.rent", "acc-1", ""},
	{1, -1050.00, "housing.rent", "acc-1", ""},
	{3, -138.50, "insurance.health", "acc-1", ""},
	{5, -110.00, "expense.essential.utilities.energy", "acc-1", ""},
	{5, -22.75, "expense.essential.utilities.water", "acc-1", ""},
	{8, -45.00, "expense.essential.telecom", "acc-1", ""},
	{10, -31.00, "expense.essential.municipal", "acc-1", ""},
	{12, -125.00, "loan.student", "acc-1", ""},
	{14, -62.40, "expense.essential.groceries", "acc-1", ""},
	{21, -88.15, "expense.essential.groceries", "acc-1", ""},
	{16, -12.99, "expense.lifestyle.streaming", "acc-1", ""},
	{18, -42.00, "expense.lifestyle.restaurants_bars", "acc-1", ""},
	{6, -64.20, "expense.transport.fuel", "acc-1", ""},
	{3, -41.75, "insurance.vehicle", "acc-1", ""},
	{4, -27.00, "tax.vehicle", "acc-1", ""},
	{27, -150.00, "investment.savings", "acc-1", ""},
	{26, -400.00, "internal.other", "acc-1", ""},
	{26, 400.00, "internal.other", "acc-2", ""},
}

// fakeSideTransactions is one month at any further bank. The transfer comes
// from the main bank, but E1 does not know that account is the user's own.
var fakeSideTransactions = []fakeMonthly{
	{15, 150.00, "income.other", "", ""},
	{9, -42.00, "expense.lifestyle.sports", "", ""},
	{27, 250.00, "other.private_transfer", "", "NL91ABNA0417164300"},
}

// transactions returns the synthetic transactions of the consent's bank
// booked in [from, to).
func transactions(bank int, from, to time.Time) []Transaction {
	monthly := fakeMonthlyTransactions
	if bank > 0 {
		monthly = fakeSideTransactions
	}
	var txs []Transaction
	start := time.Date(from.Year(), from.Month(), 1, 0, 0, 0, 0, time.UTC)
	for month := start; month.Before(to); month = month.AddDate(0, 1, 0) {
		for i, m := range monthly {
			account := m.account
			if bank > 0 {
				account = sideAccountId(bank)
			}
			date := time.Date(month.Year(), month.Month(), m.day, 0, 0, 0, 0, time.UTC)
			if date.Before(from) || !date.Before(to) {
				continue
			}
			txs = append(txs, Transaction{
				Id:                       fmt.Sprintf("%d-%s-%d", bank, date.Format("2006-01"), i),
				AccountId:                account,
				BookDate:                 date.Format("2006-01-02"),
				Amount:                   m.amount,
				Currency:                 "EUR",
				CounterpartAccountNumber: m.counterpart,
				Category:                 &TransactionCategory{E1: &E1Category{Category: m.category, Confidence: 0.97}},
			})
		}
	}
	return txs
}

func (f *FakeClient) ListTransactions(_ context.Context, consentId string, from, to time.Time) ([]Transaction, error) {
	consent, err := f.consent(consentId)
	if err != nil {
		return nil, err
	}
	return transactions(consent.bank, from, to), nil
}

// ListCategorizationJobs finds consent jobs by consent and user jobs by the
// user's external id.
func (f *FakeClient) ListCategorizationJobs(_ context.Context, filter JobFilter) ([]Job, error) {
	f.mutex.Lock()
	defer f.mutex.Unlock()
	var jobs []Job
	for _, job := range f.jobs {
		byConsent := filter.ConsentId != "" && job.ConsentId == filter.ConsentId
		user, ok := f.users[job.UserId]
		byUser := filter.ExternalUserId != "" && ok && user.ExternalUserId == filter.ExternalUserId
		if byConsent || byUser {
			jobs = append(jobs, *f.progress(job))
		}
	}
	return jobs, nil
}

func (f *FakeClient) CreateCategorizationJob(_ context.Context, req JobRequest) (*Job, error) {
	f.mutex.Lock()
	defer f.mutex.Unlock()
	_, consentOk := f.consents[req.ConsentId]
	_, userOk := f.users[req.UserId]
	if !consentOk && !userOk {
		return nil, &ApiError{Method: "POST", Path: "/v2/categorization-jobs", StatusCode: 400, Body: "unknown consent or user"}
	}
	f.JobRequests = append(f.JobRequests, req)
	job := &Job{
		Id:          newId(),
		Status:      JobPending,
		Engine:      req.Engine,
		Profile:     req.Profile,
		CountryCode: req.CountryCode,
		ConsentId:   req.ConsentId,
		UserId:      req.UserId,
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
