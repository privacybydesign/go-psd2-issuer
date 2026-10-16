package rules

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// day is a moment on the given Dutch date.
func day(date string) time.Time {
	t, err := time.ParseInLocation(time.DateOnly, date, amsterdam)
	if err != nil {
		panic(err)
	}
	return t.Add(12 * time.Hour)
}

func evaluate(t *testing.T, household Household, facts Facts) Evaluation {
	t.Helper()
	evaluation, err := Evaluate(household, facts, day("2026-10-16"))
	require.NoError(t, err)
	return evaluation
}

func TestVersionAt(t *testing.T) {
	_, err := VersionAt(day("2026-04-02"))
	require.ErrorIs(t, err, ErrNoVersion)

	v, err := VersionAt(day("2026-06-30"))
	require.NoError(t, err)
	require.Equal(t, "2026-04-03", v.ValidFrom)

	v, err = VersionAt(day("2026-07-01"))
	require.NoError(t, err)
	require.Equal(t, "2026-07-01", v.ValidFrom)
	require.Equal(t, "https://wetten.overheid.nl/BWBR0015703/2026-07-01", v.Source().Url)

	// midnight in the Netherlands is still the previous day in UTC
	v, err = VersionAt(time.Date(2026, 6, 30, 22, 30, 0, 0, time.UTC))
	require.NoError(t, err)
	require.Equal(t, "2026-07-01", v.ValidFrom)
}

func TestVersionsAreSorted(t *testing.T) {
	for i := 1; i < len(Versions); i++ {
		require.Less(t, Versions[i-1].ValidFrom, Versions[i].ValidFrom)
	}
}

func TestNorm(t *testing.T) {
	v, err := VersionAt(day("2026-10-16"))
	require.NoError(t, err)
	tests := []struct {
		name      string
		household Household
		norm      int64
	}{
		{"single", Household{AgeGroup: AgeAdult}, 141946},
		{"couple", Household{AgeGroup: AgeAdult, Partner: true}, 202779},
		{"single pensioner", Household{AgeGroup: AgePension}, 158734},
		{"pensioner couple", Household{AgeGroup: AgePension, Partner: true, PartnerPensionAge: true}, 217610},
		{"pensioner with younger partner", Household{AgeGroup: AgePension, Partner: true}, 217610},
		{"with pensioner partner", Household{AgeGroup: AgeAdult, Partner: true, PartnerPensionAge: true}, 217610},
		// (40 + 2 × 30) / (2 × 100) × 2027,79 = 1013,895
		{"one housemate", Household{AgeGroup: AgeAdult, Housemates: 1}, 101390},
		// (40 + 3 × 30) / (3 × 100) × 2027,79 = 878,709
		{"two housemates", Household{AgeGroup: AgeAdult, Housemates: 2}, 87871},
		// 2 × (40 + 3 × 30) / (3 × 100) × 2027,79
		{"couple with housemate", Household{AgeGroup: AgeAdult, Partner: true, Housemates: 1}, 2 * 87871},
		// (40 + 2 × 30) / (2 × 100) × 2176,10
		{"pensioner with housemate", Household{AgeGroup: AgePension, Housemates: 1}, 108805},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.NoError(t, tt.household.Validate())
			_, norm := Norm(tt.household, v)
			require.Equal(t, tt.norm, norm)
		})
	}
}

func TestEvaluateUsesTheVersionInForce(t *testing.T) {
	evaluation, err := Evaluate(Household{AgeGroup: AgeAdult}, Facts{}, day("2026-05-01"))
	require.NoError(t, err)
	require.Equal(t, "2026-04-03", evaluation.Source.Version)
	require.Equal(t, Step{Key: "norm_single", Article: "21.a", Amount: 140150}, evaluation.Results[0].Steps[0])

	_, err = Evaluate(Household{AgeGroup: AgeAdult}, Facts{}, day("2025-01-01"))
	require.ErrorIs(t, err, ErrNoVersion)
}

func TestIncomeCountsArticle32(t *testing.T) {
	evaluation := evaluate(t, Household{AgeGroup: AgeAdult}, Facts{
		"income.salary":        800,
		"subsidy.unemployment": 200,
		"tax.income":           -50,
		"income.rent":          0,
		// not needed by the rules, so ignored
		"subsidy.rent": 300,
	})

	require.Equal(t, int64(100000), evaluation.Income)
	require.Equal(t, []Item{
		{Attribute: "income.salary", Amount: 80000, Article: "32.1.a"},
		{Attribute: "subsidy.unemployment", Amount: 20000, Article: "32.1.a"},
	}, evaluation.Counted)
	require.Equal(t, []Item{{Attribute: "tax.income", Amount: -5000, Reason: "negative"}}, evaluation.Excluded)
}

func TestAttributesLeaveOutWhatIsNoIncome(t *testing.T) {
	attributes := Attributes()
	require.Contains(t, attributes, "income.salary")
	for _, attribute := range []string{"subsidy.family", "subsidy.health", "subsidy.rent", "subsidy.social", "subsidy.socialother"} {
		require.NotContains(t, attributes, attribute)
	}
}

func TestBijstandTopsUpToTheNorm(t *testing.T) {
	evaluation := evaluate(t, Household{AgeGroup: AgeAdult}, Facts{"income.salary": 1000})

	bijstand := evaluation.Results[0]
	require.Equal(t, "bijstand", bijstand.Rule)
	require.Equal(t, OutcomeIndication, bijstand.Outcome)
	require.Equal(t, Step{Key: "supplement", Article: "19.1", Amount: 141946 - 100000}, bijstand.Steps[len(bijstand.Steps)-1])

	minima := evaluation.Results[1]
	require.Equal(t, "minima", minima.Rule)
	require.Equal(t, OutcomeIndication, minima.Outcome)
}

func TestIncomeAboveTheNorm(t *testing.T) {
	// above the norm, but below 120% of it
	evaluation := evaluate(t, Household{AgeGroup: AgeAdult}, Facts{"income.salary": 1500})
	require.Equal(t, OutcomeNoIndication, evaluation.Results[0].Outcome)
	require.Equal(t, OutcomeIndication, evaluation.Results[1].Outcome)

	evaluation = evaluate(t, Household{AgeGroup: AgeAdult}, Facts{"income.salary": 1750})
	require.Equal(t, OutcomeNoIndication, evaluation.Results[1].Outcome)
}

func TestPensionAgeIsAio(t *testing.T) {
	evaluation := evaluate(t, Household{AgeGroup: AgePension}, Facts{"income.pensionstate": 1200})
	require.Equal(t, "aio", evaluation.Results[0].Rule)
	require.Equal(t, OutcomeIndication, evaluation.Results[0].Outcome)
}

func TestUnder21IsNotEvaluated(t *testing.T) {
	evaluation := evaluate(t, Household{AgeGroup: AgeUnder21}, Facts{})
	for _, result := range evaluation.Results {
		require.Equal(t, OutcomeNotEvaluated, result.Outcome)
	}
}

func TestNotes(t *testing.T) {
	evaluation := evaluate(t, Household{AgeGroup: AgeAdult, Partner: true}, Facts{"subsidy.student": 300, "alimony.other": 200})
	require.Equal(t, []string{"note_assets", "note_average", "note_partner_income", "note_student", "note_alimony"}, evaluation.Results[0].Notes)
}

func TestValidate(t *testing.T) {
	require.Error(t, Household{}.Validate())
	require.Error(t, Household{AgeGroup: AgeAdult, Housemates: -1}.Validate())
	require.Error(t, Household{AgeGroup: AgeAdult, PartnerPensionAge: true}.Validate())
}
