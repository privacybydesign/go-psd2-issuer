// Package rules evaluates a few social security rules ("sociaal domein") on
// the monthly averages in the enablenow credentials, for the demo verifier.
//
// The rules follow the text of the Participatiewet as published in
// regelrecht-corpus (regulation/nl/wet/participatiewet/<valid from>.yaml,
// https://github.com/MinBZK/regelrecht-corpus), using the version in force on
// the day of the check. The result is an indication,
// not a decision: only the college of the municipality (or the SVB for the
// AIO) decides on bijstand, and assets (art. 34), the partner's income and
// the exclusion grounds of art. 13 are not checked.
package rules

import (
	"errors"
	"fmt"
	"time"
	_ "time/tzdata" // the dates of the law are Dutch dates
)

// Source identifies the version of the law the norms are taken from.
type Source struct {
	Law     string `json:"law"`
	BwbId   string `json:"bwb_id"`
	Version string `json:"version"`
	Url     string `json:"url"`
}

// Version is a version of the Participatiewet with the norms per calendar
// month it sets, in euro cents.
type Version struct {
	// ValidFrom is the date the version enters into force (YYYY-MM-DD); it
	// applies until the next version.
	ValidFrom     string
	Single        int64 // art. 21 onderdeel a
	Couple        int64 // art. 21 onderdeel b
	PensionSingle int64 // art. 22 onderdeel a
	PensionCouple int64 // art. 22 onderdelen b en c
}

// Versions are sorted by ValidFrom. The norms are indexed every 1 January
// and 1 July: add the new version when it is published on
// wetten.overheid.nl (and harvested into regelrecht-corpus).
var Versions = []Version{
	{ValidFrom: "2026-04-03", Single: 140150, Couple: 200213, PensionSingle: 156469, PensionCouple: 214416},
	{ValidFrom: "2026-07-01", Single: 141946, Couple: 202779, PensionSingle: 158734, PensionCouple: 217610},
}

var ErrNoVersion = errors.New("no version of the Participatiewet known for this date")

var amsterdam = must(time.LoadLocation("Europe/Amsterdam"))

func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}

// VersionAt returns the version in force on the (Dutch) date of t.
func VersionAt(t time.Time) (Version, error) {
	date := t.In(amsterdam).Format(time.DateOnly)
	for i := len(Versions) - 1; i >= 0; i-- {
		if Versions[i].ValidFrom <= date {
			return Versions[i], nil
		}
	}
	return Version{}, ErrNoVersion
}

func (v Version) Source() Source {
	return Source{
		Law:     "Participatiewet",
		BwbId:   "BWBR0015703",
		Version: v.ValidFrom,
		Url:     "https://wetten.overheid.nl/BWBR0015703/" + v.ValidFrom,
	}
}

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
// "income.salary"), in whole euros per month. Missing attributes count as 0.
type Facts map[string]int

// Item is one line of the income calculation.
type Item struct {
	Attribute string `json:"attribute"`
	// Amount in euro cents per month, as disclosed.
	Amount int64 `json:"amount"`
	// Article is the provision the amount is counted under.
	Article string `json:"article,omitempty"`
	// Reason says why an amount is not counted: "negative" (more paid back
	// than received).
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
	article   string
}

// incomeRules lists the attributes that are income per art. 32; these are
// all the rules need. The other benefits are deliberately not asked for:
// the Awir allowances (subsidy.family, subsidy.health, subsidy.rent) are no
// income (art. 31.2.d), and subsidy.social and subsidy.socialother are the
// bijstand that is being assessed.
var incomeRules = []incomeRule{
	{"income.salary", "32.1.a"},       // inkomsten uit arbeid
	{"income.other", "32.1.a"},        // incl. inkomen als zelfstandige
	{"income.rent", "32.1.a"},         // inkomsten uit verhuur
	{"income.pensionstate", "32.1.a"}, // socialezekerheidsuitkering
	{"income.pensionprivate", "32.1.a"},
	{"income.pensionother", "32.1.a"},
	{"subsidy.unemployment", "32.1.a"},
	{"subsidy.disability", "32.1.a"},
	{"subsidy.familyincome", "32.1.a"},
	{"subsidy.student", "32.1.a"},
	{"subsidy.other", "32.1.a"},
	{"alimony.other", "32.1.a"}, // uitkeringen tot levensonderhoud, Boek 1 BW
	{"tax.income", "32.1.a"},    // teruggave inkomstenbelasting
}

// Attributes lists the attributes the rules need, keyed as in Facts.
func Attributes() []string {
	attributes := make([]string, len(incomeRules))
	for i, r := range incomeRules {
		attributes[i] = r.attribute
	}
	return attributes
}

// Evaluate applies the rules to the household and the disclosed facts, with
// the norms of the version of the law in force on the given date.
func Evaluate(household Household, facts Facts, date time.Time) (Evaluation, error) {
	version, err := VersionAt(date)
	if err != nil {
		return Evaluation{}, err
	}
	evaluation := Evaluation{Source: version.Source(), Counted: []Item{}, Excluded: []Item{}}
	for _, r := range incomeRules {
		value := facts[r.attribute]
		item := Item{Attribute: r.attribute, Amount: int64(value) * 100}
		switch {
		case value > 0:
			item.Article = r.article
			evaluation.Counted = append(evaluation.Counted, item)
			evaluation.Income += item.Amount
		case value < 0:
			// more was paid (back) than received, which is no income
			item.Reason = "negative"
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
		return evaluation, nil
	}

	normSteps, norm := Norm(household, version)
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
	return evaluation, nil
}

// Norm returns the monthly bijstandsnorm in euro cents for an applicant of
// 21 or older (art. 21, 22 and 22a), with the steps that lead to it.
func Norm(h Household, v Version) ([]Step, int64) {
	pension := h.AgeGroup == AgePension
	if h.Housemates == 0 {
		switch {
		case !h.Partner && pension:
			return []Step{{Key: "norm_single", Article: "22.a", Amount: v.PensionSingle}}, v.PensionSingle
		case !h.Partner:
			return []Step{{Key: "norm_single", Article: "21.a", Amount: v.Single}}, v.Single
		case pension:
			return []Step{{Key: "norm_couple", Article: "22.b", Amount: v.PensionCouple}}, v.PensionCouple
		case h.PartnerPensionAge:
			return []Step{{Key: "norm_couple", Article: "22.c", Amount: v.PensionCouple}}, v.PensionCouple
		default:
			return []Step{{Key: "norm_couple", Article: "21.b", Amount: v.Couple}}, v.Couple
		}
	}

	// Kostendelersnorm, art. 22a: (40 + A × 30) / (A × 100) × B per person,
	// with A the number of cost sharers including the applicant and partner
	// and B the couple norm. For a couple it is the sum of both norms.
	a := int64(h.Housemates + 1)
	if h.Partner {
		a++
	}
	own, ownArticle := v.Couple, "22a.1.a"
	switch {
	case pension:
		own, ownArticle = v.PensionCouple, "22a.1.c"
	case h.PartnerPensionAge:
		own, ownArticle = v.PensionCouple, "22a.1.b"
	}
	share := func(b int64) int64 {
		// round to whole cents, half up
		return ((40+a*30)*b*2 + a*100) / (a * 200)
	}
	steps := []Step{{Key: "cost_sharers", Article: "22a.1", Amount: a}, {Key: "norm_cost_sharing", Article: ownArticle, Amount: share(own)}}
	norm := share(own)
	if h.Partner {
		partner, partnerArticle := v.Couple, "22a.1.a"
		switch {
		case h.PartnerPensionAge:
			partner, partnerArticle = v.PensionCouple, "22a.1.c"
		case pension:
			partner, partnerArticle = v.PensionCouple, "22a.1.b"
		}
		steps = append(steps, Step{Key: "norm_cost_sharing_partner", Article: partnerArticle, Amount: share(partner)})
		norm += share(partner)
		steps = append(steps, Step{Key: "norm_couple", Article: "22a.2", Amount: norm})
	}
	return steps, norm
}
