package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"
)

// validAuthModes are the only accepted auth.mode values (plan §5). Anything
// else is a misconfiguration and must not boot into a guessed state.
var validAuthModes = map[string]bool{
	authModeNone:    true,
	authModeSignalK: true,
}

// checkAuthModeAtStartup resolves and validates auth.mode, and for
// mode:signalk probes SignalK's security status — waiting while SignalK can't
// be reached, and failing fast if it answers that security is off, since
// "Helmcentral requires login" against a server that has no login to require
// is an unsatisfiable combination that must not boot into a half-state
// (docs/adr/0040).
//
// This function never exits the process itself — main() is the only caller
// that turns a non-nil error into log.Fatalf, exactly the pattern every
// other fail-fast startup check in this codebase already follows (see
// secrets_store.go, tile_cache.go). Keeping the decision in a plain
// error-returning function is what makes it unit-testable without exec'ing
// a subprocess.
func checkAuthModeAtStartup(settingsPath string) (mode string, err error) {
	return checkAuthModeAtStartupWith(settingsPath, probeSignalKSecurityEnabled, signalKStartupRetryInterval)
}

// signalKStartupRetryInterval is how long startup waits between checks while
// SignalK can't be reached. There is no cap: SignalK is a required source.
const signalKStartupRetryInterval = 5 * time.Second

// signalKUnreachableError marks a request that got no HTTP answer at all, for
// any reason; its text is the underlying cause.
type signalKUnreachableError struct{ err error }

func (e *signalKUnreachableError) Error() string { return e.err.Error() }
func (e *signalKUnreachableError) Unwrap() error { return e.err }

// checkAuthModeAtStartupWith is checkAuthModeAtStartup with the probe and the
// retry interval injected, so tests need neither a server nor a real wait.
//
// settings.yaml is read again on every try, so a corrected SignalK address, or
// auth.mode changed away from signalk, takes effect while startup waits. A
// file that cannot be read at the first try has no mode to know and counts as
// mode none, as it always did; a file that becomes unreadable during a
// signalk-mode wait refuses startup, and is never replaced with the default
// SignalK address. The default applies only when the signalk section is absent
// from a file that reads fine.
func checkAuthModeAtStartupWith(settingsPath string, probe func(signalkURL string) (bool, error), retryInterval time.Duration) (mode string, err error) {
	return checkAuthModeAtStartupWaiting(settingsPath, probe, startupWait{
		retry: retryInterval, summary: signalKStartupSummaryInterval, now: time.Now, sleep: time.Sleep,
	})
}

// signalKStartupSummaryInterval is how often the wait is logged after the first
// failed try, so a long wait leaves a handful of lines, not thousands.
const signalKStartupSummaryInterval = time.Minute

// startupWait is the timing of the wait: the pause between tries, the interval
// between log summaries, and the clock, injectable so tests need no real time.
type startupWait struct {
	retry, summary time.Duration
	now            func() time.Time
	sleep          func(time.Duration)
}

func checkAuthModeAtStartupWaiting(settingsPath string, probe func(signalkURL string) (bool, error), w startupWait) (mode string, err error) {
	tries := 0
	var lastSummary time.Time
	sawSignalK := false
	for {
		settings, readErr := readSettings(settingsPath)
		if readErr != nil {
			// With no readable file there is no mode to know, and startup treats
			// that as none, as it always has. Only a signalk-mode wait that has
			// lost its file refuses.
			if !sawSignalK {
				return authModeNone, nil
			}
			return authModeSignalK, fmt.Errorf("could not read %s to check auth.mode: %w", settingsPath, readErr)
		}
		mode = authModeFromSettings(settings)
		if !validAuthModes[mode] {
			return mode, fmt.Errorf(
				"invalid auth.mode %q in settings.yaml — must be %q or %q (see docs/adr/0040-signalk-delegated-authentication.md)",
				mode, authModeSignalK, authModeNone,
			)
		}
		if mode != authModeSignalK {
			return mode, nil
		}

		sawSignalK = true
		address, port := signalKAddressFromSettings(settings)
		signalkURL := buildSignalKURL(address, port)

		// A SignalK that gives no HTTP answer is waited on, not fatal: after a
		// power cut it is usually still starting. Nothing proceeds as if login
		// worked; any HTTP answer is judged at once.
		enabled, probeErr := probe(signalkURL)
		if probeErr != nil {
			var unreachable *signalKUnreachableError
			if !errors.As(probeErr, &unreachable) {
				return mode, fmt.Errorf("auth.mode is %q but SignalK's security status could not be checked at %s: %w", authModeSignalK, signalkURL, probeErr)
			}
			tries++
			now := w.now()
			if tries == 1 {
				log.Printf("auth: SignalK at %s is not answering (%v); login can't start until SignalK answers, trying again %s after each failed try", signalkURL, probeErr, w.retry)
				lastSummary = now
			} else if now.Sub(lastSummary) >= w.summary {
				log.Printf("auth: still waiting for SignalK at %s after %d tries; latest error: %v", signalkURL, tries, probeErr)
				lastSummary = now
			}
			w.sleep(w.retry)
			continue
		}
		if tries > 0 {
			log.Printf("auth: SignalK at %s is answering after %d failed tries", signalkURL, tries)
		}
		return mode, securityEnabledError(signalkURL, enabled)
	}
}

// securityEnabledError is nil when SignalK reports security on, and the
// refusal to start when it reports security off.
func securityEnabledError(signalkURL string, enabled bool) error {
	if !enabled {
		return fmt.Errorf(
			"auth.mode is %q but SignalK at %s reports security is disabled — this combination is unsatisfiable. "+
				"Enable security on the SignalK server (Server -> Security), or set auth.mode: none in settings.yaml "+
				"to run Helmcentral without authentication",
			authModeSignalK, signalkURL,
		)
	}
	return nil
}

// probeSignalKSecurityEnabled reports whether SignalK's security subsystem
// is enabled by attempting POST {sk}/signalk/v1/auth/login with a
// deliberately invalid credential pair and inspecting the response.
//
// This is NOT a documented SignalK API and was not verified against a live
// server with security switched off (docs/adr/0040 records this gap
// explicitly, matching ADR 0041's precedent for stating an unverified
// assumption rather than hiding it). The reasoning it rests on:
// signalk-server only wires the /signalk/v1/auth/login route when a
// security strategy is active, so a 404 (route not found at all) is read as
// "security disabled", and any other response — including a rejection of
// the deliberately bad credentials — means the login route exists and is
// therefore read as "security enabled".
func probeSignalKSecurityEnabled(signalkURL string) (bool, error) {
	url := strings.TrimRight(signalkURL, "/") + "/signalk/v1/auth/login"
	body, err := json.Marshal(map[string]string{"username": "", "password": ""})
	if err != nil {
		return false, fmt.Errorf("signalk security probe: encode request: %w", err)
	}

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		// No HTTP answer, whatever the cause: startup waits on it.
		return false, &signalKUnreachableError{err: fmt.Errorf("could not reach signalk at %s: %w", signalkURL, err)}
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)

	return resp.StatusCode != http.StatusNotFound, nil
}
