package rules

import "time"

// Kwijtschelding (remission) of municipal and water board taxes, per
// chapter IV of the Uitvoeringsregeling Invorderingswet 1990 (art. 27, 28),
// which applies afdeling 2 (art. 11-19) to these taxes.

const (
	uri         = "uitvoeringsregeling_invorderingswet_1990"
	huurtoeslag = "wet_op_de_huurtoeslag"
	verlaging   = "wet_verlaging_eigen_bijdrage_huurtoeslag"
	zorgtoeslag = "wet_op_de_zorgtoeslag"
	hhnk        = "kwijtscheldingsregeling_hhnk"
)

// KwijtscheldingVersion holds the amounts the Uitvoeringsregeling takes from
// the Wet op de huurtoeslag (art. 15.1.b) and the Wet op de zorgtoeslag (art.
// 15.1.c), for one calendar year, in euro cents per month.
type KwijtscheldingVersion struct {
	// ValidFrom is the version of the Uitvoeringsregeling and of the Wet op
	// de huurtoeslag (YYYY-MM-DD).
	ValidFrom string
	// MaxRent is the maximale huurgrens (art. 13.1.a Wet op de huurtoeslag).
	MaxRent int64
	// Basishuur is the normhuur at the minimum income (art. 17.2), lowered
	// for a one or more person household (art. 17.3), plus € 12 (art. 16),
	// minus the reduction of the Wet verlaging eigen bijdrage huurtoeslag
	// (art. 1).
	BasishuurSingle int64
	BasishuurMulti  int64
	// Normpremie is the normpremie of art. 2 Wet op de zorgtoeslag at an
	// income up to the drempelinkomen, as included in the bijstandsnorm
	// (art. 15.1.c): a percentage of the drempelinkomen (art. 2.3), for a
	// single person and for a couple.
	NormpremieSingle int64
	NormpremieCouple int64
}

// KwijtscheldingVersions are sorted by ValidFrom; the amounts change every
// 1 January.
var KwijtscheldingVersions = []KwijtscheldingVersion{
	{
		ValidFrom: "2026-01-01",
		MaxRent:   93293,
		// 252,49 - 1,82 + 12 - 48,15 and 252,49 - 3,63 + 12 - 48,15
		BasishuurSingle: 21452,
		BasishuurMulti:  21271,
		// 1,912% and 4,289% of the drempelinkomen of € 29.736 (108% of
		// twelve times the minimum wage of January), per month
		NormpremieSingle: 4738,
		NormpremieCouple: 10628,
	},
}

func kwijtscheldingVersionAt(t time.Time) (KwijtscheldingVersion, error) {
	date := dutchDate(t)
	for i := len(KwijtscheldingVersions) - 1; i >= 0; i-- {
		if KwijtscheldingVersions[i].ValidFrom <= date {
			return KwijtscheldingVersions[i], nil
		}
	}
	return KwijtscheldingVersion{}, ErrNoVersion
}

func (v KwijtscheldingVersion) sources() []Source {
	return []Source{
		{Id: uri, Name: "Uitvoeringsregeling Invorderingswet 1990", Version: v.ValidFrom, Url: "https://wetten.overheid.nl/BWBR0004766/" + v.ValidFrom},
		{Id: huurtoeslag, Name: "Wet op de huurtoeslag", Version: v.ValidFrom, Url: "https://wetten.overheid.nl/BWBR0008659/" + v.ValidFrom},
		{Id: verlaging, Name: "Wet verlaging eigen bijdrage huurtoeslag", Version: v.ValidFrom, Url: "https://wetten.overheid.nl/BWBR0049106/" + v.ValidFrom},
		{Id: zorgtoeslag, Name: "Wet op de zorgtoeslag", Version: v.ValidFrom, Url: "https://wetten.overheid.nl/BWBR0018451/" + v.ValidFrom},
	}
}

var HhnkSource = Source{
	Id:      hhnk,
	Name:    "Kwijtscheldingsregeling waterschapsbelastingen HHNK 2023",
	Version: "2023-01-01",
	Url:     "https://lokaleregelgeving.overheid.nl/CVDR686061_1",
}

// PaymentCapacityPercentage of the payment capacity is used to pay the tax
// (art. 11.b.2); the rest of the assessment is remitted.
const PaymentCapacityPercentage = 80

// kwijtscheldingIncome is the income of art. 14: net income from work,
// pensions and benefits (a), alimony (b) and other income (c), plus the
// voorlopige teruggaaf (14.2). Not income: kinderopvangtoeslag and
// kindgebonden budget (subsidy.family, 14.1.c.3° and 7°), zorgtoeslag
// (subsidy.health, 14.1.c.8°) and the individuele inkomenstoeslag
// (subsidy.socialother, 14.1.c.5°, not asked for). The allowances do lower
// the costs they are for: huurtoeslag (subsidy.rent) the housing costs
// (15.3), zorgtoeslag the healthcare premium (15.1.c) and
// kinderopvangtoeslag the costs of childcare (28.3).
var kwijtscheldingIncome = []incomeRule{
	{"income.salary", uri, "14.1.a"},
	{"income.pensionstate", uri, "14.1.a"},
	{"income.pensionprivate", uri, "14.1.a"},
	{"income.pensionother", uri, "14.1.a"},
	{"subsidy.unemployment", uri, "14.1.a"},
	{"subsidy.disability", uri, "14.1.a"},
	{"subsidy.familyincome", uri, "14.1.a"},
	{"subsidy.social", uri, "14.1.a"},
	{"alimony.other", uri, "14.1.b"},
	{"income.other", uri, "14.1.c"},
	{"income.rent", uri, "14.1.c"},
	{"subsidy.student", uri, "14.1.c"},
	{"subsidy.other", uri, "14.1.c"},
	{"tax.income", uri, "14.2"},
}

// kwijtscheldingExpenses are the attributes for the expenses of art. 15 that
// are not already asked for as income: housing (b), taxes (a) and the
// healthcare premium (c).
var kwijtscheldingExpenses = []string{"housing.rent", "housing.mortgage", "subsidy.rent", "tax.vehicle", "insurance.health", "subsidy.health"}

// childcareExpenses are the attributes for the costs of childcare (art.
// 28.3). subsidy.family holds the kinderopvangtoeslag together with the
// kindgebonden budget.
var childcareExpenses = []string{"essential.childcare", "subsidy.family"}

// The council of the (fictitious) municipality of the demo has decided to
// count the costs of childcare (art. 28.3); the national rules do not.
var kwijtscheldingGemeenteLaw = Law{
	Id:                 "kwijtschelding_gemeente",
	Attributes:         append(append(attributesOf(kwijtscheldingIncome), kwijtscheldingExpenses...), childcareExpenses...),
	NeedsTaxAssessment: true,
	evaluate: func(input Input) (Evaluation, error) {
		return evaluateKwijtschelding("kwijtschelding_gemeente", input, false)
	},
}

var kwijtscheldingHhnkLaw = Law{
	Id:                 "kwijtschelding_hhnk",
	Attributes:         append(attributesOf(kwijtscheldingIncome), kwijtscheldingExpenses...),
	NeedsTaxAssessment: true,
	evaluate: func(input Input) (Evaluation, error) {
		return evaluateKwijtschelding("kwijtschelding_hhnk", input, true)
	},
}

// paid returns the amount paid per month in euro cents for an attribute with
// a signed net amount (negative is paid).
func paid(facts Facts, attribute string) int64 {
	return int64(max(-facts[attribute], 0)) * 100
}

func evaluateKwijtschelding(law string, input Input, isHhnk bool) (Evaluation, error) {
	version, err := kwijtscheldingVersionAt(input.Date)
	if err != nil {
		return Evaluation{}, err
	}
	pw, err := VersionAt(input.Date)
	if err != nil {
		return Evaluation{}, err
	}
	sources := append(version.sources(), pw.Source(), AowSource)
	if isHhnk {
		sources = append(sources, HhnkSource)
	}
	evaluation := newEvaluation(law, sources...)
	countIncome(&evaluation, kwijtscheldingIncome, input.Facts)
	facts, h := input.Facts, input.Household

	notes := []string{"note_kwijtschelding_average"}
	if h.Partner {
		notes = append(notes, "note_partner_income")
	}
	result := Result{Rule: law, Notes: notes}
	// criteria returns the conditions with the status of the payment
	// capacity test and the costs that are counted; assets and the costs
	// the bank data does not show are not checked.
	countsChildcare := !isHhnk
	criteria := func(capacity, health, childcare Criterion) []Criterion {
		capacity.Key, capacity.Law, capacity.Article = "payment_capacity", uri, "13.1"
		health.Key, health.Law, health.Article = "health_premium", uri, "15.1.c"
		childcare.Key, childcare.Law, childcare.Article = "childcare", uri, "28.3"
		return []Criterion{
			capacity,
			health,
			childcare,
			notChecked("household", participatiewet, "3"),
			notChecked("kwijtschelding_assets", uri, "12"),
			notChecked("children_costs", uri, "15.1.g"),
		}
	}
	unchecked := Criterion{Status: CriterionNotChecked}

	if input.AgeGroup == AgeUnder21 {
		result.Outcome, result.Criteria, result.Notes = OutcomeNotEvaluated, criteria(unchecked, unchecked, unchecked), []string{"note_under21"}
		evaluation.Result = result
		return evaluation, nil
	}
	if isHhnk && input.AgeGroup == AgePension {
		// HHNK art. 2.2 sets the cost of living of pensioners by the
		// Regeling kwijtschelding belastingen medeoverheden, which is not
		// implemented
		result.Outcome, result.Criteria, result.Notes = OutcomeNotEvaluated, criteria(unchecked, unchecked, unchecked), []string{"note_hhnk_pension"}
		evaluation.Result = result
		return evaluation, nil
	}

	step := func(key, law, article string, amount int64) Step {
		return Step{Key: key, Law: law, Article: article, Amount: amount}
	}
	steps := []Step{step("income", uri, "14.1", evaluation.Income)}

	// Expenses, art. 15. Net housing costs (15.3): rent or mortgage minus
	// huurtoeslag, shared equally with the cost sharers (15.4), at most the
	// maximum rent, for as far as above the basishuur (15.1.b).
	housing := paid(facts, "housing.rent") + paid(facts, "housing.mortgage")
	allowance := int64(max(facts["subsidy.rent"], 0)) * 100
	net := max(housing-allowance, 0)
	steps = append(steps, step("housing_costs", uri, "15.3", housing), step("rent_allowance", uri, "15.3", allowance))
	if a := costSharers(h); a > 1 {
		persons := int64(1)
		if h.Partner {
			persons = 2
		}
		net = net * persons / a
		steps = append(steps, step("housing_share", uri, "15.4", net))
	}
	basishuur := version.BasishuurMulti
	if !h.Partner && h.Housemates == 0 {
		basishuur = version.BasishuurSingle
	}
	housingDeduction := max(min(net, version.MaxRent)-basishuur, 0)
	steps = append(steps,
		step("max_rent", huurtoeslag, "13.1.a", version.MaxRent),
		step("basishuur", huurtoeslag, "16", basishuur),
		step("housing_deduction", uri, "15.1.b", housingDeduction),
	)
	if housing > 0 {
		evaluation.Expenses = append(evaluation.Expenses, Item{Attribute: "housing", Amount: housingDeduction, Law: uri, Article: "15.1.b"})
	}
	if facts["housing.mortgage"] < 0 {
		result.Notes = append(result.Notes, "note_mortgage")
	}
	for _, e := range []struct{ attribute, article string }{
		{"tax.income", "15.1.a"}, {"tax.vehicle", "15.1.a"}, {"alimony.other", "15.1.d"},
	} {
		if amount := paid(facts, e.attribute); amount > 0 {
			evaluation.Expenses = append(evaluation.Expenses, Item{Attribute: e.attribute, Amount: amount, Law: uri, Article: e.article})
		}
	}

	// Healthcare premium (15.1.c): the premium minus the normpremie that is
	// already included in the bijstandsnorm and minus the zorgtoeslag.
	premium := paid(facts, "insurance.health")
	normpremie := version.NormpremieSingle
	if h.Partner {
		normpremie = version.NormpremieCouple
	}
	healthAllowance := int64(max(facts["subsidy.health"], 0)) * 100
	healthDeduction := max(premium-normpremie-healthAllowance, 0)
	steps = append(steps,
		step("health_premium", uri, "15.1.c", premium),
		step("normpremie", zorgtoeslag, "2.3", normpremie),
		step("health_allowance", uri, "15.1.c", healthAllowance),
		step("health_deduction", uri, "15.1.c", healthDeduction),
	)
	if healthDeduction > 0 {
		evaluation.Expenses = append(evaluation.Expenses, Item{Attribute: "insurance.health", Amount: healthDeduction, Law: uri, Article: "15.1.c"})
	}
	health := Criterion{Status: CriterionCounted, Amount: healthDeduction}

	// Childcare (28.3), if the council has decided so: the costs minus the
	// kinderopvangtoeslag. That is on one card with the kindgebonden
	// budget, which is subtracted as well, so the costs counted may be too
	// low.
	childcare := unchecked
	if countsChildcare {
		costs := paid(facts, "essential.childcare")
		childcareAllowance := int64(max(facts["subsidy.family"], 0)) * 100
		childcareDeduction := max(costs-childcareAllowance, 0)
		steps = append(steps,
			step("childcare_costs", uri, "28.3", costs),
			step("childcare_allowance", uri, "28.3", childcareAllowance),
			step("childcare_deduction", uri, "28.3", childcareDeduction),
		)
		if childcareDeduction > 0 {
			evaluation.Expenses = append(evaluation.Expenses, Item{Attribute: "essential.childcare", Amount: childcareDeduction, Law: uri, Article: "28.3"})
		}
		if costs > 0 && childcareAllowance > 0 {
			result.Notes = append(result.Notes, "note_childcare_allowance")
		}
		childcare = Criterion{Status: CriterionCounted, Amount: childcareDeduction}
	}
	var expenses int64
	for _, e := range evaluation.Expenses {
		expenses += e.Amount
	}
	disposable := max(evaluation.Income-expenses, 0)
	steps = append(steps, step("expenses", uri, "15.1", expenses), step("disposable_income", uri, "14.1", disposable))

	// Cost of living, art. 16: 90% of the bijstandsnorm; HHNK uses 100%.
	normSteps, norm := Norm(h, input.AgeGroup, pw)
	percentage, costArticle, costLaw := int64(90), "16", uri
	if isHhnk {
		percentage, costArticle, costLaw = 100, "2.1", hhnk
	}
	costOfLiving := norm * percentage / 100
	steps = append(steps, normSteps...)
	steps = append(steps,
		step("cost_of_living_percentage", costLaw, costArticle, percentage),
		step("cost_of_living", costLaw, costArticle, costOfLiving),
	)

	// Payment capacity, art. 13; at least 80% of it over 12 months is used
	// to pay the tax, the rest of the assessment is remitted (art. 11).
	capacity := max(disposable-costOfLiving, 0)
	payable := capacity * 12 * PaymentCapacityPercentage / 100
	steps = append(steps,
		step("payment_capacity", uri, "13.1", capacity),
		step("payable_per_year", uri, "11.b", payable),
	)
	switch {
	case capacity == 0:
		result.Outcome = OutcomeIndication
	case input.TaxAssessment > 0:
		assessment := int64(input.TaxAssessment) * 100
		remitted := max(assessment-payable, 0)
		steps = append(steps, step("assessment", "", "", assessment), step("remitted", uri, "11.b", remitted))
		result.Outcome = OutcomeNoIndication
		if remitted > 0 {
			result.Outcome = OutcomePartial
		}
	default:
		result.Outcome = OutcomePartial
	}
	capacityStatus := map[string]string{
		OutcomeIndication:   CriterionMet,
		OutcomePartial:      CriterionPartial,
		OutcomeNoIndication: CriterionNotMet,
	}[result.Outcome]
	// the payment capacity per month, against the cost of living
	result.Criteria = criteria(Criterion{Status: capacityStatus, Amount: capacity, Limit: costOfLiving}, health, childcare)
	result.Steps = steps
	evaluation.Result = result
	return evaluation, nil
}
