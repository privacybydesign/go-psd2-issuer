package rules

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func stepAmount(t *testing.T, result Result, key string) int64 {
	t.Helper()
	for _, step := range result.Steps {
		if step.Key == key {
			return step.Amount
		}
	}
	t.Fatalf("no step %q", key)
	return 0
}

func kwijtschelding(t *testing.T, law Law, household Household, ageGroup string, facts Facts, assessment int) Evaluation {
	t.Helper()
	evaluation, err := law.Evaluate(Input{Household: household, AgeGroup: ageGroup, Facts: facts, Date: day(today), TaxAssessment: assessment})
	require.NoError(t, err)
	return evaluation
}

func TestKwijtschelding(t *testing.T) {
	// income 1600; housing (900 - 250) - basishuur 214,52 = 435,48;
	// disposable 1164,52 is below 90% of 1419,46 = 1277,51
	facts := Facts{"income.salary": 1600, "housing.rent": -900, "subsidy.rent": 250}
	evaluation := kwijtschelding(t, kwijtscheldingGemeenteLaw, Household{}, AgeAdult, facts, 0)
	result := evaluation.Result
	require.Equal(t, OutcomeIndication, result.Outcome)
	require.Equal(t, CriterionMet, criterion(result, "payment_capacity"))
	require.Equal(t, CriterionNotChecked, criterion(result, "kwijtschelding_assets"))
	require.Equal(t, int64(43548), stepAmount(t, result, "housing_deduction"))
	require.Equal(t, int64(116452), stepAmount(t, result, "disposable_income"))
	require.Equal(t, int64(127751), stepAmount(t, result, "cost_of_living"))
	require.Equal(t, int64(0), stepAmount(t, result, "payment_capacity"))
	require.Equal(t, []Item{{Attribute: "housing", Amount: 43548, Law: uri, Article: "15.1.b"}}, evaluation.Expenses)

	// income 2000: capacity 1564,52 - 1277,51 = 287,01 per month, of which
	// 80% over 12 months is 2755,29 per year
	facts["income.salary"] = 2000
	result = kwijtschelding(t, kwijtscheldingGemeenteLaw, Household{}, AgeAdult, facts, 0).Result
	require.Equal(t, OutcomePartial, result.Outcome)
	require.Equal(t, int64(28701), stepAmount(t, result, "payment_capacity"))
	require.Equal(t, int64(275529), stepAmount(t, result, "payable_per_year"))

	result = kwijtschelding(t, kwijtscheldingGemeenteLaw, Household{}, AgeAdult, facts, 3000).Result
	require.Equal(t, OutcomePartial, result.Outcome)
	require.Equal(t, CriterionPartial, criterion(result, "payment_capacity"))
	require.Equal(t, int64(300000-275529), stepAmount(t, result, "remitted"))

	result = kwijtschelding(t, kwijtscheldingGemeenteLaw, Household{}, AgeAdult, facts, 400).Result
	require.Equal(t, OutcomeNoIndication, result.Outcome)
	require.Equal(t, CriterionNotMet, criterion(result, "payment_capacity"))
	require.Equal(t, int64(0), stepAmount(t, result, "remitted"))

	// HHNK counts 100% of the norm as cost of living: 1564,52 - 1419,46
	result = kwijtschelding(t, kwijtscheldingHhnkLaw, Household{}, AgeAdult, facts, 0).Result
	require.Equal(t, int64(141946), stepAmount(t, result, "cost_of_living"))
	require.Equal(t, int64(14506), stepAmount(t, result, "payment_capacity"))
}

func TestKwijtschelding_HousingIsSharedAndCapped(t *testing.T) {
	// with a housemate the net housing costs are halved (art. 15.4) and the
	// basishuur of a multi-person household applies
	facts := Facts{"housing.rent": -900, "subsidy.rent": 250}
	result := kwijtschelding(t, kwijtscheldingGemeenteLaw, Household{Housemates: 1}, AgeAdult, facts, 0).Result
	require.Equal(t, int64(32500), stepAmount(t, result, "housing_share"))
	require.Equal(t, int64(32500-21271), stepAmount(t, result, "housing_deduction"))

	// at most the maximum rent of the huurtoeslag
	result = kwijtschelding(t, kwijtscheldingGemeenteLaw, Household{}, AgeAdult, Facts{"housing.rent": -1500}, 0).Result
	require.Equal(t, int64(93293-21452), stepAmount(t, result, "housing_deduction"))
}

func TestKwijtschelding_IncomeAndExpenses(t *testing.T) {
	evaluation := kwijtschelding(t, kwijtscheldingGemeenteLaw, Household{}, AgeAdult, Facts{
		"subsidy.social": 1300, // bijstand counts as income here
		"alimony.other":  -150, // paid alimony is an expense
		"tax.income":     -20,  // and so are tax payments
		"tax.vehicle":    -30,
	}, 0)
	require.Equal(t, int64(130000), evaluation.Income)
	require.Equal(t, []Item{
		{Attribute: "tax.income", Amount: 2000, Law: uri, Article: "15.1.a"},
		{Attribute: "tax.vehicle", Amount: 3000, Law: uri, Article: "15.1.a"},
		{Attribute: "alimony.other", Amount: 15000, Law: uri, Article: "15.1.d"},
	}, evaluation.Expenses)
	// the paid amounts are expenses, not excluded income
	require.Len(t, evaluation.Excluded, 2)
}

func TestKwijtschelding_AttributesLeaveOutWhatDoesNotCount(t *testing.T) {
	for _, law := range []Law{kwijtscheldingGemeenteLaw, kwijtscheldingHhnkLaw} {
		// the allowances lower the costs they are for
		for _, attribute := range []string{"housing.rent", "subsidy.rent", "insurance.health", "subsidy.health"} {
			require.Contains(t, law.Attributes, attribute)
		}
		require.NotContains(t, law.Attributes, "subsidy.socialother")
		require.False(t, law.NeedsPostcode)
	}
	// only the municipality has decided to count childcare (art. 28.3)
	require.Contains(t, kwijtscheldingGemeenteLaw.Attributes, "essential.childcare")
	require.Contains(t, kwijtscheldingGemeenteLaw.Attributes, "subsidy.family")
	require.NotContains(t, kwijtscheldingHhnkLaw.Attributes, "essential.childcare")
	require.NotContains(t, kwijtscheldingHhnkLaw.Attributes, "subsidy.family")
}

func TestKwijtschelding_HealthPremium(t *testing.T) {
	// premium 200 - normpremie 47,38 - zorgtoeslag 120 = 32,62 (art. 15.1.c)
	facts := Facts{"insurance.health": -200, "subsidy.health": 120}
	evaluation := kwijtschelding(t, kwijtscheldingGemeenteLaw, Household{}, AgeAdult, facts, 0)
	require.Equal(t, int64(4738), stepAmount(t, evaluation.Result, "normpremie"))
	require.Equal(t, []Item{{Attribute: "insurance.health", Amount: 3262, Law: uri, Article: "15.1.c"}}, evaluation.Expenses)
	require.Equal(t, Criterion{Key: "health_premium", Status: CriterionCounted, Law: uri, Article: "15.1.c", Amount: 3262}, evaluation.Result.Criteria[1])

	// a couple has the normpremie of a couple; usually nothing is left
	evaluation = kwijtschelding(t, kwijtscheldingGemeenteLaw, Household{Partner: true}, AgeAdult, facts, 0)
	require.Equal(t, int64(10628), stepAmount(t, evaluation.Result, "normpremie"))
	require.Empty(t, evaluation.Expenses)
}

func TestKwijtschelding_Childcare(t *testing.T) {
	// childcare 600 - allowances 450 (art. 28.3)
	facts := Facts{"essential.childcare": -600, "subsidy.family": 450}
	evaluation := kwijtschelding(t, kwijtscheldingGemeenteLaw, Household{}, AgeAdult, facts, 0)
	require.Equal(t, []Item{{Attribute: "essential.childcare", Amount: 15000, Law: uri, Article: "28.3"}}, evaluation.Expenses)
	require.Equal(t, CriterionCounted, criterion(evaluation.Result, "childcare"))
	require.Contains(t, evaluation.Result.Notes, "note_childcare_allowance")

	// HHNK has not decided to count it
	evaluation = kwijtschelding(t, kwijtscheldingHhnkLaw, Household{}, AgeAdult, facts, 0)
	require.Empty(t, evaluation.Expenses)
	require.Equal(t, CriterionNotChecked, criterion(evaluation.Result, "childcare"))
}

func TestKwijtschelding_HhnkPensionerIsNotEvaluated(t *testing.T) {
	result := kwijtschelding(t, kwijtscheldingHhnkLaw, Household{}, AgePension, Facts{}, 0).Result
	require.Equal(t, OutcomeNotEvaluated, result.Outcome)
	require.Equal(t, []string{"note_hhnk_pension"}, result.Notes)

	result = kwijtschelding(t, kwijtscheldingGemeenteLaw, Household{}, AgePension, Facts{}, 0).Result
	require.Equal(t, OutcomeIndication, result.Outcome)
}
