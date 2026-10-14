package alerts

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/smtp"
	"net/url"
	"strings"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

// Item is one certificate needing attention.
type Item struct {
	CertID   string    `json:"cert_id"`
	CN       string    `json:"cn"`
	Serial   string    `json:"serial"`
	NotAfter time.Time `json:"not_after"`
	// DaysLeft is whole days remaining (negative once expired).
	DaysLeft int `json:"days_left"`
	// Level is the threshold that triggered this alert in days, or 0 for
	// "expired".
	Level   int  `json:"level"`
	Expired bool `json:"expired"`
}

// Message is one notification: usually several Items from one check run,
// so ten certificates expiring at once means one notification, not ten.
type Message struct {
	Title  string
	Body   string // plain text, one line per item
	Items  []Item
	Urgent bool // something has expired
	Link   string
	Test   bool
}

// CertStatus is one certificate's state, for MQTT's retained status topics.
type CertStatus struct {
	CN       string    `json:"cn"`
	Serial   string    `json:"serial"`
	NotAfter time.Time `json:"not_after"`
	DaysLeft int       `json:"days_left"`
	Status   string    `json:"status"` // ok, expiring, expired
}

type Snapshot struct {
	CheckedAt time.Time    `json:"checked_at"`
	Certs     []CertStatus `json:"-"`
	Active    int          `json:"active"`
	Expiring  int          `json:"expiring"`
	Expired   int          `json:"expired"`
}

// sendEnv is what a notifier needs besides its own destination settings.
type sendEnv struct {
	tls      *tls.Config
	http     *http.Client
	secret   func(key string) (string, error)
	snapshot *Snapshot // MQTT only; nil = don't publish status
}

func send(ctx context.Context, d Destination, msg Message, env sendEnv) error {
	switch d.Type {
	case "pushover":
		return sendPushover(ctx, d, msg, env)
	case "webhook":
		return sendWebhook(ctx, d, msg, env)
	case "email":
		return sendEmail(ctx, d, msg, env)
	case "mqtt":
		return sendMQTT(ctx, d, msg, env)
	}
	return fmt.Errorf("unknown destination type %q", d.Type)
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}

// readErrorBody keeps a little of a failed HTTP response for the error
// message -- usually the most useful part ("invalid token", etc.).
func readErrorBody(resp *http.Response) string {
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 300))
	return strings.TrimSpace(string(b))
}

// --- Pushover --------------------------------------------------------------

var pushoverAPI = "https://api.pushover.net/1/messages.json"

func sendPushover(ctx context.Context, d Destination, msg Message, env sendEnv) error {
	token, err := env.secret("app_token")
	if err != nil {
		return err
	}
	user, err := env.secret("user_key")
	if err != nil {
		return err
	}
	priority := d.setting("priority_warning")
	if msg.Urgent {
		priority = d.setting("priority_expired")
	}
	form := url.Values{
		"token":    {token},
		"user":     {user},
		"title":    {truncate(msg.Title, 250)},
		"message":  {truncate(msg.Body, 1024)},
		"priority": {priority},
	}
	if v := d.setting("device"); v != "" {
		form.Set("device", v)
	}
	if v := d.setting("sound"); v != "" {
		form.Set("sound", v)
	}
	if msg.Link != "" {
		form.Set("url", msg.Link)
		form.Set("url_title", "Open step-ui")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, pushoverAPI, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := env.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		// Pushover answers {"errors": ["application token is invalid"], ...}
		var pe struct {
			Errors []string `json:"errors"`
		}
		body := readErrorBody(resp)
		if json.Unmarshal([]byte(body), &pe) == nil && len(pe.Errors) > 0 {
			return fmt.Errorf("pushover: %s", strings.Join(pe.Errors, "; "))
		}
		return fmt.Errorf("pushover: HTTP %d: %s", resp.StatusCode, body)
	}
	return nil
}

// --- Webhook ---------------------------------------------------------------

func sendWebhook(ctx context.Context, d Destination, msg Message, env sendEnv) error {
	target, err := env.secret("url")
	if err != nil {
		return err
	}
	u, err := url.Parse(target)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return errors.New("webhook URL must be a full http:// or https:// URL")
	}
	auth, err := env.secret("auth_header")
	if err != nil {
		return err
	}

	var body []byte
	headers := map[string]string{}
	switch d.setting("format") {
	case "ntfy":
		body = []byte(msg.Body)
		headers["Content-Type"] = "text/plain; charset=utf-8"
		headers["Title"] = msg.Title
		headers["Priority"] = "default"
		headers["Tags"] = "warning"
		if msg.Urgent {
			headers["Priority"] = "high"
			headers["Tags"] = "rotating_light"
		}
		if msg.Link != "" {
			headers["Click"] = msg.Link
		}
	case "discord":
		content := "**" + msg.Title + "**\n" + msg.Body
		if msg.Link != "" {
			content += "\n" + msg.Link
		}
		body, _ = json.Marshal(map[string]string{"content": truncate(content, 2000)})
		headers["Content-Type"] = "application/json"
	default:
		items := msg.Items
		if items == nil {
			items = []Item{}
		}
		body, _ = json.Marshal(map[string]any{
			"event":   "certificate_expiry",
			"test":    msg.Test,
			"title":   msg.Title,
			"message": msg.Body,
			"urgent":  msg.Urgent,
			"link":    msg.Link,
			"alerts":  items,
		})
		headers["Content-Type"] = "application/json"
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(body))
	if err != nil {
		return err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	resp, err := env.http.Do(req)
	if err != nil {
		// Don't echo the URL back: it's treated as a secret.
		var ue *url.Error
		if errors.As(err, &ue) {
			return fmt.Errorf("webhook: %v", ue.Err)
		}
		return fmt.Errorf("webhook: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("webhook: HTTP %d: %s", resp.StatusCode, readErrorBody(resp))
	}
	return nil
}

// --- Email -----------------------------------------------------------------

func splitAddresses(s string) []string {
	var out []string
	for _, a := range strings.Split(s, ",") {
		if a = strings.TrimSpace(a); a != "" {
			out = append(out, a)
		}
	}
	return out
}

// headerSafe stops a value from smuggling extra headers into the message.
func headerSafe(s string) string {
	return strings.NewReplacer("\r", " ", "\n", " ").Replace(s)
}

func sendEmail(ctx context.Context, d Destination, msg Message, env sendEnv) error {
	host := d.setting("host")
	port := d.setting("port")
	from := d.setting("from")
	to := splitAddresses(d.setting("to"))
	if len(to) == 0 {
		return errors.New("email: no recipient addresses")
	}
	password, err := env.secret("password")
	if err != nil {
		return err
	}
	username := d.setting("username")
	security := d.setting("security")
	addr := net.JoinHostPort(host, port)

	tlsCfg := env.tls.Clone()
	tlsCfg.ServerName = host

	deadline, ok := ctx.Deadline()
	if !ok {
		deadline = time.Now().Add(30 * time.Second)
	}
	dialer := &net.Dialer{Deadline: deadline}

	var conn net.Conn
	if security == "tls" {
		conn, err = tls.DialWithDialer(dialer, "tcp", addr, tlsCfg)
	} else {
		conn, err = dialer.DialContext(ctx, "tcp", addr)
	}
	if err != nil {
		return fmt.Errorf("email: connecting to %s: %w", addr, err)
	}
	_ = conn.SetDeadline(deadline)

	c, err := smtp.NewClient(conn, host)
	if err != nil {
		conn.Close()
		return fmt.Errorf("email: %w", err)
	}
	defer c.Close()

	if security == "starttls" {
		if ok, _ := c.Extension("STARTTLS"); !ok {
			return errors.New("email: server doesn't offer STARTTLS -- choose TLS or None to match the server")
		}
		if err := c.StartTLS(tlsCfg); err != nil {
			return fmt.Errorf("email: STARTTLS: %w", err)
		}
	}
	if username != "" {
		// net/smtp's PlainAuth refuses to send a password over an
		// unencrypted connection (except to localhost), which is the
		// behaviour we want.
		if err := c.Auth(smtp.PlainAuth("", username, password, host)); err != nil {
			return fmt.Errorf("email: login failed: %w", err)
		}
	}
	if err := c.Mail(from); err != nil {
		return fmt.Errorf("email: sender %s refused: %w", from, err)
	}
	for _, rcpt := range to {
		if err := c.Rcpt(rcpt); err != nil {
			return fmt.Errorf("email: recipient %s refused: %w", rcpt, err)
		}
	}
	w, err := c.Data()
	if err != nil {
		return fmt.Errorf("email: %w", err)
	}
	body := msg.Body
	if msg.Link != "" {
		body += "\n\n" + msg.Link
	}
	var sb strings.Builder
	sb.WriteString("From: " + headerSafe(from) + "\r\n")
	sb.WriteString("To: " + headerSafe(strings.Join(to, ", ")) + "\r\n")
	sb.WriteString("Subject: " + headerSafe(msg.Title) + "\r\n")
	sb.WriteString("Date: " + time.Now().Format(time.RFC1123Z) + "\r\n")
	sb.WriteString(fmt.Sprintf("Message-ID: <%d.step-ui-alerts@%s>\r\n", time.Now().UnixNano(), headerSafe(host)))
	sb.WriteString("MIME-Version: 1.0\r\nContent-Type: text/plain; charset=utf-8\r\nContent-Transfer-Encoding: 8bit\r\n\r\n")
	sb.WriteString(strings.ReplaceAll(body, "\n", "\r\n") + "\r\n")
	if _, err := io.WriteString(w, sb.String()); err != nil {
		return fmt.Errorf("email: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("email: %w", err)
	}
	return c.Quit()
}

// --- MQTT ------------------------------------------------------------------

// topicName turns a certificate name into a single, wildcard-free topic
// level: "*.example.com" -> "_.example.com".
func topicName(s string) string {
	return strings.NewReplacer("/", "_", "+", "_", "#", "_", "*", "_", " ", "_").Replace(s)
}

func sendMQTT(ctx context.Context, d Destination, msg Message, env sendEnv) error {
	password, err := env.secret("password")
	if err != nil {
		return err
	}
	broker := d.setting("broker")
	// paho understands tcp/ssl/tls/ws/wss; accept mqtt/mqtts too.
	broker = strings.Replace(strings.Replace(broker, "mqtts://", "ssl://", 1), "mqtt://", "tcp://", 1)

	prefix := strings.Trim(d.setting("topic_prefix"), "/")
	opts := mqtt.NewClientOptions().
		AddBroker(broker).
		SetClientID(d.setting("client_id")).
		SetUsername(d.setting("username")).
		SetPassword(password).
		SetTLSConfig(env.tls).
		SetConnectTimeout(10 * time.Second).
		SetWriteTimeout(10 * time.Second).
		SetAutoReconnect(false).
		SetConnectRetry(false).
		SetCleanSession(true)
	client := mqtt.NewClient(opts)
	tok := client.Connect()
	if !tok.WaitTimeout(15 * time.Second) {
		return fmt.Errorf("mqtt: timed out connecting to %s", broker)
	}
	if err := tok.Error(); err != nil {
		return fmt.Errorf("mqtt: connecting to %s: %w", broker, err)
	}
	defer client.Disconnect(250)

	publish := func(topic string, payload any, retain bool) error {
		b, _ := json.Marshal(payload)
		t := client.Publish(topic, 1, retain, b)
		if !t.WaitTimeout(10 * time.Second) {
			return fmt.Errorf("mqtt: timed out publishing to %s", topic)
		}
		if err := t.Error(); err != nil {
			return fmt.Errorf("mqtt: publishing to %s: %w", topic, err)
		}
		return nil
	}

	if msg.Test {
		return publish(prefix+"/test", map[string]any{"title": msg.Title, "message": msg.Body, "sent_at": time.Now()}, false)
	}

	// One event per alerting certificate.
	for _, it := range msg.Items {
		ev := map[string]any{
			"cn": it.CN, "serial": it.Serial, "not_after": it.NotAfter, "days_left": it.DaysLeft,
			"level": it.Level, "expired": it.Expired, "link": msg.Link,
		}
		if err := publish(prefix+"/alerts", ev, false); err != nil {
			return err
		}
	}

	// Retained status: always reflects the latest check, so a display or
	// flow that connects later still sees the current state immediately.
	if env.snapshot != nil && d.setting("publish_status") != "false" {
		for _, cs := range env.snapshot.Certs {
			if err := publish(prefix+"/certs/"+topicName(cs.CN), cs, true); err != nil {
				return err
			}
		}
		if err := publish(prefix+"/summary", env.snapshot, true); err != nil {
			return err
		}
	}
	_ = ctx
	return nil
}
