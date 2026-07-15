package main

import (
	"database/sql"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/labstack/echo/v4"
	_ "modernc.org/sqlite"
)

// contactSessionGap is the minimum quiet period, per vessel, before a
// contact is considered a new encounter rather than a continuation of the
// current one. The 5-second server poller (tracks.go) sees a vessel that
// stays in range on every tick; without this gap that would insert a row
// every 5 seconds instead of once per encounter.
const contactSessionGap = 30 * time.Minute

// globalNearbyContactStore is the process-wide nearby-vessel contact store,
// opened once in main() and shared by the track poller (writes) and the
// /api/nearby-vessels + sightings handlers (reads).
var globalNearbyContactStore *nearbyContactStore

func nearbyContactsDBPath() string {
	return cacheFilePath("NEARBY_CONTACTS_DB_PATH", "data/nearby-contacts.sqlite")
}

// nearbyContactStore is a SQLite-backed store of nearby-vessel contact
// history, mirroring tile_cache.go's pattern for this app's other SQLite
// usage. lastSeen tracks, in memory and for the process lifetime only, the
// most recent tick at which each vessel was recorded - it's what lets
// recordContactIfNew tell "still the same encounter" apart from "a new one
// has started" without a database round trip on every poll tick.
type nearbyContactStore struct {
	db *sql.DB

	mu       sync.Mutex
	lastSeen map[string]time.Time
}

// nearbyContactRecord is a single row read back from listSightings, backing
// the sighting-history popup.
type nearbyContactRecord struct {
	SeenAt     time.Time
	Lat        float64
	Lon        float64
	Geoname    string
	NavContext string
}

func newNearbyContactStore(dbPath string) (*nearbyContactStore, error) {
	dir := filepath.Dir(dbPath)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("create nearby contacts directory: %w", err)
		}
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open nearby contacts database: %w", err)
	}
	// SQLite only allows one writer at a time; modernc.org/sqlite's default
	// busy behavior surfaces concurrent writes as "database is locked"
	// rather than waiting. Capping the pool at one connection makes
	// database/sql itself queue callers instead, matching tile_cache.go's
	// reasoning for its own single-writer SQLite usage.
	db.SetMaxOpenConns(1)

	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS nearby_vessel_contacts (
		id          INTEGER PRIMARY KEY AUTOINCREMENT,
		vessel_key  TEXT NOT NULL,
		name        TEXT NOT NULL,
		seen_at     INTEGER NOT NULL,
		lat         REAL NOT NULL,
		lon         REAL NOT NULL,
		geoname     TEXT NOT NULL DEFAULT '',
		nav_context TEXT NOT NULL
	)`); err != nil {
		db.Close()
		return nil, fmt.Errorf("create nearby_vessel_contacts table: %w", err)
	}

	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_nearby_vessel_contacts_vessel_key ON nearby_vessel_contacts(vessel_key)`); err != nil {
		db.Close()
		return nil, fmt.Errorf("create nearby_vessel_contacts vessel_key index: %w", err)
	}

	return &nearbyContactStore{db: db, lastSeen: make(map[string]time.Time)}, nil
}

func (s *nearbyContactStore) close() error {
	return s.db.Close()
}

// recordContactIfNew records a contact for vesselKey at now, but only
// inserts a new row if this is a new encounter: either the vessel hasn't
// been seen before, or more than contactSessionGap has elapsed since it was
// last seen. lastSeen[vesselKey] is always updated to now regardless, so
// the gap timer resets on every tick the vessel stays in range - this is
// what makes "record once per encounter" work, not a DB uniqueness
// constraint.
//
// lastSeen is process-lifetime only, so on a cold cache (first tick after
// process start for this vesselKey) it falls back to querying the database
// for the vessel's actual most-recently-recorded seen_at via
// lastRecordedSeenAt, rather than assuming "not in the map" means "never
// seen." Without this, every backend restart would cause every
// currently-visible vessel to look brand new to the in-memory map and get
// falsely re-recorded as a new encounter, even if it had been continuously
// in range for weeks.
func (s *nearbyContactStore) recordContactIfNew(vesselKey, name string, lat, lon float64, geoname, navContext string, now time.Time) error {
	s.mu.Lock()
	last, ok := s.lastSeen[vesselKey]
	s.mu.Unlock()

	if !ok {
		dbLast, dbOk, err := s.lastRecordedSeenAt(vesselKey)
		if err != nil {
			return err
		}
		last, ok = dbLast, dbOk
	}
	isNewEncounter := !ok || now.Sub(last) > contactSessionGap

	s.mu.Lock()
	s.lastSeen[vesselKey] = now
	s.mu.Unlock()

	if !isNewEncounter {
		return nil
	}

	if _, err := s.db.Exec(
		`INSERT INTO nearby_vessel_contacts (vessel_key, name, seen_at, lat, lon, geoname, nav_context) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		vesselKey, name, now.Unix(), lat, lon, geoname, navContext,
	); err != nil {
		return fmt.Errorf("record nearby vessel contact: %w", err)
	}
	return nil
}

// lastRecordedSeenAt returns the most recent seen_at recorded for vesselKey
// across all encounters (rows), or ok=false if there is no row at all for
// it. Used by recordContactIfNew to recover the "was this vessel already
// known" answer from the database on a cold in-memory cache (e.g. right
// after a process restart), since the map alone can't distinguish "never
// seen" from "seen before this process started."
func (s *nearbyContactStore) lastRecordedSeenAt(vesselKey string) (time.Time, bool, error) {
	var lastSeenUnix sql.NullInt64
	row := s.db.QueryRow(
		`SELECT MAX(seen_at) FROM nearby_vessel_contacts WHERE vessel_key = ?`,
		vesselKey,
	)
	if err := row.Scan(&lastSeenUnix); err != nil {
		return time.Time{}, false, fmt.Errorf("read last recorded seen_at: %w", err)
	}
	if !lastSeenUnix.Valid {
		return time.Time{}, false, nil
	}
	return time.Unix(lastSeenUnix.Int64, 0).UTC(), true, nil
}

// summary returns the number of encounters recorded for vesselKey prior to
// the current, still-ongoing one, and the most recent seen_at among those
// prior encounters (zero value if there are none).
//
// Contract: this assumes vesselKey is currently visible, i.e. the caller
// already knows the vessel is present right now (today, the only caller is
// the /api/nearby-vessels handler, iterating vessels it just received from
// SignalK). Under that assumption, the single most-recently-recorded row
// for this vessel is always the current, ongoing encounter - either
// recordContactIfNew inserted it moments ago, or it's an older encounter
// being silently continued (lastSeen resets but no new row is written) - so
// it's excluded from both the count and lastSeenAt. A still-ongoing
// encounter must never count as a sighting "before itself"; without this
// exclusion, a vessel's very first-ever sighting would report seenCount=1
// (its own just-inserted row) instead of 0.
//
// Deliberately NOT time-based (e.g. "is the latest row within some recent
// window of now"): a boat docked continuously at a marina for 28 days has a
// single row whose seen_at is the encounter's *start* time, which can be
// weeks in the past. A recency check would incorrectly treat that
// old-looking row as a distinct past encounter and start counting the
// still-ongoing presence as a real prior sighting. Unconditionally dropping
// the most-recent row - not based on its age - is what's correct given the
// "vesselKey is currently visible" guarantee above. If that guarantee ever
// stops holding for some future caller, this function's result for that
// caller would be meaningless and it should not reuse this method as-is.
func (s *nearbyContactStore) summary(vesselKey string) (seenCount int, lastSeenAt time.Time, err error) {
	rows, err := s.db.Query(
		`SELECT seen_at FROM nearby_vessel_contacts WHERE vessel_key = ? ORDER BY seen_at DESC`,
		vesselKey,
	)
	if err != nil {
		return 0, time.Time{}, fmt.Errorf("read nearby vessel contact summary: %w", err)
	}
	defer rows.Close()

	var seenAts []int64
	for rows.Next() {
		var seenAtUnix int64
		if err := rows.Scan(&seenAtUnix); err != nil {
			return 0, time.Time{}, fmt.Errorf("scan nearby vessel contact summary row: %w", err)
		}
		seenAts = append(seenAts, seenAtUnix)
	}
	if err := rows.Err(); err != nil {
		return 0, time.Time{}, fmt.Errorf("iterate nearby vessel contact summary rows: %w", err)
	}

	if len(seenAts) == 0 {
		return 0, time.Time{}, nil
	}
	prior := seenAts[1:]
	if len(prior) == 0 {
		return 0, time.Time{}, nil
	}
	return len(prior), time.Unix(prior[0], 0).UTC(), nil
}

// listSightings returns every recorded contact for vesselKey, newest first,
// backing the sighting-history popup.
func (s *nearbyContactStore) listSightings(vesselKey string) ([]nearbyContactRecord, error) {
	rows, err := s.db.Query(
		`SELECT seen_at, lat, lon, geoname, nav_context FROM nearby_vessel_contacts WHERE vessel_key = ? ORDER BY seen_at DESC`,
		vesselKey,
	)
	if err != nil {
		return nil, fmt.Errorf("list nearby vessel sightings: %w", err)
	}
	defer rows.Close()

	var records []nearbyContactRecord
	for rows.Next() {
		var seenAtUnix int64
		var rec nearbyContactRecord
		if err := rows.Scan(&seenAtUnix, &rec.Lat, &rec.Lon, &rec.Geoname, &rec.NavContext); err != nil {
			return nil, fmt.Errorf("scan nearby vessel sighting: %w", err)
		}
		rec.SeenAt = time.Unix(seenAtUnix, 0).UTC()
		records = append(records, rec)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate nearby vessel sightings: %w", err)
	}
	return records, nil
}

// vesselContactKey resolves the stable identity key used to record and look
// up nearby-vessel contact history: the vessel's MMSI when known, or a
// name-based fallback ("name:<UPPER NAME>") when it isn't. Kept as the one
// place this MMSI-or-name resolution happens, so the poller (writing
// contacts), the /api/nearby-vessels handler (reading summaries), and the
// sightings endpoint all agree on the same identity for a given vessel.
func vesselContactKey(mmsi, name string) string {
	mmsi = strings.TrimSpace(mmsi)
	if mmsi != "" {
		return mmsi
	}
	return "name:" + strings.ToUpper(strings.TrimSpace(name))
}

// nearbyVesselSightingWire is the wire format for a single sighting in the
// GET /api/nearby-vessels/:key/sightings response.
type nearbyVesselSightingWire struct {
	SeenAt     string  `json:"seen_at"`
	Lat        float64 `json:"lat"`
	Lon        float64 `json:"lon"`
	Geoname    string  `json:"geoname"`
	NavContext string  `json:"nav_context"`
}

// getNearbyVesselSightingsHandler is the GET /api/nearby-vessels/:key/sightings
// handler factory. :key is the same vesselKey (MMSI, or "name:..." fallback)
// used everywhere else in this file, so the frontend passes
// vessel.mmsi || "name:" + vessel.name.
func getNearbyVesselSightingsHandler(store *nearbyContactStore) echo.HandlerFunc {
	return func(c echo.Context) error {
		// Echo's router matches on the request's escaped path and does not
		// url-decode route params for you, so a key containing reserved
		// characters (":" in the "name:<UPPER NAME>" fallback, or a space
		// in the name itself) arrives here still percent-encoded, e.g.
		// "name%3ATAKU%20X". Decode it before it's used to look up a
		// vessel_key, which is always stored decoded (see tracks.go's
		// recordNearbyVesselContacts).
		rawKey := c.Param("key")
		key, err := url.PathUnescape(rawKey)
		if err != nil || key == "" {
			return c.NoContent(http.StatusBadRequest)
		}

		records, err := store.listSightings(key)
		if err != nil {
			return err
		}

		sightings := make([]nearbyVesselSightingWire, 0, len(records))
		for _, rec := range records {
			sightings = append(sightings, nearbyVesselSightingWire{
				SeenAt:     rec.SeenAt.Format(time.RFC3339),
				Lat:        rec.Lat,
				Lon:        rec.Lon,
				Geoname:    rec.Geoname,
				NavContext: rec.NavContext,
			})
		}

		return c.JSON(http.StatusOK, map[string]any{"sightings": sightings})
	}
}
