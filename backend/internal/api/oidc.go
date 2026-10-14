package api

import (
	"context"
	"crypto/subtle"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strings"
	"sync"
	"time"

	"step-ca-webui/internal/auth"
	"step-ca-webui/internal/config"
	"step-ca-webui/internal/db"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/gin-gonic/gin"
	"golang.org/x/oauth2"
)

// OIDC single sign-on, built as a standard authorization-code flow with
// PKCE, run entirely by the backend (a "confidential client" -- the client
// secret never reaches the browser):
//
//  1. The login page's "Sign in with ..." button is a plain link to
//     GET /api/auth/oidc/login. The backend remembers a random state, nonce
//     and PKCE verifier for this attempt and redirects the browser to the
//     identity provider.
//  2. The user signs in there (password, MFA, whatever the provider asks).
//  3. The provider redirects the browser back to GET /api/auth/oidc/callback
//     with a one-time code. The backend swaps that code for tokens directly
//     with the provider, verifies the ID token's signature, issuer, audience,
//     expiry and nonce, maps the user's groups to a step-ui role, finds or
//     creates the matching account, and starts an ordinary step-ui session
//     -- the very same session cookie a password login produces.
//  4. The browser is redirected to the frontend, which sees a valid session.
//
// After step 3, nothing about the session is OIDC-specific: RequireSession,
// RequireRole, logout and expiry all work exactly as for local accounts.

const (
	oidcStateCookieName = "step_ui_oidc_state"
	oidcStateCookiePath = "/api/auth/oidc"
	// How long a user has to finish signing in at the provider.
	oidcPendingLifetime = 10 * time.Minute
	// The login endpoint is reachable without a session (it has to be), so
	// cap how many half-finished sign-ins are remembered at once -- stops a
	// flood of hits from growing this map without bound.
	oidcMaxPending = 1000
)

// Error codes passed back to the login page as ?sso_error=<code>. The
// frontend turns each one into a readable message; details go to the
// backend log, not the browser.
const (
	ssoErrUnavailable     = "sso_unavailable"
	ssoErrAccessDenied    = "access_denied"
	ssoErrState           = "state_mismatch"
	ssoErrExpired         = "state_expired"
	ssoErrFailed          = "sso_failed"
	ssoErrNotAuthorized   = "not_authorized"
	ssoErrUsernameClash   = "username_conflict"
	ssoErrServer          = "server_error"
	ssoErrTooManyAttempts = "too_many_attempts"
)

type pendingOIDCLogin struct {
	nonce    string
	verifier string
	expires  time.Time
}

// oidcRuntime is everything that comes from the provider's discovery
// document. Built once, on first use, then reused.
type oidcRuntime struct {
	provider *oidc.Provider
	oauth2   oauth2.Config
	verifier *oidc.IDTokenVerifier
}

type OIDCAuth struct {
	cfg           *config.Config
	frontendURL   string
	httpClient    *http.Client
	secureCookies bool

	mu      sync.Mutex
	runtime *oidcRuntime

	pendingMu sync.Mutex
	pending   map[string]pendingOIDCLogin
}

// NewOIDCAuth prepares SSO but doesn't contact the provider yet -- see
// ensureRuntime. frontendURL is where the browser is sent once signed in
// (and back to /login on any failure).
func NewOIDCAuth(cfg *config.Config, frontendURL string) (*OIDCAuth, error) {
	client, err := oidcHTTPClient(cfg.OIDCCAFile)
	if err != nil {
		return nil, err
	}
	return &OIDCAuth{
		cfg:           cfg,
		frontendURL:   strings.TrimRight(frontendURL, "/"),
		httpClient:    client,
		secureCookies: strings.HasPrefix(strings.ToLower(cfg.OIDCRedirectURL), "https://"),
		pending:       make(map[string]pendingOIDCLogin),
	}, nil
}

// oidcHTTPClient is the client used for every backend-to-provider call
// (discovery, signing keys, token exchange, userinfo). It trusts the
// container's normal CA store plus, optionally, extra CAs from caFile.
func oidcHTTPClient(caFile string) (*http.Client, error) {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if caFile != "" {
		pem, err := os.ReadFile(caFile)
		if err != nil {
			return nil, fmt.Errorf("reading OIDC_CA_FILE %q: %w", caFile, err)
		}
		pool, err := x509.SystemCertPool()
		if err != nil || pool == nil {
			pool = x509.NewCertPool()
		}
		if !pool.AppendCertsFromPEM(pem) {
			return nil, fmt.Errorf("OIDC_CA_FILE %q contains no usable PEM certificates", caFile)
		}
		transport.TLSClientConfig = &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}
	}
	return &http.Client{Transport: transport, Timeout: 15 * time.Second}, nil
}

func (o *OIDCAuth) clientContext(ctx context.Context) context.Context {
	return oidc.ClientContext(ctx, o.httpClient)
}

// ensureRuntime fetches the provider's discovery document the first time
// it's needed, and caches it only on success. Deliberately lazy: if the
// identity provider happens to be down when step-ui starts, the backend
// still starts (local login keeps working) and SSO simply starts working
// at the first sign-in attempt after the provider is back.
func (o *OIDCAuth) ensureRuntime(_ context.Context) (*oidcRuntime, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.runtime != nil {
		return o.runtime, nil
	}

	// Deliberately NOT the request's own context: go-oidc keeps this
	// context for fetching the provider's signing keys later, so it must
	// outlive the request that happened to trigger discovery. The HTTP
	// client's 15-second timeout bounds the discovery request itself.
	discoveryCtx := o.clientContext(context.Background())
	provider, err := oidc.NewProvider(discoveryCtx, o.cfg.OIDCIssuerURL)
	if err != nil {
		return nil, err
	}

	scopes := o.cfg.OIDCScopes
	if !slices.Contains(scopes, oidc.ScopeOpenID) {
		scopes = append([]string{oidc.ScopeOpenID}, scopes...)
	}

	o.runtime = &oidcRuntime{
		provider: provider,
		oauth2: oauth2.Config{
			ClientID:     o.cfg.OIDCClientID,
			ClientSecret: o.cfg.OIDCClientSecret,
			RedirectURL:  o.cfg.OIDCRedirectURL,
			Endpoint:     provider.Endpoint(),
			Scopes:       scopes,
		},
		verifier: provider.Verifier(&oidc.Config{ClientID: o.cfg.OIDCClientID}),
	}
	return o.runtime, nil
}

// Probe tries discovery once at startup, purely to put a clear success or
// failure line in the backend log for troubleshooting.
func (o *OIDCAuth) Probe() {
	if _, err := o.ensureRuntime(context.Background()); err != nil {
		log.Printf("WARNING: OIDC provider discovery failed (%v) -- SSO will retry at the next sign-in attempt; local login is unaffected", err)
		return
	}
	log.Printf("OIDC provider discovered at %s", o.cfg.OIDCIssuerURL)
}

func (o *OIDCAuth) storePending(state string, p pendingOIDCLogin) bool {
	o.pendingMu.Lock()
	defer o.pendingMu.Unlock()
	now := time.Now()
	for k, v := range o.pending {
		if now.After(v.expires) {
			delete(o.pending, k)
		}
	}
	if len(o.pending) >= oidcMaxPending {
		return false
	}
	o.pending[state] = p
	return true
}

// takePending returns and forgets the pending sign-in for state, so each
// state value can be used exactly once.
func (o *OIDCAuth) takePending(state string) (pendingOIDCLogin, bool) {
	o.pendingMu.Lock()
	defer o.pendingMu.Unlock()
	p, ok := o.pending[state]
	delete(o.pending, state)
	if !ok || time.Now().After(p.expires) {
		return pendingOIDCLogin{}, false
	}
	return p, true
}

// roleFor maps the user's groups to a step-ui role. Admin wins if the user
// is in both groups. No match means no access -- the identity provider
// letting someone through is not, by itself, enough.
func (o *OIDCAuth) roleFor(groups []string) (db.Role, bool) {
	if o.cfg.OIDCAdminGroup != "" && slices.Contains(groups, o.cfg.OIDCAdminGroup) {
		return db.RoleAdmin, true
	}
	if o.cfg.OIDCViewerGroup != "" && slices.Contains(groups, o.cfg.OIDCViewerGroup) {
		return db.RoleViewer, true
	}
	return "", false
}

// stringsClaim reads a claim that should be a list of strings, tolerating
// a single string too. The bool reports whether the claim was present.
func stringsClaim(claims map[string]any, name string) ([]string, bool) {
	v, ok := claims[name]
	if !ok || v == nil {
		return nil, false
	}
	switch t := v.(type) {
	case string:
		return []string{t}, true
	case []any:
		out := make([]string, 0, len(t))
		for _, item := range t {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out, true
	}
	return nil, false
}

func stringClaim(claims map[string]any, name string) string {
	if s, ok := claims[name].(string); ok {
		return strings.TrimSpace(s)
	}
	return ""
}

func (o *OIDCAuth) usernameFrom(claims map[string]any, subject string) string {
	for _, name := range []string{o.cfg.OIDCUsernameClaim, "preferred_username", "email"} {
		if v := stringClaim(claims, name); v != "" {
			return v
		}
	}
	return subject
}

func (o *OIDCAuth) loginPageWithError(code string) string {
	return o.frontendURL + "/login?sso_error=" + url.QueryEscape(code)
}

// --- Handlers ---

// EnableOIDC switches SSO on for these handlers. Without it, the OIDC
// endpoints answer 404 and /api/auth/config reports SSO as off.
func (h *Handlers) EnableOIDC(o *OIDCAuth) {
	h.oidc = o
}

// AuthConfig tells the login page which sign-in options to show. Needs no
// session (the login page calls it before anyone is signed in) and reveals
// nothing beyond whether SSO is on and the button label.
func (h *Handlers) AuthConfig(c *gin.Context) {
	resp := gin.H{"oidc_enabled": h.oidc != nil, "oidc_provider_name": ""}
	if h.oidc != nil {
		resp["oidc_provider_name"] = h.oidc.cfg.OIDCProviderName
	}
	c.JSON(http.StatusOK, resp)
}

func (h *Handlers) setOIDCStateCookie(c *gin.Context, value string, maxAge int) {
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     oidcStateCookieName,
		Value:    value,
		Path:     oidcStateCookiePath,
		MaxAge:   maxAge,
		HttpOnly: true,
		Secure:   h.oidc.secureCookies,
		// Lax still sends the cookie on the provider's top-level redirect
		// back to the callback, which is the only place it's needed.
		SameSite: http.SameSiteLaxMode,
	})
}

// OIDCLogin starts a sign-in: remember state/nonce/PKCE verifier, bind the
// state to this browser with a short-lived cookie, and send the browser to
// the identity provider.
func (h *Handlers) OIDCLogin(c *gin.Context) {
	if h.oidc == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "SSO is not configured"})
		return
	}

	rt, err := h.oidc.ensureRuntime(c.Request.Context())
	if err != nil {
		log.Printf("OIDC: provider discovery failed: %v", err)
		c.Redirect(http.StatusFound, h.oidc.loginPageWithError(ssoErrUnavailable))
		return
	}

	state, err1 := auth.GenerateSessionToken()
	nonce, err2 := auth.GenerateSessionToken()
	if err1 != nil || err2 != nil {
		c.Redirect(http.StatusFound, h.oidc.loginPageWithError(ssoErrServer))
		return
	}
	verifier := oauth2.GenerateVerifier()

	if !h.oidc.storePending(state, pendingOIDCLogin{
		nonce:    nonce,
		verifier: verifier,
		expires:  time.Now().Add(oidcPendingLifetime),
	}) {
		log.Printf("OIDC: too many sign-ins in progress; refusing a new one")
		c.Redirect(http.StatusFound, h.oidc.loginPageWithError(ssoErrTooManyAttempts))
		return
	}

	h.setOIDCStateCookie(c, state, int(oidcPendingLifetime.Seconds()))
	c.Redirect(http.StatusFound, rt.oauth2.AuthCodeURL(state, oidc.Nonce(nonce), oauth2.S256ChallengeOption(verifier)))
}

// OIDCCallback finishes a sign-in. Every failure lands back on the login
// page with an error code; the reason goes to the backend log.
func (h *Handlers) OIDCCallback(c *gin.Context) {
	if h.oidc == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "SSO is not configured"})
		return
	}
	fail := func(code, logMsg string, args ...any) {
		log.Printf("OIDC: "+logMsg, args...)
		c.Redirect(http.StatusFound, h.oidc.loginPageWithError(code))
	}

	// One-shot: clear the state cookie whatever happens next.
	stateCookie, _ := c.Cookie(oidcStateCookieName)
	h.setOIDCStateCookie(c, "", -1)

	if e := c.Query("error"); e != "" {
		code := ssoErrFailed
		if e == "access_denied" {
			code = ssoErrAccessDenied
		}
		fail(code, "provider returned error %q: %s", e, c.Query("error_description"))
		return
	}

	state := c.Query("state")
	if state == "" || stateCookie == "" || subtle.ConstantTimeCompare([]byte(state), []byte(stateCookie)) != 1 {
		fail(ssoErrState, "state parameter missing or doesn't match this browser's state cookie")
		return
	}
	pending, ok := h.oidc.takePending(state)
	if !ok {
		fail(ssoErrExpired, "unknown or expired state (sign-in took too long, was already used, or the backend restarted)")
		return
	}

	rt, err := h.oidc.ensureRuntime(c.Request.Context())
	if err != nil {
		fail(ssoErrUnavailable, "provider discovery failed: %v", err)
		return
	}

	ctx := h.oidc.clientContext(c.Request.Context())
	token, err := rt.oauth2.Exchange(ctx, c.Query("code"), oauth2.VerifierOption(pending.verifier))
	if err != nil {
		fail(ssoErrFailed, "code exchange failed: %v", err)
		return
	}
	rawIDToken, ok := token.Extra("id_token").(string)
	if !ok || rawIDToken == "" {
		fail(ssoErrFailed, "token response contained no id_token")
		return
	}
	idToken, err := rt.verifier.Verify(ctx, rawIDToken)
	if err != nil {
		fail(ssoErrFailed, "ID token verification failed: %v", err)
		return
	}
	if subtle.ConstantTimeCompare([]byte(idToken.Nonce), []byte(pending.nonce)) != 1 {
		fail(ssoErrFailed, "ID token nonce mismatch")
		return
	}

	claims := map[string]any{}
	if err := idToken.Claims(&claims); err != nil {
		fail(ssoErrFailed, "couldn't decode ID token claims: %v", err)
		return
	}

	// Authentik puts groups in the ID token via its default "profile"
	// scope mapping. Other providers may only put them in userinfo, so
	// fall back to that when the ID token doesn't carry the claim.
	groups, haveGroups := stringsClaim(claims, h.oidc.cfg.OIDCGroupsClaim)
	if !haveGroups {
		if info, err := rt.provider.UserInfo(ctx, oauth2.StaticTokenSource(token)); err == nil {
			infoClaims := map[string]any{}
			if err := info.Claims(&infoClaims); err == nil {
				groups, haveGroups = stringsClaim(infoClaims, h.oidc.cfg.OIDCGroupsClaim)
				for k, v := range infoClaims {
					if _, exists := claims[k]; !exists {
						claims[k] = v
					}
				}
			}
		} else {
			log.Printf("OIDC: userinfo request failed: %v", err)
		}
	}

	username := h.oidc.usernameFrom(claims, idToken.Subject)
	role, ok := h.oidc.roleFor(groups)
	if !ok {
		if !haveGroups {
			fail(ssoErrNotAuthorized, "%q has no %q claim -- check the provider's scope mappings", username, h.oidc.cfg.OIDCGroupsClaim)
		} else {
			fail(ssoErrNotAuthorized, "%q is in neither OIDC_ADMIN_GROUP (%q) nor OIDC_VIEWER_GROUP (%q); their groups: %v",
				username, h.oidc.cfg.OIDCAdminGroup, h.oidc.cfg.OIDCViewerGroup, groups)
		}
		return
	}

	user, err := h.provisionOIDCUser(idToken.Subject, username, role)
	if err != nil {
		if errors.Is(err, errUsernameTaken) {
			fail(ssoErrUsernameClash, "username %q is already used by a different account: %v", username, err)
		} else {
			fail(ssoErrServer, "couldn't create or update account for %q: %v", username, err)
		}
		return
	}

	if err := h.startSession(c, user); err != nil {
		fail(ssoErrServer, "couldn't create session for %q: %v", user.Username, err)
		return
	}

	log.Printf("OIDC: %q signed in as %s", user.Username, user.Role)
	c.Redirect(http.StatusFound, h.oidc.frontendURL+"/")
}

var errUsernameTaken = errors.New("username already taken")

// provisionOIDCUser finds the account for this provider identity (by the
// stable "sub" claim), creating it on first sign-in, and brings its
// username and role up to date with what the provider says now.
//
// An SSO identity never takes over an existing account with the same
// username -- in particular not a local one, or an Authentik user who
// happened to be called "admin" would walk straight into the local admin
// account. That case is refused and logged instead.
func (h *Handlers) provisionOIDCUser(subject, username string, role db.Role) (*db.User, error) {
	existing, err := h.db.GetUserByOIDCSubject(subject)
	if err == nil {
		changed := false
		if existing.Username != username {
			if other, err := h.db.GetUserByUsername(username); err == nil && other.ID != existing.ID {
				log.Printf("OIDC: %q was renamed to %q at the provider, but that name is taken here; keeping %q", existing.Username, username, existing.Username)
			} else {
				existing.Username = username
				changed = true
			}
		}
		if existing.Role != role {
			log.Printf("OIDC: %q role changed from %s to %s (provider groups)", existing.Username, existing.Role, role)
			existing.Role = role
			changed = true
		}
		if changed {
			if err := h.db.UpdateUser(existing); err != nil {
				return nil, err
			}
		}
		return existing, nil
	}

	if other, err := h.db.GetUserByUsername(username); err == nil {
		source := "local"
		if !other.IsLocal() {
			source = "SSO (different identity)"
		}
		return nil, fmt.Errorf("%w: existing %s account", errUsernameTaken, source)
	}

	sub := subject
	user := &db.User{
		Username:    username,
		Role:        role,
		AuthSource:  db.AuthSourceOIDC,
		OIDCSubject: &sub,
	}
	if err := h.db.CreateUser(user); err != nil {
		return nil, err
	}
	log.Printf("OIDC: created account %q (%s) on first sign-in", user.Username, user.Role)
	return user, nil
}
