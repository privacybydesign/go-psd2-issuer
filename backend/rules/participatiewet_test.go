package rules

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNorm(t *testing.T) {
	tests := []struct {
		name      string
		household Household
		norm      int64
	}{
		{"single", Household{AgeGroup: AgeAdult}, 140150},
		{"couple", Household{AgeGroup: AgeAdult, Partner: true}, 200213},
		{"single pensioner", Household{AgeGroup: AgePension}, 156469},
		{"pensioner couple", Household{AgeGroup: AgePension, Partner: true, PartnerPensionAge: true}, 214416},
		{"pensioner with younger partner", Household{AgeGroup: AgePension, Partner: true}, 214416},
		{"with pensioner partner", Household{AgeGroup: AgeAdult, Partner: true, PartnerPensionAge: true}, 214416},
		// (40 + 2 × 30) / (2 × 100) × 2002,13 = 1001,065
		{"one housemate", Household{AgeGroup: AgeAdult, Housemates: 1}, 100107},
		// (40 + 3 × 30) / (3 × 100) × 2002,13 = 867,59
		{"two housemates", Household{AgeGroup: AgeAdult, Housemates: 2}, 86759},
		// 2 × (40 + 3 × 30) / (3 × 100) × 2002,13
		{"couple with housemate", Household{AgeGroup: AgeAdult, Partner: true, Housemates: 1}, 2 * 86759},
		// (40 + 2 × 30) / (2 × 100) × 2144,16
		{"pensioner with housemate", Household{AgeGroup: AgePension, Housemates: 1}, 107208},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.NoError(t, tt.household.Validate())
			_, norm := Norm(tt.household)
			require.Equal(t, tt.norm, norm)
		})
	}
}

func TestIncomeCountsArticle32AndSkipsAwirAllowances(t *testing.T) {
	evaluation := Evaluate(Household{AgeGroup: AgeAdult}, Facts{
		"income.salary":        800,
		"subsidy.unemployment": 200,
		"subsidy.rent":         300,
		"subsidy.health":       120,
		"subsidy.social":       400,
		"tax.income":           -50,
		"income.rent":          0,
	})

	require.Equal(t, int64(100000), evaluation.Income)
	require.Equal(t, []Item{
		{Attribute: "income.salary", Amount: 80000, Article: "32.1.a"},
		{Attribute: "subsidy.unemployment", Amount: 20000, Article: "32.1.a"},
	}, evaluation.Counted)
	// in the order of incomeRules
	require.Equal(t, []Item{
		{Attribute: "tax.income", Amount: -5000, Reason: "negative"},
		{Attribute: "subsidy.health", Amount: 12000, Article: "31.2.d", Reason: "awir"},
		{Attribute: "subsidy.rent", Amount: 30000, Article: "31.2.d", Reason: "awir"},
		{Attribute: "subsidy.social", Amount: 40000, Reason: "assessed"},
	}, evaluation.Excluded)
}

func TestBijstandTopsUpToTheNorm(t *testing.T) {
	evaluation := Evaluate(Household{AgeGroup: AgeAdult}, Facts{"income.salary": 1000})

	bijstand := evaluation.Results[0]
	require.Equal(t, "bijstand", bijstand.Rule)
	require.Equal(t, OutcomeIndication, bijstand.Outcome)
	require.Equal(t, Step{Key: "supplement", Article: "19.1", Amount: 140150 - 100000}, bijstand.Steps[len(bijstand.Steps)-1])

	minima := evaluation.Results[1]
	require.Equal(t, "minima", minima.Rule)
	require.Equal(t, OutcomeIndication, minima.Outcome)
}

func TestIncomeAboveTheNorm(t *testing.T) {
	// above the norm, but below 120% of it
	evaluation := Evaluate(Household{AgeGroup: AgeAdult}, Facts{"income.salary": 1500})
	require.Equal(t, OutcomeNoIndication, evaluation.Results[0].Outcome)
	require.Equal(t, OutcomeIndication, evaluation.Results[1].Outcome)

	evaluation = Evaluate(Household{AgeGroup: AgeAdult}, Facts{"income.salary": 1700})
	require.Equal(t, OutcomeNoIndication, evaluation.Results[1].Outcome)
}

func TestPensionAgeIsAio(t *testing.T) {
	evaluation := Evaluate(Household{AgeGroup: AgePension}, Facts{"income.pensionstate": 1200})
	require.Equal(t, "aio", evaluation.Results[0].Rule)
	require.Equal(t, OutcomeIndication, evaluation.Results[0].Outcome)
}

func TestUnder21IsNotEvaluated(t *testing.T) {
	evaluation := Evaluate(Household{AgeGroup: AgeUnder21}, Facts{})
	for _, result := range evaluation.Results {
		require.Equal(t, OutcomeNotEvaluated, result.Outcome)
	}
}

func TestNotes(t *testing.T) {
	evaluation := Evaluate(Household{AgeGroup: AgeAdult, Partner: true}, Facts{"subsidy.student": 300, "alimony.other": 200})
	require.Equal(t, []string{"note_assets", "note_average", "note_partner_income", "note_student", "note_alimony"}, evaluation.Results[0].Notes)
}

func TestValidate(t *testing.T) {
	require.Error(t, Household{}.Validate())
	require.Error(t, Household{AgeGroup: AgeAdult, Housemates: -1}.Validate())
	require.Error(t, Household{AgeGroup: AgeAdult, PartnerPensionAge: true}.Validate())
}
