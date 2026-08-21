package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/smtp"
	"strings"
	"time"
)

// notificationMessage is one alarm transition, rendered for delivery.
type notificationMessage struct {
	Kind    string    `json:"kind"`
	RuleID  string    `json:"rule_id"`
	Label   string    `json:"label"`
	Path    string    `json:"path"`
	State   string    `json:"state"`
	Message string    `json:"message"`
	Value   float64   `json:"value"`
	Vessel  string    `json:"vessel"`
	At      time.Time `json:"at"`
}

func (m notificationMessage) title() string {
	prefix := strings.ToUpper(m.State)
	if m.Kind == alarmEventCleared {
		prefix = "CLEARED"
	}
	if m.Vessel != "" {
		return fmt.Sprintf("[%s] %s: %s", prefix, m.Vessel, m.Label)
	}
	return fmt.Sprintf("[%s] %s", prefix, m.Label)
}

type notificationTransport interface {
	ID() string
	Send(ctx context.Context, msg notificationMessage) error
}

// notifyHTTPClient is shared by the HTTP transports. The timeout is generous
// relative to the read paths: a notification is worth waiting for.
func notifyHTTPClient() *http.Client {
	return &http.Client{Timeout: 10 * time.Second}
}

// ── ntfy ──────────────────────────────────────────────────────────────────────

// ntfy is the default transport: self-hostable in one container, or the free
// public server with no account, so off-boat push needs no subscription.
type ntfyTransport struct {
	config ntfyConfig
	token  string
}

func (t ntfyTransport) ID() string { return transportNtfy }

func (t ntfyTransport) Send(ctx context.Context, msg notificationMessage) error {
	url := t.config.Server + "/" + t.config.Topic

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(msg.Message))
	if err != nil {
		return err
	}
	request.Header.Set("Title", msg.title())
	request.Header.Set("Priority", ntfyPriorityFor(msg))
	request.Header.Set("Tags", ntfyTagsFor(msg))
	if t.token != "" {
		request.Header.Set("Authorization", "Bearer "+t.token)
	}

	response, err := notifyHTTPClient().Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()

	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("ntfy returned status %d", response.StatusCode)
	}
	return nil
}

// ntfyPriorityFor maps SignalK severities onto ntfy's 1-5 scale. An emergency
// gets max priority so it bypasses the phone's do-not-disturb.
func ntfyPriorityFor(msg notificationMessage) string {
	if msg.Kind == alarmEventCleared {
		return "2"
	}
	switch msg.State {
	case alarmStateEmergency:
		return "5"
	case alarmStateAlarm:
		return "4"
	case alarmStateWarn:
		return "3"
	default:
		return "2"
	}
}

func ntfyTagsFor(msg notificationMessage) string {
	if msg.Kind == alarmEventCleared {
		return "white_check_mark"
	}
	switch msg.State {
	case alarmStateEmergency:
		return "rotating_light"
	case alarmStateAlarm:
		return "warning"
	default:
		return "bell"
	}
}

// ── webhook ───────────────────────────────────────────────────────────────────

type webhookTransport struct {
	config webhookConfig
}

func (t webhookTransport) ID() string { return transportWebhook }

func (t webhookTransport) Send(ctx context.Context, msg notificationMessage) error {
	body, err := json.Marshal(msg)
	if err != nil {
		return err
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, t.config.URL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")

	response, err := notifyHTTPClient().Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()

	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("webhook returned status %d", response.StatusCode)
	}
	return nil
}

// ── smtp ──────────────────────────────────────────────────────────────────────

type smtpTransport struct {
	config   smtpConfig
	password string
	// send is injectable so tests exercise message construction without a
	// real mail server.
	send func(addr string, a smtp.Auth, from string, to []string, msg []byte) error
}

func (t smtpTransport) ID() string { return transportSMTP }

func (t smtpTransport) Send(_ context.Context, msg notificationMessage) error {
	body := buildAlarmEmail(t.config, msg)

	var auth smtp.Auth
	if t.config.Username != "" {
		auth = smtp.PlainAuth("", t.config.Username, t.password, t.config.Host)
	}

	sender := t.send
	if sender == nil {
		sender = smtp.SendMail
	}

	addr := fmt.Sprintf("%s:%d", t.config.Host, t.config.Port)
	return sender(addr, auth, t.config.From, t.config.To, body)
}

func buildAlarmEmail(config smtpConfig, msg notificationMessage) []byte {
	var b strings.Builder
	fmt.Fprintf(&b, "From: %s\r\n", config.From)
	fmt.Fprintf(&b, "To: %s\r\n", strings.Join(config.To, ", "))
	fmt.Fprintf(&b, "Subject: %s\r\n", msg.title())
	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString("Content-Type: text/plain; charset=utf-8\r\n")
	b.WriteString("\r\n")
	fmt.Fprintf(&b, "%s\r\n\r\n", msg.Message)
	fmt.Fprintf(&b, "Path:  %s\r\n", msg.Path)
	fmt.Fprintf(&b, "State: %s\r\n", msg.State)
	fmt.Fprintf(&b, "Value: %g\r\n", msg.Value)
	fmt.Fprintf(&b, "Time:  %s\r\n", msg.At.UTC().Format(time.RFC3339))
	return []byte(b.String())
}

// ── signalk notifications ─────────────────────────────────────────────────────

// signalKNotifyTransport publishes Helmcentral's alarms back onto the bus, so a
// buzzer plugin or MFD reacts without knowing Helmcentral exists. Unlike the
// others this needs no internet at all.
type signalKNotifyTransport struct {
	put func(path string, value any) error
}

func (t signalKNotifyTransport) ID() string { return transportSignalK }

func (t signalKNotifyTransport) Send(_ context.Context, msg notificationMessage) error {
	path := notificationsRoot + "." + strings.TrimPrefix(msg.Path, notificationsRoot+".")

	if msg.Kind == alarmEventCleared {
		// SignalK clears a notification by writing null to its path.
		return t.put(path, nil)
	}

	return t.put(path, map[string]any{
		"state":   msg.State,
		"message": msg.Message,
		"method":  []string{"visual", "sound"},
	})
}

// ── dispatch ──────────────────────────────────────────────────────────────────

// buildTransports returns the transports enabled by config. Secrets are read at
// build time from the encrypted store rather than kept in the config file.
func buildTransports(config alarmTransportConfig) []notificationTransport {
	var transports []notificationTransport

	if config.Ntfy.Enabled {
		transports = append(transports, ntfyTransport{config: config.Ntfy, token: secretOrEmpty("NTFY_TOKEN")})
	}
	if config.Webhook.Enabled {
		transports = append(transports, webhookTransport{config: config.Webhook})
	}
	if config.SMTP.Enabled {
		transports = append(transports, smtpTransport{config: config.SMTP, password: secretOrEmpty("SMTP_PASSWORD")})
	}
	if config.SignalK.Enabled {
		transports = append(transports, signalKNotifyTransport{put: publishSignalKNotification})
	}
	if config.WebPush.Enabled {
		transports = append(transports, webPushTransport{
			config:     config.WebPush,
			publicKey:  secretOrEmpty(vapidPublicKeySecret),
			privateKey: secretOrEmpty(vapidPrivateKeySecret),
			store:      globalWebPushSubscriptionStore,
		})
	}

	return transports
}

func secretOrEmpty(key string) string {
	if globalSecretsStore == nil {
		return ""
	}
	value, ok, err := globalSecretsStore.Get(key)
	if err != nil || !ok {
		return ""
	}
	return value
}

// selectTransports honours a rule's explicit transport list, falling back to
// every enabled transport when it names none — an alarm with somewhere to go is
// better than one configured into silence.
func selectTransports(all []notificationTransport, wanted []string) []notificationTransport {
	if len(wanted) == 0 {
		return all
	}

	want := make(map[string]bool, len(wanted))
	for _, id := range wanted {
		want[strings.TrimSpace(id)] = true
	}

	var selected []notificationTransport
	for _, transport := range all {
		if want[transport.ID()] {
			selected = append(selected, transport)
		}
	}
	return selected
}

func notificationFor(event alarmEvent, vessel string, now time.Time) notificationMessage {
	return notificationMessage{
		Kind:    event.Kind,
		RuleID:  event.Rule.ID,
		Label:   event.Rule.Label,
		Path:    event.Rule.Path,
		State:   event.Status.State,
		Message: event.Status.Message,
		Value:   event.Status.Value,
		Vessel:  vessel,
		At:      now,
	}
}

// The signalk transport publishes as a producer on the delta stream
// (signalk_publish.go). It used to write over REST, which reached no endpoint
// the server serves — notifications have no PUT handler — so it had never once
// delivered anything. See the ADR 0038 addendum.
