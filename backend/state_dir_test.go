package main

import (
	"path/filepath"
	"testing"
)

// cacheFilePath is the single choke point every runtime state path goes
// through (routes.json, secrets.sqlite, the caches, ...). Rooting it at
// HELMCENTRAL_STATE_DIR is what lets an isolated stack — E2E runs in
// particular — keep its writes out of the developer's working tree without
// having to enumerate a separate env var per file.

func TestCacheFilePathDefaultsToRelativeFallback(t *testing.T) {
	t.Setenv("HELMCENTRAL_STATE_DIR", "")

	if got := cacheFilePath("ROUTES_FILE", "data/routes.json"); got != "data/routes.json" {
		t.Fatalf("expected bare fallback when no state dir is set, got %q", got)
	}
}

func TestCacheFilePathRootsFallbackAtStateDir(t *testing.T) {
	stateDir := t.TempDir()
	t.Setenv("HELMCENTRAL_STATE_DIR", stateDir)

	want := filepath.Join(stateDir, "data/routes.json")
	if got := cacheFilePath("ROUTES_FILE", "data/routes.json"); got != want {
		t.Fatalf("expected fallback rooted at state dir\n want: %q\n  got: %q", want, got)
	}
}

func TestCacheFilePathStateDirCoversEveryStatePath(t *testing.T) {
	stateDir := t.TempDir()
	t.Setenv("HELMCENTRAL_STATE_DIR", stateDir)

	// A representative sweep rather than an exhaustive list: the point is that
	// callers get isolation for free, so a state path added later needs no
	// change here or in the compose config.
	for _, fallback := range []string{
		"cache/anchor_watch.json",
		"data/dashboard-pages.json",
		"data/secrets.sqlite",
		"data/secrets.key",
		"data/sat-charts",
		"data/tile-cache.sqlite",
	} {
		want := filepath.Join(stateDir, fallback)
		if got := cacheFilePath("", fallback); got != want {
			t.Errorf("fallback %q not rooted at state dir\n want: %q\n  got: %q", fallback, want, got)
		}
	}
}

func TestCacheFilePathExplicitEnvOverrideWinsOverStateDir(t *testing.T) {
	t.Setenv("HELMCENTRAL_STATE_DIR", t.TempDir())
	t.Setenv("ROUTES_FILE", "/var/lib/helmcentral/routes.json")

	// An operator who names an exact path means it; the state dir is only a
	// root for the *defaults*.
	if got := cacheFilePath("ROUTES_FILE", "data/routes.json"); got != "/var/lib/helmcentral/routes.json" {
		t.Fatalf("explicit override should win over state dir, got %q", got)
	}
}

func TestCacheFilePathLeavesAbsoluteFallbackAlone(t *testing.T) {
	t.Setenv("HELMCENTRAL_STATE_DIR", t.TempDir())

	if got := cacheFilePath("", "/absolute/path.json"); got != "/absolute/path.json" {
		t.Fatalf("absolute fallback should not be re-rooted, got %q", got)
	}
}
