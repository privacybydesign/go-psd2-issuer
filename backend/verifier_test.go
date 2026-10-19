package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"psd2-issuer/credentials"
	"psd2-issuer/rules"
	"strings"
	"testing"

	"github.com/golang-jwt/jwt/v4"
	"github.com/stretchr/testify/require"
)

// fakeIrmaServer accepts one disclosure session and answers its result.
type fakeIrmaServer struct {
	server *httptest.Server
	// request is the disclosure request of the last started session.
	request map[string]any
	result  map[string]any
}

func newFakeIrmaServer(t *testing.T) *fakeIrmaServer {
	t.Helper()
	keyBytes, err := os.ReadFile("../test-secrets/pub.pem")
	require.NoError(t, err)
	key, err := jwt.ParseRSAPublicKeyFromPEM(keyBytes)
	require.NoError(t, err)

	f := &fakeIrmaServer{result: map[string]any{"status": "DONE", "type": "disclosing", "proofStatus": "VALID"}}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /session", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		claims := jwt.MapClaims{}
		_, err := jwt.ParseWithClaims(string(body), claims, func(*jwt.Token) (any, error) { return key, nil })
		if err != nil || claims["iss"] != "psd2_issuer" {
			http.Error(w, "invalid jwt", http.StatusUnauthorized)
			return
		}
		f.request = claims["sprequest"].(map[string]any)["request"].(map[string]any)
		writeJson(w, http.StatusOK, map[string]any{
			"sessionPtr":      map[string]string{"u": "http://irma.example/irma/session/client", "irmaqr": "disclosing"},
			"token":           "requestor-token",
			"frontendRequest": map[string]string{"authorization": "frontend", "minProtocolVersion": "1.0", "maxProtocolVersion": "1.1"},
		})
	})
	mux.HandleFunc("GET /session/requestor-token/result", func(w http.ResponseWriter, r *http.Request) {
		writeJson(w, http.StatusOK, f.result)
	})
	f.server = httptest.NewServer(mux)
	t.Cleanup(f.server.Close)
	return f
}

func disclosed(id, value string) map[string]any {
	return map[string]any{"id": id, "rawvalue": value, "value": map[string]string{"": value}, "status": "PRESENT", "issuancetime": 1760000000}
}

// disclosure discloses every attribute a law needs, as 0 unless given in
// values, grouped per credential like the request, plus the date of birth.
func disclosure(law rules.Law, dateOfBirth string, values map[string]string) [][]any {
	var cons [][]any
	byCredential := map[string]int{}
	for _, fact := range law.Attributes {
		key, _, _ := strings.Cut(fact, ".")
		i, ok := byCredential[key]
		if !ok {
			i = len(cons)
			byCredential[key] = i
			cons = append(cons, nil)
		}
		value, ok := values[fact]
		if !ok {
			value = "0"
		}
		cons[i] = append(cons[i], disclosed("irma-demo.enablenow."+fact, value))
	}
	return append(cons, []any{disclosed(DefaultDateOfBirthAttribute, dateOfBirth)})
}

func newVerifierTestEnv(t *testing.T) (*testEnv, *fakeIrmaServer) {
	t.Helper()
	env := newTestEnv(t)
	irmaServer := newFakeIrmaServer(t)
	ids := map[string]string{}
	for _, key := range credentials.AllKeys {
		ids[key] = "irma-demo.enablenow." + key
	}
	verifier, err := NewVerifier(irmaServer.server.URL+"/", env.state.jwtCreator.(*DefaultJwtCreator), ids, "")
	require.NoError(t, err)
	env.state.verifier = verifier
	return env, irmaServer
}

func law(t *testing.T, id string) rules.Law {
	t.Helper()
	l, ok := rules.LawById(id)
	require.True(t, ok)
	return l
}

// requested lists the attributes of a disclosure request, checking that
// every disjunction is required (a single option).
func requested(t *testing.T, request map[string]any) []string {
	t.Helper()
	var attributes []string
	for _, discon := range request["disclose"].([]any) {
		require.Len(t, discon, 1)
		for _, attribute := range discon.([]any)[0].([]any) {
			attributes = append(attributes, attribute.(string))
		}
	}
	return attributes
}

func TestVerifierLaws(t *testing.T) {
	env, _ := newVerifierTestEnv(t)
	resp, body := env.do(t, http.MethodGet, "/api/verifier/laws", nil)
	require.Equal(t, http.StatusOK, resp.StatusCode, string(body))
	var laws []VerifierLaw
	require.NoError(t, json.Unmarshal(body, &laws))
	require.Len(t, laws, len(rules.Laws))
	require.Equal(t, "bijstand", laws[0].Id)
	require.Contains(t, laws[0].Attributes, "income.salary")
}

func TestVerifierBijstandFlow(t *testing.T) {
	env, irmaServer := newVerifierTestEnv(t)

	household := rules.Household{}
	resp, body := env.do(t, http.MethodPost, "/api/verifier/start", VerifierStartPayload{Law: "bijstand", Household: household})
	require.Equal(t, http.StatusOK, resp.StatusCode, string(body))
	var start map[string]any
	require.NoError(t, json.Unmarshal(body, &start))
	require.Contains(t, start, "sessionPtr")
	require.Contains(t, start, "frontendRequest")
	require.NotContains(t, string(body), "requestor-token")

	// exactly the attributes of the law and the date of birth, all required
	attributes := requested(t, irmaServer.request)
	require.Len(t, attributes, len(law(t, "bijstand").Attributes)+1)
	require.Contains(t, attributes, "irma-demo.enablenow.income.salary")
	require.Contains(t, attributes, DefaultDateOfBirthAttribute)
	require.NotContains(t, attributes, "irma-demo.enablenow.subsidy.rent")

	irmaServer.result["disclosed"] = disclosure(law(t, "bijstand"), "15-03-1985", map[string]string{"income.salary": "900"})
	resp, body = env.do(t, http.MethodGet, "/api/verifier/result", nil)
	require.Equal(t, http.StatusOK, resp.StatusCode, string(body))
	var result VerifierResultResponse
	require.NoError(t, json.Unmarshal(body, &result))
	require.Equal(t, household, result.Household)
	require.Equal(t, rules.AgeAdult, result.AgeGroup)
	require.NotContains(t, string(body), "1985")
	require.Equal(t, "bijstand", result.Evaluation.Law)
	require.Equal(t, int64(90000), result.Evaluation.Income)
	require.Equal(t, rules.OutcomeIndication, result.Evaluation.Result.Outcome)

	// the session is forgotten once the result is fetched
	resp, body = env.do(t, http.MethodGet, "/api/verifier/result", nil)
	require.Equal(t, http.StatusNotFound, resp.StatusCode)
	require.Equal(t, ErrorSessionNotFound, string(body))
}

func TestVerifierAgeComesFromTheDateOfBirth(t *testing.T) {
	env, irmaServer := newVerifierTestEnv(t)
	resp, body := env.do(t, http.MethodPost, "/api/verifier/start", VerifierStartPayload{Law: "bijstand"})
	require.Equal(t, http.StatusOK, resp.StatusCode, string(body))

	irmaServer.result["disclosed"] = disclosure(law(t, "bijstand"), "1940-01-01", map[string]string{"income.pensionstate": "1200"})
	resp, body = env.do(t, http.MethodGet, "/api/verifier/result", nil)
	require.Equal(t, http.StatusOK, resp.StatusCode, string(body))
	var result VerifierResultResponse
	require.NoError(t, json.Unmarshal(body, &result))
	require.Equal(t, rules.AgePension, result.AgeGroup)
	require.Equal(t, "aio", result.Evaluation.Result.Rule)
}

func TestVerifierKwijtschelding(t *testing.T) {
	env, irmaServer := newVerifierTestEnv(t)
	resp, body := env.do(t, http.MethodPost, "/api/verifier/start", VerifierStartPayload{Law: "kwijtschelding_gemeente", TaxAssessment: 450})
	require.Equal(t, http.StatusOK, resp.StatusCode, string(body))
	attributes := requested(t, irmaServer.request)
	require.Contains(t, attributes, "irma-demo.enablenow.housing.rent")
	require.Contains(t, attributes, "irma-demo.enablenow.subsidy.rent")

	irmaServer.result["disclosed"] = disclosure(law(t, "kwijtschelding_gemeente"), "15-03-1985", map[string]string{
		"income.salary": "1600", "housing.rent": "-900", "subsidy.rent": "250",
	})
	resp, body = env.do(t, http.MethodGet, "/api/verifier/result", nil)
	require.Equal(t, http.StatusOK, resp.StatusCode, string(body))
	var result VerifierResultResponse
	require.NoError(t, json.Unmarshal(body, &result))
	require.Equal(t, 450, result.TaxAssessment)
	require.Equal(t, rules.OutcomeIndication, result.Evaluation.Result.Outcome)
}

func TestVerifierRejectsIncompleteDisclosure(t *testing.T) {
	env, irmaServer := newVerifierTestEnv(t)
	resp, body := env.do(t, http.MethodPost, "/api/verifier/start", VerifierStartPayload{Law: "bijstand"})
	require.Equal(t, http.StatusOK, resp.StatusCode, string(body))

	// without the date of birth
	cons := disclosure(law(t, "bijstand"), "15-03-1985", nil)
	irmaServer.result["disclosed"] = cons[:len(cons)-1]
	resp, body = env.do(t, http.MethodGet, "/api/verifier/result", nil)
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
	require.Equal(t, ErrorInvalidProof, string(body))
}

func TestVerifierRejectsInvalidProof(t *testing.T) {
	env, irmaServer := newVerifierTestEnv(t)
	resp, body := env.do(t, http.MethodPost, "/api/verifier/start", VerifierStartPayload{Law: "bijstand"})
	require.Equal(t, http.StatusOK, resp.StatusCode, string(body))

	irmaServer.result["proofStatus"] = "EXPIRED"
	resp, body = env.do(t, http.MethodGet, "/api/verifier/result", nil)
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
	require.Equal(t, ErrorInvalidProof, string(body))
}

func TestVerifierResultBeforeDone(t *testing.T) {
	env, irmaServer := newVerifierTestEnv(t)
	resp, body := env.do(t, http.MethodPost, "/api/verifier/start", VerifierStartPayload{Law: "bijstand"})
	require.Equal(t, http.StatusOK, resp.StatusCode, string(body))

	irmaServer.result["status"] = "CONNECTED"
	resp, body = env.do(t, http.MethodGet, "/api/verifier/result", nil)
	require.Equal(t, http.StatusConflict, resp.StatusCode)
	require.Equal(t, ErrorNotReady, string(body))
}

func TestVerifierRejectsInvalidStart(t *testing.T) {
	for _, payload := range []VerifierStartPayload{
		{Law: "unknown"},
		{Law: "bijstand", Household: rules.Household{Housemates: -1}},
		// a tax assessment only applies to kwijtschelding
		{Law: "bijstand", TaxAssessment: 100},
		{Law: "kwijtschelding_gemeente", TaxAssessment: -1},
	} {
		// a new env each time, to stay below the rate limit
		env, _ := newVerifierTestEnv(t)
		resp, body := env.do(t, http.MethodPost, "/api/verifier/start", payload)
		require.Equal(t, http.StatusBadRequest, resp.StatusCode, "%+v", payload)
		require.Equal(t, ErrorBadRequest, string(body))
	}
}

func TestVerifierDisabled(t *testing.T) {
	env := newTestEnv(t)
	resp, body := env.do(t, http.MethodPost, "/api/verifier/start", VerifierStartPayload{Law: "bijstand"})
	require.Equal(t, http.StatusNotFound, resp.StatusCode)
	require.Equal(t, ErrorVerifierDisabled, string(body))
}

func TestParseDate(t *testing.T) {
	for _, value := range []string{"15-03-1985", "1985-03-15"} {
		date, err := parseDate(value)
		require.NoError(t, err)
		require.Equal(t, 1985, date.Year())
		require.Equal(t, 15, date.Day())
	}
	_, err := parseDate("15/03/1985")
	require.Error(t, err)
}
