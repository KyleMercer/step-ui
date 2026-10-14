package alerts

import (
	"errors"
	"fmt"
	"net/url"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
	// Bundled time zone database, so a time zone works even in a slim
	// container image without tzdata installed.
	_ "time/tzdata"

	"step-ca-webui/internal/settings"

	"github.com/google/uuid"
)

// settingsKey is this feature's one entry in the settings store.
const settingsKey = "alerts"

// Config is what's stored (with secrets sealed).
type Config struct {
	Enabled bool `json:"enabled"`
	// Days before expiry at which to alert, e.g. [30, 7, 1]. An "expired"
	// alert is always sent on top of these.
	Thresholds []int `json:"thresholds"`
	// StepUIURL is the browser-facing address of step-ui, used for the
	// "open step-ui" link in alerts.
	StepUIURL string `json:"step_ui_url"`
	// TrustCARoot adds step-ca's own root certificate to the trusted set
	// for HTTPS/MQTTS/SMTP-TLS destinations -- for ntfy, Home Assistant,
	// a mail server, etc. whose certificates come from this same CA.
	TrustCARoot bool `json:"trust_ca_root"`
	// TimeZone for dates shown in alerts, as an IANA name like
	// "America/Edmonton". Empty = the container's own (usually UTC).
	TimeZone     string        `json:"time_zone"`
	Destinations []Destination `json:"destinations"`
}

func (c Config) location() *time.Location {
	if c.TimeZone != "" {
		if loc, err := time.LoadLocation(c.TimeZone); err == nil {
			return loc
		}
	}
	return time.Local
}

type Destination struct {
	ID       string            `json:"id"`
	Name     string            `json:"name"`
	Type     string            `json:"type"`
	Enabled  bool              `json:"enabled"`
	Settings map[string]string `json:"settings"`
	// Secrets holds sealed values (settings.Store.Seal) -- never sent to a
	// browser in any form.
	Secrets map[string]string `json:"secrets"`
}

func defaultConfig(stepUIURL string) Config {
	return Config{
		Enabled:      false,
		Thresholds:   []int{30, 7, 1},
		StepUIURL:    stepUIURL,
		TrustCARoot:  true,
		Destinations: []Destination{},
	}
}

// --- Destination field schema ---------------------------------------------
//
// Each destination type describes its own fields here. The frontend builds
// its forms from this (GET /api/alerts/schema), so adding a destination
// type is a backend-only change: a schema entry plus a notifier.

type Field struct {
	Key         string   `json:"key"`
	Label       string   `json:"label"`
	Kind        string   `json:"kind"` // text, number, select, checkbox, password
	Secret      bool     `json:"secret"`
	Required    bool     `json:"required"`
	Placeholder string   `json:"placeholder,omitempty"`
	Help        string   `json:"help,omitempty"`
	Options     []Option `json:"options,omitempty"`
	Default     string   `json:"default,omitempty"`
}

type Option struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

type TypeSchema struct {
	Type        string  `json:"type"`
	Label       string  `json:"label"`
	Description string  `json:"description"`
	Fields      []Field `json:"fields"`
}

var pushoverPriorities = []Option{
	{"-2", "Lowest (no notification)"}, {"-1", "Low (quiet)"}, {"0", "Normal"}, {"1", "High (bypasses quiet hours)"},
}

var schemas = []TypeSchema{
	{
		Type:        "pushover",
		Label:       "Pushover",
		Description: "Push notifications to phones and desktops through pushover.net.",
		Fields: []Field{
			{Key: "app_token", Label: "Application API token", Kind: "password", Secret: true, Required: true,
				Help: "Create an application at pushover.net/apps/build and copy its API token."},
			{Key: "user_key", Label: "User or group key", Kind: "password", Secret: true, Required: true,
				Help: "Shown on your pushover.net dashboard. A group key sends to everyone in the group."},
			{Key: "device", Label: "Device (optional)", Kind: "text", Placeholder: "all devices",
				Help: "Limit to one device name; leave blank for all."},
			{Key: "priority_warning", Label: "Priority for warnings", Kind: "select", Options: pushoverPriorities, Default: "0"},
			{Key: "priority_expired", Label: "Priority when something has expired", Kind: "select", Options: pushoverPriorities, Default: "1"},
			{Key: "sound", Label: "Sound (optional)", Kind: "text", Placeholder: "device default",
				Help: "A Pushover sound name, e.g. pushover, siren, none."},
		},
	},
	{
		Type:        "mqtt",
		Label:       "MQTT",
		Description: "Publishes alerts as events, plus a retained status message per certificate (useful for Node-RED, Home Assistant or a status display).",
		Fields: []Field{
			{Key: "broker", Label: "Broker URL", Kind: "text", Required: true, Placeholder: "tcp://mqtt.example.com:1883",
				Help: "tcp:// for plain MQTT, ssl:// (usually port 8883) for MQTT over TLS."},
			{Key: "username", Label: "Username (optional)", Kind: "text"},
			{Key: "password", Label: "Password (optional)", Kind: "password", Secret: true},
			{Key: "topic_prefix", Label: "Topic prefix", Kind: "text", Default: "step-ui",
				Help: "Events go to <prefix>/alerts, per-certificate status to <prefix>/certs/<name>, a summary to <prefix>/summary."},
			{Key: "client_id", Label: "Client ID", Kind: "text", Default: "step-ui-alerts"},
			{Key: "publish_status", Label: "Publish retained status for every certificate on each check", Kind: "checkbox", Default: "true"},
		},
	},
	{
		Type:        "webhook",
		Label:       "Webhook",
		Description: "HTTP POST to a URL: generic JSON (Home Assistant, Node-RED, Gotify adapters), ntfy, or Discord.",
		Fields: []Field{
			{Key: "url", Label: "URL", Kind: "password", Secret: true, Required: true, Placeholder: "https://ntfy.example.com/certs",
				Help: "Stored encrypted, since webhook URLs often contain their own access key."},
			{Key: "format", Label: "Format", Kind: "select", Default: "json", Options: []Option{
				{"json", "Generic JSON"}, {"ntfy", "ntfy"}, {"discord", "Discord"},
			}},
			{Key: "auth_header", Label: "Authorization header (optional)", Kind: "password", Secret: true, Placeholder: "Bearer abc123",
				Help: "Sent as the Authorization header, e.g. for an ntfy access token or a Home Assistant long-lived token."},
		},
	},
	{
		Type:        "email",
		Label:       "Email (SMTP)",
		Description: "Plain-text email through your own mail server or provider.",
		Fields: []Field{
			{Key: "host", Label: "SMTP server", Kind: "text", Required: true, Placeholder: "smtp.example.com"},
			{Key: "port", Label: "Port", Kind: "number", Default: "587"},
			{Key: "security", Label: "Security", Kind: "select", Default: "starttls", Options: []Option{
				{"starttls", "STARTTLS (usually port 587)"}, {"tls", "TLS (usually port 465)"}, {"none", "None (trusted networks only)"},
			}},
			{Key: "username", Label: "Username (optional)", Kind: "text"},
			{Key: "password", Label: "Password (optional)", Kind: "password", Secret: true},
			{Key: "from", Label: "From address", Kind: "text", Required: true, Placeholder: "step-ui@example.com"},
			{Key: "to", Label: "To address(es)", Kind: "text", Required: true, Placeholder: "you@example.com, team@example.com",
				Help: "Separate several addresses with commas."},
		},
	},
}

func schemaFor(typ string) (TypeSchema, bool) {
	for _, s := range schemas {
		if s.Type == typ {
			return s, true
		}
	}
	return TypeSchema{}, false
}

// setting returns a destination's non-secret setting, or the schema default.
func (d Destination) setting(key string) string {
	if v := strings.TrimSpace(d.Settings[key]); v != "" {
		return v
	}
	if s, ok := schemaFor(d.Type); ok {
		for _, f := range s.Fields {
			if f.Key == key {
				return f.Default
			}
		}
	}
	return ""
}

// --- What the browser sees -------------------------------------------------

type DestinationView struct {
	ID       string            `json:"id"`
	Name     string            `json:"name"`
	Type     string            `json:"type"`
	Enabled  bool              `json:"enabled"`
	Settings map[string]string `json:"settings"`
	// Secrets maps each secret field to "set", "unset", or "undecryptable"
	// (stored, but the settings key has changed since -- re-enter it).
	Secrets map[string]string `json:"secrets"`
}

type ConfigView struct {
	Enabled      bool              `json:"enabled"`
	Thresholds   []int             `json:"thresholds"`
	StepUIURL    string            `json:"step_ui_url"`
	TrustCARoot  bool              `json:"trust_ca_root"`
	TimeZone     string            `json:"time_zone"`
	Destinations []DestinationView `json:"destinations"`
}

func toView(c Config, store *settings.Store) ConfigView {
	v := ConfigView{Enabled: c.Enabled, Thresholds: c.Thresholds, StepUIURL: c.StepUIURL, TrustCARoot: c.TrustCARoot,
		TimeZone: c.TimeZone, Destinations: []DestinationView{}}
	for _, d := range c.Destinations {
		dv := DestinationView{ID: d.ID, Name: d.Name, Type: d.Type, Enabled: d.Enabled,
			Settings: map[string]string{}, Secrets: map[string]string{}}
		for k, val := range d.Settings {
			dv.Settings[k] = val
		}
		if s, ok := schemaFor(d.Type); ok {
			for _, f := range s.Fields {
				if !f.Secret {
					continue
				}
				state := "unset"
				if sealed := d.Secrets[f.Key]; sealed != "" {
					state = "set"
					if _, err := store.Open(sealed); err != nil {
						state = "undecryptable"
					}
				}
				dv.Secrets[f.Key] = state
			}
		}
		v.Destinations = append(v.Destinations, dv)
	}
	return v
}

// --- Saving ----------------------------------------------------------------

type DestinationUpdate struct {
	ID       string            `json:"id"` // empty = new destination
	Name     string            `json:"name"`
	Type     string            `json:"type"`
	Enabled  bool              `json:"enabled"`
	Settings map[string]string `json:"settings"`
	// Secrets: only fields being set or replaced. A field left out (or
	// sent empty) keeps its current value.
	Secrets      map[string]string `json:"secrets"`
	ClearSecrets []string          `json:"clear_secrets"`
}

type ConfigUpdate struct {
	Enabled      bool                `json:"enabled"`
	Thresholds   []int               `json:"thresholds"`
	StepUIURL    string              `json:"step_ui_url"`
	TrustCARoot  bool                `json:"trust_ca_root"`
	TimeZone     string              `json:"time_zone"`
	Destinations []DestinationUpdate `json:"destinations"`
}

// applyUpdate validates an update and merges it into the current config:
// destinations are matched by ID (unknown IDs are rejected, missing ones are
// deleted), new secrets are sealed, untouched secrets are carried over.
func applyUpdate(cur Config, up ConfigUpdate, store *settings.Store) (Config, error) {
	next := Config{Enabled: up.Enabled, TrustCARoot: up.TrustCARoot, Destinations: []Destination{}}

	// Thresholds: positive, unique, at most a year, largest first.
	seen := map[int]bool{}
	for _, t := range up.Thresholds {
		if t < 1 || t > 365 {
			return cur, fmt.Errorf("alert thresholds must be between 1 and 365 days (got %d)", t)
		}
		if !seen[t] {
			seen[t] = true
			next.Thresholds = append(next.Thresholds, t)
		}
	}
	if len(next.Thresholds) == 0 {
		return cur, errors.New("set at least one alert threshold, e.g. 30, 7, 1")
	}
	if len(next.Thresholds) > 10 {
		return cur, errors.New("use at most 10 alert thresholds")
	}
	sort.Sort(sort.Reverse(sort.IntSlice(next.Thresholds)))

	next.StepUIURL = strings.TrimRight(strings.TrimSpace(up.StepUIURL), "/")
	if next.StepUIURL != "" {
		if u, err := url.Parse(next.StepUIURL); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return cur, errors.New("step-ui address must be a full http:// or https:// URL")
		}
	}

	next.TimeZone = strings.TrimSpace(up.TimeZone)
	if next.TimeZone != "" {
		if _, err := time.LoadLocation(next.TimeZone); err != nil {
			return cur, fmt.Errorf("unknown time zone %q -- use a name like America/Edmonton or Europe/London", next.TimeZone)
		}
	}

	existing := map[string]Destination{}
	for _, d := range cur.Destinations {
		existing[d.ID] = d
	}

	for i, du := range up.Destinations {
		schema, ok := schemaFor(du.Type)
		if !ok {
			return cur, fmt.Errorf("destination %d: unknown type %q", i+1, du.Type)
		}
		name := strings.TrimSpace(du.Name)
		if name == "" {
			name = schema.Label
		}
		d := Destination{ID: du.ID, Name: name, Type: du.Type, Enabled: du.Enabled,
			Settings: map[string]string{}, Secrets: map[string]string{}}

		if d.ID == "" {
			d.ID = uuid.NewString()
		} else {
			prev, found := existing[d.ID]
			if !found {
				return cur, fmt.Errorf("destination %q no longer exists -- reload the page", name)
			}
			if prev.Type == d.Type {
				for k, v := range prev.Secrets {
					d.Secrets[k] = v
				}
			}
		}

		for _, f := range schema.Fields {
			if f.Secret {
				if slices.Contains(du.ClearSecrets, f.Key) {
					delete(d.Secrets, f.Key)
				}
				if v := strings.TrimSpace(du.Secrets[f.Key]); v != "" {
					sealed, err := store.Seal(v)
					if err != nil {
						return cur, err
					}
					d.Secrets[f.Key] = sealed
				}
				if f.Required && d.Secrets[f.Key] == "" {
					return cur, fmt.Errorf("%s: %s is required", name, f.Label)
				}
				continue
			}
			v := strings.TrimSpace(du.Settings[f.Key])
			if v == "" && f.Required {
				return cur, fmt.Errorf("%s: %s is required", name, f.Label)
			}
			if v != "" && f.Kind == "number" {
				if _, err := strconv.Atoi(v); err != nil {
					return cur, fmt.Errorf("%s: %s must be a number", name, f.Label)
				}
			}
			if v != "" && f.Kind == "select" {
				valid := false
				for _, o := range f.Options {
					valid = valid || o.Value == v
				}
				if !valid {
					return cur, fmt.Errorf("%s: invalid choice for %s", name, f.Label)
				}
			}
			if v != "" {
				d.Settings[f.Key] = v
			}
		}
		if err := validateDestination(d); err != nil {
			return cur, fmt.Errorf("%s: %w", name, err)
		}
		next.Destinations = append(next.Destinations, d)
	}
	return next, nil
}

// validateDestination catches mistakes the schema can't express.
func validateDestination(d Destination) error {
	switch d.Type {
	case "mqtt":
		u, err := url.Parse(d.setting("broker"))
		if err != nil || u.Host == "" {
			return errors.New("broker URL must look like tcp://host:1883 or ssl://host:8883")
		}
		switch u.Scheme {
		case "tcp", "ssl", "tls", "mqtt", "mqtts":
		default:
			return fmt.Errorf("broker URL scheme %q isn't supported -- use tcp:// or ssl://", u.Scheme)
		}
		if strings.ContainsAny(d.setting("topic_prefix"), "#+") {
			return errors.New("topic prefix can't contain MQTT wildcards (# or +)")
		}
	case "email":
		if p := d.setting("port"); p != "" {
			if n, _ := strconv.Atoi(p); n < 1 || n > 65535 {
				return errors.New("port must be between 1 and 65535")
			}
		}
	}
	return nil
}
