package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"psd2-issuer/rules"
	"strconv"
	"strings"
	"time"

	"github.com/privacybydesign/irmago/irma"
)

// The demo verifier asks the user to disclose income and benefit attributes
// and evaluates a few social security rules (package rules) on them. The
// IRMA server session is started and its result fetched by the backend, so
// the browser never holds the requestor token and cannot alter the result.

// verifierCookieName holds the id of a verifier session; it is separate from
// the issuance session so the two do not replace each other.
const verifierCookieName = "psd2_verifier_session"

const ErrorVerifierDisabled = "error:verifier-disabled"
const ErrorIrmaServer = "error:irma-server"
const ErrorInvalidProof = "error:invalid-proof"

type DemoVerifierConfig struct {
	Enabled bool `json:"enabled"`
	// IrmaServerUrl is the IRMA server as reached from this backend, e.g.
	// http://irma:8088 in Docker Compose. Defaults to irma_server_url.
	IrmaServerUrl string `json:"irma_server_url,omitempty"`
}

// RequestSigner signs session requests as the requestor of this service.
type RequestSigner interface {
	SignRequest(request irma.SessionRequest) (string, error)
}

// Verification is what a verifier session keeps between start and result.
type Verification struct {
	// RequestorToken gives access to the session result at the IRMA
	// server; it never leaves the backend.
	RequestorToken string          `json:"requestor_token"`
	Household      rules.Household `json:"household"`
}

type Verifier struct {
	irmaServerURL string
	signer        RequestSigner
	client        *http.Client
	request       *irma.DisclosureRequest
	// facts maps the Yivi attribute ids to the keys of rules.Facts.
	facts map[irma.AttributeTypeIdentifier]string
}

// NewVerifier builds the disclosure request for the attributes the rules
// need, and nothing else. All of them are required: the income per art. 32 is
// the sum of them, so it cannot be determined without any one of them.
func NewVerifier(irmaServerURL string, signer RequestSigner, credentialIds map[string]string) (*Verifier, error) {
	v := &Verifier{
		irmaServerURL: strings.TrimRight(irmaServerURL, "/"),
		signer:        signer,
		client:        &http.Client{Timeout: 10 * time.Second},
		request:       irma.NewDisclosureRequest(),
		facts:         map[irma.AttributeTypeIdentifier]string{},
	}

	var order []string
	byCredential := map[string]irma.AttributeCon{}
	for _, fact := range rules.Attributes() {
		key, attribute, _ := strings.Cut(fact, ".")
		id, ok := credentialIds[key]
		if !ok {
			return nil, fmt.Errorf("no credential id for %q", key)
		}
		if _, ok := byCredential[key]; !ok {
			order = append(order, key)
		}
		typeId := irma.NewAttributeTypeIdentifier(id + "." + attribute)
		byCredential[key] = append(byCredential[key], irma.AttributeRequest{Type: typeId})
		v.facts[typeId] = fact
	}
	for _, key := range order {
		v.request.Disclose = append(v.request.Disclose, irma.AttributeDisCon{byCredential[key]})
	}
	return v, nil
}

// irmaSessionPackage is the IRMA server's response to a new session.
type irmaSessionPackage struct {
	SessionPtr      json.RawMessage `json:"sessionPtr"`
	Token           string          `json:"token"`
	FrontendRequest json.RawMessage `json:"frontendRequest"`
}

// irmaSessionResult holds the fields of the IRMA server's session result
// that the verifier uses.
type irmaSessionResult struct {
	Status      irma.ServerStatus            `json:"status"`
	Type        irma.Action                  `json:"type"`
	ProofStatus irma.ProofStatus             `json:"proofStatus"`
	Disclosed   [][]*irma.DisclosedAttribute `json:"disclosed"`
}

func (v *Verifier) startSession(ctx context.Context) (*irmaSessionPackage, error) {
	jwt, err := v.signer.SignRequest(v.request)
	if err != nil {
		return nil, fmt.Errorf("failed to sign disclosure request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, v.irmaServerURL+"/session", bytes.NewBufferString(jwt))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "text/plain")
	var pkg irmaSessionPackage
	if err := v.do(req, &pkg); err != nil {
		return nil, err
	}
	if pkg.Token == "" || len(pkg.SessionPtr) == 0 {
		return nil, errors.New("IRMA server returned no session")
	}
	return &pkg, nil
}

func (v *Verifier) sessionResult(ctx context.Context, token string) (*irmaSessionResult, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, v.irmaServerURL+"/session/"+url.PathEscape(token)+"/result", nil)
	if err != nil {
		return nil, err
	}
	var result irmaSessionResult
	if err := v.do(req, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (v *Verifier) do(req *http.Request, out any) error {
	resp, err := v.client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("IRMA server returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return json.Unmarshal(body, out)
}

// readFacts converts the disclosed attributes to rules.Facts. Every attribute
// the rules need must be present.
func (v *Verifier) readFacts(disclosed [][]*irma.DisclosedAttribute) (rules.Facts, error) {
	facts := rules.Facts{}
	for _, con := range disclosed {
		for _, attribute := range con {
			if attribute == nil || attribute.Status != irma.AttributeProofStatusPresent || attribute.RawValue == nil {
				continue
			}
			fact, ok := v.facts[attribute.Identifier]
			if !ok {
				continue
			}
			value, err := strconv.Atoi(*attribute.RawValue)
			if err != nil {
				return nil, fmt.Errorf("attribute %s is not a number", attribute.Identifier)
			}
			facts[fact] = value
		}
	}
	for id, fact := range v.facts {
		if _, ok := facts[fact]; !ok {
			return nil, fmt.Errorf("attribute %s was not disclosed", id)
		}
	}
	return facts, nil
}

// -----------------------------------------------------------------------------------

type VerifierStartPayload struct {
	Household rules.Household `json:"household"`
}

type VerifierStartResponse struct {
	SessionPtr      json.RawMessage `json:"sessionPtr"`
	FrontendRequest json.RawMessage `json:"frontendRequest,omitempty"`
}

// handleVerifierStart starts a disclosure session at the IRMA server for the
// household the user declared.
func handleVerifierStart(state *ServerState, w http.ResponseWriter, r *http.Request) {
	endpoint := r.URL.Path
	if state.verifier == nil {
		respondWithErr(w, http.StatusNotFound, ErrorVerifierDisabled, "demo verifier is disabled", nil, "endpoint", endpoint)
		return
	}
	ip := getIpAddressForRequest(r, state.trustedProxies)
	logReceivedRequest(r, ip)

	allow, timeout, err := state.startRateLimiter.Allow("verify-ip:" + ip)
	if err != nil {
		respondWithErr(w, http.StatusInternalServerError, ErrorInternal, "rate limiter failed", err, "endpoint", endpoint)
		return
	}
	if !allow {
		respondWithRateLimitErr(w, endpoint, ip, timeout)
		return
	}

	var body VerifierStartPayload
	if !readJson(w, r, &body) {
		return
	}
	if err := body.Household.Validate(); err != nil {
		respondWithErr(w, http.StatusBadRequest, ErrorBadRequest, "invalid household", err, "endpoint", endpoint)
		return
	}

	pkg, err := state.verifier.startSession(r.Context())
	if err != nil {
		respondWithErr(w, http.StatusBadGateway, ErrorIrmaServer, "failed to start disclosure session", err, "endpoint", endpoint)
		return
	}

	if old := verifierIdFromCookie(r); old != "" {
		if err := state.sessionStorage.Delete(old); err != nil {
			slog.Error("failed to delete verifier session", "error", err)
		}
	}
	session := NewSession(time.Now())
	session.Status = StatusVerifying
	session.Verification = &Verification{RequestorToken: pkg.Token, Household: body.Household}
	if err := state.sessionStorage.Put(session); err != nil {
		respondWithErr(w, http.StatusInternalServerError, ErrorInternal, "failed to store session", err, "endpoint", endpoint)
		return
	}
	setVerifierCookie(state, w, session.Id, int(SessionTTL.Seconds()))
	writeJson(w, http.StatusOK, VerifierStartResponse{SessionPtr: pkg.SessionPtr, FrontendRequest: pkg.FrontendRequest})
}

type VerifierResultResponse struct {
	Household  rules.Household  `json:"household"`
	Evaluation rules.Evaluation `json:"evaluation"`
}

// handleVerifierResult fetches the disclosed attributes from the IRMA server
// and evaluates the rules. The session is forgotten afterwards.
func handleVerifierResult(state *ServerState, w http.ResponseWriter, r *http.Request) {
	endpoint := r.URL.Path
	if state.verifier == nil {
		respondWithErr(w, http.StatusNotFound, ErrorVerifierDisabled, "demo verifier is disabled", nil, "endpoint", endpoint)
		return
	}
	id := verifierIdFromCookie(r)
	if id == "" {
		respondWithErr(w, http.StatusNotFound, ErrorSessionNotFound, "no verifier cookie", nil, "endpoint", endpoint)
		return
	}
	session, err := state.sessionStorage.Get(id)
	if errors.Is(err, ErrSessionNotFound) || (err == nil && session.Verification == nil) {
		respondWithErr(w, http.StatusNotFound, ErrorSessionNotFound, "verifier session not found", nil, "endpoint", endpoint)
		return
	}
	if err != nil {
		respondWithErr(w, http.StatusInternalServerError, ErrorInternal, "failed to load session", err, "endpoint", endpoint)
		return
	}

	result, err := state.verifier.sessionResult(r.Context(), session.Verification.RequestorToken)
	if err != nil {
		respondWithErr(w, http.StatusBadGateway, ErrorIrmaServer, "failed to fetch session result", err, "endpoint", endpoint)
		return
	}
	if result.Status != irma.ServerStatusDone {
		respondWithErr(w, http.StatusConflict, ErrorNotReady, "disclosure session not done", nil, "endpoint", endpoint, "status", result.Status)
		return
	}
	if result.Type != irma.ActionDisclosing || result.ProofStatus != irma.ProofStatusValid {
		respondWithErr(w, http.StatusBadRequest, ErrorInvalidProof, "disclosure not valid", nil, "endpoint", endpoint, "type", result.Type, "proof_status", result.ProofStatus)
		return
	}
	facts, err := state.verifier.readFacts(result.Disclosed)
	if err != nil {
		respondWithErr(w, http.StatusBadRequest, ErrorInvalidProof, "unexpected disclosure", err, "endpoint", endpoint)
		return
	}
	evaluation, err := rules.Evaluate(session.Verification.Household, facts, time.Now())
	if err != nil {
		respondWithErr(w, http.StatusInternalServerError, ErrorInternal, "failed to evaluate rules", err, "endpoint", endpoint)
		return
	}

	if err := state.sessionStorage.Delete(id); err != nil {
		slog.Error("failed to delete verifier session", "error", err)
	}
	setVerifierCookie(state, w, "", -1)

	slog.Info("evaluated disclosed attributes", "law_version", evaluation.Source.Version)
	w.Header().Set("Cache-Control", "no-store")
	writeJson(w, http.StatusOK, VerifierResultResponse{
		Household:  session.Verification.Household,
		Evaluation: evaluation,
	})
}

func verifierIdFromCookie(r *http.Request) string {
	cookie, err := r.Cookie(verifierCookieName)
	if err != nil {
		return ""
	}
	return cookie.Value
}

func setVerifierCookie(state *ServerState, w http.ResponseWriter, value string, maxAge int) {
	http.SetCookie(w, &http.Cookie{
		Name:     verifierCookieName,
		Value:    value,
		Path:     "/api/verifier",
		MaxAge:   maxAge,
		HttpOnly: true,
		Secure:   state.secureCookies,
		SameSite: http.SameSiteStrictMode,
	})
}
