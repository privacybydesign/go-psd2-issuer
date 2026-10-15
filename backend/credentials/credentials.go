// Package credentials turns E1-categorized bank transactions into a set of
// small, independent Yivi credentials. Each credential answers one question a
// verifier may have (what do you earn, what are your fixed costs, do you have
// debts, do you receive benefits, which account is yours) so a user only ever
// discloses what is needed.
//
// All amounts are monthly averages in whole euros, rounded to Options.Rounding,
// and computed over the full calendar months in the analysis period.
package credentials

import (
	"fmt"
	"math"
	"strings"
	"time"

	"psd2-issuer/enablenow"
)

// Credential keys. They double as the last part of the Yivi credential id
// (e.g. irma-demo.enablenow.income).
const (
	Income      = "income"
	Housing     = "housing"
	Debts       = "debts"
	Benefits    = "benefits"
	BankAccount = "bankaccount"
)

// AllKeys lists every credential this package can produce, in display order.
var AllKeys = []string{Income, Housing, Debts, Benefits, BankAccount}

// Credential is one credential instance ready to be issued.
type Credential struct {
	Key        string            `json:"key"`
	Attributes map[string]string `json:"attributes"`
}

type Options struct {
	// Months is the length of the analysis window in full calendar months.
	Months int
	// Rounding rounds every amount to a multiple of this many euros.
	Rounding int
	// MinConfidence ignores categorizations with a lower confidence score.
	MinConfidence float64
	// Now is the moment of analysis; the window ends at the start of its month.
	Now time.Time
}

const (
	yes = "yes"
	no  = "no"
)

// Window returns the analysis window [from, to): the last opts.Months full
// calendar months before opts.Now.
func Window(opts Options) (from, to time.Time) {
	now := opts.Now.UTC()
	to = time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	from = to.AddDate(0, -opts.Months, 0)
	return from, to
}

// Compute derives all credentials from the accounts and transactions of a
// consent. providerKey identifies the bank. Transactions outside the window,
// in a foreign currency or without an E1 category are ignored.
func Compute(accounts []enablenow.Account, transactions []enablenow.Transaction, providerKey string, opts Options) []Credential {
	if opts.Months <= 0 {
		opts.Months = 12
	}
	if opts.Rounding <= 0 {
		opts.Rounding = 1
	}

	p := newPeriod(transactions, opts)
	totals := p.totals

	months := float64(p.months)
	avg := func(amount float64) string {
		return roundAmount(amount/months, opts.Rounding)
	}
	common := func(attrs map[string]string) map[string]string {
		attrs["months"] = fmt.Sprint(p.months)
		attrs["periodstart"] = p.start.Format("2006-01-02")
		attrs["periodend"] = p.end.AddDate(0, 0, -1).Format("2006-01-02")
		return attrs
	}

	// income: everything flows in, so the net inflow per category counts
	salary := totals.inflow("income.salary")
	pension := totals.inflow("income.pension")
	otherIncome := totals.inflow("income.rent") + totals.inflow("income.other")
	income := common(map[string]string{
		"salary":       avg(salary),
		"pension":      avg(pension),
		"otherincome":  avg(otherIncome),
		"total":        avg(salary + pension + otherIncome),
		"salarymonths": fmt.Sprint(p.monthsWith("income.salary")),
	})

	// housing and fixed costs
	rent := totals.outflow("housing.rent")
	mortgage := totals.outflow("housing.mortgage")
	otherHousing := totals.outflow("housing.other")
	housingCost := rent + mortgage + otherHousing
	energy := totals.outflow("expense.essential.utilities.energy")
	water := totals.outflow("expense.essential.utilities.water")
	otherUtilities := totals.outflow("expense.essential.utilities.other")
	insurance := totals.outflow("insurance")
	telecom := totals.outflow("expense.essential.telecom")
	municipal := totals.outflow("expense.essential.municipal")
	housing := common(map[string]string{
		"housingtype":    housingType(rent, mortgage, otherHousing),
		"housingcost":    avg(housingCost),
		"energy":         avg(energy),
		"water":          avg(water + otherUtilities),
		"insurance":      avg(insurance),
		"telecom":        avg(telecom),
		"municipaltaxes": avg(municipal),
		"total":          avg(housingCost + energy + water + otherUtilities + insurance + telecom + municipal),
	})

	// debts and risk signals
	repayments := totals.outflow("loan") - totals.outflow("loan.debt_collection")
	debts := common(map[string]string{
		"loanrepayments": avg(repayments),
		"studentloan":    avg(totals.outflow("loan.student")),
		"debtcollection": yesNo(totals.any("loan.debt_collection")),
		"flashloan":      yesNo(totals.any("loan.flash")),
		"bnpl":           yesNo(totals.any("loan.bnpl")),
		"gambling":       yesNo(totals.any("gambling")),
	})

	// benefits (toeslagen and uitkeringen)
	health := totals.inflow("subsidy.health")
	rentSubsidy := totals.inflow("subsidy.rent")
	child := totals.inflow("subsidy.family")
	unemployment := totals.inflow("subsidy.unemployment")
	social := totals.inflow("subsidy.social")
	disability := totals.inflow("subsidy.disability")
	student := totals.inflow("subsidy.student")
	otherBenefits := totals.inflow("subsidy.other")
	allBenefits := health + rentSubsidy + child + unemployment + social + disability + student + otherBenefits
	benefits := common(map[string]string{
		"receivesbenefits": yesNo(allBenefits > 0),
		"healthcare":       avg(health),
		"rent":             avg(rentSubsidy),
		"child":            avg(child),
		"unemployment":     avg(unemployment),
		"social":           avg(social),
		"disability":       avg(disability),
		"student":          avg(student),
		"other":            avg(otherBenefits),
		"total":            avg(allBenefits),
	})

	creds := []Credential{
		{Key: Income, Attributes: income},
		{Key: Housing, Attributes: housing},
		{Key: Debts, Attributes: debts},
		{Key: Benefits, Attributes: benefits},
	}

	// one bank account credential per account with an IBAN
	seen := map[string]bool{}
	for _, account := range accounts {
		iban := normalizeIban(account.Iban)
		if iban == "" || seen[iban] {
			continue
		}
		seen[iban] = true
		creds = append(creds, Credential{Key: BankAccount, Attributes: map[string]string{
			"iban":          iban,
			"accountholder": strings.TrimSpace(account.AccountHolder),
			"bank":          providerKey,
		}})
	}

	return creds
}

// categoryTotals sums transaction amounts per E1 category code.
type categoryTotals map[string]float64

// sum returns the total of all categories equal to or below prefix in the
// dot separated hierarchy, e.g. "loan" includes "loan.student".
func (t categoryTotals) sum(prefix string) float64 {
	total := 0.0
	for code, amount := range t {
		if matches(code, prefix) {
			total += amount
		}
	}
	return total
}

// inflow is the net amount received in the category, never negative.
// Repayments of e.g. overpaid benefits keep the benefit category with a
// negative amount, so they are netted against the inflow.
func (t categoryTotals) inflow(prefix string) float64 {
	return math.Max(0, t.sum(prefix))
}

// outflow is the net amount spent in the category, never negative. Refunds
// and reversals keep the category of the original payment, so they are
// netted against the spending.
func (t categoryTotals) outflow(prefix string) float64 {
	return math.Max(0, -t.sum(prefix))
}

// any reports whether there was any money movement in the category.
func (t categoryTotals) any(prefix string) bool {
	for code, amount := range t {
		if matches(code, prefix) && amount != 0 {
			return true
		}
	}
	return false
}

func matches(code, prefix string) bool {
	return code == prefix || strings.HasPrefix(code, prefix+".")
}

// period is the part of the analysis window covered by the bank history.
type period struct {
	start, end time.Time
	months     int
	totals     categoryTotals
	// perMonth holds the totals per month (keyed by "2006-01").
	perMonth map[string]categoryTotals
}

func newPeriod(transactions []enablenow.Transaction, opts Options) *period {
	from, to := Window(opts)

	type dated struct {
		date     time.Time
		amount   float64
		category string
	}
	var relevant []dated
	var earliest time.Time
	for _, tx := range transactions {
		date, ok := tx.Date()
		if !ok || date.Before(from) || !date.Before(to) {
			continue
		}
		if earliest.IsZero() || date.Before(earliest) {
			earliest = date
		}
		if tx.Currency != "" && !strings.EqualFold(tx.Currency, "EUR") {
			continue
		}
		code := tx.E1()
		if code == "" || tx.Category.E1.Confidence < opts.MinConfidence {
			continue
		}
		relevant = append(relevant, dated{date: date, amount: tx.Amount, category: code})
	}

	// When the bank history starts after the window does, the first month is
	// only partially covered; skip it so it doesn't drag the averages down.
	start := from
	if !earliest.IsZero() {
		firstMonth := time.Date(earliest.Year(), earliest.Month(), 1, 0, 0, 0, 0, time.UTC)
		if firstMonth.After(from) {
			start = firstMonth
			if earliest.After(firstMonth) && firstMonth.AddDate(0, 1, 0).Before(to) {
				start = firstMonth.AddDate(0, 1, 0)
			}
		}
	}

	p := &period{
		start:    start,
		end:      to,
		totals:   categoryTotals{},
		perMonth: map[string]categoryTotals{},
	}
	for m := start; m.Before(to); m = m.AddDate(0, 1, 0) {
		p.months++
	}
	if p.months == 0 {
		p.months = 1
	}
	for _, tx := range relevant {
		if tx.date.Before(start) {
			continue
		}
		p.totals[tx.category] += tx.amount
		key := tx.date.Format("2006-01")
		if p.perMonth[key] == nil {
			p.perMonth[key] = categoryTotals{}
		}
		p.perMonth[key][tx.category] += tx.amount
	}
	return p
}

// monthsWith counts the months with a net inflow in the category.
func (p *period) monthsWith(prefix string) int {
	count := 0
	for _, totals := range p.perMonth {
		if totals.inflow(prefix) > 0 {
			count++
		}
	}
	return count
}

func housingType(rent, mortgage, other float64) string {
	switch {
	case rent == 0 && mortgage == 0 && other == 0:
		return "none"
	case mortgage > rent && mortgage >= other:
		return "mortgage"
	case rent >= mortgage && rent >= other:
		return "rent"
	default:
		return "other"
	}
}

func yesNo(b bool) string {
	if b {
		return yes
	}
	return no
}

// roundAmount rounds a euro amount to the nearest multiple of step.
func roundAmount(amount float64, step int) string {
	s := float64(step)
	return fmt.Sprint(int64(math.Round(amount/s) * s))
}

func normalizeIban(iban string) string {
	return strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(iban), " ", ""))
}
