package enablenow

import "time"

// Consent statuses as documented on
// https://docs.enablenow.nl/account-information/data-structures
const (
	ConsentInitialized    = "INITIALIZED"
	ConsentAuthenticating = "AUTHENTICATING"
	ConsentAuthenticated  = "AUTHENTICATED"
	ConsentUpdating       = "UPDATING"
	ConsentUpdated        = "UPDATED"
	ConsentFailing        = "FAILING"
	ConsentInvalidated    = "INVALIDATED"
	ConsentError          = "ERROR"
	ConsentTimedOut       = "TIMEDOUT"
)

// Categorization job statuses as documented on
// https://docs.enablenow.nl/categorization/jobs/monitoring-jobs
const (
	JobPending    = "PENDING"
	JobInProgress = "INPROGRESS"
	JobCompleted  = "COMPLETED"
	JobFailed     = "FAILED"
)

type Consent struct {
	Id                      string `json:"id"`
	Status                  string `json:"status"`
	ProviderKey             string `json:"providerKey"`
	DateStatusUpdated       *Time  `json:"dateStatusUpdated"`
	DateRequested           *Time  `json:"dateRequested"`
	DateExpire              *Time  `json:"dateExpire"`
	DateTransactionsUpdated *Time  `json:"dateTransactionsUpdated"`
}

// IsFailed reports whether the consent ended up in a state from which no
// transactions will become available anymore.
func (c *Consent) IsFailed() bool {
	switch c.Status {
	case ConsentInvalidated, ConsentError, ConsentTimedOut:
		return true
	}
	return false
}

// HasTransactions reports whether the bank connection is established and the
// transactions have been fetched at least once.
func (c *Consent) HasTransactions() bool {
	if c.DateTransactionsUpdated == nil {
		return false
	}
	return c.Status == ConsentAuthenticated || c.Status == ConsentUpdated
}

type Balance struct {
	Currency string  `json:"currency"`
	Date     *Time   `json:"date"`
	Amount   float64 `json:"amount"`
}

type Account struct {
	Id                string   `json:"id"`
	Iban              string   `json:"iban"`
	ProviderAccountId string   `json:"providerAccountId"`
	AccountHolder     string   `json:"accountHolder"`
	Bban              string   `json:"bban"`
	Currency          string   `json:"currency"`
	AccountBalance    *Balance `json:"accountBalance"`
}

// E1Category is the output of the E1 categorization engine for one transaction.
// https://docs.enablenow.nl/categorization/engines/e1-engine/categories
type E1Category struct {
	Category   string   `json:"category"`
	Confidence float64  `json:"confidence"`
	Flags      []string `json:"flags"`
}

type TransactionCategory struct {
	E1 *E1Category `json:"e1"`
}

type Transaction struct {
	Id                       string               `json:"id"`
	AccountId                string               `json:"accountId"`
	AccountNumber            string               `json:"accountNumber"`
	Description              string               `json:"description"`
	BookDate                 string               `json:"bookDate"`
	TransactionDateTime      *Time                `json:"transactionDateTime"`
	Amount                   float64              `json:"amount"`
	Currency                 string               `json:"currency"`
	CounterpartDescription   string               `json:"counterpartDescription"`
	CounterpartAccountNumber string               `json:"counterpartAccountNumber"`
	Category                 *TransactionCategory `json:"category"`
}

// E1 returns the E1 category code of the transaction, or an empty string when
// the transaction was not categorized by the E1 engine.
func (t *Transaction) E1() string {
	if t.Category == nil || t.Category.E1 == nil {
		return ""
	}
	return t.Category.E1.Category
}

// Date returns the booking date of the transaction, falling back to the
// transaction timestamp when the book date is missing or malformed.
func (t *Transaction) Date() (time.Time, bool) {
	if t.BookDate != "" {
		for _, layout := range []string{"2006-01-02", time.RFC3339, "2006-01-02T15:04:05"} {
			if d, err := time.Parse(layout, t.BookDate); err == nil {
				return d, true
			}
		}
	}
	if t.TransactionDateTime != nil {
		return t.TransactionDateTime.Time, true
	}
	return time.Time{}, false
}

// JobRequest creates a categorization job over either one consent or all
// consents of a user.
type JobRequest struct {
	Engine      string       `json:"engine"`
	Profile     string       `json:"profile"`
	CountryCode string       `json:"countryCode,omitempty"`
	ConsentId   string       `json:"consentId,omitempty"`
	UserId      string       `json:"userId,omitempty"`
	MetaData    *JobMetaData `json:"metaData,omitempty"`
}

type JobMetaData struct {
	// InternalIbans are the user's own accounts, so transfers between them
	// are not mistaken for income or expenses.
	InternalIbans []string `json:"internal_ibans,omitempty"`
}

// JobFilter selects the categorization jobs of a consent or of a user.
type JobFilter struct {
	ConsentId      string
	ExternalUserId string
}

type Job struct {
	Id            string `json:"id"`
	Status        string `json:"status"`
	Engine        string `json:"engine"`
	Profile       string `json:"profile"`
	CountryCode   string `json:"countryCode"`
	EngineVersion string `json:"engineVersion"`
	ConsentId     string `json:"consentId"`
	UserId        string `json:"userId"`
}

// User groups the consents of one end-user, so the accounts of several banks
// can be categorized together.
// https://docs.enablenow.nl/account-information/data-access/user-based-access
type User struct {
	Id             string `json:"id"`
	ExternalUserId string `json:"externalUserId"`
}
