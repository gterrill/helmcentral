package main

import (
	"database/sql"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/labstack/echo/v4"
)

// AIS tracks recorded during an anchor watch (ADR 0263). Helmcentral keeps
// each nearby target's reported positions itself, in helmcentral.sqlite, from
// the vessel list the server's own poll tick already fetches. It replaced the
// SignalK tracks plugin, whose trails had no real times and vanished silently
// when the plugin did.

const (
	// aisAnchorRecordRadiusM is how close to the ANCHOR (not the boat) a target
	// must be for its reports to be stored. A fixed distance, not a setting.
	aisAnchorRecordRadiusM = 500.0
	// aisAnchorRetention is how long a stored point is kept and served.
	aisAnchorRetention = 24 * time.Hour
	// aisAnchorSegmentGap is the silence between two stored reports of one
	// target after which the line between them is not drawn.
	aisAnchorSegmentGap = 15 * time.Minute
	// aisAnchorMinInterval is the least time between two stored points of one
	// target, unless it has moved aisAnchorMinMoveM. A Class A boat reports every
	// 2 to 10 seconds, which would store about 17,000 points a day; a report
	// sooner than this after the target's last stored point is skipped. Not a
	// setting.
	aisAnchorMinInterval = 30 * time.Second
	// aisAnchorMinMoveM is how far from its last stored point a target must be
	// for a report to be stored inside aisAnchorMinInterval, so a boat crossing
	// the anchorage at speed keeps its trail up to its marker. Not a setting.
	aisAnchorMinMoveM = 25.0
	// aisAnchorPruneEvery is how often the table is trimmed to the retention.
	// Reads and the client apply the 24 hour cut themselves.
	aisAnchorPruneEvery = time.Minute
)

// withinAISAnchorRecordRadius is the one test of "inside the circle", shared by
// the recorder and the nearby feed so a recorded boat always has a marker.
func withinAISAnchorRecordRadius(watch anchorWatchData, lat, lon float64) bool {
	return haversineMeters(watch.Lat, watch.Lon, lat, lon) <= aisAnchorRecordRadiusM
}

func createAISAnchorTrackSchema(db sqlExecer) error {
	for _, q := range []string{
		// seq is the client's cursor: t_ms is each target's own receive time and
		// does not rise across targets. new_segment is decided when the point is
		// written: 1 on a target's first point, on re-entry to the circle, and
		// after a gap of more than aisAnchorSegmentGap.
		`CREATE TABLE IF NOT EXISTS ais_anchor_track_points (
			seq INTEGER PRIMARY KEY AUTOINCREMENT,
			mmsi TEXT NOT NULL,
			t_ms INTEGER NOT NULL,
			lat REAL NOT NULL,
			lon REAL NOT NULL,
			new_segment INTEGER NOT NULL,
			UNIQUE (mmsi, t_ms)
		)`,
		`CREATE INDEX IF NOT EXISTS ais_anchor_track_points_t ON ais_anchor_track_points (t_ms)`,
	} {
		if _, err := db.Exec(q); err != nil {
			return fmt.Errorf("create ais anchor track schema: %w", err)
		}
	}
	return nil
}

// aisAnchorSample is one target's current report, ready to store.
type aisAnchorSample struct {
	mmsi       string
	lat, lon   float64
	atMs       int64
	newSegment bool
}

// appendAISAnchorSamples stores the samples in one transaction.
func (s *passageStore) appendAISAnchorSamples(samples []aisAnchorSample) (bool, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return false, fmt.Errorf("append ais anchor tracks: begin: %w", err)
	}
	defer tx.Rollback()
	// A tick that read the watch as active can arrive after Raise emptied the
	// tables, or after a raise and a fresh drop. Store only while a trail is
	// running, and only reports from after it started, as appendAnchorTrailPoint
	// does.
	var startedMs int64
	if err := tx.QueryRow(`SELECT started_ms FROM anchor_trail_state WHERE id = 1`).Scan(&startedMs); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return false, fmt.Errorf("read anchor trail start: %w", err)
	}
	if startedMs == 0 {
		return false, nil
	}
	for _, sm := range samples {
		if sm.atMs < startedMs {
			continue
		}
		// A point already stored (an ambiguous commit, a recorder out of step
		// with the table) is the same report, so it is skipped rather than
		// failing the tick for every other target.
		if _, err := tx.Exec(`INSERT OR IGNORE INTO ais_anchor_track_points (mmsi, t_ms, lat, lon, new_segment) VALUES (?,?,?,?,?)`,
			sm.mmsi, sm.atMs, sm.lat, sm.lon, boolInt(sm.newSegment)); err != nil {
			return false, fmt.Errorf("append ais anchor track point for %s: %w", sm.mmsi, err)
		}
	}
	return true, tx.Commit()
}

func (s *passageStore) pruneAISAnchorTracks(now time.Time) error {
	if _, err := s.db.Exec(`DELETE FROM ais_anchor_track_points WHERE t_ms < ?`, now.Add(-aisAnchorRetention).UnixMilli()); err != nil {
		return fmt.Errorf("prune ais anchor tracks: %w", err)
	}
	return nil
}

// aisAnchorLastReports is each stored target's newest report time, to warm the
// recorder after a process start.
func (s *passageStore) aisAnchorLastReports() (map[string]int64, error) {
	rows, err := s.db.Query(`SELECT mmsi, MAX(t_ms) FROM ais_anchor_track_points GROUP BY mmsi`)
	if err != nil {
		return nil, fmt.Errorf("read last ais anchor reports: %w", err)
	}
	defer rows.Close()
	out := map[string]int64{}
	for rows.Next() {
		var mmsi string
		var ms int64
		if err := rows.Scan(&mmsi, &ms); err != nil {
			return nil, fmt.Errorf("scan last ais anchor reports: %w", err)
		}
		out[mmsi] = ms
	}
	return out, rows.Err()
}

// aisTrackPointWire is one stored report. NewSegment is true where the line
// must not join this point to the one before it.
type aisTrackPointWire struct {
	Lat        float64 `json:"lat"`
	Lon        float64 `json:"lon"`
	TMs        int64   `json:"t_ms"`
	NewSegment bool    `json:"new_segment,omitempty"`
}

type aisTrackWire struct {
	MMSI   string              `json:"mmsi"`
	Points []aisTrackPointWire `json:"points"`
}

// aisAnchorTracksSince returns the points stored after cursor, from the last
// 24 hours, grouped by target in storage order, with the new cursor (the
// newest seq returned, or cursor itself when nothing is new).
func (s *passageStore) aisAnchorTracksSince(cursor int64, now time.Time) ([]aisTrackWire, int64, error) {
	rows, err := s.db.Query(`SELECT seq, mmsi, t_ms, lat, lon, new_segment FROM ais_anchor_track_points
		WHERE seq > ? AND t_ms >= ? ORDER BY seq`, cursor, now.Add(-aisAnchorRetention).UnixMilli())
	if err != nil {
		return nil, cursor, fmt.Errorf("query ais anchor tracks: %w", err)
	}
	defer rows.Close()
	tracks := []aisTrackWire{}
	index := map[string]int{}
	next := cursor
	for rows.Next() {
		var seq int64
		var mmsi string
		var seg int
		p := aisTrackPointWire{}
		if err := rows.Scan(&seq, &mmsi, &p.TMs, &p.Lat, &p.Lon, &seg); err != nil {
			return nil, cursor, fmt.Errorf("scan ais anchor tracks: %w", err)
		}
		p.NewSegment = seg == 1
		i, ok := index[mmsi]
		if !ok {
			i = len(tracks)
			index[mmsi] = i
			tracks = append(tracks, aisTrackWire{MMSI: mmsi})
		}
		tracks[i].Points = append(tracks[i].Points, p)
		next = seq
	}
	if err := rows.Err(); err != nil {
		return nil, cursor, fmt.Errorf("query ais anchor tracks: %w", err)
	}
	return tracks, next, nil
}

// aisTrackState is the recorder's memory, kept for the life of the process.
// The nearby list also drops a target after ten minutes of silence and when
// the boat's own fix is lost, so "not in the list" says nothing about the
// circle: inside changes only on a target that is listed.
type aisTrackState struct {
	mu sync.Mutex
	// inside holds the targets last seen within the circle. A restart starts it
	// empty, so the first new point after one begins a new segment.
	inside map[string]bool
	// lastMs is each target's newest stored report time.
	lastMs map[string]int64
	// lastPos is each target's newest stored position, [lat, lon]. A target
	// known only from the database (after a restart) has none.
	lastPos map[string][2]float64
	// pendingSegment holds the targets whose next stored point must start a new
	// segment because a report that would have was skipped by
	// aisAnchorMinInterval. It clears when a point is stored.
	pendingSegment map[string]bool
	warmed         bool
	// notBefore is the last reset: a report received before it belongs to the
	// old anchor and is not stored.
	notBefore int64
	lastPrune time.Time
}

var aisTracks = &aisTrackState{inside: map[string]bool{}, lastMs: map[string]int64{}, lastPos: map[string][2]float64{}, pendingSegment: map[string]bool{}}

// reset forgets every target, after the tracks were erased at reset time (a
// drop or a raise; moving the anchor mark does not reset).
func (st *aisTrackState) reset(at time.Time) {
	st.mu.Lock()
	defer st.mu.Unlock()
	st.inside = map[string]bool{}
	st.lastMs = map[string]int64{}
	st.lastPos = map[string][2]float64{}
	st.pendingSegment = map[string]bool{}
	st.warmed = true // the table was just emptied: nothing to warm from
	st.notBefore = at.UnixMilli()
	// A new anchorage logs a boat with no MMSI again.
	missingMMSILoggedForAISAnchorTrack.Clear()
}

// missingMMSILoggedForAISAnchorTrack is recordAISAnchorTracks' own once-per-
// vessel log dedup, separate from the contact recorder's so neither
// suppresses the other's line.
var missingMMSILoggedForAISAnchorTrack sync.Map // key: vessel id (string)

// recordAISAnchorTracks stores the current report of every listed target
// within aisAnchorRecordRadiusM of the anchor. It runs once per server poll
// tick on the vessel list the tick already fetched, and does nothing without a
// watch. A report is stored only when it is newer than the target's last
// stored report and than the last reset, and either at least
// aisAnchorMinInterval after that last stored report or at least
// aisAnchorMinMoveM from its position.
func recordAISAnchorTracks(nearby []nearbyVessel, now time.Time) {
	watch, active, _ := currentAnchorWatch()
	if !active {
		return
	}
	store := globalPassageStore
	if store == nil {
		log.Printf("ais anchor tracks: %v, tick not recorded", errAnchorTrailNoStore)
		return
	}

	st := aisTracks
	st.mu.Lock()
	defer st.mu.Unlock()
	if !st.warmed {
		last, err := store.aisAnchorLastReports()
		if err != nil {
			log.Printf("ais anchor tracks: %v", err)
			return
		}
		st.lastMs = last
		st.warmed = true
	}

	cutoffMs := now.Add(-aisAnchorRetention).UnixMilli()
	evidence := map[string]bool{} // listed targets: inside the circle or not
	var samples []aisAnchorSample
	newest := map[string]int{} // MMSI -> index in samples
	for _, v := range nearby {
		isInside := withinAISAnchorRecordRadius(watch, v.Lat, v.Lon)
		key, ok := vesselContactKey(v.Mmsi)
		if !ok {
			if isInside {
				if _, already := missingMMSILoggedForAISAnchorTrack.LoadOrStore(v.ID, struct{}{}); !already {
					log.Printf("Skipping anchor watch AIS track for %q: no MMSI reported", v.Name)
				}
			}
			continue
		}
		// Two SignalK contexts can carry one MMSI: it is inside if any of them is,
		// and only the newest of their reports is kept for the tick.
		evidence[key] = evidence[key] || isInside
		if !isInside {
			continue
		}
		atMs := v.PositionSeen.UTC().Truncate(time.Millisecond).UnixMilli()
		if i, dup := newest[key]; dup {
			if atMs > samples[i].atMs {
				samples[i].lat, samples[i].lon, samples[i].atMs = v.Lat, v.Lon, atMs
			}
			continue
		}
		newest[key] = len(samples)
		samples = append(samples, aisAnchorSample{mmsi: key, lat: v.Lat, lon: v.Lon, atMs: atMs})
	}
	// Keep what is newer than the last stored report, and decide the flags.
	fresh := samples[:0]
	for _, sm := range samples {
		last, seen := st.lastMs[sm.mmsi]
		if sm.atMs <= max(last, st.notBefore, cutoffMs) {
			continue
		}
		sm.newSegment = !st.inside[sm.mmsi] || !seen || time.Duration(sm.atMs-last)*time.Millisecond > aisAnchorSegmentGap
		pos, known := st.lastPos[sm.mmsi]
		moved := !known || haversineMeters(pos[0], pos[1], sm.lat, sm.lon) >= aisAnchorMinMoveM
		if seen && !moved && time.Duration(sm.atMs-last)*time.Millisecond < aisAnchorMinInterval {
			// Too soon after the last stored point, and not far from it. A report
			// that would have started a segment passes the flag on to the next
			// stored point.
			if sm.newSegment {
				st.pendingSegment[sm.mmsi] = true
			}
			continue
		}
		sm.newSegment = sm.newSegment || st.pendingSegment[sm.mmsi]
		fresh = append(fresh, sm)
	}
	samples = fresh

	if len(samples) > 0 {
		// State moves only once the points are committed, so a failed write
		// keeps its segment flags for the next tick.
		stored, err := store.appendAISAnchorSamples(samples)
		if err != nil {
			log.Printf("ais anchor tracks: %v", err)
			return
		}
		if !stored {
			return // the watch ended under this tick
		}
		for _, sm := range samples {
			st.lastMs[sm.mmsi] = sm.atMs
			st.lastPos[sm.mmsi] = [2]float64{sm.lat, sm.lon}
			delete(st.pendingSegment, sm.mmsi)
		}
	}
	for key, isInside := range evidence {
		if isInside {
			st.inside[key] = true
		} else {
			delete(st.inside, key)
			// Seen outside, so the re-entry rule flags its next point anyway: a
			// boat that only clipped the circle leaves nothing pending.
			delete(st.pendingSegment, key)
		}
	}

	if now.Sub(st.lastPrune) >= aisAnchorPruneEvery {
		if err := store.pruneAISAnchorTracks(now); err != nil {
			log.Printf("ais anchor tracks: %v", err)
		} else {
			st.lastPrune = now
		}
	}
}

// GET /api/anchor-watch/trails/ais?since=<cursor>
// Returns the AIS tracks recorded around the anchor since the cursor, which is
// the "cursor" of the previous answer (0 for all). trail_started_at has the
// same meaning as on trails/self: a client that sees it change drops the points
// it holds and asks again from 0. server_time_ms is the server's own clock, so
// the map can age the points by the boat's time rather than the browser's.
func getAISTrailsHandler(c echo.Context) error {
	var cursor int64
	if s := c.QueryParam("since"); s != "" {
		var err error
		cursor, err = strconv.ParseInt(s, 10, 64)
		if err != nil || cursor < 0 {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid since cursor"})
		}
	}

	if !anchorWatchActive() {
		return c.JSON(http.StatusOK, map[string]any{"trail_started_at": nil, "cursor": 0, "tracks": []aisTrackWire{}, "server_time_ms": time.Now().UnixMilli()})
	}
	if globalPassageStore == nil {
		log.Printf("ais anchor tracks: %v", errAnchorTrailNoStore)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "AIS tracks unavailable: track store is not open"})
	}
	started, err := globalPassageStore.anchorTrailStarted()
	if err != nil {
		log.Printf("ais anchor tracks: %v", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "AIS tracks unavailable: could not read the tracks"})
	}
	var startedAt any
	tracks := []aisTrackWire{}
	next := cursor
	if !started.IsZero() {
		startedAt = started.Format(time.RFC3339Nano)
		tracks, next, err = globalPassageStore.aisAnchorTracksSince(cursor, time.Now().UTC())
		if err != nil {
			log.Printf("ais anchor tracks: %v", err)
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": "AIS tracks unavailable: could not read the tracks"})
		}
	}
	return c.JSON(http.StatusOK, map[string]any{"trail_started_at": startedAt, "cursor": next, "tracks": tracks, "server_time_ms": time.Now().UnixMilli()})
}
