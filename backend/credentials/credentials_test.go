package credentials

import (
	"regexp"
	"strings"
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

func TestSignedNetMonthlyAveragePerE1Code(t *testing.T) {
	var txs []enablenow.Transaction
	txs = append(txs, monthly(25, 3000, "income.salary")...)
	txs = append(txs, monthly(1, 400, "income.pension.private")...)
	txs = append(txs, tx("2026-03-10", 1200, "income.other"))
	txs = append(txs, monthly(1, -1053, "housing.rent")...)
	txs = append(txs, monthly(5, -110, "expense.essential.utilities.energy")...)
	// a refund keeps the original category and is netted
	txs = append(txs, tx("2026-01-20", 120, "expense.essential.utilities.energy"))
	// a parent code does not include its children
	txs = append(txs, monthly(20, 154, "subsidy.family")...)
	txs = append(txs, monthly(20, 300, "subsidy.family.income")...)
	// two-way categories keep their sign
	txs = append(txs, monthly(2, -250, "alimony.other")...)
	txs = append(txs, tx("2026-04-02", 600, "alimony.other"))
	txs = append(txs, monthly(15, -240, "loan.vehicle_lease")...)
	txs = append(txs, monthly(15, -55, "insurance.vehicle")...)
	// outside the window: current month and before the window
	txs = append(txs, tx("2026-10-02", 99999, "income.salary"))
	txs = append(txs, tx("2025-09-30", 99999, "income.salary"))
	// codes that are not part of any credential
	txs = append(txs, monthly(26, -400, "internal.other")...)
	txs = append(txs, monthly(26, -400, "office.rent")...)

	got := compute(txs)
	require.Equal(t, "3000", got["income"]["salary"])
	require.Equal(t, "400", got["income"]["pensionprivate"])
	require.Equal(t, "0", got["income"]["pensionstate"])
	require.Equal(t, "100", got["income"]["other"])
	require.Equal(t, "-1050", got["housing"]["rent"])
	require.Equal(t, "-100", got["essential"]["utilitiesenergy"])
	require.Equal(t, "150", got["subsidy"]["family"])
	require.Equal(t, "300", got["subsidy"]["familyincome"])
	require.Equal(t, "-200", got["alimony"]["other"])
	require.Equal(t, "-240", got["loan"]["vehiclelease"])
	require.Equal(t, "-60", got["insurance"]["vehicle"])
	require.Equal(t, "0", got["transport"]["fuel"])

	for key, attrs := range got {
		require.Equal(t, "12", attrs["months"], key)
		require.Equal(t, "2025-10-01", attrs["periodstart"], key)
		require.Equal(t, "2026-09-30", attrs["periodend"], key)
	}
}

func TestEveryE1CodeHasAnAttribute(t *testing.T) {
	codes := map[string]bool{}
	for _, def := range Defs.Credentials {
		for _, attr := range def.Attributes {
			if attr.E1 != "" {
				codes[attr.E1] = true
			}
		}
	}
	// all retail codes except internal.other, from
	// https://docs.enablenow.nl/categorization/engines/e1-engine/categories
	retail := []string{
		"income.rent", "income.salary", "income.pension.state", "income.pension.private", "income.pension.other", "income.other",
		"housing.rent", "housing.mortgage", "housing.other",
		"subsidy.family", "subsidy.family.income", "subsidy.health", "subsidy.disability", "subsidy.social", "subsidy.social.other",
		"subsidy.rent", "subsidy.unemployment", "subsidy.student", "subsidy.other",
		"loan.debt_collection", "loan.student", "loan.flash", "loan.bnpl", "loan.credit_card", "loan.vehicle_lease", "loan.private", "loan.other",
		"alimony.other",
		"insurance.health", "insurance.vehicle", "insurance.life", "insurance.other",
		"tax.income", "tax.vehicle", "tax.corporate", "tax.payroll", "tax.social", "tax.vat", "tax.other",
		"cash.other",
		"gambling.lottery", "gambling.other",
		"investment.savings", "investment.crypto", "investment.pension", "investment.other",
		"expense.essential.groceries", "expense.essential.utilities.energy", "expense.essential.utilities.water",
		"expense.essential.utilities.other", "expense.essential.childcare", "expense.essential.clothing", "expense.essential.medical",
		"expense.essential.personal_care", "expense.essential.education", "expense.essential.financial", "expense.essential.telecom",
		"expense.essential.municipal", "expense.essential.other",
		"expense.transport.public_transport", "expense.transport.fuel", "expense.transport.vehicle", "expense.transport.parking",
		"expense.transport.other",
		"expense.lifestyle.restaurants_bars", "expense.lifestyle.streaming", "expense.lifestyle.subscriptions", "expense.lifestyle.sports",
		"expense.lifestyle.culture", "expense.lifestyle.travel", "expense.lifestyle.donations", "expense.lifestyle.pets",
		"expense.lifestyle.other",
		"expense.salary", "expense.other",
		"other.fines", "other.private_transfer", "other.other",
	}
	for _, code := range retail {
		require.True(t, codes[code], "no attribute for %s", code)
		delete(codes, code)
	}
	require.Empty(t, codes, "attributes for unknown E1 codes")
}

func TestDefinitionsAreConsistent(t *testing.T) {
	validId := regexp.MustCompile(`^[a-z0-9]+$`)
	keys := map[string]bool{}
	for _, def := range Defs.Credentials {
		require.True(t, validId.MatchString(def.Key), def.Key)
		require.False(t, keys[def.Key], "duplicate credential %s", def.Key)
		keys[def.Key] = true
		require.NotEmpty(t, def.Name.En)
		require.NotEmpty(t, def.Name.Nl)

		ids := map[string]bool{}
		for _, p := range Defs.PeriodAttributes {
			ids[p.Id] = true
		}
		for _, attr := range def.Attributes {
			require.True(t, validId.MatchString(attr.Id), attr.Id)
			require.False(t, ids[attr.Id], "duplicate attribute %s.%s", def.Key, attr.Id)
			ids[attr.Id] = true
			require.NotEmpty(t, attr.Name.En)
			require.NotEmpty(t, attr.Name.Nl)
			require.NotEmpty(t, attr.Description.En)
			require.NotEmpty(t, attr.Description.Nl)
			if def.E1Group == "" {
				require.Empty(t, attr.E1)
				continue
			}
			// the attribute id is the E1 code below the group, without separators
			require.True(t, strings.HasPrefix(attr.E1, def.E1Group+"."), attr.E1)
			want := strings.NewReplacer(".", "", "_", "").Replace(strings.TrimPrefix(attr.E1, def.E1Group+"."))
			require.Equal(t, want, attr.Id, attr.E1)
		}
	}
	require.True(t, keys[BankAccount])
}

func TestShortHistorySkipsPartialFirstMonth(t *testing.T) {
	// history starts mid June: June is partial, so only July-September count
	txs := []enablenow.Transaction{
		tx("2026-06-17", 3000, "income.salary"),
		tx("2026-07-25", 3000, "income.salary"),
		tx("2026-08-25", 3000, "income.salary"),
		tx("2026-09-25", 3000, "income.salary"),
	}
	income := compute(txs)["income"]
	require.Equal(t, "3", income["months"])
	require.Equal(t, "2026-07-01", income["periodstart"])
	require.Equal(t, "3000", income["salary"])
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
	require.Equal(t, "100", byKey(creds)["income"]["salary"])
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
	require.Len(t, creds, len(AllKeys)+1) // two bank accounts
	got := byKey(creds)
	require.Equal(t, "3220", got["income"]["salary"])
	require.Equal(t, "-1050", got["housing"]["rent"])
	require.Equal(t, "-130", got["loan"]["student"])
	require.Equal(t, "150", got["subsidy"]["health"])
}

func TestRounding(t *testing.T) {
	require.Equal(t, "3200", roundAmount(3215.4, 50))
	require.Equal(t, "3250", roundAmount(3226, 50))
	require.Equal(t, "3215", roundAmount(3215.4, 1))
	require.Equal(t, "0", roundAmount(4, 10))
	require.Equal(t, "-1050", roundAmount(-1053, 10))
	require.Equal(t, "0", roundAmount(-4, 10))
}
