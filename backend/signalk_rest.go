package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// ── Low-level SignalK REST helper (raw body, not {"value":...}-wrapped) ─────

// signalkRequestJSON performs an HTTP request against a SignalK endpoint with a
// raw JSON body (no {"value":...} envelope — unlike putSignalKValue, which is
// for plain SignalK data-model writes). Returns the response status and raw
// body so callers can propagate SignalK's exact error text.
func signalkRequestJSON(signalkURL, path, method string, body any, token string) (int, []byte, error) {
	url := strings.TrimRight(signalkURL, "/") + "/" + strings.TrimLeft(path, "/")

	var reqBody io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			return 0, nil, err
		}
		reqBody = bytes.NewReader(payload)
	}

	client := &http.Client{Timeout: 5 * time.Second}
	req, err := http.NewRequest(method, url, reqBody)
	if err != nil {
		return 0, nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	response, err := client.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer response.Body.Close()

	respBody, _ := io.ReadAll(response.Body)
	return response.StatusCode, respBody, nil
}

// signalkRequestJSONWithAuthBody wraps signalkRequestJSON with the same
// acquire-token/retry mechanics as generatorPut, but retries only on an
// auth-looking failure (401/403, or a transport error) rather than on every
// non-2xx, so a genuine 4xx/5xx upstream error surfaces immediately instead of
// being retried pointlessly (and fires the retry at most once). Unlike
// signalkRequestJSONWithAuth below, it returns SignalK's raw status and body
// on every outcome (not just errors) — needed by callers such as the
// autopilot GET proxies, which must relay SignalK's exact response rather
// than only knowing whether the call succeeded.
func signalkRequestJSONWithAuthBody(signalkURL, settingsPath, path, method string, body any) (int, []byte, error) {
	username, password, err := loadSignalKCredentials(settingsPath)
	if err != nil {
		return 0, nil, fmt.Errorf("could not read the SignalK credentials: %w", err)
	}
	token, err := acquireSignalKToken(signalkURL, username, password)
	if err != nil {
		return 0, nil, err
	}

	status, respBody, reqErr := signalkRequestJSON(signalkURL, path, method, body, token)

	needsRetry := reqErr != nil || status == http.StatusUnauthorized || status == http.StatusForbidden
	if needsRetry && token != "" {
		invalidateSignalKToken()
		token, err = acquireSignalKToken(signalkURL, username, password)
		if err != nil {
			return 0, nil, err
		}
		status, respBody, reqErr = signalkRequestJSON(signalkURL, path, method, body, token)
	}

	if reqErr != nil {
		return 0, nil, reqErr
	}
	return status, respBody, nil
}

// signalkRequestJSONWithAuth is signalkRequestJSONWithAuthBody for callers
// that only care whether the request succeeded (e.g. route activation PUTs),
// collapsing any non-2xx result into an error carrying SignalK's raw
// status+body verbatim.
func signalkRequestJSONWithAuth(signalkURL, settingsPath, path, method string, body any) error {
	status, respBody, err := signalkRequestJSONWithAuthBody(signalkURL, settingsPath, path, method, body)
	if err != nil {
		return err
	}
	if status < 200 || status >= 300 {
		return fmt.Errorf("signalk returned status %d: %s", status, string(respBody))
	}
	return nil
}

func resolveSignalKURL() (string, string) {
	settingsPath := getEnv("SETTINGS_FILE", "../settings.yaml")
	address, port, err := loadSignalKSettings(settingsPath)
	if err != nil {
		address = defaultSignalKAddress
		port = defaultSignalKPort
	}
	return buildSignalKURL(address, port), settingsPath
}
