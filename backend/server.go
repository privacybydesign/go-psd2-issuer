package main

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"math"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"psd2-issuer/credentials"
	"psd2-issuer/enablenow"
	"psd2-issuer/logging"
	rate "psd2-issuer/rate_limiter"
	"slices"
	"strings"
	"time"

	"github.com/gorilla/mux"
)

// maxRequestBodyBytes caps the number of bytes read from any request body.
// The JSON payloads we accept are tiny (a consent id and state, or a list of
// credential keys), so 4 KiB is a generous ceiling.
const maxRequestBodyBytes = 4096

// sessionCookieName holds the session id. The cookie is HttpOnly and
// SameSite=Lax, so it is sent along when enablenow redirects back to us.
const sessionCookieName = "psd2_issuer_session"

const ErrorRateLimit = "error:ratelimit"
const ErrorInternal = "error:internal"
const ErrorBadRequest = "error:bad-request"
const ErrorSessionNotFound = "error:session-not-found"
const ErrorInvalidState = "error:invalid-state"
const ErrorNotReady = "error:not-ready"

type ServerConfig struct {
	Host           string `json:"host"`
	Port           int    `json:"port"`
	UseTls         bool   `json:"use_tls,omitempty"`
	TlsPrivKeyPath string `json:"tls_priv_key_path,omitempty"`
	TlsCertPath    string `json:"tls_cert_path,omitempty"`
}

type ServerState struct {
	irmaServerURL string
	// publicURL is the externally reachable base URL of this issuer; the
	// AppScreen redirects back to <publicURL>/callback.
	publicURL        string
	secureCookies    bool
	enablenow        enablenow.Client
	flow             *Flow
	sessionStorage   SessionStorage
	jwtCreator       JwtCreator
	startRateLimiter rate.RateLimiter
	// trustedProxies lists the CIDR ranges of reverse proxies we trust to
	// set the X-Real-IP header.
	trustedProxies []*net.IPNet
	// staticPath is the directory with the built frontend.
	staticPath string
	// verifier is the demo verifier, nil when it is disabled.
	verifier *Verifier
}

type SpaHandler struct {
	staticPath string
	indexPath  string
}

type Server struct {
	server *http.Server
	config ServerConfig
}

func (s *Server) ListenAndServe() error {
	if s.config.UseTls {
		return s.server.ListenAndServeTLS(s.config.TlsCertPath, s.config.TlsPrivKeyPath)
	}

	return s.server.ListenAndServe()
}

func (s *Server) Stop() error {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	return s.server.Shutdown(ctx)
}

// ServeHTTP serves a file from the static dir when it exists and the SPA's
// index.html otherwise.
// https://github.com/gorilla/mux?tab=readme-ov-file#serving-single-page-applications
func (h SpaHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/callback" {
		logCallback(r)
	}
	// Join internally call path.Clean to prevent directory traversal
	path := filepath.Join(h.staticPath, r.URL.Path)
	fi, err := os.Stat(path)
	if os.IsNotExist(err) || (err == nil && fi.IsDir()) {
		http.ServeFile(w, r, filepath.Join(h.staticPath, h.indexPath))
		return
	}

	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	http.FileServer(http.Dir(h.staticPath)).ServeHTTP(w, r)
}

// logCallback logs which query parameters the AppScreen redirected back with
// (names only, the values are identifiers), to debug the redirect.
func logCallback(r *http.Request) {
	names := slices.Sorted(maps.Keys(r.URL.Query()))
	slog.Info("redirected back from AppScreen", "params", names)
}

func NewServer(state *ServerState, config ServerConfig) (*Server, error) {
	router := mux.NewRouter()

	router.HandleFunc("/api/health", func(w http.ResponseWriter, r *http.Request) {
		writeJson(w, http.StatusOK, map[string]bool{"ok": true})
	})

	// the credential and attribute definitions with their translations, so
	// the frontend doesn't keep its own copy
	router.HandleFunc("/api/definitions", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "public, max-age=300")
		_, _ = w.Write(credentials.DefinitionsJSON)
	}).Methods(http.MethodGet)

	router.HandleFunc("/api/session/start", func(w http.ResponseWriter, r *http.Request) {
		handleStart(state, w, r)
	}).Methods(http.MethodPost)
	router.HandleFunc("/api/session/consent", func(w http.ResponseWriter, r *http.Request) {
		handleConsent(state, w, r)
	}).Methods(http.MethodPost)
	router.HandleFunc("/api/session/status", func(w http.ResponseWriter, r *http.Request) {
		handleStatus(state, w, r)
	}).Methods(http.MethodGet)
	router.HandleFunc("/api/session/issue", func(w http.ResponseWriter, r *http.Request) {
		handleIssue(state, w, r)
	}).Methods(http.MethodPost)
	router.HandleFunc("/api/session", func(w http.ResponseWriter, r *http.Request) {
		handleDelete(state, w, r)
	}).Methods(http.MethodDelete)

	router.HandleFunc("/api/verifier/start", func(w http.ResponseWriter, r *http.Request) {
		handleVerifierStart(state, w, r)
	}).Methods(http.MethodPost)
	router.HandleFunc("/api/verifier/result", func(w http.ResponseWriter, r *http.Request) {
		handleVerifierResult(state, w, r)
	}).Methods(http.MethodGet)

	staticPath := state.staticPath
	if staticPath == "" {
		staticPath = "../frontend/build"
	}
	router.PathPrefix("/").Handler(SpaHandler{staticPath: staticPath, indexPath: "index.html"})

	addr := fmt.Sprintf("%v:%v", config.Host, config.Port)
	srv := &http.Server{
		Handler: router,
		Addr:    addr,
		// fetching a year of transactions can take a while
		WriteTimeout: 60 * time.Second,
		ReadTimeout:  15 * time.Second,
	}

	return &Server{
		server: srv,
		config: config,
	}, nil
}

// -----------------------------------------------------------------------------------

type StartResponse struct {
	// Url is the enablenow AppScreen URL to send the user to.
	Url string `json:"url"`
}

// handleStart creates a new session and returns the AppScreen URL. The session
// id is stored in a cookie; the state travels through the AppScreen.
func handleStart(state *ServerState, w http.ResponseWriter, r *http.Request) {
	endpoint := r.URL.Path
	ip := getIpAddressForRequest(r, state.trustedProxies)
	logReceivedRequest(r, ip)

	allow, timeout, err := state.startRateLimiter.Allow("ip:" + ip)
	if err != nil {
		respondWithErr(w, http.StatusInternalServerError, ErrorInternal, "rate limiter failed", err, "endpoint", endpoint)
		return
	}
	if !allow {
		respondWithRateLimitErr(w, endpoint, ip, timeout)
		return
	}

	// a new start replaces any previous session of this browser
	if old := sessionIdFromCookie(r); old != "" {
		deleteSession(r.Context(), state, old)
	}

	session := NewSession(time.Now())
	if err := state.sessionStorage.Put(session); err != nil {
		respondWithErr(w, http.StatusInternalServerError, ErrorInternal, "failed to store session", err, "endpoint", endpoint)
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    session.Id,
		Path:     "/",
		MaxAge:   int(SessionTTL.Seconds()),
		HttpOnly: true,
		Secure:   state.secureCookies,
		SameSite: http.SameSiteLaxMode,
	})

	// The AppScreen forwards query parameters of the redirect URI back to it,
	// so the state travels inside the redirect URI.
	redirectURI := strings.TrimRight(state.publicURL, "/") + "/callback?" + url.Values{"state": {session.State}}.Encode()
	consentURL := state.enablenow.ConsentURL(redirectURI, nil)
	writeJson(w, http.StatusOK, StartResponse{Url: consentURL})
}

type ConsentPayload struct {
	ConsentId string `json:"consent_id"`
	State     string `json:"state"`
}

// handleConsent attaches the consent id enablenow redirected back with to the
// session, after checking the state matches the one we handed out.
func handleConsent(state *ServerState, w http.ResponseWriter, r *http.Request) {
	endpoint := r.URL.Path
	ip := getIpAddressForRequest(r, state.trustedProxies)
	logReceivedRequest(r, ip)

	var body ConsentPayload
	if !readJson(w, r, &body) {
		return
	}

	session, ok := sessionFromRequest(state, w, r)
	if !ok {
		return
	}

	if body.ConsentId == "" || subtle.ConstantTimeCompare([]byte(body.State), []byte(session.State)) != 1 {
		respondWithErr(w, http.StatusBadRequest, ErrorInvalidState, "state mismatch or missing consent id", nil, "endpoint", endpoint)
		return
	}

	// the same consent may be posted twice (e.g. a page reload); a different
	// one is not allowed to replace it
	if session.Status != StatusAwaitingConsent {
		if session.ConsentId == body.ConsentId {
			writeJson(w, http.StatusOK, statusResponse(session))
			return
		}
		respondWithErr(w, http.StatusConflict, ErrorInvalidState, "session already has a consent", nil, "endpoint", endpoint)
		return
	}

	session.ConsentId = body.ConsentId
	session.setStatus(StatusAwaitingTransactions, time.Now())
	if err := state.sessionStorage.Put(session); err != nil {
		respondWithErr(w, http.StatusInternalServerError, ErrorInternal, "failed to store session", err, "endpoint", endpoint)
		return
	}
	slog.Info("consent received", "consent", logging.MaskIdentifier(body.ConsentId))
	writeJson(w, http.StatusOK, statusResponse(session))
}

type StatusResponse struct {
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
	// Credentials holds the computed values once the status is ready, so
	// the user can review them before adding them to Yivi.
	Credentials []credentials.Credential `json:"credentials,omitempty"`
}

func statusResponse(s *Session) StatusResponse {
	return StatusResponse{Status: s.Status, Error: s.Error, Credentials: s.Credentials}
}

// handleStatus advances the session as far as possible and reports where it is.
func handleStatus(state *ServerState, w http.ResponseWriter, r *http.Request) {
	endpoint := r.URL.Path
	id := sessionIdFromCookie(r)
	if id == "" {
		respondWithErr(w, http.StatusNotFound, ErrorSessionNotFound, "no session cookie", nil, "endpoint", endpoint)
		return
	}

	session, err := state.flow.Advance(r.Context(), state.sessionStorage, id)
	if errors.Is(err, ErrSessionNotFound) {
		respondWithErr(w, http.StatusNotFound, ErrorSessionNotFound, "session not found", nil, "endpoint", endpoint)
		return
	}
	if err != nil {
		respondWithErr(w, http.StatusInternalServerError, ErrorInternal, "failed to advance session", err, "endpoint", endpoint)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJson(w, http.StatusOK, statusResponse(session))
}

type IssuePayload struct {
	// Credentials lists the credential keys the user chose to add.
	Credentials []string `json:"credentials"`
}

type IssueResponse struct {
	Jwt           string `json:"jwt"`
	IrmaServerURL string `json:"irma_server_url"`
}

// handleIssue returns a signed issuance request for the selected credentials.
func handleIssue(state *ServerState, w http.ResponseWriter, r *http.Request) {
	endpoint := r.URL.Path
	ip := getIpAddressForRequest(r, state.trustedProxies)
	logReceivedRequest(r, ip)

	var body IssuePayload
	if !readJson(w, r, &body) {
		return
	}

	session, ok := sessionFromRequest(state, w, r)
	if !ok {
		return
	}

	if session.Status != StatusReady {
		respondWithErr(w, http.StatusConflict, ErrorNotReady, "session is not ready for issuance", nil, "endpoint", endpoint, "status", session.Status)
		return
	}

	var selected []credentials.Credential
	for _, cred := range session.Credentials {
		if slices.Contains(body.Credentials, cred.Key) {
			selected = append(selected, cred)
		}
	}
	if len(selected) == 0 {
		respondWithErr(w, http.StatusBadRequest, ErrorBadRequest, "no known credentials selected", nil, "endpoint", endpoint)
		return
	}

	jwt, err := state.jwtCreator.CreateJwt(selected)
	if err != nil {
		respondWithErr(w, http.StatusInternalServerError, ErrorInternal, "failed to create JWT", err, "endpoint", endpoint)
		return
	}

	slog.Info("issuing credentials", "credentials", body.Credentials)
	writeJson(w, http.StatusOK, IssueResponse{Jwt: jwt, IrmaServerURL: state.irmaServerURL})
}

// handleDelete forgets the session and deletes its consent at enablenow.
func handleDelete(state *ServerState, w http.ResponseWriter, r *http.Request) {
	if id := sessionIdFromCookie(r); id != "" {
		deleteSession(r.Context(), state, id)
	}
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   state.secureCookies,
		SameSite: http.SameSiteLaxMode,
	})
	w.WriteHeader(http.StatusNoContent)
}

func deleteSession(ctx context.Context, state *ServerState, id string) {
	session, err := state.sessionStorage.Get(id)
	if err != nil {
		return
	}
	if session.ConsentId != "" && session.Status != StatusReady && session.Status != StatusFailed {
		// the consent is only still around while the flow is in progress
		state.flow.cleanup(ctx, session)
	}
	if err := state.sessionStorage.Delete(id); err != nil {
		slog.Error("failed to delete session", "error", err)
	}
}

// -----------------------------------------------------------------------------------

func sessionIdFromCookie(r *http.Request) string {
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil {
		return ""
	}
	return cookie.Value
}

// sessionFromRequest loads the session of the request's cookie, writing an
// error response and returning false when there is none.
func sessionFromRequest(state *ServerState, w http.ResponseWriter, r *http.Request) (*Session, bool) {
	endpoint := r.URL.Path
	id := sessionIdFromCookie(r)
	if id == "" {
		respondWithErr(w, http.StatusNotFound, ErrorSessionNotFound, "no session cookie", nil, "endpoint", endpoint)
		return nil, false
	}
	session, err := state.sessionStorage.Get(id)
	if errors.Is(err, ErrSessionNotFound) {
		respondWithErr(w, http.StatusNotFound, ErrorSessionNotFound, "session not found", nil, "endpoint", endpoint)
		return nil, false
	}
	if err != nil {
		respondWithErr(w, http.StatusInternalServerError, ErrorInternal, "failed to load session", err, "endpoint", endpoint)
		return nil, false
	}
	return session, true
}

// readJson decodes a size limited JSON body, writing an error response and
// returning false when that fails.
func readJson(w http.ResponseWriter, r *http.Request, out any) bool {
	defer closeRequestBody(r)
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
	content, err := io.ReadAll(r.Body)
	if err != nil {
		respondWithErr(w, http.StatusBadRequest, ErrorBadRequest, "failed to read request body", err, "endpoint", r.URL.Path)
		return false
	}
	if err := json.Unmarshal(content, out); err != nil {
		respondWithErr(w, http.StatusBadRequest, ErrorBadRequest, "failed to parse request body", err, "endpoint", r.URL.Path)
		return false
	}
	return true
}

func writeJson(w http.ResponseWriter, code int, body any) {
	payload, err := json.Marshal(body)
	if err != nil {
		respondWithErr(w, http.StatusInternalServerError, ErrorInternal, "failed to marshal response", err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	if _, err := w.Write(payload); err != nil {
		slog.Error("failed to write body to http response", "error", err)
	}
}

// logReceivedRequest logs an incoming request with enough context
// (method, endpoint, client ip) to be useful for request tracing
func logReceivedRequest(r *http.Request, ip string) {
	slog.Info("received request",
		"method", r.Method,
		"endpoint", r.URL.Path,
		"ip", ip)
}

// getIpAddressForRequest resolves the client IP used for rate limiting.
//
// The X-Real-IP header is client-controlled, so it is only trusted when the
// request's direct peer (RemoteAddr) is one of the configured trusted
// proxies. In every other case the peer address is used.
func getIpAddressForRequest(r *http.Request, trustedProxies []*net.IPNet) string {
	remoteIP, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		remoteIP = r.RemoteAddr
	}

	if isTrustedProxy(remoteIP, trustedProxies) {
		if realIP := strings.TrimSpace(r.Header.Get("X-Real-IP")); realIP != "" {
			return realIP
		}
	}

	return remoteIP
}

// isTrustedProxy reports whether ip falls within any of the trusted-proxy
// CIDR ranges.
func isTrustedProxy(ip string, trustedProxies []*net.IPNet) bool {
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return false
	}
	for _, cidr := range trustedProxies {
		if cidr != nil && cidr.Contains(parsed) {
			return true
		}
	}
	return false
}

func closeRequestBody(r *http.Request) {
	if err := r.Body.Close(); err != nil {
		slog.Error("failed to close request body", "error", err)
	}
}

// respondWithRateLimitErr writes a 429 response with a Retry-After header and
// logs the relevant rate limiting context
func respondWithRateLimitErr(w http.ResponseWriter, endpoint, ip string, timeout time.Duration) {
	roundedSecs := int(math.Round(timeout.Seconds()))
	w.Header().Set("Retry-After", fmt.Sprintf("%d", roundedSecs))
	respondWithErr(w, http.StatusTooManyRequests, ErrorRateLimit, "too many requests", nil,
		"endpoint", endpoint,
		"ip", ip,
		"retry_after_seconds", roundedSecs)
}

func respondWithErr(w http.ResponseWriter, code int, responseBody string, logMsg string, e error, extras ...any) {
	args := []any{"status_code", code, "response_body", responseBody}
	if e != nil {
		args = append(args, "error", e)
	}
	args = append(args, extras...)
	// client errors (4xx) are expected operational events; only server
	// errors (5xx) should count towards the error-rate signal
	if code >= 500 {
		slog.Error(logMsg, args...)
	} else {
		slog.Warn(logMsg, args...)
	}
	w.WriteHeader(code)
	if _, err := w.Write([]byte(responseBody)); err != nil {
		slog.Error("failed to write body to http response", "error", err)
	}
}
