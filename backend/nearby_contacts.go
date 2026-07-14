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
func (s *nearbyContactStore) recordContactIfNew(vesselKey, name string, lat, lon float64, geoname, navContext string, now time.Time) error {
	s.mu.Lock()
	last, ok := s.lastSeen[vesselKey]
	isNewEncounter := !ok || now.Sub(last) > contactSessionGap
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

// summary returns the total number of recorded encounters and the most
// recent seen_at for vesselKey. Replaces the old Influx-backed
// queryInfluxNearbyVesselHistory + summarizeEncounterSessions path - no
// session-gap aggregation is needed at read time, since sessions are now
// discrete rows written once at contact time by recordContactIfNew.
func (s *nearbyContactStore) summary(vesselKey string) (seenCount int, lastSeenAt time.Time, err error) {
	var lastSeenUnix sql.NullInt64
	row := s.db.QueryRow(
		`SELECT COUNT(*), MAX(seen_at) FROM nearby_vessel_contacts WHERE vessel_key = ?`,
		vesselKey,
	)
	if err := row.Scan(&seenCount, &lastSeenUnix); err != nil {
		return 0, time.Time{}, fmt.Errorf("read nearby vessel contact summary: %w", err)
	}
	if lastSeenUnix.Valid {
		lastSeenAt = time.Unix(lastSeenUnix.Int64, 0).UTC()
	}
	return seenCount, lastSeenAt, nil
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
