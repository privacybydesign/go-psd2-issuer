package rules

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAgeGroupAt(t *testing.T) {
	tests := []struct {
		dateOfBirth, date, ageGroup string
	}{
		{"2005-10-17", "2026-10-16", AgeUnder21},
		{"2005-10-16", "2026-10-16", AgeAdult},
		{"1959-10-17", "2026-10-16", AgeAdult},
		// 67 in 2026
		{"1959-10-16", "2026-10-16", AgePension},
		{"1940-01-01", "2026-10-16", AgePension},
		// 67 in 2027
		{"1960-12-31", "2027-12-31", AgePension},
		// 67 in 2028, pension age 67 years and three months
		{"1961-01-15", "2028-04-14", AgeAdult},
		{"1961-01-15", "2028-04-15", AgePension},
	}
	for _, tt := range tests {
		age, err := AgeGroupAt(day(tt.dateOfBirth), day(tt.date))
		require.NoError(t, err)
		require.Equal(t, tt.ageGroup, age, "%s on %s", tt.dateOfBirth, tt.date)
	}
}

func TestPensionAgeUnknown(t *testing.T) {
	_, err := AgeGroupAt(day("1962-06-01"), day("2029-07-01"))
	require.ErrorIs(t, err, ErrPensionAgeUnknown)
}
