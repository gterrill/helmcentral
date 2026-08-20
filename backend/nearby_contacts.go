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
const contactSessionGap = 1 * time.Hour

// contactSessionMoveThresholdMeters is the "hasn't relocated" distance
// tolerance applied once contactSessionGap has already been exceeded: if
// the vessel's current position is within this many meters of where it was
// last seen, the gap is still treated as a continuation of the same
// encounter rather than a new sighting. This absorbs ordinary positional
// noise for a vessel that hasn't actually moved - anchor swing, GPS drift,
// marina berth positioning - so a routine AIS dropout while stationary
// doesn't fragment one encounter into several.
//
// 750.0 is not a finely-tuned number: bucketing every consecutive
// same-vessel row pair 1h-24h apart across the live contact database showed
// the distances involved are strictly bimodal. Every pair is either <=555m
// (69 of 72 pairs - one boat that never left, re-recorded because anchor
// swing carried its position past whatever threshold was in force during an
// AIS dropout) or >=5117m (the remaining pairs - genuine relocations to a
// different anchorage), with nothing recorded in between. 750m sits inside
// that ~4.5km empty band, comfortably clear of both the largest observed
// non-relocation excursion and the smallest genuine relocation.
const contactSessionMoveThresholdMeters = 750.0

// contactSessionMaxGapForPositionOverride bounds how long the position
// override above can be trusted: beyond this gap, a contact is always a new
// encounter regardless of how close its position is to where it was last
// seen. A boat silent for a week that reappears in the same slip more
// plausibly left and came back than stayed continuously the whole time;
// without this cap, a boat's home berth would generate exactly one sighting
// forever.
const contactSessionMaxGapForPositionOverride = 24 * time.Hour

// contactConfirmDwell is how long a candidate new encounter must stay
// continuously in range - with its AIS position refreshed at least once -
// before recordContactIfNew commits it as a real sighting, rather than on
// the very first tick a vessel looks like it's arrived. This closes the
// "ring-graze" defect: fetchSignalKNearbyVessels's 5000m range cutoff,
// combined with anchor swing and GPS noise on both ends, can put a vessel
// that is actually anchored just outside the ring inside it for a single
// optimistic tick, and nearbyVesselMaxAge's 10-minute staleness window then
// keeps that one fix looking "present" for up to 10 more minutes even
// though no further data corroborates it. 5 minutes is longer than the
// ~3-minute stationary Class A/B AIS report interval already cited by
// nearbyVesselMaxAge's comment, so a single optimistic fix cannot carry a
// candidate to confirmation by dwell time alone - a genuine visit will also
// see at least one fresh AIS position report land inside the window, which
// the position-refresh requirement below checks for separately.
const contactConfirmDwell = 5 * time.Minute

// contactConfirmMaxTickGap bounds how long a pending candidate can go
// without a poll tick before its dwell timer restarts from scratch instead
// of resuming where it left off. main.go's server-owned poller
// (recordNearbyVesselContacts, tracks.go) runs every 5 seconds, so a
// genuinely continuous encounter never sees a gap wider than that between
// ticks. 15 seconds is 3x that interval - enough headroom to absorb an
// occasional missed tick - but a gap wider than this means the vessel
// dropped out of the contact set entirely (fell outside the range or
// staleness filters) and later reappeared, which should start a fresh dwell
// rather than quietly resuming an old one across the gap.
const contactConfirmMaxTickGap = 15 * time.Second

// globalNearbyContactStore is the process-wide nearby-vessel contact store,
// opened once in main() and shared by the track poller (writes) and the
// /api/nearby-vessels + sightings handlers (reads).
var globalNearbyContactStore *nearbyContactStore

func nearbyContactsDBPath() string {
	return cacheFilePath("NEARBY_CONTACTS_DB_PATH", "data/nearby-contacts.sqlite")
}

// lastContact is the most recent seen-at time and position recorded for a
// vessel, tracked in memory by nearbyContactStore.lastSeen and, on a cold
// cache, recovered from the database by lastRecordedContact.
// recordContactIfNew uses both fields together to decide whether a gap past
// contactSessionGap should still count as the same encounter, per its
// position-override rule.
type lastContact struct {
	seenAt time.Time
	lat    float64
	lon    float64
}

// pendingContact is a candidate new encounter that recordContactIfNew has
// seen but not yet confirmed: it has not been in range continuously (with a
// refreshed AIS position) for contactConfirmDwell. It is never written to
// the database on its own - only confirmation does that, and the row it
// writes is backdated to firstSeenAt/lat/lon/geoname/navContext, the
// candidate's arrival moment, not whichever tick happened to cross the
// dwell threshold.
type pendingContact struct {
	firstSeenAt time.Time // encounter start; backdated into the row on confirmation
	lastTickAt  time.Time // continuity check against contactConfirmMaxTickGap

	// positionSeen is the AIS receive time recorded at dwell start (the
	// tick that created this candidate), held fixed for the candidate's
	// whole pending lifetime. Confirmation requires a later tick's
	// positionSeen to differ from this value - i.e. proof that a fresh AIS
	// position report, not just a repeated poll of a frozen fix, arrived
	// during the window.
	positionSeen time.Time

	lat, lon   float64
	geoname    string
	navContext string
}

// nearbyContactStore is a SQLite-backed store of nearby-vessel contact
// history, mirroring tile_cache.go's pattern for this app's other SQLite
// usage. lastSeen tracks, in memory and for the process lifetime only, the
// most recent tick at which each vessel was recorded and where it was seen
// - it's what lets recordContactIfNew tell "still the same encounter" apart
// from "a new one has started" without a database round trip on every poll
// tick. pending tracks, also in memory only, candidate new encounters
// awaiting confirmation per the dwell state machine documented on
// recordContactIfNew; dwell is the confirmation duration required, normally
// contactConfirmDwell but overridable (to 0) in tests that want the
// pre-dwell one-call-one-row behavior.
type nearbyContactStore struct {
	db *sql.DB

	mu       sync.Mutex
	lastSeen map[string]lastContact
	pending  map[string]pendingContact
	dwell    time.Duration
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

	return &nearbyContactStore{
		db:       db,
		lastSeen: make(map[string]lastContact),
		pending:  make(map[string]pendingContact),
		dwell:    contactConfirmDwell,
	}, nil
}

func (s *nearbyContactStore) close() error {
	return s.db.Close()
}

// recordContactIfNew records a contact for vesselKey at now, but only
// inserts a new row once a new encounter has been confirmed. A contact is
// treated as a continuation of the current encounter (no new row, no
// pending state touched beyond being cleared) if either:
//   - it's within contactSessionGap of the last time this vessel was seen,
//     regardless of position; or
//   - the gap exceeds contactSessionGap but is still within
//     contactSessionMaxGapForPositionOverride, and the vessel's current
//     position is within contactSessionMoveThresholdMeters of where it was
//     last seen - e.g. an AIS dropout while the vessel sits at anchor or a
//     dock. Past contactSessionMaxGapForPositionOverride, a contact is
//     always a new encounter regardless of position: a long enough silence
//     is presumed to mean the vessel actually left and came back, not that
//     it stayed continuously.
//
// Anything else - the vessel hasn't been seen before, or the gap exceeds
// both thresholds above - looks like a new encounter, but is not recorded
// immediately. Instead it becomes (or continues) a pendingContact candidate,
// which is only turned into a row once it has been continuously in range
// for store.dwell (normally contactConfirmDwell) *and* its AIS position has
// been refreshed at least once during that window - see pendingContact's
// doc comment for why both conditions are required. While a candidate is
// pending, lastSeen[vesselKey] is deliberately left untouched: that's what
// keeps this same "looks new" branch evaluating on every tick of the
// candidacy instead of falling into the continuation branch above.
//
// A candidate resumes across ticks as long as they arrive within
// contactConfirmMaxTickGap of each other; a wider gap discards it and starts
// a fresh one, since that gap means the vessel actually dropped out of the
// contact set rather than just being in the middle of confirmation. Once
// confirmed, the row is inserted using the *candidate's* firstSeenAt/lat/
// lon/geoname/navContext (the arrival moment), not the confirming tick's,
// and only then does lastSeen[vesselKey] advance, to the confirming tick's
// own now/lat/lon.
//
// lastSeen (and lastRecordedContact's DB fallback for it) is unaffected by
// any of the above: it is process-lifetime only, so on a cold cache (first
// tick after process start for this vesselKey) it falls back to querying
// the database for the vessel's actual most-recently-recorded seen_at and
// position via lastRecordedContact, rather than assuming "not in the map"
// means "never seen." Without this, every backend restart would cause every
// currently-visible vessel to look brand new to the in-memory map and get
// falsely re-recorded as a new encounter, even if it had been continuously
// in range for weeks. pending, by contrast, is allowed to reset on
// restart - a candidate that hadn't yet confirmed is not durable state
// worth recovering, and will simply re-accumulate its dwell.
func (s *nearbyContactStore) recordContactIfNew(vesselKey, name string, lat, lon float64, geoname, navContext string, positionSeen, now time.Time) error {
	s.mu.Lock()
	last, ok := s.lastSeen[vesselKey]
	s.mu.Unlock()

	if !ok {
		dbLast, dbOk, err := s.lastRecordedContact(vesselKey)
		if err != nil {
			return err
		}
		last, ok = dbLast, dbOk
	}

	gap := now.Sub(last.seenAt)
	withinSessionGap := ok && gap <= contactSessionGap
	withinMoveOverride := ok &&
		gap <= contactSessionMaxGapForPositionOverride &&
		haversineMeters(last.lat, last.lon, lat, lon) <= contactSessionMoveThresholdMeters
	isNewEncounter := !withinSessionGap && !withinMoveOverride

	if !isNewEncounter {
		s.mu.Lock()
		delete(s.pending, vesselKey)
		s.lastSeen[vesselKey] = lastContact{seenAt: now, lat: lat, lon: lon}
		s.mu.Unlock()
		return nil
	}

	s.mu.Lock()
	pend, pendOk := s.pending[vesselKey]
	fresh := !pendOk || now.Sub(pend.lastTickAt) > contactConfirmMaxTickGap
	if fresh {
		pend = pendingContact{
			firstSeenAt:  now,
			positionSeen: positionSeen,
			lat:          lat,
			lon:          lon,
			geoname:      geoname,
			navContext:   navContext,
		}
	}
	pend.lastTickAt = now

	elapsed := now.Sub(pend.firstSeenAt)
	// A freshly-created candidate has nothing yet to compare positionSeen
	// against, so it can never be judged "stale" on the very tick that
	// creates it - that's what lets a zero dwell (the test helper's
	// default) confirm on the first tick.
	stalePosition := !fresh && positionSeen.Equal(pend.positionSeen)
	if elapsed < s.dwell || stalePosition {
		s.pending[vesselKey] = pend
		s.mu.Unlock()
		return nil
	}
	delete(s.pending, vesselKey)
	s.mu.Unlock()

	if _, err := s.db.Exec(
		`INSERT INTO nearby_vessel_contacts (vessel_key, name, seen_at, lat, lon, geoname, nav_context) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		vesselKey, name, pend.firstSeenAt.Unix(), pend.lat, pend.lon, pend.geoname, pend.navContext,
	); err != nil {
		return fmt.Errorf("record nearby vessel contact: %w", err)
	}

	s.mu.Lock()
	s.lastSeen[vesselKey] = lastContact{seenAt: now, lat: lat, lon: lon}
	s.mu.Unlock()
	return nil
}

// lastRecordedContact returns the most recently recorded seen_at and
// position for vesselKey across all encounters (rows), i.e. the vessel's
// single newest row, or ok=false if there is no row at all for it. Used by
// recordContactIfNew to recover the "was this vessel already known, and
// where" answer from the database on a cold in-memory cache (e.g. right
// after a process restart), since the map alone can't distinguish "never
// seen" from "seen before this process started" - and the position is
// needed too, for the session-gap-exceeded position override described on
// recordContactIfNew.
func (s *nearbyContactStore) lastRecordedContact(vesselKey string) (lastContact, bool, error) {
	var seenAtUnix int64
	var lat, lon float64
	row := s.db.QueryRow(
		`SELECT seen_at, lat, lon FROM nearby_vessel_contacts WHERE vessel_key = ? ORDER BY seen_at DESC LIMIT 1`,
		vesselKey,
	)
	if err := row.Scan(&seenAtUnix, &lat, &lon); err != nil {
		if err == sql.ErrNoRows {
			return lastContact{}, false, nil
		}
		return lastContact{}, false, fmt.Errorf("read last recorded contact: %w", err)
	}
	return lastContact{seenAt: time.Unix(seenAtUnix, 0).UTC(), lat: lat, lon: lon}, true, nil
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
//
// Known transient undercount: while a returning vessel is a pendingContact
// candidate awaiting confirmation (recordContactIfNew), it is genuinely
// visible but has no row yet at all - the "most-recently-recorded row"
// this function relies on is still the previous encounter's. For up to
// store.dwell (normally contactConfirmDwell, 5 minutes), that means
// seenCount reads one lower than the vessel's real prior-sightings count.
// It self-corrects the moment the candidate confirms and its row is
// inserted. This is accepted rather than special-cased here: it is a
// narrow, self-healing window, and this function has no way to know a
// pending candidate exists without new plumbing between the two files.
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
// up nearby-vessel contact history: the vessel's MMSI. Vessels are assumed
// to always report MMSI; when one doesn't, ok is false and the caller must
// skip recording/enriching contact history for it rather than substituting
// a synthetic identity. Kept as the one place this resolution happens, so
// the poller (writing contacts), the /api/nearby-vessels handler (reading
// summaries), and the sightings endpoint all agree on the same identity for
// a given vessel.
func vesselContactKey(mmsi string) (key string, ok bool) {
	mmsi = strings.TrimSpace(mmsi)
	if mmsi == "" {
		return "", false
	}
	return mmsi, true
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
// handler factory. :key is the same vesselKey (the vessel's MMSI) used
// everywhere else in this file, so the frontend passes vessel.mmsi.
func getNearbyVesselSightingsHandler(store *nearbyContactStore) echo.HandlerFunc {
	return func(c echo.Context) error {
		// Echo's router matches on the request's escaped path and does not
		// url-decode route params for you, so a key containing reserved
		// characters arrives here still percent-encoded. Decode it before
		// it's used to look up a vessel_key, which is always stored decoded
		// (see tracks.go's recordNearbyVesselContacts).
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
