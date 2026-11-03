// Package credentials turns E1-categorized bank transactions into a set of
// Yivi credentials that mirror the E1 data model: one credential per E1
// top-level group (income, housing, subsidy, loan, ...) with one attribute
// per E1 category code. The definitions live in definitions.json.
//
// Every amount is the signed net monthly average in whole euros over the full
// calendar months in the analysis period, rounded to Options.Rounding:
// positive means received, negative means paid.
package credentials

import (
	"fmt"
	"math"
	"strings"
	"time"

	"psd2-issuer/enablenow"
)

// BankAccount is the key of the credential that is issued once per account.
const BankAccount = "bankaccount"

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

// Window returns the analysis window [from, to): the last opts.Months full
// calendar months before opts.Now.
func Window(opts Options) (from, to time.Time) {
	now := opts.Now.UTC()
	to = time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	from = to.AddDate(0, -opts.Months, 0)
	return from, to
}

// Bank holds the accounts and transactions of one consent.
type Bank struct {
	// ProviderKey identifies the bank, e.g. RABONL2U.
	ProviderKey  string
	Accounts     []enablenow.Account
	Transactions []enablenow.Transaction
}

// Compute derives all credentials from the accounts and transactions of one
// or more banks of the same user. Transactions outside the window, in a
// foreign currency or without an E1 category are ignored, as are E1 codes that
// are not in the definitions (internal.other, corporate codes) and transfers
// between the user's accounts at different banks.
func Compute(banks []Bank, opts Options) []Credential {
	if opts.Months <= 0 {
		opts.Months = 12
	}
	if opts.Rounding <= 0 {
		opts.Rounding = 1
	}

	p := newPeriod(banks, opts)
	months := float64(p.months)

	var creds []Credential
	for _, def := range Defs.Credentials {
		if def.E1Group == "" {
			continue
		}
		attrs := map[string]string{
			"months":      fmt.Sprint(p.months),
			"periodstart": p.start.Format("2006-01-02"),
			"periodend":   p.end.AddDate(0, 0, -1).Format("2006-01-02"),
		}
		for _, attr := range def.Attributes {
			attrs[attr.Id] = roundAmount(p.totals[attr.E1]/months, opts.Rounding)
		}
		creds = append(creds, Credential{Key: def.Key, Attributes: attrs})
	}

	// one bank account credential per account with an IBAN
	seen := map[string]bool{}
	for _, bank := range banks {
		for _, account := range bank.Accounts {
			iban := NormalizeIban(account.Iban)
			if iban == "" || seen[iban] {
				continue
			}
			seen[iban] = true
			creds = append(creds, Credential{Key: BankAccount, Attributes: map[string]string{
				"iban":          iban,
				"accountholder": strings.TrimSpace(account.AccountHolder),
				"bank":          bank.ProviderKey,
			}})
		}
	}

	return creds
}

// period is the part of the analysis window covered by the bank history.
type period struct {
	start, end time.Time
	months     int
	// totals sums the amounts per E1 category code.
	totals map[string]float64
}

func newPeriod(banks []Bank, opts Options) *period {
	from, to := Window(opts)

	// which bank each of the user's accounts is at
	bankOf := map[string]int{}
	for i, bank := range banks {
		for _, account := range bank.Accounts {
			if iban := NormalizeIban(account.Iban); iban != "" {
				bankOf[iban] = i
			}
		}
	}

	type dated struct {
		date     time.Time
		amount   float64
		category string
	}
	var relevant []dated
	// The period starts where the history of every bank is available, so a
	// bank that returns less history doesn't drag the averages down.
	start := from
	for i, bank := range banks {
		var earliest time.Time
		for _, tx := range bank.Transactions {
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
			// money moved between the user's own banks is neither income
			// nor an expense
			if other, ok := bankOf[NormalizeIban(tx.CounterpartAccountNumber)]; ok && other != i {
				continue
			}
			code := tx.E1()
			if code == "" || tx.Category.E1.Confidence < opts.MinConfidence {
				continue
			}
			relevant = append(relevant, dated{date: date, amount: tx.Amount, category: code})
		}
		if bankStart := historyStart(earliest, from, to); bankStart.After(start) {
			start = bankStart
		}
	}

	p := &period{
		start:  start,
		end:    to,
		totals: map[string]float64{},
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
	}
	return p
}

// historyStart is the first full month of a bank history that starts at
// earliest. When the history starts after the window does, the first month is
// only partially covered; it is skipped so it doesn't drag the averages down.
func historyStart(earliest, from, to time.Time) time.Time {
	if earliest.IsZero() {
		return from
	}
	firstMonth := time.Date(earliest.Year(), earliest.Month(), 1, 0, 0, 0, 0, time.UTC)
	if !firstMonth.After(from) {
		return from
	}
	if earliest.After(firstMonth) && firstMonth.AddDate(0, 1, 0).Before(to) {
		return firstMonth.AddDate(0, 1, 0)
	}
	return firstMonth
}

// roundAmount rounds a euro amount to the nearest multiple of step.
func roundAmount(amount float64, step int) string {
	s := float64(step)
	return fmt.Sprint(int64(math.Round(amount/s) * s))
}

// NormalizeIban removes spaces and upper-cases an IBAN.
func NormalizeIban(iban string) string {
	return strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(iban), " ", ""))
}
