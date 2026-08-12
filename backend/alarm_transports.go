package main

import (
	"encoding/json"
	"fmt"
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
		if !strings.HasPrefix(config.Webhook.URL, "http://") && !strings.HasPrefix(config.Webhook.URL, "https://") {
			return fmt.Errorf("webhook url must be http or https")
		}
	}

	return nil
}
