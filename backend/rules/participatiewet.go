// Package rules evaluates a few social security rules ("sociaal domein") on
// the monthly averages in the enablenow credentials, for the demo verifier.
//
// The rules follow the text of the Participatiewet as published in
// regelrecht-corpus (regulation/nl/wet/participatiewet/2026-04-03.yaml,
// https://github.com/MinBZK/regelrecht-corpus). The result is an indication,
// not a decision: only the college of the municipality (or the SVB for the
// AIO) decides on bijstand, and assets (art. 34), the partner's income and
// the exclusion grounds of art. 13 are not checked.
package rules

import "fmt"

// Source identifies the version of the law the norms are taken from.
type Source struct {
	Law     string `json:"law"`
	BwbId   string `json:"bwb_id"`
	Version string `json:"version"`
	Url     string `json:"url"`
}

var Participatiewet = Source{
	Law:     "Participatiewet",
	BwbId:   "BWBR0015703",
	Version: "2026-04-03",
	Url:     "https://wetten.overheid.nl/BWBR0015703/2026-04-03",
}

// Norms per calendar month in euro cents, art. 21 and 22.
const (
	normSingle        = 140150 // art. 21 onderdeel a
	normCouple        = 200213 // art. 21 onderdeel b
	normPensionSingle = 156469 // art. 22 onderdeel a
	normPensionCouple = 214416 // art. 22 onderdelen b en c
)

// MinimaPercentage is the income limit of the example municipal minima
// schemes, as a percentage of the bijstandsnorm. Every municipality sets its
// own limit, usually between 110% and 130%.
const MinimaPercentage = 120

// Age groups of the applicant.
const (
	// AgeUnder21: the youth norms of art. 20 apply, which are not implemented.
	AgeUnder21 = "under21"
	// AgeAdult: 21 or older, below the state pension age (art. 21).
	AgeAdult = "adult"
	// AgePension: the state pension age is reached (art. 22).
	AgePension = "pension"
)

// Household is declared by the user: the bank data says nothing about it.
type Household struct {
	AgeGroup string `json:"age_group"`
	// Partner: married or living together as if married (art. 3).
	Partner bool `json:"partner"`
	// PartnerPensionAge: the partner has reached the state pension age.
	PartnerPensionAge bool `json:"partner_pension_age"`
	// Housemates counts the cost-sharing housemates of 21 or older, not
	// counting the partner (art. 19a, 22a).
	Housemates int `json:"housemates"`
}

func (h Household) Validate() error {
	switch h.AgeGroup {
	case AgeUnder21, AgeAdult, AgePension:
	default:
		return fmt.Errorf("invalid age group %q", h.AgeGroup)
	}
	if h.Housemates < 0 || h.Housemates > 20 {
		return fmt.Errorf("invalid number of housemates %d", h.Housemates)
	}
	if !h.Partner && h.PartnerPensionAge {
		return fmt.Errorf("partner pension age without partner")
	}
	return nil
}

// Facts are the disclosed attributes, keyed "<credential>.<attribute>" (e.g.
// "income.salary"), in whole euros per month. Attributes that were not
// disclosed are missing.
type Facts map[string]int

// Item is one line of the income calculation.
type Item struct {
	Attribute string `json:"attribute"`
	// Amount in euro cents per month, as disclosed.
	Amount int64 `json:"amount"`
	// Article is the provision the amount is or is not counted under.
	Article string `json:"article,omitempty"`
	// Reason says why an amount is not counted: "awir" (an allowance under
	// the Awir), "assessed" (the bijstand being assessed) or "negative"
	// (more paid back than received).
	Reason string `json:"reason,omitempty"`
}

// Step is one line of a norm or outcome calculation.
type Step struct {
	// Key names the step; the frontend translates it.
	Key     string `json:"key"`
	Article string `json:"article,omitempty"`
	Amount  int64  `json:"amount"`
}

// Outcomes of a rule.
const (
	OutcomeIndication   = "indication"
	OutcomeNoIndication = "no_indication"
	OutcomeNotEvaluated = "not_evaluated"
)

type Result struct {
	// Rule is "bijstand" (art. 21), "aio" (art. 22, paid by the SVB) or
	// "minima" (example municipal income limit).
	Rule    string `json:"rule"`
	Outcome string `json:"outcome"`
	Steps   []Step `json:"steps,omitempty"`
	// Notes are keys of caveats that apply to this result.
	Notes []string `json:"notes,omitempty"`
}

type Evaluation struct {
	Source Source `json:"source"`
	// Counted and Excluded split the disclosed amounts per art. 31 and 32.
	Counted  []Item `json:"counted"`
	Excluded []Item `json:"excluded"`
	// Income is the monthly income per art. 32, in euro cents.
	Income  int64    `json:"income"`
	Results []Result `json:"results"`
}

type incomeRule struct {
	attribute string
	counted   bool
	article   string
	reason    string
}

// incomeRules says for every attribute the verifier asks for whether it is
// income per art. 32, or why not.
var incomeRules = []incomeRule{
	{"income.salary", true, "32.1.a", ""},       // inkomsten uit arbeid
	{"income.other", true, "32.1.a", ""},        // incl. inkomen als zelfstandige
	{"income.rent", true, "32.1.a", ""},         // inkomsten uit verhuur
	{"income.pensionstate", true, "32.1.a", ""}, // socialezekerheidsuitkering
	{"income.pensionprivate", true, "32.1.a", ""},
	{"income.pensionother", true, "32.1.a", ""},
	{"subsidy.unemployment", true, "32.1.a", ""},
	{"subsidy.disability", true, "32.1.a", ""},
	{"subsidy.familyincome", true, "32.1.a", ""},
	{"subsidy.student", true, "32.1.a", ""},
	{"subsidy.other", true, "32.1.a", ""},
	{"alimony.other", true, "32.1.a", ""},       // uitkeringen tot levensonderhoud, Boek 1 BW
	{"tax.income", true, "32.1.a", ""},          // teruggave inkomstenbelasting
	{"subsidy.family", false, "31.2.d", "awir"}, // kindgebonden budget, kinderopvangtoeslag
	{"subsidy.health", false, "31.2.d", "awir"}, // zorgtoeslag
	{"subsidy.rent", false, "31.2.d", "awir"},   // huurtoeslag
	{"subsidy.social", false, "", "assessed"},
	{"subsidy.socialother", false, "", "assessed"},
}

// Attributes lists the attributes the rules use, keyed as in Facts.
func Attributes() []string {
	attributes := make([]string, len(incomeRules))
	for i, r := range incomeRules {
		attributes[i] = r.attribute
	}
	return attributes
}

// Evaluate applies the rules to the household and the disclosed facts.
func Evaluate(household Household, facts Facts) Evaluation {
	evaluation := Evaluation{Source: Participatiewet, Counted: []Item{}, Excluded: []Item{}}
	for _, r := range incomeRules {
		value, ok := facts[r.attribute]
		if !ok || value == 0 {
			continue
		}
		item := Item{Attribute: r.attribute, Amount: int64(value) * 100, Article: r.article, Reason: r.reason}
		// a negative net amount means more was paid (back) than received,
		// which is not income
		if r.counted && value > 0 {
			evaluation.Counted = append(evaluation.Counted, item)
			evaluation.Income += item.Amount
		} else {
			if r.counted {
				item.Article, item.Reason = "", "negative"
			}
			evaluation.Excluded = append(evaluation.Excluded, item)
		}
	}

	notes := []string{"note_assets", "note_average"}
	if household.Partner {
		notes = append(notes, "note_partner_income")
	}
	if facts["subsidy.student"] > 0 {
		notes = append(notes, "note_student")
	}
	if facts["alimony.other"] > 0 {
		notes = append(notes, "note_alimony")
	}

	rule := "bijstand"
	if household.AgeGroup == AgePension {
		rule = "aio"
	}
	if household.AgeGroup == AgeUnder21 {
		evaluation.Results = []Result{
			{Rule: rule, Outcome: OutcomeNotEvaluated, Notes: []string{"note_under21"}},
			{Rule: "minima", Outcome: OutcomeNotEvaluated, Notes: []string{"note_under21"}},
		}
		return evaluation
	}

	normSteps, norm := Norm(household)
	income := Step{Key: "income", Article: "32", Amount: evaluation.Income}

	// art. 19: the bijstand tops up the income to the norm
	supplement := max(norm-evaluation.Income, 0)
	bijstand := Result{Rule: rule, Outcome: OutcomeNoIndication, Notes: notes}
	bijstand.Steps = append(append(bijstand.Steps, normSteps...), income, Step{Key: "supplement", Article: "19.1", Amount: supplement})
	if supplement > 0 {
		bijstand.Outcome = OutcomeIndication
	}

	limit := norm * MinimaPercentage / 100
	minima := Result{Rule: "minima", Outcome: OutcomeNoIndication, Notes: []string{"note_minima", "note_assets"}}
	minima.Steps = []Step{
		{Key: "norm", Amount: norm},
		{Key: "minima_limit", Amount: limit},
		income,
	}
	if evaluation.Income <= limit {
		minima.Outcome = OutcomeIndication
	}

	evaluation.Results = []Result{bijstand, minima}
	return evaluation
}

// Norm returns the monthly bijstandsnorm in euro cents for an applicant of
// 21 or older (art. 21, 22 and 22a), with the steps that lead to it.
func Norm(h Household) ([]Step, int64) {
	pension := h.AgeGroup == AgePension
	if h.Housemates == 0 {
		switch {
		case !h.Partner && pension:
			return []Step{{Key: "norm_single", Article: "22.a", Amount: normPensionSingle}}, normPensionSingle
		case !h.Partner:
			return []Step{{Key: "norm_single", Article: "21.a", Amount: normSingle}}, normSingle
		case pension:
			return []Step{{Key: "norm_couple", Article: "22.b", Amount: normPensionCouple}}, normPensionCouple
		case h.PartnerPensionAge:
			return []Step{{Key: "norm_couple", Article: "22.c", Amount: normPensionCouple}}, normPensionCouple
		default:
			return []Step{{Key: "norm_couple", Article: "21.b", Amount: normCouple}}, normCouple
		}
	}

	// Kostendelersnorm, art. 22a: (40 + A × 30) / (A × 100) × B per person,
	// with A the number of cost sharers including the applicant and partner
	// and B the couple norm. For a couple it is the sum of both norms.
	a := int64(h.Housemates + 1)
	if h.Partner {
		a++
	}
	own, ownArticle := int64(normCouple), "22a.1.a"
	switch {
	case pension:
		own, ownArticle = normPensionCouple, "22a.1.c"
	case h.PartnerPensionAge:
		own, ownArticle = normPensionCouple, "22a.1.b"
	}
	share := func(b int64) int64 {
		// round to whole cents, half up
		return ((40+a*30)*b*2 + a*100) / (a * 200)
	}
	steps := []Step{{Key: "cost_sharers", Article: "22a.1", Amount: a}, {Key: "norm_cost_sharing", Article: ownArticle, Amount: share(own)}}
	norm := share(own)
	if h.Partner {
		partner, partnerArticle := int64(normCouple), "22a.1.a"
		switch {
		case h.PartnerPensionAge:
			partner, partnerArticle = normPensionCouple, "22a.1.c"
		case pension:
			partner, partnerArticle = normPensionCouple, "22a.1.b"
		}
		steps = append(steps, Step{Key: "norm_cost_sharing_partner", Article: partnerArticle, Amount: share(partner)})
		norm += share(partner)
		steps = append(steps, Step{Key: "norm_couple", Article: "22a.2", Amount: norm})
	}
	return steps, norm
}
