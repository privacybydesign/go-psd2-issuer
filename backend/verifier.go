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

// The demo verifier lets the user choose a law (package rules), asks for
// exactly the attributes that law needs plus the date of birth (and the
// postcode, for the laws that check where the applicant lives), and
// evaluates the law on them. The IRMA server session is started and its
// result fetched by the backend, so the browser never holds the requestor
// token and cannot alter the result.

// verifierCookieName holds the id of a verifier session; it is separate from
// the issuance session so the two do not replace each other.
const verifierCookieName = "psd2_verifier_session"

const ErrorVerifierDisabled = "error:verifier-disabled"
const ErrorIrmaServer = "error:irma-server"
const ErrorInvalidProof = "error:invalid-proof"

// DefaultDateOfBirthAttribute is the attribute the age is derived from.
const DefaultDateOfBirthAttribute = "irma-demo.gemeente.personalData.dateofbirth"

// DefaultPostcodeAttribute is the attribute the municipality is checked on.
const DefaultPostcodeAttribute = "irma-demo.gemeente.address.zipcode"

// maxTaxAssessment bounds the tax assessment a user can enter, in euros.
const maxTaxAssessment = 100_000

type DemoVerifierConfig struct {
	Enabled bool `json:"enabled"`
	// IrmaServerUrl is the IRMA server as reached from this backend, e.g.
	// http://irma:8088 in Docker Compose. Defaults to irma_server_url.
	IrmaServerUrl string `json:"irma_server_url,omitempty"`
	// DateOfBirthAttribute is the Yivi attribute with the date of birth,
	// e.g. pbdf.gemeente.personalData.dateofbirth. Defaults to
	// DefaultDateOfBirthAttribute.
	DateOfBirthAttribute string `json:"date_of_birth_attribute,omitempty"`
	// PostcodeAttribute is the Yivi attribute with the postcode of the
	// address, e.g. pbdf.gemeente.address.zipcode. Defaults to
	// DefaultPostcodeAttribute.
	PostcodeAttribute string `json:"postcode_attribute,omitempty"`
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
	Law            string          `json:"law"`
	Household      rules.Household `json:"household"`
	TaxAssessment  int             `json:"tax_assessment,omitempty"`
}

type Verifier struct {
	irmaServerURL string
	signer        RequestSigner
	client        *http.Client
	// attributes maps the keys of rules.Facts to the Yivi attribute ids.
	attributes  map[string]irma.AttributeTypeIdentifier
	dateOfBirth irma.AttributeTypeIdentifier
	postcode    irma.AttributeTypeIdentifier
}

func NewVerifier(irmaServerURL string, signer RequestSigner, credentialIds map[string]string, dateOfBirthAttribute, postcodeAttribute string) (*Verifier, error) {
	if dateOfBirthAttribute == "" {
		dateOfBirthAttribute = DefaultDateOfBirthAttribute
	}
	if postcodeAttribute == "" {
		postcodeAttribute = DefaultPostcodeAttribute
	}
	for _, attribute := range []string{dateOfBirthAttribute, postcodeAttribute} {
		if strings.Count(attribute, ".") != 3 {
			return nil, fmt.Errorf("attribute %q is not of the form scheme.issuer.credential.attribute", attribute)
		}
	}
	v := &Verifier{
		irmaServerURL: strings.TrimRight(irmaServerURL, "/"),
		signer:        signer,
		client:        &http.Client{Timeout: 10 * time.Second},
		attributes:    map[string]irma.AttributeTypeIdentifier{},
		dateOfBirth:   irma.NewAttributeTypeIdentifier(dateOfBirthAttribute),
		postcode:      irma.NewAttributeTypeIdentifier(postcodeAttribute),
	}
	for _, law := range rules.Laws {
		for _, fact := range law.Attributes {
			key, attribute, _ := strings.Cut(fact, ".")
			id, ok := credentialIds[key]
			if !ok {
				return nil, fmt.Errorf("no credential id for %q", key)
			}
			v.attributes[fact] = irma.NewAttributeTypeIdentifier(id + "." + attribute)
		}
	}
	return v, nil
}

// request builds the disclosure request for a law: the attributes it needs,
// grouped per credential, the date of birth and, if the law needs it, the
// postcode. All of them are required, because the law cannot be applied
// without any one of them.
func (v *Verifier) request(law rules.Law) *irma.DisclosureRequest {
	request := irma.NewDisclosureRequest()
	var order []string
	byCredential := map[string]irma.AttributeCon{}
	for _, fact := range law.Attributes {
		key, _, _ := strings.Cut(fact, ".")
		if _, ok := byCredential[key]; !ok {
			order = append(order, key)
		}
		byCredential[key] = append(byCredential[key], irma.AttributeRequest{Type: v.attributes[fact]})
	}
	for _, key := range order {
		request.Disclose = append(request.Disclose, irma.AttributeDisCon{byCredential[key]})
	}
	request.Disclose = append(request.Disclose, irma.AttributeDisCon{{{Type: v.dateOfBirth}}})
	if law.NeedsPostcode {
		request.Disclose = append(request.Disclose, irma.AttributeDisCon{{{Type: v.postcode}}})
	}
	return request
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

func (v *Verifier) startSession(ctx context.Context, law rules.Law) (*irmaSessionPackage, error) {
	jwt, err := v.signer.SignRequest(v.request(law))
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

// dateFormats are the formats of the date of birth in the gemeente
// credentials (DD-MM-YYYY) and ISO 8601.
var dateFormats = []string{"02-01-2006", time.DateOnly}

func parseDate(value string) (time.Time, error) {
	for _, format := range dateFormats {
		if t, err := time.ParseInLocation(format, strings.TrimSpace(value), time.Local); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("unknown date format %q", value)
}

// disclosure is what readDisclosure reads from the disclosed attributes.
type disclosure struct {
	facts       rules.Facts
	dateOfBirth time.Time
	// postcode is empty unless the law needs it
	postcode string
}

// readDisclosure converts the disclosed attributes to the facts of a law,
// the date of birth and the postcode. Every attribute the law needs must be
// present.
func (v *Verifier) readDisclosure(disclosed [][]*irma.DisclosedAttribute, law rules.Law) (disclosure, error) {
	present := map[irma.AttributeTypeIdentifier]string{}
	for _, con := range disclosed {
		for _, attribute := range con {
			if attribute != nil && attribute.Status == irma.AttributeProofStatusPresent && attribute.RawValue != nil {
				present[attribute.Identifier] = *attribute.RawValue
			}
		}
	}
	facts := rules.Facts{}
	for _, fact := range law.Attributes {
		id := v.attributes[fact]
		raw, ok := present[id]
		if !ok {
			return disclosure{}, fmt.Errorf("attribute %s was not disclosed", id)
		}
		value, err := strconv.Atoi(raw)
		if err != nil {
			return disclosure{}, fmt.Errorf("attribute %s is not a number", id)
		}
		facts[fact] = value
	}
	raw, ok := present[v.dateOfBirth]
	if !ok {
		return disclosure{}, fmt.Errorf("attribute %s was not disclosed", v.dateOfBirth)
	}
	dateOfBirth, err := parseDate(raw)
	if err != nil {
		return disclosure{}, fmt.Errorf("attribute %s: %w", v.dateOfBirth, err)
	}
	result := disclosure{facts: facts, dateOfBirth: dateOfBirth}
	if law.NeedsPostcode {
		postcode := strings.TrimSpace(present[v.postcode])
		if postcode == "" {
			return disclosure{}, fmt.Errorf("attribute %s was not disclosed", v.postcode)
		}
		result.postcode = postcode
	}
	return result, nil
}

// -----------------------------------------------------------------------------------

type VerifierLaw struct {
	Id string `json:"id"`
	// Attributes are the facts the law asks for, keyed as in the
	// definitions ("income.salary"); the date of birth, and the postcode if
	// NeedsPostcode, come on top.
	Attributes         []string `json:"attributes"`
	NeedsTaxAssessment bool     `json:"needs_tax_assessment"`
	NeedsPostcode      bool     `json:"needs_postcode"`
	// Checked and NotChecked are the keys of the criteria the check does
	// and does not establish.
	Checked    []string `json:"checked"`
	NotChecked []string `json:"not_checked"`
}

// handleVerifierLaws lists the laws, so the user sees what each one asks
// for before sharing anything.
func handleVerifierLaws(state *ServerState, w http.ResponseWriter, r *http.Request) {
	if state.verifier == nil {
		respondWithErr(w, http.StatusNotFound, ErrorVerifierDisabled, "demo verifier is disabled", nil, "endpoint", r.URL.Path)
		return
	}
	laws := make([]VerifierLaw, len(rules.Laws))
	for i, law := range rules.Laws {
		checked, notChecked, err := law.Criteria(time.Now())
		if err != nil {
			respondWithErr(w, http.StatusInternalServerError, ErrorInternal, "failed to list criteria", err, "endpoint", r.URL.Path, "law", law.Id)
			return
		}
		laws[i] = VerifierLaw{Id: law.Id, Attributes: law.Attributes, NeedsTaxAssessment: law.NeedsTaxAssessment, NeedsPostcode: law.NeedsPostcode, Checked: checked, NotChecked: notChecked}
	}
	writeJson(w, http.StatusOK, laws)
}

type VerifierStartPayload struct {
	Law       string          `json:"law"`
	Household rules.Household `json:"household"`
	// TaxAssessment is the amount of the assessment in euros, optional.
	TaxAssessment int `json:"tax_assessment,omitempty"`
}

type VerifierStartResponse struct {
	SessionPtr      json.RawMessage `json:"sessionPtr"`
	FrontendRequest json.RawMessage `json:"frontendRequest,omitempty"`
}

// handleVerifierStart starts a disclosure session at the IRMA server for the
// chosen law and the household the user declared.
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
	law, ok := rules.LawById(body.Law)
	if !ok {
		respondWithErr(w, http.StatusBadRequest, ErrorBadRequest, "unknown law", nil, "endpoint", endpoint, "law", body.Law)
		return
	}
	if err := body.Household.Validate(); err != nil {
		respondWithErr(w, http.StatusBadRequest, ErrorBadRequest, "invalid household", err, "endpoint", endpoint)
		return
	}
	if body.TaxAssessment < 0 || body.TaxAssessment > maxTaxAssessment || (body.TaxAssessment != 0 && !law.NeedsTaxAssessment) {
		respondWithErr(w, http.StatusBadRequest, ErrorBadRequest, "invalid tax assessment", nil, "endpoint", endpoint)
		return
	}

	pkg, err := state.verifier.startSession(r.Context(), law)
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
	session.Verification = &Verification{
		RequestorToken: pkg.Token,
		Law:            law.Id,
		Household:      body.Household,
		TaxAssessment:  body.TaxAssessment,
	}
	if err := state.sessionStorage.Put(session); err != nil {
		respondWithErr(w, http.StatusInternalServerError, ErrorInternal, "failed to store session", err, "endpoint", endpoint)
		return
	}
	setVerifierCookie(state, w, session.Id, int(SessionTTL.Seconds()))
	writeJson(w, http.StatusOK, VerifierStartResponse{SessionPtr: pkg.SessionPtr, FrontendRequest: pkg.FrontendRequest})
}

type VerifierResultResponse struct {
	Household     rules.Household  `json:"household"`
	AgeGroup      string           `json:"age_group"`
	TaxAssessment int              `json:"tax_assessment,omitempty"`
	Evaluation    rules.Evaluation `json:"evaluation"`
}

// handleVerifierResult fetches the disclosed attributes from the IRMA server
// and evaluates the law. The session is forgotten afterwards.
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
	verification := session.Verification
	law, ok := rules.LawById(verification.Law)
	if !ok {
		respondWithErr(w, http.StatusNotFound, ErrorSessionNotFound, "law of verifier session not found", nil, "endpoint", endpoint)
		return
	}

	result, err := state.verifier.sessionResult(r.Context(), verification.RequestorToken)
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
	disclosed, err := state.verifier.readDisclosure(result.Disclosed, law)
	if err != nil {
		respondWithErr(w, http.StatusBadRequest, ErrorInvalidProof, "unexpected disclosure", err, "endpoint", endpoint)
		return
	}

	now := time.Now()
	ageGroup, err := rules.AgeGroupAt(disclosed.dateOfBirth, now)
	if err != nil {
		respondWithErr(w, http.StatusInternalServerError, ErrorInternal, "failed to determine age group", err, "endpoint", endpoint)
		return
	}
	evaluation, err := law.Evaluate(rules.Input{
		Household:     verification.Household,
		AgeGroup:      ageGroup,
		Facts:         disclosed.facts,
		Date:          now,
		TaxAssessment: verification.TaxAssessment,
		Postcode:      disclosed.postcode,
	})
	if err != nil {
		respondWithErr(w, http.StatusInternalServerError, ErrorInternal, "failed to evaluate law", err, "endpoint", endpoint)
		return
	}

	if err := state.sessionStorage.Delete(id); err != nil {
		slog.Error("failed to delete verifier session", "error", err)
	}
	setVerifierCookie(state, w, "", -1)

	slog.Info("evaluated disclosed attributes", "law", law.Id)
	w.Header().Set("Cache-Control", "no-store")
	writeJson(w, http.StatusOK, VerifierResultResponse{
		Household:     verification.Household,
		AgeGroup:      ageGroup,
		TaxAssessment: verification.TaxAssessment,
		Evaluation:    evaluation,
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
