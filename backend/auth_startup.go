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
// be reached, and failing fast if it answers that security is off, since "Helmcentral requires login" against a
// server that has no login to require is an unsatisfiable combination that
// must not boot into a half-state (docs/adr/0040).
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

// signalKUnreachableError marks a transport failure: SignalK could not be
// reached at all, as opposed to answering with something unusable.
type signalKUnreachableError struct{ err error }

func (e *signalKUnreachableError) Error() string { return e.err.Error() }
func (e *signalKUnreachableError) Unwrap() error { return e.err }

// checkAuthModeAtStartupWith is checkAuthModeAtStartup with the probe and the
// retry interval injected, so tests need neither a server nor a real wait.
func checkAuthModeAtStartupWith(settingsPath string, probe func(signalkURL string) (bool, error), retryInterval time.Duration) (mode string, err error) {
	mode = resolveAuthMode(settingsPath)
	if !validAuthModes[mode] {
		return mode, fmt.Errorf(
			"invalid auth.mode %q in settings.yaml — must be %q or %q (see docs/adr/0040-signalk-delegated-authentication.md)",
			mode, authModeSignalK, authModeNone,
		)
	}

	if mode != authModeSignalK {
		return mode, nil
	}

	address, port, loadErr := loadSignalKSettings(settingsPath)
	if loadErr != nil {
		address = defaultSignalKAddress
		port = defaultSignalKPort
	}
	signalkURL := buildSignalKURL(address, port)

	// An unreachable SignalK is waited on, not fatal: after a power cut it is
	// usually still starting. Nothing proceeds as if login worked; every
	// other outcome is judged on the first answer.
	waited := false
	var enabled bool
	for {
		var probeErr error
		enabled, probeErr = probe(signalkURL)
		if probeErr == nil {
			break
		}
		var unreachable *signalKUnreachableError
		if !errors.As(probeErr, &unreachable) {
			return mode, fmt.Errorf("auth.mode is %q but SignalK's security status could not be checked at %s: %w", authModeSignalK, signalkURL, probeErr)
		}
		log.Printf("auth: SignalK at %s is not answering (%v); login can't start until it does, retrying in %s", signalkURL, probeErr, retryInterval)
		waited = true
		time.Sleep(retryInterval)
	}
	if waited {
		log.Printf("auth: SignalK at %s is answering after waiting for it", signalkURL)
	}
	if !enabled {
		return mode, fmt.Errorf(
			"auth.mode is %q but SignalK at %s reports security is disabled — this combination is unsatisfiable. "+
				"Enable security on the SignalK server (Server -> Security), or set auth.mode: none in settings.yaml "+
				"to run Helmcentral without authentication",
			authModeSignalK, signalkURL,
		)
	}

	return mode, nil
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
		return false, &signalKUnreachableError{err: fmt.Errorf("could not reach signalk at %s: %w", signalkURL, err)}
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)

	return resp.StatusCode != http.StatusNotFound, nil
}
