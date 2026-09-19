package main

import (
	"os"
	"sync"
	"testing"
)

// resetInfluxClientCache leaves globalInfluxClient exactly as a fresh
// process would have it, so one test's client never leaks into the next.
func resetInfluxClientCache(t *testing.T) {
	t.Helper()
	globalInfluxClient.reset()
	t.Cleanup(func() { globalInfluxClient.reset() })
}

func TestNewInfluxClient_ReusesSameClientAcrossCalls(t *testing.T) {
	resetInfluxClientCache(t)
	path := writeInfluxSettingsFixture(t, "influxdb:\n  enabled: true\n  url: http://127.0.0.1:1\n  org: myorg\n  bucket: mybucket\n")
	t.Setenv("SETTINGS_FILE", path)
	withSeededSecretsStore(t, map[string]string{"INFLUXDB_TOKEN": "sometoken"})

	first, _, _, ok := newInfluxClient()
	if !ok {
		t.Fatalf("expected a configured client")
	}
	second, _, _, ok := newInfluxClient()
	if !ok {
		t.Fatalf("expected a configured client")
	}
	if first != second {
		t.Fatalf("expected the same shared client across calls with unchanged settings, got two different clients")
	}
}

// The URL differs in length (":1" -> ":10"), not just content, so this does
// not depend on the settings cache (1a) also noticing a same-size rewrite
// within its mtime resolution -- a genuinely different config must always
// force a rebuild.
func TestNewInfluxClient_RebuildsWhenSettingsChange(t *testing.T) {
	resetInfluxClientCache(t)
	path := writeInfluxSettingsFixture(t, "influxdb:\n  enabled: true\n  url: http://127.0.0.1:1\n  org: myorg\n  bucket: mybucket\n")
	t.Setenv("SETTINGS_FILE", path)
	withSeededSecretsStore(t, map[string]string{"INFLUXDB_TOKEN": "sometoken"})

	first, _, _, ok := newInfluxClient()
	if !ok {
		t.Fatalf("expected a configured client")
	}

	if err := os.WriteFile(path, []byte("influxdb:\n  enabled: true\n  url: http://127.0.0.1:10\n  org: myorg\n  bucket: mybucket\n"), 0o644); err != nil {
		t.Fatalf("rewrite settings: %v", err)
	}

	second, _, _, ok := newInfluxClient()
	if !ok {
		t.Fatalf("expected a configured client")
	}
	if first == second {
		t.Fatalf("expected a rebuilt client once the Influx url changed, got the same cached client")
	}
}

// Disabling Influx must release the cached client rather than leave it
// sitting open indefinitely against an abandoned config.
func TestNewInfluxClient_UnconfiguredReleasesCachedClient(t *testing.T) {
	resetInfluxClientCache(t)
	path := writeInfluxSettingsFixture(t, "influxdb:\n  enabled: true\n  url: http://127.0.0.1:1\n  org: myorg\n  bucket: mybucket\n")
	t.Setenv("SETTINGS_FILE", path)
	withSeededSecretsStore(t, map[string]string{"INFLUXDB_TOKEN": "sometoken"})

	if _, _, _, ok := newInfluxClient(); !ok {
		t.Fatalf("expected a configured client")
	}

	if err := os.WriteFile(path, []byte("influxdb:\n  enabled: false\n"), 0o644); err != nil {
		t.Fatalf("rewrite settings: %v", err)
	}

	if _, _, _, ok := newInfluxClient(); ok {
		t.Fatalf("expected not configured once influxdb.enabled is false")
	}

	globalInfluxClient.mu.Lock()
	stillCached := globalInfluxClient.client != nil
	globalInfluxClient.mu.Unlock()
	if stillCached {
		t.Fatalf("expected the cached client to be released once Influx became unconfigured")
	}
}

// Every /api/vessel-state build, every stream client and every solar-state
// build calls newInfluxClient concurrently in production; this exercises
// that under -race with settings changing mid-flight (the worst case for
// the client-swap path in clientFor/reset).
func TestNewInfluxClient_ConcurrentCallsAreRaceFree(t *testing.T) {
	resetInfluxClientCache(t)
	path := writeInfluxSettingsFixture(t, "influxdb:\n  enabled: true\n  url: http://127.0.0.1:1\n  org: myorg\n  bucket: mybucket\n")
	t.Setenv("SETTINGS_FILE", path)
	withSeededSecretsStore(t, map[string]string{"INFLUXDB_TOKEN": "sometoken"})

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			if n%3 == 0 {
				if err := os.WriteFile(path, []byte("influxdb:\n  enabled: true\n  url: http://127.0.0.1:1\n  org: myorg\n  bucket: mybucket\n"), 0o644); err != nil {
					t.Errorf("rewrite settings: %v", err)
				}
			}
			newInfluxClient()
		}(i)
	}
	wg.Wait()
}
