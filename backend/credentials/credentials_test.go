package credentials

import (
	"testing"
	"time"

	"psd2-issuer/enablenow"

	"github.com/stretchr/testify/require"
)

var now = time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)

func tx(date string, amount float64, category string) enablenow.Transaction {
	return enablenow.Transaction{
		BookDate: date,
		Amount:   amount,
		Currency: "EUR",
		Category: &enablenow.TransactionCategory{E1: &enablenow.E1Category{Category: category, Confidence: 0.9}},
	}
}

// monthly repeats a transaction on the given day of every month in the window.
func monthly(day int, amount float64, category string) []enablenow.Transaction {
	var txs []enablenow.Transaction
	for m := 1; m <= 12; m++ {
		d := time.Date(2025, time.Month(9+m), day, 0, 0, 0, 0, time.UTC)
		txs = append(txs, tx(d.Format("2006-01-02"), amount, category))
	}
	return txs
}

func byKey(creds []Credential) map[string]map[string]string {
	out := map[string]map[string]string{}
	for _, c := range creds {
		if c.Key != BankAccount {
			out[c.Key] = c.Attributes
		}
	}
	return out
}

func compute(txs []enablenow.Transaction) map[string]map[string]string {
	return byKey(Compute(nil, txs, "bank", Options{Months: 12, Rounding: 10, Now: now}))
}

func TestWindowIsLastFullMonths(t *testing.T) {
	from, to := Window(Options{Months: 12, Now: now})
	require.Equal(t, time.Date(2025, 10, 1, 0, 0, 0, 0, time.UTC), from)
	require.Equal(t, time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), to)
}

func TestIncome(t *testing.T) {
	var txs []enablenow.Transaction
	txs = append(txs, monthly(25, 3000, "income.salary")...)
	txs = append(txs, monthly(1, 400, "income.pension.private")...)
	txs = append(txs, tx("2026-03-10", 1200, "income.other"))
	// outside the window: current month and before the window
	txs = append(txs, tx("2026-10-02", 99999, "income.salary"))
	txs = append(txs, tx("2025-09-30", 99999, "income.salary"))

	income := compute(txs)[Income]
	require.Equal(t, "3000", income["salary"])
	require.Equal(t, "400", income["pension"])
	require.Equal(t, "100", income["otherincome"])
	require.Equal(t, "3500", income["total"])
	require.Equal(t, "12", income["salarymonths"])
	require.Equal(t, "12", income["months"])
	require.Equal(t, "2025-10-01", income["periodstart"])
	require.Equal(t, "2026-09-30", income["periodend"])
}

func TestHousingAndFixedCosts(t *testing.T) {
	var txs []enablenow.Transaction
	txs = append(txs, monthly(1, -1053, "housing.rent")...)
	txs = append(txs, monthly(3, -140, "insurance.health")...)
	txs = append(txs, monthly(3, -12, "insurance.other")...)
	txs = append(txs, monthly(5, -110, "expense.essential.utilities.energy")...)
	txs = append(txs, monthly(5, -21, "expense.essential.utilities.water")...)
	txs = append(txs, monthly(8, -45, "expense.essential.telecom")...)
	// a refund keeps the original category and is netted
	txs = append(txs, tx("2026-01-20", 120, "expense.essential.utilities.energy"))
	// groceries are not a fixed cost
	txs = append(txs, monthly(9, -300, "expense.essential.groceries")...)

	housing := compute(txs)[Housing]
	require.Equal(t, "rent", housing["housingtype"])
	require.Equal(t, "1050", housing["housingcost"])
	require.Equal(t, "100", housing["energy"])
	require.Equal(t, "20", housing["water"])
	require.Equal(t, "150", housing["insurance"])
	require.Equal(t, "50", housing["telecom"])
	require.Equal(t, "0", housing["municipaltaxes"])
	require.Equal(t, "1370", housing["total"])
}

func TestHousingType(t *testing.T) {
	require.Equal(t, "none", housingType(0, 0, 0))
	require.Equal(t, "mortgage", housingType(100, 900, 0))
	require.Equal(t, "rent", housingType(900, 0, 0))
	require.Equal(t, "other", housingType(0, 0, 500))
}

func TestDebtsAndRiskSignals(t *testing.T) {
	var txs []enablenow.Transaction
	txs = append(txs, monthly(12, -120, "loan.student")...)
	txs = append(txs, monthly(15, -240, "loan.vehicle_lease")...)
	txs = append(txs, tx("2026-02-03", -500, "loan.debt_collection"))
	txs = append(txs, tx("2026-05-03", -10, "gambling.lottery"))

	debts := compute(txs)[Debts]
	require.Equal(t, "360", debts["loanrepayments"])
	require.Equal(t, "120", debts["studentloan"])
	require.Equal(t, "yes", debts["debtcollection"])
	require.Equal(t, "no", debts["flashloan"])
	require.Equal(t, "no", debts["bnpl"])
	require.Equal(t, "yes", debts["gambling"])
}

func TestBenefits(t *testing.T) {
	var txs []enablenow.Transaction
	txs = append(txs, monthly(20, 154, "subsidy.health")...)
	txs = append(txs, monthly(20, 300, "subsidy.family.income")...)
	// repaying an overpaid benefit keeps the category with a negative amount
	txs = append(txs, tx("2026-06-01", -240, "subsidy.health"))

	benefits := compute(txs)[Benefits]
	require.Equal(t, "yes", benefits["receivesbenefits"])
	require.Equal(t, "130", benefits["healthcare"])
	require.Equal(t, "300", benefits["child"])
	require.Equal(t, "0", benefits["rent"])
	require.Equal(t, "430", benefits["total"])

	none := compute(nil)[Benefits]
	require.Equal(t, "no", none["receivesbenefits"])
	require.Equal(t, "0", none["total"])
}

func TestShortHistorySkipsPartialFirstMonth(t *testing.T) {
	// history starts mid June: June is partial, so only July-September count
	txs := []enablenow.Transaction{
		tx("2026-06-17", 3000, "income.salary"),
		tx("2026-07-25", 3000, "income.salary"),
		tx("2026-08-25", 3000, "income.salary"),
		tx("2026-09-25", 3000, "income.salary"),
	}
	income := compute(txs)[Income]
	require.Equal(t, "3", income["months"])
	require.Equal(t, "2026-07-01", income["periodstart"])
	require.Equal(t, "3000", income["salary"])
	require.Equal(t, "3", income["salarymonths"])
}

func TestIgnoresUncategorizedForeignAndLowConfidence(t *testing.T) {
	uncategorized := enablenow.Transaction{BookDate: "2025-10-01", Amount: 5000, Currency: "EUR"}
	foreign := tx("2025-10-01", 5000, "income.salary")
	foreign.Currency = "USD"
	unsure := tx("2025-10-01", 5000, "income.salary")
	unsure.Category.E1.Confidence = 0.1
	sure := tx("2025-10-01", 1200, "income.salary")

	creds := Compute(nil, []enablenow.Transaction{uncategorized, foreign, unsure, sure}, "bank",
		Options{Months: 12, Rounding: 10, MinConfidence: 0.5, Now: now})
	require.Equal(t, "100", byKey(creds)[Income]["salary"])
}

func TestBankAccounts(t *testing.T) {
	accounts := []enablenow.Account{
		{Iban: "nl91 abna 0417 1643 00", AccountHolder: " J. Jansen "},
		{Iban: "NL91ABNA0417164300"}, // duplicate
		{Iban: ""},                   // no IBAN, e.g. a credit card
		{Iban: "NL20INGB0001234567", AccountHolder: "J. Jansen"},
	}
	creds := Compute(accounts, nil, "ing", Options{Now: now})

	var accs []map[string]string
	for _, c := range creds {
		if c.Key == BankAccount {
			accs = append(accs, c.Attributes)
		}
	}
	require.Len(t, accs, 2)
	require.Equal(t, map[string]string{"iban": "NL91ABNA0417164300", "accountholder": "J. Jansen", "bank": "ing"}, accs[0])
	require.Equal(t, "NL20INGB0001234567", accs[1]["iban"])
}

func TestFakeClientData(t *testing.T) {
	fake := enablenow.NewFakeClient()
	fake.Now = func() time.Time { return now }
	consent := fake.CreateConsent()
	opts := Options{Months: 12, Rounding: 10, Now: now}
	from, to := Window(opts)
	txs, err := fake.ListTransactions(t.Context(), consent, from, to)
	require.NoError(t, err)
	accounts, err := fake.ListAccounts(t.Context(), consent)
	require.NoError(t, err)

	creds := Compute(accounts, txs, "fake-bank", opts)
	require.Len(t, creds, 6)
	got := byKey(creds)
	require.Equal(t, "3220", got[Income]["salary"])
	require.Equal(t, "rent", got[Housing]["housingtype"])
	require.Equal(t, "130", got[Debts]["studentloan"])
	require.Equal(t, "250", got[Benefits]["total"])
}

func TestRounding(t *testing.T) {
	require.Equal(t, "3200", roundAmount(3215.4, 50))
	require.Equal(t, "3250", roundAmount(3226, 50))
	require.Equal(t, "3215", roundAmount(3215.4, 1))
	require.Equal(t, "0", roundAmount(4, 10))
}
