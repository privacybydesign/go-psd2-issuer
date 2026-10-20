// Package rules evaluates social security rules ("sociaal domein") on the
// monthly averages in the enablenow credentials, for the demo verifier.
//
// Every law the verifier offers is a Law: the attributes it needs and how it
// evaluates them. The rules follow the texts in regelrecht-corpus
// (https://github.com/MinBZK/regelrecht-corpus), using the versions in force
// on the day of the check. A result is an indication, not a decision.
package rules

import (
	"errors"
	"fmt"
	"slices"
	"time"
	_ "time/tzdata" // the dates of the law are Dutch dates
)

// Source identifies the version of a law or regulation a rule is taken from.
type Source struct {
	// Id is how steps and items refer to the source, e.g. "participatiewet".
	Id      string `json:"id"`
	Name    string `json:"name"`
	Version string `json:"version"`
	Url     string `json:"url"`
}

// Age groups of the applicant, derived from the date of birth.
const (
	// AgeUnder21: the youth norms of art. 20 Participatiewet apply, which are
	// not implemented.
	AgeUnder21 = "under21"
	// AgeAdult: 21 or older, below the state pension age.
	AgeAdult = "adult"
	// AgePension: the state pension age is reached.
	AgePension = "pension"
)

// Household is declared by the user, until it is available as a credential.
type Household struct {
	// Partner: married or living together as if married (art. 3
	// Participatiewet).
	Partner bool `json:"partner"`
	// PartnerPensionAge: the partner has reached the state pension age.
	PartnerPensionAge bool `json:"partner_pension_age"`
	// Housemates counts the cost-sharing housemates of 21 or older, not
	// counting the partner (art. 19a, 22a Participatiewet).
	Housemates int `json:"housemates"`
}

func (h Household) Validate() error {
	if h.Housemates < 0 || h.Housemates > 20 {
		return fmt.Errorf("invalid number of housemates %d", h.Housemates)
	}
	if !h.Partner && h.PartnerPensionAge {
		return errors.New("partner pension age without partner")
	}
	return nil
}

// Facts are the disclosed attributes, keyed "<credential>.<attribute>" (e.g.
// "income.salary"), in whole euros per month. Missing attributes count as 0.
type Facts map[string]int

// Input is everything a law is evaluated on.
type Input struct {
	Household Household
	AgeGroup  string
	Facts     Facts
	// Date is the day of the check; it selects the versions of the laws.
	Date time.Time
	// TaxAssessment is the amount of the tax assessment in euros, for
	// kwijtschelding; 0 when not given.
	TaxAssessment int
}

// Item is one line of an income or expenses calculation.
type Item struct {
	Attribute string `json:"attribute"`
	// Amount in euro cents per month, as disclosed (or as counted, for
	// expenses).
	Amount int64 `json:"amount"`
	// Law and Article are the provision the amount is counted under.
	Law     string `json:"law,omitempty"`
	Article string `json:"article,omitempty"`
	// Reason says why an amount is not counted: "negative" (more paid back
	// than received).
	Reason string `json:"reason,omitempty"`
}

// Step is one line of the calculation of a result.
type Step struct {
	// Key names the step; the frontend translates it.
	Key     string `json:"key"`
	Law     string `json:"law,omitempty"`
	Article string `json:"article,omitempty"`
	// Amount is in euro cents, except for counts and percentages (see Key).
	Amount int64 `json:"amount"`
}

// Outcomes of a law.
const (
	OutcomeIndication   = "indication"
	OutcomePartial      = "partial"
	OutcomeNoIndication = "no_indication"
	OutcomeNotEvaluated = "not_evaluated"
)

type Result struct {
	// Rule is the scheme the result is about, e.g. "bijstand" or "aio".
	Rule    string `json:"rule"`
	Outcome string `json:"outcome"`
	Steps   []Step `json:"steps,omitempty"`
	// Notes are keys of caveats that apply to this result.
	Notes []string `json:"notes,omitempty"`
}

type Evaluation struct {
	Law string `json:"law"`
	// Sources are the versions of the laws the calculation uses.
	Sources []Source `json:"sources"`
	// Counted and Excluded split the disclosed income; Expenses are the
	// costs that are taken into account, if the law has any.
	Counted  []Item `json:"counted"`
	Excluded []Item `json:"excluded"`
	Expenses []Item `json:"expenses,omitempty"`
	// Income is the counted income per month, in euro cents.
	Income int64  `json:"income"`
	Result Result `json:"result"`
}

// Law is a scheme the verifier can check.
type Law struct {
	Id string
	// Attributes are the facts the law needs; the verifier asks for exactly
	// these, all required.
	Attributes []string
	// NeedsTaxAssessment: the user may give the amount of the assessment.
	NeedsTaxAssessment bool
	evaluate           func(Input) (Evaluation, error)
}

func (l Law) Evaluate(input Input) (Evaluation, error) {
	return l.evaluate(input)
}

// Laws are the laws the verifier offers, in the order they are shown.
var Laws = []Law{bijstandLaw, minimaLaw, kwijtscheldingGemeenteLaw, kwijtscheldingHhnkLaw}

// LawById returns the law with the given id.
func LawById(id string) (Law, bool) {
	i := slices.IndexFunc(Laws, func(l Law) bool { return l.Id == id })
	if i == -1 {
		return Law{}, false
	}
	return Laws[i], true
}

var amsterdam = must(time.LoadLocation("Europe/Amsterdam"))

func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}

// dutchDate formats the (Dutch) date of t as YYYY-MM-DD.
func dutchDate(t time.Time) string {
	return t.In(amsterdam).Format(time.DateOnly)
}

// incomeRule says that an attribute is income, under which provision.
type incomeRule struct {
	attribute string
	law       string
	article   string
}

func attributesOf(rules []incomeRule) []string {
	attributes := make([]string, len(rules))
	for i, r := range rules {
		attributes[i] = r.attribute
	}
	return attributes
}

// countIncome adds up the positive amounts of the income rules; a negative
// net amount means more was paid (back) than received, which is no income.
func countIncome(evaluation *Evaluation, rules []incomeRule, facts Facts) {
	for _, r := range rules {
		value := facts[r.attribute]
		item := Item{Attribute: r.attribute, Amount: int64(value) * 100}
		switch {
		case value > 0:
			item.Law, item.Article = r.law, r.article
			evaluation.Counted = append(evaluation.Counted, item)
			evaluation.Income += item.Amount
		case value < 0:
			item.Reason = "negative"
			evaluation.Excluded = append(evaluation.Excluded, item)
		}
	}
}

func newEvaluation(law string, sources ...Source) Evaluation {
	return Evaluation{Law: law, Sources: sources, Counted: []Item{}, Excluded: []Item{}}
}
