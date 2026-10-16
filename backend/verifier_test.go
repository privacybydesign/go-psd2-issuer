package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
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

// disclosure discloses every attribute the rules need, as 0 unless given in
// values, grouped per credential like the request.
func disclosure(values map[string]string) [][]any {
	var cons [][]any
	byCredential := map[string]int{}
	for _, fact := range rules.Attributes() {
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
	return cons
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

	// one required disjunction per credential, with a single option
	disclose := irmaServer.request["disclose"].([]any)
	require.Len(t, disclose, 4)
	var requested []string
	for _, discon := range disclose {
		require.Len(t, discon, 1)
		for _, attribute := range discon.([]any)[0].([]any) {
			requested = append(requested, attribute.(string))
		}
	}
	require.Len(t, requested, len(rules.Attributes()))
	require.Contains(t, requested, "irma-demo.enablenow.income.salary")
	require.NotContains(t, requested, "irma-demo.enablenow.subsidy.rent")

	irmaServer.result["disclosed"] = disclosure(map[string]string{"income.salary": "900"})
	resp, body = env.do(t, http.MethodGet, "/api/verifier/result", nil)
	require.Equal(t, http.StatusOK, resp.StatusCode, string(body))
	var result VerifierResultResponse
	require.NoError(t, json.Unmarshal(body, &result))
	require.Equal(t, household, result.Household)
	require.Equal(t, int64(90000), result.Evaluation.Income)
	require.Equal(t, rules.OutcomeIndication, result.Evaluation.Results[0].Outcome)

	// the session is forgotten once the result is fetched
	resp, body = env.do(t, http.MethodGet, "/api/verifier/result", nil)
	require.Equal(t, http.StatusNotFound, resp.StatusCode)
	require.Equal(t, ErrorSessionNotFound, string(body))
}

func TestVerifierRejectsIncompleteDisclosure(t *testing.T) {
	env, irmaServer := newVerifierTestEnv(t)
	resp, body := env.do(t, http.MethodPost, "/api/verifier/start", VerifierStartPayload{Household: rules.Household{AgeGroup: rules.AgeAdult}})
	require.Equal(t, http.StatusOK, resp.StatusCode, string(body))

	cons := disclosure(nil)
	irmaServer.result["disclosed"] = cons[:len(cons)-1]
	resp, body = env.do(t, http.MethodGet, "/api/verifier/result", nil)
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
	require.Equal(t, ErrorInvalidProof, string(body))
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
