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
	require.Equal(t, int64(300000-275529), stepAmount(t, result, "remitted"))

	result = kwijtschelding(t, kwijtscheldingGemeenteLaw, Household{}, AgeAdult, facts, 400).Result
	require.Equal(t, OutcomeNoIndication, result.Outcome)
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
		require.Contains(t, law.Attributes, "subsidy.rent")
		require.Contains(t, law.Attributes, "housing.rent")
		for _, attribute := range []string{"subsidy.family", "subsidy.health", "subsidy.socialother"} {
			require.NotContains(t, law.Attributes, attribute)
		}
	}
}

func TestKwijtschelding_HhnkPensionerIsNotEvaluated(t *testing.T) {
	result := kwijtschelding(t, kwijtscheldingHhnkLaw, Household{}, AgePension, Facts{}, 0).Result
	require.Equal(t, OutcomeNotEvaluated, result.Outcome)
	require.Equal(t, []string{"note_hhnk_pension"}, result.Notes)

	result = kwijtschelding(t, kwijtscheldingGemeenteLaw, Household{}, AgePension, Facts{}, 0).Result
	require.Equal(t, OutcomeIndication, result.Outcome)
}
