package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/smtp"
	"strings"
	"time"
)

// notificationMessage is one alarm transition, rendered for delivery.
type notificationMessage struct {
	Kind    string  `json:"kind"`
	RuleID  string  `json:"rule_id"`
	Label   string  `json:"label"`
	Path    string  `json:"path"`
	State   string  `json:"state"`
	Message string  `json:"message"`
	Value   float64 `json:"value"`
	// Unit is the SI unit of Value, when known -- carried so a transport can
	// render Value the way an operator reads it (formatAlarmReading,
	// alarm_units.go) instead of raw SI. Empty for the heartbeat and test
	// notifications, which have no Value to convert.
	Unit   string    `json:"unit,omitempty"`
	Vessel string    `json:"vessel"`
	At     time.Time `json:"at"`
	// Watch is the report of a watch_report message (watches_notify.go). It
	// is nil on every alarm notification.
	Watch *watchReportMessage `json:"watch,omitempty"`
	// Charts is the chart of the reading that raised an alarm (ADR 0235), as
	// data, or one entry with Problem set and no series when it was left out.
	// It is never set on a watch report, whose charts are on Watch.
	Charts []watchChart `json:"charts,omitempty"`

	// note, when set, is told where a delivery was not as meant (a phone
	// message sent without its chart). It is the dispatcher's, never queued.
	note func(string)
	// pngs are the pictures of Charts, drawn once by the dispatcher for the
	// channels that send one (ADR 0235). Unset on a message read back from the
	// queue and on a watch report, which draw their own.
	pngs [][]byte
}

// renderedCharts is the message's charts with a picture for each that could be
// drawn (nil for one that could not), drawing them unless the dispatcher has.
// A channel that sends only the first chart asks for limit 1, so a chart it
// never sends cannot put a note on the run. Limit 0 means all of them.
func (m notificationMessage) renderedCharts(channel string, limit int) ([]watchChart, [][]byte) {
	charts, pngs := m.chartList(), m.pngs
	if limit > 0 && len(charts) > limit {
		charts = charts[:limit]
	}
	if pngs != nil {
		return charts, pngs[:len(charts)]
	}
	return renderChartsForSend(charts, channel, m.note)
}

func (m notificationMessage) isWatchReport() bool { return m.Kind == notificationKindWatchReport }

// chartList is the charts this message carries: a report's, or an alarm's.
func (m notificationMessage) chartList() []watchChart {
	if m.isWatchReport() {
		if m.Watch == nil {
			return nil
		}
		return m.Watch.Charts
	}
	return m.Charts
}

// clickLink is the link a phone opens from the notification, if there is one.
func (m notificationMessage) clickLink() string {
	if m.isWatchReport() && m.Watch != nil {
		return m.Watch.Link
	}
	return ""
}

func (m notificationMessage) title() string {
	if m.isWatchReport() {
		return m.Label
	}
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
//
// CheckRedirect re-runs the address check on every hop. validateWebhookURL
// vets a URL when it is saved, but that only settles where the first request
// goes: a host that passed then is free to answer a real alarm with a 302
// into loopback or link-local, and Go follows redirects by default. Since
// the whole point of the save-time check is that Helmcentral should not be
// made to originate requests into ranges only it can reach, the check has to
// hold for wherever the chain actually ends up, not just its first hop.
func notifyHTTPClient() *http.Client {
	return &http.Client{
		Timeout: 10 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return fmt.Errorf("stopped after %d redirects", len(via))
			}
			return validateWebhookURL(req.URL.String())
		},
	}
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
	if len(msg.chartList()) > 0 {
		err := t.sendWithChart(ctx, msg)
		var refused *ntfyAttachmentRefused
		if !errors.As(err, &refused) {
			return err
		}
		// A delivery choice (ADR 0228): the server will not take the picture,
		// so the message goes as text rather than not at all. It is logged
		// and noted on the run, never silent.
		log.Printf("notify: ntfy refused the chart of %q, so it is sent as text only: %s", msg.Label, refused.reason)
		if msg.note != nil {
			msg.note("ntfy: sent without the chart (" + refused.reason + ")")
		}
	}
	return t.sendText(ctx, msg)
}

func (t ntfyTransport) sendText(ctx context.Context, msg notificationMessage) error {
	url := t.config.Server + "/" + t.config.Topic

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(msg.Message))
	if err != nil {
		return err
	}
	if msg.isWatchReport() {
		// A watch's name is typed by the operator: control characters would end
		// the header line, and anything not ASCII needs RFC 2047.
		request.Header.Set("Title", watchEncodedHeaderText(msg.title()))
		if msg.Watch != nil && msg.Watch.Link != "" {
			request.Header.Set("Click", msg.Watch.Link)
		}
	} else {
		request.Header.Set("Title", msg.title())
	}
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

// withChartLeftOut is the message with a closing line saying a chart was left
// out and why.
func withChartLeftOut(msg notificationMessage, problem string) notificationMessage {
	msg.Message += "\nChart left out: " + problem
	return msg
}

// ntfyAttachmentRefused is an ntfy server saying it will not take the chart:
// attachments are off, or the file is over its limits.
type ntfyAttachmentRefused struct{ reason string }

func (e *ntfyAttachmentRefused) Error() string { return "ntfy refused the attachment: " + e.reason }

// watchNtfyMessageChars caps the Message header of a report sent with a chart.
const watchNtfyMessageChars = 500

// sendWithChart sends a watch report with its first chart as an attachment:
// ntfy's PUT API takes the file as the body and the text in the Message header.
func (t ntfyTransport) sendWithChart(ctx context.Context, msg notificationMessage) error {
	charts, pngs := msg.renderedCharts("ntfy", 1)
	chart, data := charts[0], pngs[0]
	if data == nil {
		// Left out, and the message says so: it goes as text with the line.
		return t.sendText(ctx, withChartLeftOut(msg, chart.Problem))
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPut, t.config.Server+"/"+t.config.Topic, bytes.NewReader(data))
	if err != nil {
		return err
	}
	request.Header.Set("Filename", watchChartFilename(chart.Title))
	request.Header.Set("Message", watchEncodedHeaderText(truncateRunes(watchHeaderText(msg.Message), watchNtfyMessageChars)))
	request.Header.Set("Title", watchEncodedHeaderText(msg.title()))
	if link := msg.clickLink(); link != "" {
		request.Header.Set("Click", link)
	}
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
	if response.StatusCode >= 200 && response.StatusCode < 300 {
		return nil
	}
	body, _ := io.ReadAll(io.LimitReader(response.Body, 2048))
	if (response.StatusCode >= 400 && response.StatusCode < 500 || response.StatusCode == http.StatusInsufficientStorage) &&
		strings.Contains(strings.ToLower(string(body)), "attach") {
		reason := strings.TrimSpace(string(body))
		var parsed struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(body, &parsed) == nil && parsed.Error != "" {
			reason = parsed.Error
		}
		return &ntfyAttachmentRefused{reason: fmt.Sprintf("the server answered %d: %s", response.StatusCode, reason)}
	}
	return fmt.Errorf("ntfy returned status %d", response.StatusCode)
}

// watchChartFilename is a title as a file name: "Pressure, last 12 hours" is
// pressure-last-12-hours.png.
func watchChartFilename(title string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(title) {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			b.WriteRune(r)
			dash = false
		} else if !dash && b.Len() > 0 {
			b.WriteByte('-')
			dash = true
		}
	}
	name := strings.TrimRight(b.String(), "-")
	if name == "" {
		name = "chart"
	}
	return name + ".png"
}

// ntfyPriorityFor maps SignalK severities onto ntfy's 1-5 scale. An emergency
// gets max priority so it bypasses the phone's do-not-disturb.
func ntfyPriorityFor(msg notificationMessage) string {
	if msg.isWatchReport() {
		// A report is never urgent: it waits for the operator, an alarm does not.
		return "3"
	}
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
	if msg.isWatchReport() {
		return "clipboard"
	}
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
	var payload any = msg
	if msg.isWatchReport() {
		if msg.Watch == nil {
			return fmt.Errorf("watch report has no report to send")
		}
		withPNGs := *msg.Watch
		withPNGs.Charts = webhookCharts(msg)
		payload = &withPNGs
	} else if len(msg.Charts) > 0 {
		withPNGs := msg
		withPNGs.Charts = webhookCharts(msg)
		payload = withPNGs
	}
	body, err := json.Marshal(payload)
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

// webhookCharts is the message's charts with each picture drawn and encoded, or
// its problem when it could not be.
func webhookCharts(msg notificationMessage) []watchChart {
	rendered, pngs := msg.renderedCharts("webhook", 0)
	charts := append([]watchChart(nil), rendered...)
	for i := range charts {
		if pngs[i] != nil {
			charts[i].PNGBase64 = base64.StdEncoding.EncodeToString(pngs[i])
		}
	}
	return charts
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

func buildWatchEmail(config smtpConfig, msg notificationMessage) []byte {
	if len(msg.Watch.Charts) > 0 {
		return buildWatchEmailWithCharts(config, msg)
	}
	return plainTextEmail(config, msg.title(), watchEmailText(*msg.Watch))
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
	if msg.isWatchReport() {
		return fmt.Errorf("a watch report is never published to SignalK: it would appear on the bus as an alarm")
	}
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
		Unit:    event.Status.Unit,
		Vessel:  vessel,
		At:      now,
	}
}

// The signalk transport publishes as a producer on the delta stream
// (signalk_publish.go). It used to write over REST, which reached no endpoint
// the server serves — notifications have no PUT handler — so it had never once
// delivered anything. See the ADR 0038 addendum.
