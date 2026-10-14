package alerts

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"step-ca-webui/internal/catrust"
	"step-ca-webui/internal/db"
	"step-ca-webui/internal/settings"
)

// --- helpers ---------------------------------------------------------------

type harness struct {
	t     *testing.T
	svc   *Service
	db    *db.Database
	store *settings.Store
	clock time.Time
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	dir := t.TempDir()
	database, err := db.NewDatabase(filepath.Join(dir, "certs.db"))
	if err != nil {
		t.Fatal(err)
	}
	store, err := settings.Open(database.DB, "", dir)
	if err != nil {
		t.Fatal(err)
	}
	svc, err := New(database, store, catrust.New("", ""), "https://step-ui.test")
	if err != nil {
		t.Fatal(err)
	}
	h := &harness{t: t, svc: svc, db: database, store: store, clock: time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)}
	svc.now = func() time.Time { return h.clock }
	return h
}

func (h *harness) addCert(id, cn string, notAfter time.Time, status string) {
	h.t.Helper()
	if err := h.db.CreateCertificate(&db.Certificate{ID: id, CN: cn, SANs: "[]", Serial: "s-" + id, NotAfter: notAfter, Status: status}); err != nil {
		h.t.Fatal(err)
	}
}

// webhookSink collects JSON webhook posts.
type webhookSink struct {
	mu    sync.Mutex
	posts []map[string]any
	fail  bool
	srv   *httptest.Server
}

func newSink(t *testing.T) *webhookSink {
	s := &webhookSink{}
	s.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.fail {
			http.Error(w, "nope", http.StatusInternalServerError)
			return
		}
		var m map[string]any
		_ = json.NewDecoder(r.Body).Decode(&m)
		m["_auth"] = r.Header.Get("Authorization")
		s.posts = append(s.posts, m)
	}))
	t.Cleanup(s.srv.Close)
	return s
}

func (s *webhookSink) count() int { s.mu.Lock(); defer s.mu.Unlock(); return len(s.posts) }
func (s *webhookSink) last() map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.posts[len(s.posts)-1]
}

func (h *harness) configure(up ConfigUpdate) Config {
	h.t.Helper()
	cur, _ := h.svc.loadConfig()
	next, err := applyUpdate(cur, up, h.store)
	if err != nil {
		h.t.Fatal(err)
	}
	if err := h.store.Put(settingsKey, next, "test"); err != nil {
		h.t.Fatal(err)
	}
	return next
}

func webhookDest(url string) DestinationUpdate {
	return DestinationUpdate{Name: "hook", Type: "webhook", Enabled: true,
		Settings: map[string]string{"format": "json"}, Secrets: map[string]string{"url": url, "auth_header": "Bearer t0k"}}
}

func levelsOf(m map[string]any) []int {
	var out []int
	for _, a := range m["alerts"].([]any) {
		out = append(out, int(a.(map[string]any)["level"].(float64)))
	}
	return out
}

// --- tests -----------------------------------------------------------------

func TestLevelFor(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	th := []int{30, 7, 1}
	cases := []struct {
		in     time.Duration
		level  int
		active bool
	}{
		{40 * 24 * time.Hour, 0, false},
		{30 * 24 * time.Hour, 30, true},
		{10 * 24 * time.Hour, 30, true},
		{7 * 24 * time.Hour, 7, true},
		{3 * time.Hour, 1, true},
		{0, 0, true},
		{-5 * 24 * time.Hour, 0, true},
	}
	for _, c := range cases {
		lv, ok := levelFor(now.Add(c.in), now, th)
		if ok != c.active || (ok && lv != c.level) {
			t.Errorf("remaining %v: got (%d,%v), want (%d,%v)", c.in, lv, ok, c.level, c.active)
		}
	}
}

func TestProgressionAndRenewal(t *testing.T) {
	h := newHarness(t)
	sink := newSink(t)
	h.configure(ConfigUpdate{Enabled: true, Thresholds: []int{30, 7, 1}, Destinations: []DestinationUpdate{webhookDest(sink.srv.URL)}})

	exp := h.clock.Add(20 * 24 * time.Hour)
	h.addCert("c1", "web.test", exp, "active")
	h.addCert("c2", "far.test", h.clock.Add(200*24*time.Hour), "active")
	h.addCert("c3", "gone.test", h.clock.Add(-3*24*time.Hour), "revoked")

	ctx := context.Background()
	step := func(at time.Time) int {
		h.clock = at
		before := sink.count()
		if _, err := h.svc.Check(ctx, false); err != nil {
			t.Fatal(err)
		}
		return sink.count() - before
	}

	// 20 days out: the 30-day warning, once.
	if n := step(h.clock); n != 1 || levelsOf(sink.last())[0] != 30 {
		t.Fatalf("expected one 30-day alert, got %d posts (%v)", n, sink.posts)
	}
	if sink.last()["_auth"] != "Bearer t0k" {
		t.Errorf("auth header not sent: %v", sink.last()["_auth"])
	}
	if n := step(h.clock.Add(time.Hour)); n != 0 {
		t.Fatalf("30-day alert repeated")
	}
	// 6 days out: 7-day warning.
	if n := step(exp.Add(-6 * 24 * time.Hour)); n != 1 || levelsOf(sink.last())[0] != 7 {
		t.Fatalf("expected 7-day alert")
	}
	// 12 hours out: 1-day warning.
	if n := step(exp.Add(-12 * time.Hour)); n != 1 || levelsOf(sink.last())[0] != 1 {
		t.Fatalf("expected 1-day alert")
	}
	// Expired: urgent alert, once.
	if n := step(exp.Add(time.Hour)); n != 1 || levelsOf(sink.last())[0] != 0 || sink.last()["urgent"] != true {
		t.Fatalf("expected expired alert: %v", sink.last())
	}
	if n := step(exp.Add(48 * time.Hour)); n != 0 {
		t.Fatalf("expired alert repeated")
	}

	// Renewal: new expiry -> warnings start over.
	cert, _ := h.db.GetCertificate("c1")
	cert.NotAfter = h.clock.Add(5 * 24 * time.Hour)
	_ = h.db.UpdateCertificate(cert)
	if n := step(h.clock.Add(time.Hour)); n != 1 || levelsOf(sink.last())[0] != 7 {
		t.Fatalf("expected fresh 7-day alert after renewal, got %d", n)
	}
}

func TestSkipsLessUrgentWhenEnabledLate(t *testing.T) {
	// A cert already 3 days from expiry when alerts are switched on gets
	// the 7-day alert only -- not 30 and 7 both.
	h := newHarness(t)
	sink := newSink(t)
	h.configure(ConfigUpdate{Enabled: true, Thresholds: []int{30, 7, 1}, Destinations: []DestinationUpdate{webhookDest(sink.srv.URL)}})
	h.addCert("a", "a.test", h.clock.Add(3*24*time.Hour), "active")
	h.addCert("b", "b.test", h.clock.Add(-23*24*time.Hour), "active") // like the September ones
	if _, err := h.svc.Check(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	if sink.count() != 1 {
		t.Fatalf("want one combined message, got %d", sink.count())
	}
	lv := levelsOf(sink.last())
	if len(lv) != 2 || lv[0] != 0 || lv[1] != 7 {
		t.Fatalf("want [expired, 7-day] (soonest first), got %v", lv)
	}
	if !strings.Contains(sink.last()["title"].(string), "1 expired") {
		t.Errorf("title: %v", sink.last()["title"])
	}
}

func TestAllDestinationsFailRetries(t *testing.T) {
	h := newHarness(t)
	sink := newSink(t)
	sink.fail = true
	h.configure(ConfigUpdate{Enabled: true, Thresholds: []int{7}, Destinations: []DestinationUpdate{webhookDest(sink.srv.URL)}})
	h.addCert("a", "a.test", h.clock.Add(2*24*time.Hour), "active")

	res, _ := h.svc.Check(context.Background(), false)
	if res.Sent != 0 || res.Failed != 1 {
		t.Fatalf("expected failure: %+v", res)
	}
	sink.fail = false
	h.clock = h.clock.Add(time.Hour)
	res, _ = h.svc.Check(context.Background(), false)
	if res.Sent != 1 || sink.count() != 1 {
		t.Fatalf("expected retry to deliver: %+v", res)
	}
	st, _ := h.svc.Status(true)
	if len(st.Recent) != 1 || !strings.Contains(st.Recent[0].Results, "hook: ok") {
		t.Fatalf("history: %+v", st.Recent)
	}
}

func TestSecretsKeptReplacedClearedAndNeverShown(t *testing.T) {
	h := newHarness(t)
	sink := newSink(t)
	cfg := h.configure(ConfigUpdate{Enabled: true, Thresholds: []int{7}, Destinations: []DestinationUpdate{webhookDest(sink.srv.URL)}})
	id := cfg.Destinations[0].ID

	// Stored sealed, not in plaintext.
	var raw settings.Setting
	h.db.DB.Where("key = ?", settingsKey).First(&raw)
	if strings.Contains(raw.Value, sink.srv.URL) || strings.Contains(raw.Value, "t0k") {
		t.Fatal("secret stored in plaintext")
	}
	// View shows state only.
	v := toView(cfg, h.store)
	b, _ := json.Marshal(v)
	if strings.Contains(string(b), "t0k") || v.Destinations[0].Secrets["url"] != "set" {
		t.Fatalf("view leaks or wrong state: %s", b)
	}

	// Save again without secrets: kept.
	cfg = h.configure(ConfigUpdate{Enabled: true, Thresholds: []int{7}, Destinations: []DestinationUpdate{
		{ID: id, Name: "hook", Type: "webhook", Enabled: true, Settings: map[string]string{"format": "json"}}}})
	if err := h.svc.Test(context.Background(), id); err != nil || sink.last()["_auth"] != "Bearer t0k" {
		t.Fatalf("secrets not kept: %v %v", err, sink.last()["_auth"])
	}
	// Clear the optional one.
	h.configure(ConfigUpdate{Enabled: true, Thresholds: []int{7}, Destinations: []DestinationUpdate{
		{ID: id, Name: "hook", Type: "webhook", Enabled: true, ClearSecrets: []string{"auth_header"}}}})
	_ = h.svc.Test(context.Background(), id)
	if sink.last()["_auth"] != "" {
		t.Fatal("auth header not cleared")
	}
	// Required secret can't be cleared.
	cur, _ := h.svc.loadConfig()
	if _, err := applyUpdate(cur, ConfigUpdate{Thresholds: []int{7}, Destinations: []DestinationUpdate{
		{ID: id, Type: "webhook", ClearSecrets: []string{"url"}}}}, h.store); err == nil {
		t.Fatal("expected error clearing required URL")
	}
	// Unknown ID refused.
	if _, err := applyUpdate(cur, ConfigUpdate{Thresholds: []int{7}, Destinations: []DestinationUpdate{
		{ID: "nope", Type: "webhook"}}}, h.store); err == nil {
		t.Fatal("expected error for unknown destination id")
	}
}

func TestKeyChangeMakesSecretsUndecryptable(t *testing.T) {
	dir := t.TempDir()
	database, _ := db.NewDatabase(filepath.Join(dir, "certs.db"))
	keyA := filepath.Join(dir, "a.key")
	os.WriteFile(keyA, []byte("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"), 0o600)
	storeA, err := settings.Open(database.DB, keyA, dir)
	if err != nil {
		t.Fatal(err)
	}
	sealed, _ := storeA.Seal("hunter2")
	if v, _ := storeA.Open(sealed); v != "hunter2" {
		t.Fatal("round trip failed")
	}
	keyB := filepath.Join(dir, "b.key")
	os.WriteFile(keyB, []byte("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"), 0o600)
	storeB, _ := settings.Open(database.DB, keyB, dir)
	if _, err := storeB.Open(sealed); err != settings.ErrUndecryptable {
		t.Fatalf("want ErrUndecryptable, got %v", err)
	}
	// Missing Docker-secret file is an error, not a silent new key.
	if _, err := settings.Open(database.DB, filepath.Join(dir, "missing"), dir); err == nil {
		t.Fatal("expected error for missing SETTINGS_KEY_FILE")
	}
	// Default key file is created once, 0600, and reused.
	s1, err := settings.Open(database.DB, "", dir)
	if err != nil {
		t.Fatal(err)
	}
	fi, _ := os.Stat(filepath.Join(dir, "settings.key"))
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("key file mode %v", fi.Mode().Perm())
	}
	x, _ := s1.Seal("v")
	s2, _ := settings.Open(database.DB, "", dir)
	if v, err := s2.Open(x); err != nil || v != "v" {
		t.Fatal("default key not reused")
	}
}

func TestPushoverRequest(t *testing.T) {
	var got url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		got, _ = url.ParseQuery(string(b))
		if got.Get("token") == "bad" {
			w.WriteHeader(400)
			w.Write([]byte(`{"token":"invalid","errors":["application token is invalid"],"status":0}`))
			return
		}
		w.Write([]byte(`{"status":1}`))
	}))
	defer srv.Close()
	old := pushoverAPI
	pushoverAPI = srv.URL
	defer func() { pushoverAPI = old }()

	d := Destination{Type: "pushover", Settings: map[string]string{"sound": "siren"}}
	secrets := map[string]string{"app_token": "tok", "user_key": "usr"}
	env := sendEnv{http: srv.Client(), secret: func(k string) (string, error) { return secrets[k], nil }}

	msg := Message{Title: "t", Body: "b", Urgent: true, Link: "https://x/inventory"}
	if err := sendPushover(context.Background(), d, msg, env); err != nil {
		t.Fatal(err)
	}
	if got.Get("priority") != "1" || got.Get("sound") != "siren" || got.Get("url") != "https://x/inventory" || got.Get("user") != "usr" {
		t.Fatalf("unexpected form: %v", got)
	}
	msg.Urgent = false
	_ = sendPushover(context.Background(), d, msg, env)
	if got.Get("priority") != "0" {
		t.Fatalf("warning priority: %v", got.Get("priority"))
	}
	secrets["app_token"] = "bad"
	if err := sendPushover(context.Background(), d, msg, env); err == nil || !strings.Contains(err.Error(), "application token is invalid") {
		t.Fatalf("want readable pushover error, got %v", err)
	}
}

func TestNtfyAndDiscordFormats(t *testing.T) {
	var hdr http.Header
	var body string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hdr = r.Header.Clone()
		b, _ := io.ReadAll(r.Body)
		body = string(b)
	}))
	defer srv.Close()
	env := sendEnv{http: srv.Client(), secret: func(k string) (string, error) {
		if k == "url" {
			return srv.URL, nil
		}
		return "", nil
	}}
	msg := Message{Title: "step-ui: 1 certificates have expired", Body: "• a — expired", Urgent: true, Link: "https://x"}

	_ = sendWebhook(context.Background(), Destination{Type: "webhook", Settings: map[string]string{"format": "ntfy"}}, msg, env)
	if hdr.Get("Priority") != "high" || hdr.Get("Click") != "https://x" || body != msg.Body {
		t.Fatalf("ntfy: %v %q", hdr, body)
	}
	_ = sendWebhook(context.Background(), Destination{Type: "webhook", Settings: map[string]string{"format": "discord"}}, msg, env)
	var dc map[string]string
	if json.Unmarshal([]byte(body), &dc) != nil || !strings.HasPrefix(dc["content"], "**step-ui:") {
		t.Fatalf("discord: %q", body)
	}
}
