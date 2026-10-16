package rules

import (
	"errors"
	"time"
)

var AowSource = Source{
	Id:      "algemene_ouderdomswet",
	Name:    "Algemene Ouderdomswet",
	Version: "2026-07-01",
	Url:     "https://wetten.overheid.nl/BWBR0002221/2026-07-01",
}

// aowExtraMonths is the state pension age above 67 years, per calendar year
// from 2028. Until 2027 it is at most 67 years (art. 7a AOW); from 2026 it
// is set every year by ministerial regulation: 67 years and
// three months in 2028 (art. 7a and the regulation setting it for 2028).
// Extend the map when a later year is set.
var aowExtraMonths = map[int]int{2028: 3}

var ErrPensionAgeUnknown = errors.New("state pension age not known for this date")

// PensionAgeReached reports whether someone born on dateOfBirth has reached
// the state pension age on date.
func PensionAgeReached(dateOfBirth, date time.Time) (bool, error) {
	at67 := dateOfBirth.AddDate(67, 0, 0)
	if at67.After(date) {
		return false, nil
	}
	// before 2028 the pension age was 67 at most, so it is reached at 67
	if at67.Year() <= 2027 {
		return true, nil
	}
	for year := at67.Year(); year <= date.Year(); year++ {
		extra, ok := aowExtraMonths[year]
		if !ok {
			return false, ErrPensionAgeUnknown
		}
		reached := at67.AddDate(0, extra, 0)
		if reached.Year() == year {
			return !reached.After(date), nil
		}
	}
	return false, nil
}

// AgeGroupAt derives the age group on date from the date of birth.
func AgeGroupAt(dateOfBirth, date time.Time) (string, error) {
	if dateOfBirth.AddDate(21, 0, 0).After(date) {
		return AgeUnder21, nil
	}
	pension, err := PensionAgeReached(dateOfBirth, date)
	if err != nil {
		return "", err
	}
	if pension {
		return AgePension, nil
	}
	return AgeAdult, nil
}
