package main

import (
	"net/http"
	"testing"
)

func TestUpdateSettings_MinuteBucketRoundTrips(t *testing.T) {
	withTestSecretsStore(t)
	srv := trustedSignalKPayloadServer(t, -21.1, 149.2)
	defer srv.Close()
	host, port := hostPort(t, srv.URL)
	settingsPath := writeTestSettings(t, host, port)

	code, body := postSettings(t, settingsPath, func(p *settingsPayload) {
		p.Influxdb.Enabled = true
		p.Influxdb.URL = "http://192.168.50.20:8086"
		p.Influxdb.Org = "myorg"
		p.Influxdb.Bucket = "mybucket"
		p.Influxdb.MinuteBucket = "  my_1m  "
	})
	if code != http.StatusOK {
		t.Fatalf("expected 200, got %d (body %v)", code, body)
	}
	saved, err := readSettings(settingsPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := buildSettingsPayload(saved).Influxdb.MinuteBucket; got != "my_1m" {
		t.Fatalf("minute bucket after a save = %q, want my_1m trimmed", got)
	}
	if cfg, _, err := loadInfluxConfig(settingsPath); err != nil || cfg.MinuteBucket != "my_1m" {
		t.Fatalf("loadInfluxConfig = %+v, %v", cfg, err)
	}
}

// The main bucket is a Flux string value too, and is refused for $ and { the
// same way. The organization never enters a Flux literal and is not refused.
func TestUpdateSettings_MainBucketRefusesFluxInterpolation(t *testing.T) {
	withTestSecretsStore(t)
	srv := trustedSignalKPayloadServer(t, -21.1, 149.2)
	defer srv.Close()
	host, port := hostPort(t, srv.URL)
	settingsPath := writeTestSettings(t, host, port)
	cases := map[string]func(p *settingsPayload){
		"influxdb.bucket": func(p *settingsPayload) { p.Influxdb.Bucket = "b${r._field}" },
	}
	for field, mutate := range cases {
		code, body := postSettings(t, settingsPath, func(p *settingsPayload) {
			p.Influxdb.Enabled = true
			p.Influxdb.URL = "http://192.168.50.20:8086"
			p.Influxdb.Org = "myorg"
			p.Influxdb.Bucket = "mybucket"
			mutate(p)
		})
		if code == http.StatusOK || body["field"] != field {
			t.Fatalf("%s: code %d body %v", field, code, body)
		}
	}
}

func TestUpdateSettings_MinuteBucketMayBeBlank(t *testing.T) {
	withTestSecretsStore(t)
	srv := trustedSignalKPayloadServer(t, -21.1, 149.2)
	defer srv.Close()
	host, port := hostPort(t, srv.URL)
	settingsPath := writeTestSettings(t, host, port)
	code, body := postSettings(t, settingsPath, func(p *settingsPayload) {
		p.Influxdb.Enabled = true
		p.Influxdb.URL = "http://192.168.50.20:8086"
		p.Influxdb.Org = "myorg"
		p.Influxdb.Bucket = "mybucket"
	})
	if code != http.StatusOK {
		t.Fatalf("expected 200, got %d (body %v)", code, body)
	}
	saved, _ := readSettings(settingsPath)
	if got := buildSettingsPayload(saved).Influxdb.MinuteBucket; got != "" {
		t.Fatalf("minute bucket = %q, want blank", got)
	}
}

// Flux evaluates ${...} inside a string literal, so a bucket name carrying it
// is refused when it is saved, with the field named.
func TestUpdateSettings_MinuteBucketRefusesFluxInterpolation(t *testing.T) {
	withTestSecretsStore(t)
	srv := trustedSignalKPayloadServer(t, -21.1, 149.2)
	defer srv.Close()
	host, port := hostPort(t, srv.URL)
	settingsPath := writeTestSettings(t, host, port)
	for _, bad := range []string{"b${r._field}", "b{x"} {
		code, body := postSettings(t, settingsPath, func(p *settingsPayload) {
			p.Influxdb.Enabled = true
			p.Influxdb.URL = "http://192.168.50.20:8086"
			p.Influxdb.Org = "myorg"
			p.Influxdb.Bucket = "mybucket"
			p.Influxdb.MinuteBucket = bad
		})
		if code == http.StatusOK {
			t.Fatalf("%q was saved", bad)
		}
		if body["field"] != "influxdb.minute_bucket" {
			t.Fatalf("%q: field = %v, want influxdb.minute_bucket", bad, body["field"])
		}
	}
}
