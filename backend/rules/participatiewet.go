package rules

import (
	"errors"
	"time"
)

// Version is a version of the Participatiewet (as published in
// regelrecht-corpus, regulation/nl/wet/participatiewet/<ValidFrom>.yaml)
// with the norms per calendar month it sets, in euro cents.
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

var ErrNoVersion = errors.New("no version of the law known for this date")

// VersionAt returns the version in force on the (Dutch) date of t.
func VersionAt(t time.Time) (Version, error) {
	date := dutchDate(t)
	for i := len(Versions) - 1; i >= 0; i-- {
		if Versions[i].ValidFrom <= date {
			return Versions[i], nil
		}
	}
	return Version{}, ErrNoVersion
}

const participatiewet = "participatiewet"

func (v Version) Source() Source {
	return Source{
		Id:      participatiewet,
		Name:    "Participatiewet",
		Version: v.ValidFrom,
		Url:     "https://wetten.overheid.nl/BWBR0015703/" + v.ValidFrom,
	}
}

// MinimaPercentage is the income limit of the example municipal minima
// schemes, as a percentage of the bijstandsnorm. Every municipality sets its
// own limit, usually between 110% and 130%.
const MinimaPercentage = 120

// bijstandIncome lists the attributes that are income per art. 32; these
// are all the bijstand rules need. The other benefits are deliberately not
// asked for: the Awir allowances (subsidy.family, subsidy.health,
// subsidy.rent) are no income (art. 31.2.d), and subsidy.social and
// subsidy.socialother are the bijstand that is being assessed.
var bijstandIncome = []incomeRule{
	{"income.salary", participatiewet, "32.1.a"}, // inkomsten uit arbeid
	{"income.other", participatiewet, "32.1.a"},  // incl. inkomen als zelfstandige
	{"income.rent", participatiewet, "32.1.a"},   // inkomsten uit verhuur
	{"income.pensionstate", participatiewet, "32.1.a"},
	{"income.pensionprivate", participatiewet, "32.1.a"},
	{"income.pensionother", participatiewet, "32.1.a"},
	{"subsidy.unemployment", participatiewet, "32.1.a"}, // socialezekerheidsuitkeringen
	{"subsidy.disability", participatiewet, "32.1.a"},
	{"subsidy.familyincome", participatiewet, "32.1.a"},
	{"subsidy.student", participatiewet, "32.1.a"},
	{"subsidy.other", participatiewet, "32.1.a"},
	{"alimony.other", participatiewet, "32.1.a"}, // uitkeringen tot levensonderhoud, Boek 1 BW
	{"tax.income", participatiewet, "32.1.a"},    // teruggave inkomstenbelasting
}

var bijstandLaw = Law{
	Id:         "bijstand",
	Attributes: attributesOf(bijstandIncome),
	evaluate:   evaluateBijstand,
}

var minimaLaw = Law{
	Id:         "minima",
	Attributes: attributesOf(bijstandIncome),
	evaluate:   evaluateMinima,
}

// participatiewetIncome counts the income per art. 32 and returns the norm
// and its steps, or ok false when the norm is not implemented (under 21).
func participatiewetIncome(law string, input Input) (evaluation Evaluation, normSteps []Step, norm int64, ok bool, err error) {
	version, err := VersionAt(input.Date)
	if err != nil {
		return Evaluation{}, nil, 0, false, err
	}
	evaluation = newEvaluation(law, version.Source(), AowSource)
	countIncome(&evaluation, bijstandIncome, input.Facts)
	if input.AgeGroup == AgeUnder21 {
		return evaluation, nil, 0, false, nil
	}
	normSteps, norm = Norm(input.Household, input.AgeGroup, version)
	return evaluation, normSteps, norm, true, nil
}

// evaluateBijstand: art. 19, the bijstand tops up the income to the norm.
func evaluateBijstand(input Input) (Evaluation, error) {
	rule := "bijstand"
	if input.AgeGroup == AgePension {
		// paid by the SVB as the AIO supplement (art. 47a)
		rule = "aio"
	}
	evaluation, normSteps, norm, ok, err := participatiewetIncome("bijstand", input)
	if err != nil {
		return Evaluation{}, err
	}
	if !ok {
		evaluation.Result = Result{Rule: rule, Outcome: OutcomeNotEvaluated, Notes: []string{"note_under21"}}
		return evaluation, nil
	}

	notes := []string{"note_assets", "note_average"}
	if input.Household.Partner {
		notes = append(notes, "note_partner_income")
	}
	if input.Facts["subsidy.student"] > 0 {
		notes = append(notes, "note_student")
	}
	if input.Facts["alimony.other"] > 0 {
		notes = append(notes, "note_alimony")
	}

	supplement := max(norm-evaluation.Income, 0)
	result := Result{Rule: rule, Outcome: OutcomeNoIndication, Notes: notes}
	result.Steps = append(normSteps,
		Step{Key: "income", Law: participatiewet, Article: "32", Amount: evaluation.Income},
		Step{Key: "supplement", Law: participatiewet, Article: "19.1", Amount: supplement},
	)
	if supplement > 0 {
		result.Outcome = OutcomeIndication
	}
	evaluation.Result = result
	return evaluation, nil
}

// evaluateMinima: income at most MinimaPercentage of the bijstandsnorm.
func evaluateMinima(input Input) (Evaluation, error) {
	evaluation, normSteps, norm, ok, err := participatiewetIncome("minima", input)
	if err != nil {
		return Evaluation{}, err
	}
	if !ok {
		evaluation.Result = Result{Rule: "minima", Outcome: OutcomeNotEvaluated, Notes: []string{"note_under21"}}
		return evaluation, nil
	}
	limit := norm * MinimaPercentage / 100
	result := Result{Rule: "minima", Outcome: OutcomeNoIndication, Notes: []string{"note_minima", "note_assets"}}
	result.Steps = append(normSteps,
		Step{Key: "minima_limit", Amount: limit},
		Step{Key: "income", Law: participatiewet, Article: "32", Amount: evaluation.Income},
	)
	if evaluation.Income <= limit {
		result.Outcome = OutcomeIndication
	}
	evaluation.Result = result
	return evaluation, nil
}

// Norm returns the monthly bijstandsnorm in euro cents for an applicant of
// 21 or older (art. 21, 22 and 22a), with the steps that lead to it.
func Norm(h Household, ageGroup string, v Version) ([]Step, int64) {
	pension := ageGroup == AgePension
	step := func(key, article string, amount int64) Step {
		return Step{Key: key, Law: participatiewet, Article: article, Amount: amount}
	}
	if h.Housemates == 0 {
		switch {
		case !h.Partner && pension:
			return []Step{step("norm_single", "22.a", v.PensionSingle)}, v.PensionSingle
		case !h.Partner:
			return []Step{step("norm_single", "21.a", v.Single)}, v.Single
		case pension:
			return []Step{step("norm_couple", "22.b", v.PensionCouple)}, v.PensionCouple
		case h.PartnerPensionAge:
			return []Step{step("norm_couple", "22.c", v.PensionCouple)}, v.PensionCouple
		default:
			return []Step{step("norm_couple", "21.b", v.Couple)}, v.Couple
		}
	}

	// Kostendelersnorm, art. 22a: (40 + A × 30) / (A × 100) × B per person,
	// with A the number of cost sharers including the applicant and partner
	// and B the couple norm. For a couple it is the sum of both norms.
	a := costSharers(h)
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
	steps := []Step{step("cost_sharers", "22a.1", a), step("norm_cost_sharing", ownArticle, share(own))}
	norm := share(own)
	if h.Partner {
		partner, partnerArticle := v.Couple, "22a.1.a"
		switch {
		case h.PartnerPensionAge:
			partner, partnerArticle = v.PensionCouple, "22a.1.c"
		case pension:
			partner, partnerArticle = v.PensionCouple, "22a.1.b"
		}
		steps = append(steps, step("norm_cost_sharing_partner", partnerArticle, share(partner)))
		norm += share(partner)
		steps = append(steps, step("norm_couple", "22a.2", norm))
	}
	return steps, norm
}

// costSharers is A of art. 22a: the housemates plus the applicant and the
// partner.
func costSharers(h Household) int64 {
	a := int64(h.Housemates + 1)
	if h.Partner {
		a++
	}
	return a
}
