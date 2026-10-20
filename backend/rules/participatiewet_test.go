package rules

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// day is noon on the given Dutch date.
func day(date string) time.Time {
	t, err := time.ParseInLocation(time.DateOnly, date, amsterdam)
	if err != nil {
		panic(err)
	}
	return t.Add(12 * time.Hour)
}

const today = "2026-10-16"

func evaluate(t *testing.T, law Law, household Household, ageGroup string, facts Facts) Evaluation {
	t.Helper()
	evaluation, err := law.Evaluate(Input{Household: household, AgeGroup: ageGroup, Facts: facts, Date: day(today)})
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
	for i := 1; i < len(KwijtscheldingVersions); i++ {
		require.Less(t, KwijtscheldingVersions[i-1].ValidFrom, KwijtscheldingVersions[i].ValidFrom)
	}
}

func TestNorm(t *testing.T) {
	v, err := VersionAt(day(today))
	require.NoError(t, err)
	tests := []struct {
		name      string
		household Household
		ageGroup  string
		norm      int64
	}{
		{"single", Household{}, AgeAdult, 141946},
		{"couple", Household{Partner: true}, AgeAdult, 202779},
		{"single pensioner", Household{}, AgePension, 158734},
		{"pensioner couple", Household{Partner: true, PartnerPensionAge: true}, AgePension, 217610},
		{"pensioner with younger partner", Household{Partner: true}, AgePension, 217610},
		{"with pensioner partner", Household{Partner: true, PartnerPensionAge: true}, AgeAdult, 217610},
		// (40 + 2 × 30) / (2 × 100) × 2027,79 = 1013,895
		{"one housemate", Household{Housemates: 1}, AgeAdult, 101390},
		// (40 + 3 × 30) / (3 × 100) × 2027,79 = 878,709
		{"two housemates", Household{Housemates: 2}, AgeAdult, 87871},
		// 2 × (40 + 3 × 30) / (3 × 100) × 2027,79
		{"couple with housemate", Household{Partner: true, Housemates: 1}, AgeAdult, 2 * 87871},
		// (40 + 2 × 30) / (2 × 100) × 2176,10
		{"pensioner with housemate", Household{Housemates: 1}, AgePension, 108805},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.NoError(t, tt.household.Validate())
			_, norm := Norm(tt.household, tt.ageGroup, v)
			require.Equal(t, tt.norm, norm)
		})
	}
}

func TestBijstandUsesTheVersionInForce(t *testing.T) {
	evaluation, err := bijstandLaw.Evaluate(Input{AgeGroup: AgeAdult, Facts: Facts{}, Date: day("2026-05-01")})
	require.NoError(t, err)
	require.Equal(t, "2026-04-03", evaluation.Sources[0].Version)
	require.Equal(t, Step{Key: "norm_single", Law: participatiewet, Article: "21.a", Amount: 140150}, evaluation.Result.Steps[0])

	_, err = bijstandLaw.Evaluate(Input{AgeGroup: AgeAdult, Facts: Facts{}, Date: day("2025-01-01")})
	require.ErrorIs(t, err, ErrNoVersion)
}

func TestBijstandIncomeCountsArticle32(t *testing.T) {
	evaluation := evaluate(t, bijstandLaw, Household{}, AgeAdult, Facts{
		"income.salary":        800,
		"subsidy.unemployment": 200,
		"tax.income":           -50,
		"income.rent":          0,
		// not needed by the rules, so ignored
		"subsidy.rent": 300,
	})

	require.Equal(t, int64(100000), evaluation.Income)
	require.Equal(t, []Item{
		{Attribute: "income.salary", Amount: 80000, Law: participatiewet, Article: "32.1.a"},
		{Attribute: "subsidy.unemployment", Amount: 20000, Law: participatiewet, Article: "32.1.a"},
	}, evaluation.Counted)
	require.Equal(t, []Item{{Attribute: "tax.income", Amount: -5000, Reason: "negative"}}, evaluation.Excluded)
}

func TestBijstandAttributesLeaveOutWhatIsNoIncome(t *testing.T) {
	require.Contains(t, bijstandLaw.Attributes, "income.salary")
	for _, attribute := range []string{"subsidy.family", "subsidy.health", "subsidy.rent", "subsidy.social", "subsidy.socialother"} {
		require.NotContains(t, bijstandLaw.Attributes, attribute)
	}
}

func TestBijstandTopsUpToTheNorm(t *testing.T) {
	result := evaluate(t, bijstandLaw, Household{}, AgeAdult, Facts{"income.salary": 1000}).Result
	require.Equal(t, "bijstand", result.Rule)
	require.Equal(t, OutcomeIndication, result.Outcome)
	require.Equal(t, Step{Key: "supplement", Law: participatiewet, Article: "19.1", Amount: 141946 - 100000}, result.Steps[len(result.Steps)-1])

	result = evaluate(t, bijstandLaw, Household{}, AgeAdult, Facts{"income.salary": 1500}).Result
	require.Equal(t, OutcomeNoIndication, result.Outcome)
}

func TestMinima(t *testing.T) {
	// above the norm, but below 120% of it
	require.Equal(t, OutcomeIndication, evaluate(t, minimaLaw, Household{}, AgeAdult, Facts{"income.salary": 1500}).Result.Outcome)
	require.Equal(t, OutcomeNoIndication, evaluate(t, minimaLaw, Household{}, AgeAdult, Facts{"income.salary": 1750}).Result.Outcome)
}

func TestPensionAgeIsAio(t *testing.T) {
	result := evaluate(t, bijstandLaw, Household{}, AgePension, Facts{"income.pensionstate": 1200}).Result
	require.Equal(t, "aio", result.Rule)
	require.Equal(t, OutcomeIndication, result.Outcome)
}

func TestUnder21IsNotEvaluated(t *testing.T) {
	for _, law := range Laws {
		require.Equal(t, OutcomeNotEvaluated, evaluate(t, law, Household{}, AgeUnder21, Facts{}).Result.Outcome, law.Id)
	}
}

func TestBijstandNotes(t *testing.T) {
	result := evaluate(t, bijstandLaw, Household{Partner: true}, AgeAdult, Facts{"subsidy.student": 300, "alimony.other": 200}).Result
	require.Equal(t, []string{"note_assets", "note_average", "note_partner_income", "note_student", "note_alimony"}, result.Notes)
}

func TestValidate(t *testing.T) {
	require.NoError(t, Household{}.Validate())
	require.Error(t, Household{Housemates: -1}.Validate())
	require.Error(t, Household{PartnerPensionAge: true}.Validate())
}

func TestLawById(t *testing.T) {
	for _, law := range Laws {
		found, ok := LawById(law.Id)
		require.True(t, ok)
		require.Equal(t, law.Id, found.Id)
		require.NotEmpty(t, law.Attributes)
	}
	_, ok := LawById("unknown")
	require.False(t, ok)
}
