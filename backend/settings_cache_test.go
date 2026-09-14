package main

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func writeSettingsCacheFixture(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.yaml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write settings fixture: %v", err)
	}
	t.Cleanup(func() { invalidateSettingsCache(path) })
	return path
}

// cachedSettingsMapPointer returns the identity of the map object currently
// cached for path, so two reads can be proven to share one parse (same
// underlying map) rather than each having re-run yaml.Unmarshal (which
// would always produce a distinct map, even with identical content).
func cachedSettingsMapPointer(t *testing.T, path string) uintptr {
	t.Helper()
	settingsCacheMu.Lock()
	entry, ok := settingsCache[path]
	settingsCacheMu.Unlock()
	if !ok {
		t.Fatalf("expected a cache entry for %q after a read", path)
	}
	return reflect.ValueOf(entry.settings).Pointer()
}

func TestReadSettings_ParsesOnceAcrossRepeatedReads(t *testing.T) {
	path := writeSettingsCacheFixture(t, "boat:\n    model: Test Boat\n")

	if _, err := readSettings(path); err != nil {
		t.Fatalf("first read: %v", err)
	}
	firstParse := cachedSettingsMapPointer(t, path)

	for i := 0; i < 3; i++ {
		if _, err := readSettings(path); err != nil {
			t.Fatalf("repeated read %d: %v", i, err)
		}
	}
	secondParse := cachedSettingsMapPointer(t, path)

	if firstParse != secondParse {
		t.Fatalf("expected the same cached parse across repeated reads, got distinct map objects (reparsed)")
	}
}

// A rewrite that changes the file's size must be picked up on the very next
// read, without going through writeSettings -- readSettings is also called
// against files this backend does not itself write (e.g. an operator
// hand-editing settings.yaml over SSH).
func TestReadSettings_PicksUpExternalSizeChange(t *testing.T) {
	path := writeSettingsCacheFixture(t, "boat:\n    model: Original\n")

	first, err := readSettings(path)
	if err != nil {
		t.Fatalf("first read: %v", err)
	}
	if got := first["boat"].(map[string]any)["model"]; got != "Original" {
		t.Fatalf("expected model Original, got %v", got)
	}

	if err := os.WriteFile(path, []byte("boat:\n    model: Renamed With A Longer Value\n"), 0o644); err != nil {
		t.Fatalf("rewrite fixture: %v", err)
	}

	second, err := readSettings(path)
	if err != nil {
		t.Fatalf("second read: %v", err)
	}
	if got := second["boat"].(map[string]any)["model"]; got != "Renamed With A Longer Value" {
		t.Fatalf("expected the external rewrite to be picked up, got %v", got)
	}
}

// writeSettings must invalidate the cache even when the new content happens
// to be the exact same length as the old -- relying on mtime/size alone
// would leave this to filesystem timestamp resolution, which is exactly the
// gap invalidateSettingsCache exists to close.
func TestWriteSettings_InvalidatesCacheEvenWhenSizeIsUnchanged(t *testing.T) {
	path := writeSettingsCacheFixture(t, "boat:\n    model: AAA\n")

	first, err := readSettings(path)
	if err != nil {
		t.Fatalf("first read: %v", err)
	}
	if got := first["boat"].(map[string]any)["model"]; got != "AAA" {
		t.Fatalf("expected model AAA, got %v", got)
	}

	if err := writeSettings(path, map[string]any{"boat": map[string]any{"model": "BBB"}}); err != nil {
		t.Fatalf("write settings: %v", err)
	}

	second, err := readSettings(path)
	if err != nil {
		t.Fatalf("second read: %v", err)
	}
	if got := second["boat"].(map[string]any)["model"]; got != "BBB" {
		t.Fatalf("expected the write to be visible on the next read, got %v", got)
	}
}

// Some callers (updateSettingsHandler most notably) mutate the map
// readSettings hands back before writing it out again. That must never
// corrupt what a concurrent or subsequent caller sees, which is the whole
// reason readSettings returns a deep copy rather than the cached map.
func TestReadSettings_MutatingReturnedMapDoesNotAffectNextRead(t *testing.T) {
	path := writeSettingsCacheFixture(t, "boat:\n    model: Untouched\n")

	first, err := readSettings(path)
	if err != nil {
		t.Fatalf("first read: %v", err)
	}
	first["boat"].(map[string]any)["model"] = "Mutated By Caller"
	first["new_top_level_key"] = "should not leak"

	second, err := readSettings(path)
	if err != nil {
		t.Fatalf("second read: %v", err)
	}
	if got := second["boat"].(map[string]any)["model"]; got != "Untouched" {
		t.Fatalf("mutation of a previously returned map leaked into the cache: model = %v", got)
	}
	if _, present := second["new_top_level_key"]; present {
		t.Fatalf("mutation of a previously returned map leaked a new key into the cache")
	}
}

func TestReadSettings_MissingFileReturnsEmptyMapNoError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "does-not-exist.yaml")

	settings, err := readSettings(path)
	if err != nil {
		t.Fatalf("expected no error for a missing settings file, got %v", err)
	}
	if len(settings) != 0 {
		t.Fatalf("expected an empty map for a missing settings file, got %+v", settings)
	}
}

// Guards the cache key against two different settings files racing to
// occupy the same path over time in a test process, and confirms the cache
// is genuinely per-path, not a single global slot.
func TestReadSettings_CacheIsIsolatedPerPath(t *testing.T) {
	pathA := writeSettingsCacheFixture(t, "boat:\n    model: Boat A\n")
	pathB := writeSettingsCacheFixture(t, "boat:\n    model: Boat B\n")

	a, err := readSettings(pathA)
	if err != nil {
		t.Fatalf("read A: %v", err)
	}
	b, err := readSettings(pathB)
	if err != nil {
		t.Fatalf("read B: %v", err)
	}

	if got := a["boat"].(map[string]any)["model"]; got != "Boat A" {
		t.Fatalf("path A: expected Boat A, got %v", got)
	}
	if got := b["boat"].(map[string]any)["model"]; got != "Boat B" {
		t.Fatalf("path B: expected Boat B, got %v", got)
	}
}

// Sanity check that the cache entry's mtime is genuinely sourced from the
// file (not e.g. left zero-valued), since a bug there would make every read
// look like a permanent cache hit regardless of what changed on disk.
func TestReadSettings_CacheEntryRecordsRealModTime(t *testing.T) {
	path := writeSettingsCacheFixture(t, "boat:\n    model: Test Boat\n")

	if _, err := readSettings(path); err != nil {
		t.Fatalf("read: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}

	settingsCacheMu.Lock()
	entry, ok := settingsCache[path]
	settingsCacheMu.Unlock()
	if !ok {
		t.Fatalf("expected a cache entry after reading %q", path)
	}
	if !entry.modTime.Equal(info.ModTime()) {
		t.Fatalf("cache entry modTime %v does not match file modTime %v", entry.modTime, info.ModTime())
	}
	if entry.size != info.Size() {
		t.Fatalf("cache entry size %d does not match file size %d", entry.size, info.Size())
	}
	if entry.modTime.IsZero() {
		t.Fatalf("cache entry modTime must not be the zero value")
	}
}
