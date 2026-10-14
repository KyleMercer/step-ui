// Package alerts sends certificate expiry warnings -- at configurable
// thresholds before expiry (30, 7 and 1 days by default) and once on expiry
// -- to Pushover, MQTT, webhooks and email.
//
// The feature is self-contained: its settings live under one key in the
// settings store, its history in its own table, its routes are added
// through api.RouteExtension, and it's switched on and off from its own
// page in the UI. Removing it means removing this package, its frontend
// page, and the few lines in cmd/main.go that start it.
package alerts

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"step-ca-webui/internal/catrust"
	"step-ca-webui/internal/db"
	"step-ca-webui/internal/settings"

	"gorm.io/gorm"
)

// CheckInterval: how often certificates are checked. Thresholds are in
// days, so hourly is plenty, and it means a new alert goes out at most an
// hour after a certificate crosses a threshold.
const CheckInterval = time.Hour

// startupDelay gives the rest of the stack (and the network) a moment
// before the first check after the backend starts.
const startupDelay = time.Minute

// SentAlert records each alert that went out, so each threshold fires once
// per certificate. It's keyed on the certificate's expiry time too: renewing
// a certificate changes its expiry, so the next round of warnings for the
// renewed certificate starts fresh.
type SentAlert struct {
	ID           uint      `gorm:"primaryKey" json:"id"`
	CertID       string    `gorm:"uniqueIndex:idx_alert_once" json:"cert_id"`
	NotAfterUnix int64     `gorm:"uniqueIndex:idx_alert_once" json:"-"`
	Level        int       `gorm:"uniqueIndex:idx_alert_once" json:"level"` // threshold in days, 0 = expired
	CN           string    `json:"cn"`
	Serial       string    `json:"serial"`
	NotAfter     time.Time `json:"not_after"`
	SentAt       time.Time `json:"sent_at"`
	// Results: which destinations accepted it, e.g. "Phone: ok; Node-RED: failed (timeout)"
	Results string `json:"results"`
}

func (SentAlert) TableName() string { return "alert_history" }

type destState struct {
	LastSuccess *time.Time `json:"last_success"`
	LastError   string     `json:"last_error"`
	LastErrorAt *time.Time `json:"last_error_at"`
}

type Service struct {
	db          *gorm.DB
	store       *settings.Store
	trust       *catrust.Fetcher
	defaultLink string
	httpTimeout time.Duration

	checkMu sync.Mutex // one check at a time

	mu         sync.Mutex
	lastCheck  *time.Time
	lastResult string
	destStates map[string]*destState

	now func() time.Time // replaceable in tests
}

// New prepares the feature. defaultLink seeds the "step-ui address" setting
// the first time (normally CORS_ALLOWED_ORIGIN).
func New(database *db.Database, store *settings.Store, trust *catrust.Fetcher, defaultLink string) (*Service, error) {
	if err := database.DB.AutoMigrate(&SentAlert{}); err != nil {
		return nil, fmt.Errorf("migrating alert history table: %w", err)
	}
	s := &Service{
		db: database.DB, store: store, trust: trust,
		defaultLink: strings.TrimRight(defaultLink, "/"), httpTimeout: 20 * time.Second,
		destStates: map[string]*destState{}, now: time.Now,
	}
	var ps persistedState
	if found, _ := store.Get(stateKey, &ps); found {
		s.lastCheck, s.lastResult = ps.LastCheck, ps.LastResult
	}
	return s, nil
}

// stateKey keeps the last check's time and result across restarts.
const stateKey = "alerts_state"

type persistedState struct {
	LastCheck  *time.Time `json:"last_check"`
	LastResult string     `json:"last_result"`
}

func (s *Service) loadConfig() (Config, error) {
	cfg := defaultConfig(s.defaultLink)
	if _, err := s.store.Get(settingsKey, &cfg); err != nil {
		return defaultConfig(s.defaultLink), err
	}
	if cfg.Destinations == nil {
		cfg.Destinations = []Destination{}
	}
	return cfg, nil
}

// Start runs checks in the background until ctx is cancelled. A disabled
// feature costs one settings read per hour.
func (s *Service) Start(ctx context.Context) {
	go func() {
		select {
		case <-ctx.Done():
			return
		case <-time.After(startupDelay):
		}
		ticker := time.NewTicker(CheckInterval)
		defer ticker.Stop()
		for {
			if cfg, err := s.loadConfig(); err != nil {
				log.Printf("alerts: couldn't load settings: %v", err)
			} else if cfg.Enabled {
				if _, err := s.Check(ctx, false); err != nil {
					log.Printf("alerts: check failed: %v", err)
				}
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}

// --- TLS trust -------------------------------------------------------------

func (s *Service) tlsConfig(ctx context.Context, cfg Config) *tls.Config {
	pool, warn := s.trust.Pool(ctx, cfg.TrustCARoot, "")
	if warn != nil {
		log.Printf("alerts: not trusting step-ca root for destinations: %v", warn)
	}
	return &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}
}

func (s *Service) env(ctx context.Context, cfg Config, d Destination, snap *Snapshot) sendEnv {
	tlsCfg := s.tlsConfig(ctx, cfg)
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = tlsCfg
	return sendEnv{
		tls:  tlsCfg,
		http: &http.Client{Timeout: s.httpTimeout, Transport: transport},
		secret: func(key string) (string, error) {
			v, err := s.store.Open(d.Secrets[key])
			if err != nil {
				return "", fmt.Errorf("the stored %s can't be decrypted (the settings key changed?) -- re-enter it", key)
			}
			return v, nil
		},
		snapshot: snap,
	}
}

// --- The check -------------------------------------------------------------

// daysLeft counts whole days, rounding toward zero both ways: 6.5 days
// left is 6, expired 40.2 days ago is -40 (matching the dashboard's
// "Expired 40 days ago"). Under a day either side is 0.
func daysLeft(notAfter, now time.Time) int {
	return int(notAfter.Sub(now).Hours() / 24)
}

// levelFor picks the most urgent threshold a certificate has reached: 0 if
// expired, otherwise the smallest threshold it's within. ok=false means
// it's not within any threshold yet.
func levelFor(notAfter, now time.Time, thresholds []int) (level int, ok bool) {
	remaining := notAfter.Sub(now)
	if remaining <= 0 {
		return 0, true
	}
	best := -1
	for _, t := range thresholds {
		if remaining <= time.Duration(t)*24*time.Hour && (best == -1 || t < best) {
			best = t
		}
	}
	return best, best != -1
}

type CheckResult struct {
	Checked  int    `json:"checked"`
	NewItems []Item `json:"new_alerts"`
	Sent     int    `json:"destinations_ok"`
	Failed   int    `json:"destinations_failed"`
	Summary  string `json:"summary"`
}

// Check looks at every active certificate, works out which alerts are due,
// and sends them as one message per destination. An alert is recorded as
// sent once at least one destination accepted it; if every destination
// failed, nothing is recorded and the next check tries again.
//
// manual=true (the "Check now" button) runs even when the feature is off.
func (s *Service) Check(ctx context.Context, manual bool) (CheckResult, error) {
	s.checkMu.Lock()
	defer s.checkMu.Unlock()

	cfg, err := s.loadConfig()
	if err != nil {
		return CheckResult{}, err
	}
	now := s.now()

	var certs []db.Certificate
	if err := s.db.Where("status = ?", "active").Find(&certs).Error; err != nil {
		return CheckResult{}, err
	}

	var sent []SentAlert
	if err := s.db.Find(&sent).Error; err != nil {
		return CheckResult{}, err
	}
	type key struct {
		cert string
		na   int64
	}
	mostUrgentSent := map[key]int{} // smallest level already sent
	for _, a := range sent {
		k := key{a.CertID, a.NotAfterUnix}
		if cur, ok := mostUrgentSent[k]; !ok || a.Level < cur {
			mostUrgentSent[k] = a.Level
		}
	}

	maxThreshold := 0
	for _, t := range cfg.Thresholds {
		maxThreshold = max(maxThreshold, t)
	}

	snap := &Snapshot{CheckedAt: now}
	var items []Item
	for _, c := range certs {
		dl := daysLeft(c.NotAfter, now)
		st := "ok"
		switch {
		case !now.Before(c.NotAfter):
			st = "expired"
			snap.Expired++
		case c.NotAfter.Sub(now) <= time.Duration(maxThreshold)*24*time.Hour:
			st = "expiring"
			snap.Expiring++
			snap.Active++
		default:
			snap.Active++
		}
		snap.Certs = append(snap.Certs, CertStatus{CN: c.CN, Serial: c.Serial, NotAfter: c.NotAfter, DaysLeft: dl, Status: st})

		level, due := levelFor(c.NotAfter, now, cfg.Thresholds)
		if !due {
			continue
		}
		// Already sent this level or a more urgent one for this expiry?
		if prev, ok := mostUrgentSent[key{c.ID, c.NotAfter.Unix()}]; ok && prev <= level {
			continue
		}
		items = append(items, Item{CertID: c.ID, CN: c.CN, Serial: c.Serial, NotAfter: c.NotAfter,
			DaysLeft: dl, Level: level, Expired: level == 0})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].NotAfter.Before(items[j].NotAfter) })

	res := CheckResult{Checked: len(certs), NewItems: items}
	if res.NewItems == nil {
		res.NewItems = []Item{}
	}

	var enabled []Destination
	for _, d := range cfg.Destinations {
		if d.Enabled {
			enabled = append(enabled, d)
		}
	}

	var results []string
	if len(items) > 0 || hasStatusPublisher(enabled) {
		msg := s.compose(items, cfg)
		for _, d := range enabled {
			// Destinations that only notify have nothing to do without
			// new alerts; MQTT still refreshes its retained status.
			if len(items) == 0 && d.Type != "mqtt" {
				continue
			}
			sctx, cancel := context.WithTimeout(ctx, 45*time.Second)
			err := send(sctx, d, msg, s.env(sctx, cfg, d, snap))
			cancel()
			s.recordDest(d.ID, err)
			if err != nil {
				log.Printf("alerts: %s (%s) failed: %v", d.Name, d.Type, err)
				res.Failed++
				results = append(results, d.Name+": failed ("+truncate(err.Error(), 120)+")")
			} else {
				res.Sent++
				results = append(results, d.Name+": ok")
			}
		}
	}

	switch {
	case len(items) == 0:
		res.Summary = fmt.Sprintf("Checked %d active certificates; no new alerts.", len(certs))
	case len(enabled) == 0:
		res.Summary = fmt.Sprintf("%d alert(s) due, but no destinations are enabled -- nothing sent.", len(items))
	case res.Sent == 0:
		res.Summary = fmt.Sprintf("%d alert(s) due, but every destination failed; will retry at the next check.", len(items))
	default:
		res.Summary = fmt.Sprintf("Sent %d alert(s) to %d destination(s)", len(items), res.Sent)
		if res.Failed > 0 {
			res.Summary += fmt.Sprintf("; %d destination(s) failed", res.Failed)
		}
		res.Summary += "."
	}

	// Record only if something was actually delivered.
	if len(items) > 0 && res.Sent > 0 {
		joined := strings.Join(results, "; ")
		for _, it := range items {
			rec := SentAlert{CertID: it.CertID, NotAfterUnix: it.NotAfter.Unix(), Level: it.Level, CN: it.CN,
				Serial: it.Serial, NotAfter: it.NotAfter, SentAt: now, Results: joined}
			if err := s.db.Create(&rec).Error; err != nil {
				log.Printf("alerts: couldn't record alert for %s: %v", it.CN, err)
			}
			_ = s.db.Create(&db.AuditEvent{CertID: it.CertID, Who: "system", Action: "expiry_alert",
				Details: levelText(it) + " -- " + joined, Timestamp: now}).Error
		}
	}

	if !manual || len(items) > 0 {
		log.Printf("alerts: %s", res.Summary)
	}
	s.mu.Lock()
	t := now
	s.lastCheck, s.lastResult = &t, res.Summary
	s.mu.Unlock()
	_ = s.store.Put(stateKey, persistedState{LastCheck: &t, LastResult: res.Summary}, "system")
	return res, nil
}

func hasStatusPublisher(ds []Destination) bool {
	for _, d := range ds {
		if d.Type == "mqtt" && d.setting("publish_status") != "false" {
			return true
		}
	}
	return false
}

func (s *Service) recordDest(id string, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.destStates[id]
	if st == nil {
		st = &destState{}
		s.destStates[id] = st
	}
	t := s.now()
	if err != nil {
		st.LastError, st.LastErrorAt = err.Error(), &t
	} else {
		st.LastSuccess, st.LastError, st.LastErrorAt = &t, "", nil
	}
}

func levelText(it Item) string {
	switch {
	case it.Expired:
		ago := -it.DaysLeft
		if ago <= 0 {
			return "expired today"
		}
		return fmt.Sprintf("expired %d day%s ago", ago, plural(ago))
	case it.DaysLeft <= 0:
		return "expires in less than a day"
	default:
		return fmt.Sprintf("expires in %d day%s", it.DaysLeft, plural(it.DaysLeft))
	}
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// compose builds the one message sent for a check run.
func (s *Service) compose(items []Item, cfg Config) Message {
	expired := 0
	for _, it := range items {
		if it.Expired {
			expired++
		}
	}
	var title string
	switch {
	case len(items) == 1 && expired == 1:
		title = "Certificate expired: " + items[0].CN
	case len(items) == 1:
		title = "Certificate " + levelText(items[0]) + ": " + items[0].CN
	case expired == len(items):
		title = fmt.Sprintf("%d certificates have expired", len(items))
	case expired > 0:
		title = fmt.Sprintf("%d certificate alerts (%d expired)", len(items), expired)
	default:
		title = fmt.Sprintf("%d certificates expiring soon", len(items))
	}

	var lines []string
	for _, it := range items {
		lines = append(lines, fmt.Sprintf("• %s — %s (%s)", it.CN, levelText(it), it.NotAfter.In(cfg.location()).Format("Jan 2, 2006 15:04 MST")))
	}
	if expired > 0 {
		lines = append(lines, "", "Expired certificates no longer work; issue replacements.")
	}
	link := ""
	if cfg.StepUIURL != "" {
		link = cfg.StepUIURL + "/inventory"
	}
	return Message{Title: "step-ui: " + title, Body: strings.Join(lines, "\n"), Items: items, Urgent: expired > 0, Link: link}
}

// Test sends a test message to one saved destination.
func (s *Service) Test(ctx context.Context, id string) error {
	cfg, err := s.loadConfig()
	if err != nil {
		return err
	}
	for _, d := range cfg.Destinations {
		if d.ID != id {
			continue
		}
		link := ""
		if cfg.StepUIURL != "" {
			link = cfg.StepUIURL + "/alerts"
		}
		msg := Message{
			Title: "step-ui: test alert",
			Body:  fmt.Sprintf("This is a test from step-ui's expiry alerts. If you can read this, the destination \"%s\" is set up correctly.", d.Name),
			Link:  link, Test: true,
		}
		sctx, cancel := context.WithTimeout(ctx, 45*time.Second)
		defer cancel()
		err := send(sctx, d, msg, s.env(sctx, cfg, d, nil))
		s.recordDest(d.ID, err)
		return err
	}
	return errors.New("destination not found -- save your changes first")
}

// --- Status ----------------------------------------------------------------

type DestinationStatus struct {
	ID string `json:"id"`
	destState
}

type Upcoming struct {
	CertID   string    `json:"cert_id"`
	CN       string    `json:"cn"`
	NotAfter time.Time `json:"not_after"`
	DaysLeft int       `json:"days_left"`
	Expired  bool      `json:"expired"`
}

type Status struct {
	Enabled          bool                `json:"enabled"`
	LastCheck        *time.Time          `json:"last_check"`
	LastResult       string              `json:"last_result"`
	CheckIntervalMin int                 `json:"check_interval_minutes"`
	KeySource        string              `json:"key_source"`
	CARootTrusted    bool                `json:"ca_root_trusted"`
	CARootError      string              `json:"ca_root_error"`
	Destinations     []DestinationStatus `json:"destinations"`
	Recent           []SentAlert         `json:"recent"`
	// Upcoming: active certificates within the largest threshold or
	// already expired, soonest first -- what alerts are about.
	Upcoming []Upcoming `json:"upcoming"`
}

func (s *Service) Status(admin bool) (Status, error) {
	cfg, err := s.loadConfig()
	if err != nil {
		return Status{}, err
	}
	st := Status{Enabled: cfg.Enabled, CheckIntervalMin: int(CheckInterval.Minutes()),
		Destinations: []DestinationStatus{}, Recent: []SentAlert{}, Upcoming: []Upcoming{}}
	if admin {
		st.KeySource = s.store.KeySource()
	}

	s.mu.Lock()
	st.LastCheck, st.LastResult = s.lastCheck, s.lastResult
	st.CARootTrusted = s.trust.Trusted()
	st.CARootError = s.trust.LastError()
	for _, d := range cfg.Destinations {
		ds := DestinationStatus{ID: d.ID}
		if v := s.destStates[d.ID]; v != nil {
			ds.destState = *v
		}
		st.Destinations = append(st.Destinations, ds)
	}
	s.mu.Unlock()

	if err := s.db.Order("sent_at DESC").Limit(25).Find(&st.Recent).Error; err != nil {
		return st, err
	}
	if !admin {
		for i := range st.Recent {
			st.Recent[i].Results = ""
		}
	}

	maxThreshold := 0
	for _, t := range cfg.Thresholds {
		maxThreshold = max(maxThreshold, t)
	}
	now := s.now()
	var certs []db.Certificate
	horizon := now.Add(time.Duration(maxThreshold) * 24 * time.Hour)
	if err := s.db.Where("status = ?", "active").Find(&certs).Error; err != nil {
		return st, err
	}
	for _, c := range certs {
		if c.NotAfter.Before(horizon) {
			st.Upcoming = append(st.Upcoming, Upcoming{CertID: c.ID, CN: c.CN, NotAfter: c.NotAfter,
				DaysLeft: daysLeft(c.NotAfter, now), Expired: !now.Before(c.NotAfter)})
		}
	}
	sort.Slice(st.Upcoming, func(i, j int) bool { return st.Upcoming[i].NotAfter.Before(st.Upcoming[j].NotAfter) })
	return st, nil
}
