package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
)

const wasmTLSTimeoutErr = `weather provider "open-meteo" unavailable: plugin "open-meteo": fetch_forecast call failed: Get "https://api.open-meteo.com/v1/forecast?latitude=-36.8&longitude=174.7&current=temperature_2m,wind_speed_10m&hourly=wind_speed_10m": net/http: TLS handshake timeout (recovered by wazero)
wasm stack trace:
	extism:host/env.http_request(i64,i64) i64
	main.main.fetchForecast()
	/tmp/go/pkg/mod/github.com/extism/go-pdk@v1.1.3/http.go:12`

func TestFetchFailureReason(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want string
	}{
		{"nil", nil, ""},
		{"wasm TLS timeout with trace", errors.New(wasmTLSTimeoutErr), "the forecast did not arrive in time"},
		{"client timeout", errors.New(`Get "https://x.example/a?b=c": net/http: request canceled (Client.Timeout exceeded while awaiting headers)`), "the forecast did not arrive in time"},
		{"context deadline", fmt.Errorf("wrapped: %w", context.DeadlineExceeded), "the forecast did not arrive in time"},
		{"deadline text", errors.New("context deadline exceeded"), "the forecast did not arrive in time"},
		{"refused", errors.New(`Get "https://api.open-meteo.com/v1/forecast?x=1": dial tcp 1.2.3.4:443: connect: connection refused`), "the weather service could not be reached"},
		{"no such host", errors.New(`dial tcp: lookup api.open-meteo.com: no such host`), "the weather service could not be reached"},
		{"unreachable", errors.New(`connect: network is unreachable`), "the weather service could not be reached"},
		{"status wins over body text", errors.New(`unexpected status 503: <html>connection refused</html>`), "the weather service answered with an error (503)"},
		{"timed out text", errors.New(`weather provider "open-meteo": timed out`), "the forecast did not arrive in time"},
		{"timeout awaiting text", errors.New(`timeout awaiting response headers`), "the forecast did not arrive in time"},
		{"closed by cancel", errors.New("module closed with context canceled"), "the request was cancelled"},
		{"url error redirects", &url.Error{Op: "Get", URL: "https://x.example", Err: errors.New("stopped after 10 redirects")}, "the weather service sent an error"},
		{"dns error type", &net.DNSError{Err: "weird", Name: "x.example"}, "the weather service could not be reached"},
		{"status in plugin text with url", errors.New(`plugin "open-meteo": fetch_forecast call failed: Get "https://api.open-meteo.com/v1/forecast?a=b": unexpected status 500`), "the weather service answered with an error (500)"},
		{"plans wrapping", errors.New(`forecast at -36.80, 174.70: weather provider "open-meteo": plugin "open-meteo": fetch_forecast call failed: Get "https://api.open-meteo.com/v1/forecast?a=b": unexpected status 500 (recovered by wazero)`), "the weather service answered with an error (500)"},
		{"plugin panic", errors.New(`plugin "open-meteo" panicked calling fetch_forecast: boom`), "the weather plugin failed"},
		{"cancelled", fmt.Errorf("x: %w", context.Canceled), "the request was cancelled"},
		{"wasm trap", errors.New("wasm error: unreachable\nwasm stack trace:"), "the weather plugin failed"},
		{"exit code", errors.New("module closed with exit_code(1)"), "the weather plugin failed"},
		{"upstream quota", errors.New("upstream API call failed: quota exceeded"), "the weather service sent an error"},
		{"json parse", errors.New(`plugin "open-meteo": unparseable fetch_forecast JSON: invalid character '<' looking for beginning of value`), "the weather service sent an error"},
		{"conn reset", errors.New("read tcp 1.2.3.4:5->6.7.8.9:443: connection reset by peer"), "the weather service could not be reached"},
		{"broken pipe", errors.New("write tcp: broken pipe"), "the weather service could not be reached"},
		{"eof", errors.New(`Get "https://x.example/a": EOF`), "the weather service could not be reached"},
		{"x509", errors.New("tls: failed to verify certificate: x509: certificate signed by unknown authority"), "the weather service could not be reached"},
		{"net error type", &net.OpError{Op: "dial", Err: errors.New("weird")}, "the weather service could not be reached"},
		{"status 400 with body", errors.New(`plugin "open-meteo": fetch_forecast call failed: unexpected status 400: {"error":true}`), "the weather service answered with an error (400)"},
		{"plans status 500", errors.New(`unexpected status 500 (recovered by wazero)`), "the weather service answered with an error (500)"},
	}
	fixed := map[string]bool{
		"the request was cancelled":                        true,
		"the forecast did not arrive in time":              true,
		"the weather service could not be reached":         true,
		"the weather plugin failed":                        true,
		"the weather service sent an error":                true,
		"the weather service answered with an error (400)": true,
		"the weather service answered with an error (500)": true,
		"the weather service answered with an error (503)": true,
		"": true,
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := fetchFailureReason(tc.err)
			if got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
			if !fixed[got] {
				t.Fatalf("not in the fixed set: %q", got)
			}
		})
	}
}

func TestWeatherHandlers_FailureTextIsShortAndPlain(t *testing.T) {
	withCleanWeatherProviderRegistry(t)
	registerWeatherProvider(&stubWeatherProvider{id: "w", name: "W", err: errors.New(wasmTLSTimeoutErr)})
	t.Setenv("SETTINGS_FILE", writeWeatherSettings(t, "w", "http://127.0.0.1:1"))

	for name, h := range map[string]echo.HandlerFunc{"today": weatherToday, "forecast": weatherForecast} {
		rec := serveForecastPoint(t, h, "?lat=-27.4&lon=150")
		if rec.Code != http.StatusBadGateway {
			t.Fatalf("%s: expected 502, got %d: %s", name, rec.Code, rec.Body.String())
		}
		want := `{"error":"the forecast did not arrive in time"}`
		if got := strings.TrimSpace(rec.Body.String()); got != want {
			t.Errorf("%s: got %s, want %s", name, got, want)
		}
	}
}
