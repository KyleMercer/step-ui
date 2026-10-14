package api

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"step-ca-webui/internal/catrust"
	"step-ca-webui/internal/config"
	"step-ca-webui/internal/db"
	"step-ca-webui/internal/settings"

	"github.com/gin-gonic/gin"
)

// SSO settings live in the settings store (key "sso") and are edited by
// admins on the Settings page. Saving takes effect immediately -- the next
// sign-in uses the new settings, no restart.
//
// Older versions configured SSO with OIDC_* environment variables. On the
// first start with a settings store, those are imported once (see
// ImportSSOFromEnv); after that the stored settings are the only source
// and the environment variables are ignored.

const ssoSettingsKey = "sso"

// SSOSettings is what's stored. ClientSecret is sealed (settings.Store.Seal).
type SSOSettings struct {
	Enabled       bool   `json:"enabled"`
	ProviderName  string `json:"provider_name"`
	IssuerURL     string `json:"issuer_url"`
	ClientID      string `json:"client_id"`
	ClientSecret  string `json:"client_secret"`
	RedirectURL   string `json:"redirect_url"`
	AdminGroup    string `json:"admin_group"`
	ViewerGroup   string `json:"viewer_group"`
	GroupsClaim   string `json:"groups_claim"`
	UsernameClaim string `json:"username_claim"`
	Scopes        string `json:"scopes"`
	TrustCARoot   bool   `json:"trust_ca_root"`
	ExtraCAPEM    string `json:"extra_ca_pem"`
}

func defaultSSOSettings() SSOSettings {
	return SSOSettings{ProviderName: "SSO", GroupsClaim: "groups", UsernameClaim: "preferred_username",
		Scopes: "openid profile email"}
}

// SSOManager owns the current SSO configuration and the OIDCAuth built
// from it, swapping in a new one whenever the settings are saved.
type SSOManager struct {
	store       *settings.Store
	trust       *catrust.Fetcher
	frontendURL string

	mu      sync.RWMutex
	current *OIDCAuth // nil = SSO off (or not fully configured)
}

func NewSSOManager(store *settings.Store, trust *catrust.Fetcher, frontendURL string) *SSOManager {
	return &SSOManager{store: store, trust: trust, frontendURL: frontendURL}
}

func (m *SSOManager) load() (SSOSettings, bool, error) {
	s := defaultSSOSettings()
	found, err := m.store.Get(ssoSettingsKey, &s)
	return s, found, err
}

// options turns stored settings into a usable configuration, decrypting
// the client secret.
func (m *SSOManager) options(s SSOSettings) (OIDCOptions, error) {
	secret, err := m.store.Open(s.ClientSecret)
	if err != nil {
		return OIDCOptions{}, errors.New("the stored client secret can't be decrypted (the settings key changed?) -- re-enter it on the Settings page")
	}
	return OIDCOptions{
		ProviderName: s.ProviderName, IssuerURL: s.IssuerURL, ClientID: s.ClientID, ClientSecret: secret,
		RedirectURL: s.RedirectURL, AdminGroup: s.AdminGroup, ViewerGroup: s.ViewerGroup,
		GroupsClaim: s.GroupsClaim, UsernameClaim: s.UsernameClaim,
		Scopes:      strings.Fields(strings.ReplaceAll(s.Scopes, ",", " ")),
		TrustCARoot: s.TrustCARoot, ExtraCAPEM: s.ExtraCAPEM,
	}, nil
}

// Reload rebuilds SSO from the stored settings. Sign-ins already in
// progress at the provider when settings change will need to start over.
func (m *SSOManager) Reload() {
	s, _, err := m.load()
	if err != nil {
		log.Printf("WARNING: SSO disabled: couldn't load SSO settings: %v", err)
		m.set(nil)
		return
	}
	if !s.Enabled {
		m.set(nil)
		log.Println("SSO is off")
		return
	}
	if missing := missingRequired(s); len(missing) > 0 {
		log.Printf("WARNING: SSO is switched on but not fully configured (missing %s) -- staying off", strings.Join(missing, ", "))
		m.set(nil)
		return
	}
	opts, err := m.options(s)
	if err != nil {
		log.Printf("WARNING: SSO disabled: %v", err)
		m.set(nil)
		return
	}
	o := NewOIDCAuth(opts, m.frontendURL, m.trust)
	m.set(o)
	log.Printf("SSO on: issuer=%s, redirect=%s, admin group=%q, viewer group=%q", s.IssuerURL, s.RedirectURL, s.AdminGroup, s.ViewerGroup)
	go o.Probe()
}

func (m *SSOManager) set(o *OIDCAuth) {
	m.mu.Lock()
	m.current = o
	m.mu.Unlock()
}

// Current is the active SSO configuration, or nil when SSO is off.
func (m *SSOManager) Current() *OIDCAuth {
	if m == nil {
		return nil
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.current
}

// SetSSO attaches the SSO manager to the handlers.
func (h *Handlers) SetSSO(m *SSOManager) { h.sso = m }

func (h *Handlers) ssoAuth() *OIDCAuth { return h.sso.Current() }

func missingRequired(s SSOSettings) []string {
	var missing []string
	if s.IssuerURL == "" {
		missing = append(missing, "issuer URL")
	}
	if s.ClientID == "" {
		missing = append(missing, "client ID")
	}
	if s.ClientSecret == "" {
		missing = append(missing, "client secret")
	}
	if s.RedirectURL == "" {
		missing = append(missing, "redirect URI")
	}
	if s.AdminGroup == "" && s.ViewerGroup == "" {
		missing = append(missing, "an admin or viewer group")
	}
	return missing
}

// ImportSSOFromEnv copies an older OIDC_* environment configuration into
// the settings store -- once, and only if SSO has never been saved there.
// Returns a line for the startup log, or "".
func ImportSSOFromEnv(store *settings.Store, cfg *config.Config) string {
	var probe SSOSettings
	found, err := store.Get(ssoSettingsKey, &probe)
	if err != nil {
		return fmt.Sprintf("WARNING: couldn't check stored SSO settings: %v", err)
	}
	anyEnv := cfg.OIDCIssuerURL != "" || cfg.OIDCClientID != "" || cfg.OIDCClientSecret != "" || cfg.OIDCRedirectURL != ""
	if found {
		if anyEnv {
			return "NOTE: OIDC_* environment variables are ignored -- SSO is managed on the Settings page now. You can remove them."
		}
		return ""
	}
	if !cfg.OIDCEnabled() {
		if anyEnv {
			return "NOTE: incomplete OIDC_* environment variables found and not imported; set SSO up on the Settings page instead."
		}
		return ""
	}

	s := defaultSSOSettings()
	s.Enabled = true
	s.IssuerURL, s.ClientID, s.RedirectURL = cfg.OIDCIssuerURL, cfg.OIDCClientID, cfg.OIDCRedirectURL
	s.AdminGroup, s.ViewerGroup = cfg.OIDCAdminGroup, cfg.OIDCViewerGroup
	if cfg.OIDCProviderName != "" {
		s.ProviderName = cfg.OIDCProviderName
	}
	if cfg.OIDCGroupsClaim != "" {
		s.GroupsClaim = cfg.OIDCGroupsClaim
	}
	if cfg.OIDCUsernameClaim != "" {
		s.UsernameClaim = cfg.OIDCUsernameClaim
	}
	if len(cfg.OIDCScopes) > 0 {
		s.Scopes = strings.Join(cfg.OIDCScopes, " ")
	}
	note := ""
	if cfg.OIDCCAFile != "" {
		if b, err := os.ReadFile(cfg.OIDCCAFile); err == nil && catrust.ValidatePEM(string(b)) == nil {
			s.ExtraCAPEM = string(b)
		} else {
			note = fmt.Sprintf(" (couldn't import OIDC_CA_FILE %q -- add the certificate on the Settings page)", cfg.OIDCCAFile)
		}
	}
	sealed, err := store.Seal(cfg.OIDCClientSecret)
	if err != nil {
		return fmt.Sprintf("WARNING: couldn't import SSO settings: %v", err)
	}
	s.ClientSecret = sealed
	if err := store.Put(ssoSettingsKey, s, "import from environment"); err != nil {
		return fmt.Sprintf("WARNING: couldn't import SSO settings: %v", err)
	}
	return "Imported SSO settings from OIDC_* environment variables into the Settings page" + note +
		". From now on they're managed there; the environment variables are ignored and can be removed."
}

// --- Admin API -------------------------------------------------------------

type ssoView struct {
	SSOSettings
	ClientSecret string `json:"client_secret"` // "set", "unset", "undecryptable"
	Active       bool   `json:"active"`        // SSO is actually running
}

func (m *SSOManager) view(s SSOSettings) ssoView {
	state := "unset"
	if s.ClientSecret != "" {
		state = "set"
		if _, err := m.store.Open(s.ClientSecret); err != nil {
			state = "undecryptable"
		}
	}
	return ssoView{SSOSettings: s, ClientSecret: state, Active: m.Current() != nil}
}

type ssoUpdate struct {
	SSOSettings
	// ClientSecret here: a new value to set, or "" to keep the current one.
}

func (h *Handlers) GetSSOSettings(c *gin.Context) {
	if h.sso == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "the settings store is unavailable -- see the backend log"})
		return
	}
	s, _, err := h.sso.load()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load SSO settings"})
		return
	}
	c.JSON(http.StatusOK, h.sso.view(s))
}

func validURL(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}

func (h *Handlers) PutSSOSettings(c *gin.Context) {
	if h.sso == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "the settings store is unavailable -- see the backend log"})
		return
	}
	var up ssoUpdate
	if err := c.ShouldBindJSON(&up); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request: " + err.Error()})
		return
	}
	cur, _, err := h.sso.load()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load SSO settings"})
		return
	}

	next := up.SSOSettings
	trim := func(p *string) { *p = strings.TrimSpace(*p) }
	for _, p := range []*string{&next.ProviderName, &next.IssuerURL, &next.ClientID, &next.RedirectURL,
		&next.AdminGroup, &next.ViewerGroup, &next.GroupsClaim, &next.UsernameClaim, &next.Scopes} {
		trim(p)
	}
	d := defaultSSOSettings()
	if next.ProviderName == "" {
		next.ProviderName = d.ProviderName
	}
	if next.GroupsClaim == "" {
		next.GroupsClaim = d.GroupsClaim
	}
	if next.UsernameClaim == "" {
		next.UsernameClaim = d.UsernameClaim
	}
	if next.Scopes == "" {
		next.Scopes = d.Scopes
	}

	// Secret: write-only. Empty = keep.
	if v := strings.TrimSpace(up.SSOSettings.ClientSecret); v != "" {
		sealed, err := h.sso.store.Seal(v)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to store client secret"})
			return
		}
		next.ClientSecret = sealed
	} else {
		next.ClientSecret = cur.ClientSecret
	}

	bad := func(msg string) { c.JSON(http.StatusBadRequest, gin.H{"error": msg}) }
	if next.IssuerURL != "" && !validURL(next.IssuerURL) {
		bad("Issuer URL must be a full https:// URL")
		return
	}
	if next.RedirectURL != "" && !validURL(next.RedirectURL) {
		bad("Redirect URI must be a full https:// URL")
		return
	}
	if next.RedirectURL != "" && !strings.HasSuffix(strings.TrimRight(next.RedirectURL, "/"), "/api/auth/oidc/callback") {
		bad("Redirect URI must end with /api/auth/oidc/callback (it's step-ui's backend address plus that path)")
		return
	}
	if strings.TrimSpace(next.ExtraCAPEM) != "" {
		if err := catrust.ValidatePEM(next.ExtraCAPEM); err != nil {
			bad("Extra CA certificates: " + err.Error())
			return
		}
	}
	if next.Enabled {
		if missing := missingRequired(next); len(missing) > 0 {
			bad("To turn SSO on, fill in: " + strings.Join(missing, ", "))
			return
		}
	}

	if err := h.sso.store.Put(ssoSettingsKey, next, actorName(c)); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save SSO settings"})
		return
	}
	h.sso.Reload()

	onOff := "off"
	if next.Enabled {
		onOff = "on"
	}
	secretNote := ""
	if strings.TrimSpace(up.SSOSettings.ClientSecret) != "" {
		secretNote = "; client secret changed"
	}
	_ = h.db.LogAuditEvent(&db.AuditEvent{Who: actorName(c), Action: "sso_settings_changed", Timestamp: time.Now(),
		Details: fmt.Sprintf("SSO %s; issuer %s; admin group %q; viewer group %q%s", onOff, next.IssuerURL, next.AdminGroup, next.ViewerGroup, secretNote)})

	c.JSON(http.StatusOK, h.sso.view(next))
}

// TestSSOSettings checks the saved settings against the provider without
// signing anyone in: discovery (reachability, TLS trust, exact issuer
// match) and the endpoints it advertises. Works whether SSO is on or off,
// so it can be checked before switching it on.
func (h *Handlers) TestSSOSettings(c *gin.Context) {
	if h.sso == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "the settings store is unavailable -- see the backend log"})
		return
	}
	s, _, err := h.sso.load()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load SSO settings"})
		return
	}
	if s.IssuerURL == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "save an issuer URL first"})
		return
	}
	opts, err := h.sso.options(s)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 20*time.Second)
	defer cancel()
	rt, err := NewOIDCAuth(opts, h.sso.frontendURL, h.sso.trust).ensureRuntime(ctx)
	if err != nil {
		msg := err.Error()
		if strings.Contains(msg, "issuer did not match") {
			msg += " -- copy the issuer exactly as the provider shows it (Authentik: include the trailing slash)"
		}
		c.JSON(http.StatusBadGateway, gin.H{"error": msg})
		return
	}
	var warnings []string
	if missing := missingRequired(s); len(missing) > 0 {
		warnings = append(warnings, "still missing before SSO can be switched on: "+strings.Join(missing, ", "))
	}
	c.JSON(http.StatusOK, gin.H{
		"message":                "Provider reachable and issuer matches.",
		"authorization_endpoint": rt.oauth2.Endpoint.AuthURL,
		"token_endpoint":         rt.oauth2.Endpoint.TokenURL,
		"warnings":               warnings,
	})
}
