package main

import (
	"os"
	"sync"
	"time"
)

/*
settings.yaml is read constantly: every stream client rebuild touches it
several times a second (most telemetryEmitters builders re-read it), and
every API request goes through requireRole -> resolveAuthMode, which reads
it even when auth.mode is "none". The disk read itself is cheap; the
yaml.Unmarshal is not (measured ~120us / ~900 allocs on this file's size on
an M1) -- so this caches the parsed result rather than the bytes, keyed by
the file's mtime and size, and invalidated explicitly by writeSettings so a
same-size rewrite (or a filesystem with coarse mtime resolution) can never
serve pre-write content.

readSettings' callers are a mix: most only read, but at least one
(updateSettingsHandler) mutates the map it gets back in place before handing
it to writeSettings. Handing out the cached map itself would let that
mutation corrupt what every other caller sees before the write even lands on
disk, so every readSettings call returns its own deep copy via
deepCopyMap/deepCopyValue (signalk_snapshot.go) -- settings.yaml is a few KB,
so copying it is far cheaper than parsing it, and importantly a fixed cost
regardless of how many stream clients are reading it in parallel.
*/

type settingsCacheEntry struct {
	modTime  time.Time
	size     int64
	settings map[string]any
	// rolesYAML is the "roles" block re-marshalled from settings (nil for
	// none), filled by the first loadVesselRoles after the parse, so the
	// vessel-state and electrical-state builders skip the whole-document
	// copy and re-marshal every tick. It goes with the entry: a write or an
	// external change drops both.
	rolesYAML   []byte
	rolesCached bool
}

var (
	settingsCacheMu sync.Mutex
	settingsCache   = map[string]settingsCacheEntry{}
)

// invalidateSettingsCache drops settingsPath's cached entry. Called by
// writeSettings right after a successful write, and when readSettings finds
// the file gone -- both cases where the next read must not trust whatever
// is cached.
func invalidateSettingsCache(settingsPath string) {
	settingsCacheMu.Lock()
	delete(settingsCache, settingsPath)
	settingsCacheMu.Unlock()
}

// cachedSettingsFor returns a deep copy of settingsPath's cached parse if
// info's mtime and size still match what was cached, along with whether it
// found one.
func cachedSettingsFor(settingsPath string, info os.FileInfo) (map[string]any, bool) {
	settingsCacheMu.Lock()
	entry, ok := settingsCache[settingsPath]
	settingsCacheMu.Unlock()

	if !ok || !entry.modTime.Equal(info.ModTime()) || entry.size != info.Size() {
		return nil, false
	}

	return deepCopyMap(entry.settings), true
}

// sharedCachedSettings is cachedSettingsFor without the copy. Cached maps are
// never mutated after storing, so a caller that only reads may share one.
func sharedCachedSettings(settingsPath string, info os.FileInfo) (map[string]any, bool) {
	settingsCacheMu.Lock()
	entry, ok := settingsCache[settingsPath]
	settingsCacheMu.Unlock()
	if !ok || !entry.modTime.Equal(info.ModTime()) || entry.size != info.Size() {
		return nil, false
	}
	return entry.settings, true
}

// storeSettingsCache records settings (already parsed, never mutated
// in-place after this call) as settingsPath's current cached parse.
func storeSettingsCache(settingsPath string, info os.FileInfo, settings map[string]any) {
	settingsCacheMu.Lock()
	settingsCache[settingsPath] = settingsCacheEntry{
		modTime:  info.ModTime(),
		size:     info.Size(),
		settings: settings,
	}
	settingsCacheMu.Unlock()
}

// cachedRolesYAMLFor returns the roles block's YAML cached with
// settingsPath's parse, if info still matches it and the block has been
// cached. The bytes are never written after caching; callers only decode them.
func cachedRolesYAMLFor(settingsPath string, info os.FileInfo) ([]byte, bool) {
	settingsCacheMu.Lock()
	entry, ok := settingsCache[settingsPath]
	settingsCacheMu.Unlock()
	if !ok || !entry.rolesCached || !entry.modTime.Equal(info.ModTime()) || entry.size != info.Size() {
		return nil, false
	}
	return entry.rolesYAML, true
}

// storeRolesYAMLCache records buf as the roles block of settingsPath's
// cached parse, only while that parse is still the one info describes.
func storeRolesYAMLCache(settingsPath string, info os.FileInfo, buf []byte) {
	settingsCacheMu.Lock()
	defer settingsCacheMu.Unlock()
	entry, ok := settingsCache[settingsPath]
	if !ok || !entry.modTime.Equal(info.ModTime()) || entry.size != info.Size() {
		return
	}
	entry.rolesYAML, entry.rolesCached = buf, true
	settingsCache[settingsPath] = entry
}
