package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"psd2-issuer/rules"
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

func newVerifierTestEnv(t *testing.T) (*testEnv, *fakeIrmaServer) {
	t.Helper()
	env := newTestEnv(t)
	irmaServer := newFakeIrmaServer(t)
	ids := map[string]string{}
	for _, key := range []string{"income", "subsidy", "alimony", "tax"} {
		ids[key] = "irma-demo.enablenow." + key
	}
	verifier, err := NewVerifier(irmaServer.server.URL+"/", env.state.jwtCreator.(*DefaultJwtCreator), ids)
	require.NoError(t, err)
	env.state.verifier = verifier
	return env, irmaServer
}

func TestVerifierFlow(t *testing.T) {
	env, irmaServer := newVerifierTestEnv(t)

	household := rules.Household{AgeGroup: rules.AgeAdult}
	resp, body := env.do(t, http.MethodPost, "/api/verifier/start", VerifierStartPayload{Household: household})
	require.Equal(t, http.StatusOK, resp.StatusCode, string(body))
	var start map[string]any
	require.NoError(t, json.Unmarshal(body, &start))
	require.Contains(t, start, "sessionPtr")
	require.Contains(t, start, "frontendRequest")
	require.NotContains(t, string(body), "requestor-token")

	// one optional disjunction per credential: [[attributes...], []]
	disclose := irmaServer.request["disclose"].([]any)
	require.Len(t, disclose, 4)
	income := disclose[0].([]any)
	require.Len(t, income, 2)
	require.Empty(t, income[1])
	require.Contains(t, income[0], "irma-demo.enablenow.income.salary")

	irmaServer.result["disclosed"] = [][]any{
		{disclosed("irma-demo.enablenow.income.salary", "900"), disclosed("irma-demo.enablenow.income.rent", "0")},
		{disclosed("irma-demo.enablenow.subsidy.rent", "350")},
	}
	resp, body = env.do(t, http.MethodGet, "/api/verifier/result", nil)
	require.Equal(t, http.StatusOK, resp.StatusCode, string(body))
	var result VerifierResultResponse
	require.NoError(t, json.Unmarshal(body, &result))
	require.Equal(t, household, result.Household)
	require.Equal(t, []string{"income", "subsidy"}, result.Disclosed)
	require.Equal(t, int64(90000), result.Evaluation.Income)
	require.Equal(t, rules.OutcomeIndication, result.Evaluation.Results[0].Outcome)

	// the session is forgotten once the result is fetched
	resp, body = env.do(t, http.MethodGet, "/api/verifier/result", nil)
	require.Equal(t, http.StatusNotFound, resp.StatusCode)
	require.Equal(t, ErrorSessionNotFound, string(body))
}

func TestVerifierRejectsInvalidProof(t *testing.T) {
	env, irmaServer := newVerifierTestEnv(t)
	resp, body := env.do(t, http.MethodPost, "/api/verifier/start", VerifierStartPayload{Household: rules.Household{AgeGroup: rules.AgeAdult}})
	require.Equal(t, http.StatusOK, resp.StatusCode, string(body))

	irmaServer.result["proofStatus"] = "EXPIRED"
	resp, body = env.do(t, http.MethodGet, "/api/verifier/result", nil)
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
	require.Equal(t, ErrorInvalidProof, string(body))
}

func TestVerifierResultBeforeDone(t *testing.T) {
	env, irmaServer := newVerifierTestEnv(t)
	resp, body := env.do(t, http.MethodPost, "/api/verifier/start", VerifierStartPayload{Household: rules.Household{AgeGroup: rules.AgeAdult}})
	require.Equal(t, http.StatusOK, resp.StatusCode, string(body))

	irmaServer.result["status"] = "CONNECTED"
	resp, body = env.do(t, http.MethodGet, "/api/verifier/result", nil)
	require.Equal(t, http.StatusConflict, resp.StatusCode)
	require.Equal(t, ErrorNotReady, string(body))
}

func TestVerifierRejectsInvalidHousehold(t *testing.T) {
	env, _ := newVerifierTestEnv(t)
	resp, body := env.do(t, http.MethodPost, "/api/verifier/start", VerifierStartPayload{Household: rules.Household{AgeGroup: "child"}})
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
	require.Equal(t, ErrorBadRequest, string(body))
}

func TestVerifierDisabled(t *testing.T) {
	env := newTestEnv(t)
	resp, body := env.do(t, http.MethodPost, "/api/verifier/start", VerifierStartPayload{Household: rules.Household{AgeGroup: rules.AgeAdult}})
	require.Equal(t, http.StatusNotFound, resp.StatusCode)
	require.Equal(t, ErrorVerifierDisabled, string(body))
}
