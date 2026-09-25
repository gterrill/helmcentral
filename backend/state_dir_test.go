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
	// change here or in the compose config. The anchor watch and its
	// placemarks are covered by their own dedicated tests below instead of
	// here (TestAnchorWatchFilePathDefaultsToDataDir,
	// TestAnchorPlacemarksFilePathDefaultsToDataDir), which pin the real
	// anchorWatchFilePath()/anchorPlacemarksFilePath() functions rather than a
	// literal fallback string handed to cacheFilePath.
	for _, fallback := range []string{
		"data/dashboard-pages.json",
		"data/secrets.sqlite",
		"data/secrets.key",
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

// In the Docker deployment only /app/data is a persisted volume; /app/cache
// lives in the container layer and is gone on the next recreate. The anchor
// watch and its placemarks used to default under cache/, so every drop was
// lost on redeploy even though every other piece of operator state already
// defaulted under data/. These pin the real fallbacks (not a representative
// literal passed into cacheFilePath, but the production functions
// themselves) to data/, with no env override and no state dir set.
func TestAnchorWatchFilePathDefaultsToDataDir(t *testing.T) {
	t.Setenv("HELMCENTRAL_STATE_DIR", "")
	t.Setenv("ANCHOR_WATCH_FILE", "")

	if got := anchorWatchFilePath(); got != "data/anchor_watch.json" {
		t.Fatalf("expected the anchor watch default to live under data/, got %q", got)
	}
}

func TestAnchorPlacemarksFilePathDefaultsToDataDir(t *testing.T) {
	t.Setenv("HELMCENTRAL_STATE_DIR", "")
	t.Setenv("ANCHOR_PLACEMARKS_FILE", "")

	if got := anchorPlacemarksFilePath(); got != "data/anchor_placemarks.json" {
		t.Fatalf("expected the anchor placemarks default to live under data/, got %q", got)
	}
}
