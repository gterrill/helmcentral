package main

import (
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"
	"sync"
)

// Transport configuration lives in its own file for the same reason alarm rules
// do: POST /api/settings rebuilds its sections wholesale and would silently
// destroy a new top-level key. Secrets are never stored here — SMTP_PASSWORD and
// NTFY_TOKEN come from the encrypted secrets store at send time.

const (
	transportNtfy    = "ntfy"
	transportSMTP    = "smtp"
	transportWebhook = "webhook"
	transportSignalK = "signalk"
	transportWebPush = "webpush"
)

const defaultNtfyServer = "https://ntfy.sh"

type ntfyConfig struct {
	Enabled bool   `json:"enabled"`
	Server  string `json:"server"`
	Topic   string `json:"topic"`
}

type smtpConfig struct {
	Enabled  bool     `json:"enabled"`
	Host     string   `json:"host"`
	Port     int      `json:"port"`
	Username string   `json:"username"`
	From     string   `json:"from"`
	To       []string `json:"to"`
}

type webhookConfig struct {
	Enabled bool   `json:"enabled"`
	URL     string `json:"url"`
}

// signalKNotifyConfig publishes Helmcentral's own alarms back onto the bus, so
// a physical buzzer plugin or an MFD reacts without knowing Helmcentral exists.
type signalKNotifyConfig struct {
	Enabled bool `json:"enabled"`
}

// webPushConfig carries no fields beyond the toggle, deliberately. The VAPID
// keypair is generated into the secrets store and the registered devices live
// in their own SQLite file, so there is nothing for an operator to type and
// nothing for validateAlarmTransports to check.
type webPushConfig struct {
	Enabled bool `json:"enabled"`
}

// watchdogConfig covers the failures the alarm rules cannot see: the data
// source dying, and the boat itself going off.
type watchdogConfig struct {
	// StreamSilenceSeconds is how long the delta stream may be quiet before it
	// is treated as an outage. Zero uses the default.
	StreamSilenceSeconds int `json:"stream_silence_seconds"`
	// HeartbeatMinutes is how often to send a "still alive" notification, so
	// that its absence is itself the alarm. Zero disables it.
	HeartbeatMinutes int `json:"heartbeat_minutes"`
}

type alarmTransportConfig struct {
	Ntfy     ntfyConfig          `json:"ntfy"`
	SMTP     smtpConfig          `json:"smtp"`
	Webhook  webhookConfig       `json:"webhook"`
	SignalK  signalKNotifyConfig `json:"signalk"`
	WebPush  webPushConfig       `json:"webpush"`
	Watchdog watchdogConfig      `json:"watchdog"`
}

var (
	alarmTransportsMu    sync.RWMutex
	alarmTransportsState alarmTransportConfig
)

func alarmTransportsFilePath() string {
	return cacheFilePath("ALARM_TRANSPORTS_FILE", "data/alarm-transports.json")
}

func loadAlarmTransports() error {
	data, err := os.ReadFile(alarmTransportsFilePath())
	if err != nil {
		if os.IsNotExist(err) {
			alarmTransportsMu.Lock()
			alarmTransportsState = alarmTransportConfig{}
			alarmTransportsMu.Unlock()
			return nil
		}
		return fmt.Errorf("reading alarm transports: %w", err)
	}

	var config alarmTransportConfig
	if len(data) > 0 {
		if err := json.Unmarshal(data, &config); err != nil {
			return fmt.Errorf("parsing alarm transports: %w", err)
		}
	}

	alarmTransportsMu.Lock()
	alarmTransportsState = config
	alarmTransportsMu.Unlock()
	return nil
}

func getAlarmTransports() alarmTransportConfig {
	alarmTransportsMu.RLock()
	defer alarmTransportsMu.RUnlock()
	return alarmTransportsState
}

func setAlarmTransports(config alarmTransportConfig) (alarmTransportConfig, error) {
	if err := validateAlarmTransports(&config); err != nil {
		return alarmTransportConfig{}, err
	}

	alarmTransportsMu.Lock()
	defer alarmTransportsMu.Unlock()

	previous := alarmTransportsState
	alarmTransportsState = config
	if err := writeJSONFileAtomic(alarmTransportsFilePath(), config); err != nil {
		alarmTransportsState = previous
		return alarmTransportConfig{}, err
	}
	return config, nil
}

// validateAlarmTransports normalizes in place. A transport enabled but
// misconfigured is worse than one that is off, because it is trusted to deliver
// and silently cannot.
func validateAlarmTransports(config *alarmTransportConfig) error {
	config.Ntfy.Server = strings.TrimRight(strings.TrimSpace(config.Ntfy.Server), "/")
	config.Ntfy.Topic = strings.TrimSpace(config.Ntfy.Topic)
	if config.Ntfy.Enabled {
		if config.Ntfy.Server == "" {
			config.Ntfy.Server = defaultNtfyServer
		}
		if config.Ntfy.Topic == "" {
			return fmt.Errorf("ntfy requires a topic")
		}
	}

	config.SMTP.Host = strings.TrimSpace(config.SMTP.Host)
	config.SMTP.From = strings.TrimSpace(config.SMTP.From)
	config.SMTP.Username = strings.TrimSpace(config.SMTP.Username)
	cleanTo := config.SMTP.To[:0]
	for _, address := range config.SMTP.To {
		if trimmed := strings.TrimSpace(address); trimmed != "" {
			cleanTo = append(cleanTo, trimmed)
		}
	}
	config.SMTP.To = cleanTo
	if config.SMTP.Enabled {
		if config.SMTP.Host == "" {
			return fmt.Errorf("smtp requires a host")
		}
		if config.SMTP.Port <= 0 || config.SMTP.Port > 65535 {
			return fmt.Errorf("smtp port %d is out of range (1-65535)", config.SMTP.Port)
		}
		if config.SMTP.From == "" {
			return fmt.Errorf("smtp requires a from address")
		}
		if len(config.SMTP.To) == 0 {
			return fmt.Errorf("smtp requires at least one recipient")
		}
	}

	if config.Watchdog.StreamSilenceSeconds < 0 {
		return fmt.Errorf("stream silence seconds cannot be negative")
	}
	if config.Watchdog.HeartbeatMinutes < 0 {
		return fmt.Errorf("heartbeat minutes cannot be negative")
	}

	config.Webhook.URL = strings.TrimSpace(config.Webhook.URL)
	if config.Webhook.Enabled {
		if config.Webhook.URL == "" {
			return fmt.Errorf("webhook requires a url")
		}
		if err := validateWebhookURL(config.Webhook.URL); err != nil {
			return err
		}
	}

	return nil
}

// webhookURLResolver looks up a hostname's addresses. A package variable so
// tests can supply a fixed answer instead of depending on real DNS (and, for
// the "rejects an unresolvable host" case, so the test doesn't have to wait
// out a real resolver timeout).
var webhookURLResolver = net.LookupIP

// disallowedWebhookIP reports whether ip has no legitimate use as a webhook
// destination for this container: loopback (the container's own other
// listeners) and link-local (which also covers the cloud metadata address,
// 169.254.169.254) and unspecified (0.0.0.0/::). RFC1918/ULA private
// addresses are deliberately NOT included here -- see validateWebhookURL's
// doc comment for why.
func disallowedWebhookIP(ip net.IP) bool {
	return ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified()
}

// validateWebhookURL is E-3's fix: before this existed, the only check on a
// webhook URL was its http(s) scheme, and /api/alarm-transports/test
// (alarm_service.go) reflects the upstream status code or connect error
// straight into its JSON response -- so an operator's Settings form doubled
// as an SSRF oracle that could scan the container's own loopback and the
// compose network, with an attacker-chosen JSON body delivered to whatever
// answered.
//
// Private-address decision: RFC1918 and IPv6 ULA ranges are deliberately
// ALLOWED. This project's own deployment target is a boat LAN address
// (192.168.50.240), and a webhook to a LAN-resident Home Assistant or
// Node-RED instance is exactly the kind of thing an operator legitimately
// configures here -- blocking all of RFC1918 would break that for no real
// gain, since whoever can edit Settings already has LAN access. What is
// blocked is loopback, link-local (169.254.0.0/16, which covers the cloud
// metadata address 169.254.169.254 -- meaningless on a boat with no cloud
// metadata service, but excluded anyway since it costs nothing here) and
// unspecified: none of those have a legitimate webhook use from inside this
// container, and every one of them is a classic SSRF pivot target. Because
// RFC1918 stays reachable, this is not a complete SSRF fix on its own -- see
// the oracle fix in alarm_service.go's testAlarmTransportsHandler, which
// matters more as a result.
//
// A hostname (not an IP literal) is resolved and EVERY returned address is
// checked, not just the first, so a second A/AAAA record can't slip a
// disallowed address past this. An unresolvable hostname is rejected rather
// than silently accepted, matching this function's own "misconfigured is
// worse than off" doc comment above: a webhook nobody can reach is exactly
// the failure this whole file exists to catch at save time.
//
// This only closes the redirect-based variant of this bug at the point a
// URL is entered, not at delivery time: the HTTP client that actually POSTs
// a webhook (notifyHTTPClient, alarm_notify.go) follows redirects with no
// policy of its own, so a URL that is safe when saved could still redirect
// into a disallowed range when Helmcentral no longer originates the
// request. Closing that needs a CheckRedirect policy on that client, which
// is out of scope for this phase (alarm_notify.go is deliberately
// untouched here, the same as E-1).
func validateWebhookURL(rawURL string) error {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("webhook url is invalid: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("webhook url must be http or https")
	}

	host := parsed.Hostname()
	if host == "" {
		return fmt.Errorf("webhook url has no host")
	}

	if literal := net.ParseIP(host); literal != nil {
		if disallowedWebhookIP(literal) {
			return fmt.Errorf("webhook url must not point at a loopback, link-local, or unspecified address")
		}
		return nil
	}

	addrs, err := webhookURLResolver(host)
	if err != nil {
		return fmt.Errorf("webhook host %q does not resolve: %w", host, err)
	}
	if len(addrs) == 0 {
		return fmt.Errorf("webhook host %q does not resolve to any address", host)
	}
	for _, addr := range addrs {
		if disallowedWebhookIP(addr) {
			return fmt.Errorf("webhook host %q resolves to a loopback, link-local, or unspecified address", host)
		}
	}
	return nil
}
