package main

import (
	"database/sql"
	"errors"
	"fmt"
	"log"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/labstack/echo/v4"
)

// The anchor swing trail (ADR 0199): the boat's fixes since the anchor was
// set (or last moved), one per poll tick, kept in helmcentral.sqlite so the
// whole swing survives a restart. The track point log (ADR 0176) keeps a
// point only every 25 m or ten minutes, which is far too sparse to draw a
// swing of a few metres, so the trail has its own table.

const (
	// anchorTrailMaxPoints is the one bound on the trail: the table keeps the
	// newest this many points and a response serves up to this many. At the
	// 60 s heartbeat that is a week of a boat that never moves; a swinging
	// boat fills it sooner and the oldest points drop off.
	anchorTrailMaxPoints = 10000
	// A fix is stored only when the boat has moved this far from the last
	// stored point, or anchorTrailHeartbeat has passed.
	anchorTrailMinMoveM  = 1.0
	anchorTrailHeartbeat = 60 * time.Second
	// The table is trimmed to the bound every this many stored points rather
	// than on each one.
	anchorTrailPruneEvery = 100
)

var errAnchorTrailNoStore = errors.New("anchor trail: track store unavailable")

// anchorTrailStored counts stored points to schedule pruning. Writers are
// already serialised by the database's immediate transactions on its single
// connection, so no further lock is needed.
var anchorTrailStored atomic.Int64

func createAnchorTrailSchema(db sqlExecer) error {
	for _, q := range []string{
		`CREATE TABLE IF NOT EXISTS anchor_trail_points (
			t_ms INTEGER PRIMARY KEY,
			lat REAL NOT NULL,
			lon REAL NOT NULL
		)`,
		// started_ms is when the current trail began; 0 means no trail.
		`CREATE TABLE IF NOT EXISTS anchor_trail_state (
			id INTEGER PRIMARY KEY CHECK (id = 1),
			started_ms INTEGER NOT NULL
		)`,
	} {
		if _, err := db.Exec(q); err != nil {
			return fmt.Errorf("create anchor trail schema: %w", err)
		}
	}
	return nil
}

// resetAnchorTrail empties the trail. A restart stamps a new start time (a
// drop or a moved anchor); otherwise the trail is over (raise).
func (s *passageStore) resetAnchorTrail(restart bool, now time.Time) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("reset anchor trail: begin: %w", err)
	}
	defer tx.Rollback()
	var started int64
	if restart {
		started = now.UnixMilli()
	}
	if _, err := tx.Exec(`DELETE FROM anchor_trail_points`); err != nil {
		return fmt.Errorf("reset anchor trail: %w", err)
	}
	if _, err := tx.Exec(`INSERT INTO anchor_trail_state (id, started_ms) VALUES (1, ?)
		ON CONFLICT(id) DO UPDATE SET started_ms = excluded.started_ms`, started); err != nil {
		return fmt.Errorf("reset anchor trail: %w", err)
	}
	return tx.Commit()
}

// startAnchorTrailIfAbsent begins a trail for a watch restored at startup
// that predates the trail table. It never touches a trail that has started.
func (s *passageStore) startAnchorTrailIfAbsent(now time.Time) error {
	_, err := s.db.Exec(`INSERT INTO anchor_trail_state (id, started_ms) VALUES (1, ?)
		ON CONFLICT(id) DO UPDATE SET started_ms = excluded.started_ms WHERE started_ms = 0`, now.UnixMilli())
	if err != nil {
		return fmt.Errorf("start anchor trail: %w", err)
	}
	return nil
}

// appendAnchorTrailPoint stores one fix, in one transaction that also checks
// the trail is running. A fix sampled before the trail's start, or while no
// trail runs, is dropped; so is one that is neither 1 m from the last stored
// point nor 60 s after it.
func (s *passageStore) appendAnchorTrailPoint(at time.Time, lat, lon float64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("append anchor trail point: begin: %w", err)
	}
	defer tx.Rollback()

	var startedMs int64
	if err := tx.QueryRow(`SELECT started_ms FROM anchor_trail_state WHERE id = 1`).Scan(&startedMs); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		return fmt.Errorf("read anchor trail start: %w", err)
	}
	if startedMs == 0 || at.UnixMilli() < startedMs {
		return nil
	}

	var lastMs int64
	var lastLat, lastLon float64
	err = tx.QueryRow(`SELECT t_ms, lat, lon FROM anchor_trail_points ORDER BY t_ms DESC LIMIT 1`).Scan(&lastMs, &lastLat, &lastLon)
	if err == nil {
		if at.UnixMilli() <= lastMs {
			return nil
		}
		if at.Sub(time.UnixMilli(lastMs)) < anchorTrailHeartbeat && haversineMeters(lastLat, lastLon, lat, lon) < anchorTrailMinMoveM {
			return nil
		}
	} else if !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("read last anchor trail point: %w", err)
	}

	if _, err := tx.Exec(`INSERT INTO anchor_trail_points (t_ms, lat, lon) VALUES (?,?,?)`, at.UnixMilli(), lat, lon); err != nil {
		return fmt.Errorf("append anchor trail point: %w", err)
	}
	if anchorTrailStored.Add(1)%anchorTrailPruneEvery == 0 {
		if _, err := tx.Exec(`DELETE FROM anchor_trail_points WHERE t_ms <= (SELECT t_ms FROM anchor_trail_points ORDER BY t_ms DESC LIMIT 1 OFFSET ?)`, anchorTrailMaxPoints); err != nil {
			return fmt.Errorf("prune anchor trail: %w", err)
		}
	}
	return tx.Commit()
}

// anchorTrailStarted is the current trail's start, zero when there is none.
func (s *passageStore) anchorTrailStarted() (time.Time, error) {
	var ms int64
	err := s.db.QueryRow(`SELECT started_ms FROM anchor_trail_state WHERE id = 1`).Scan(&ms)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return time.Time{}, nil
		}
		return time.Time{}, fmt.Errorf("read anchor trail start: %w", err)
	}
	if ms == 0 {
		return time.Time{}, nil
	}
	return time.UnixMilli(ms).UTC(), nil
}

// anchorTrailSince returns the points after since, oldest first, at most
// anchorTrailMaxPoints of the newest, with the trail's start time.
func (s *passageStore) anchorTrailSince(since time.Time) ([]*trailPoint, time.Time, error) {
	started, err := s.anchorTrailStarted()
	if err != nil || started.IsZero() {
		return nil, started, err
	}
	sinceMs := int64(-1)
	if !since.IsZero() {
		sinceMs = since.UnixMilli()
	}
	rows, err := s.db.Query(`SELECT t_ms, lat, lon FROM anchor_trail_points WHERE t_ms > ? AND t_ms >= ? ORDER BY t_ms DESC LIMIT ?`,
		sinceMs, started.UnixMilli(), anchorTrailMaxPoints)
	if err != nil {
		return nil, started, fmt.Errorf("query anchor trail: %w", err)
	}
	defer rows.Close()
	var out []*trailPoint
	for rows.Next() {
		var ms int64
		p := &trailPoint{}
		if err := rows.Scan(&ms, &p.Lat, &p.Lon); err != nil {
			return nil, started, fmt.Errorf("scan anchor trail: %w", err)
		}
		p.Timestamp = time.UnixMilli(ms).UTC()
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, started, fmt.Errorf("query anchor trail: %w", err)
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out, started, nil
}

// recordAnchorTrailPoint is called on every poll tick; it stores the fix only
// while an anchor watch is active and the position is a real fix.
func recordAnchorTrailPoint(lat, lon float64, at time.Time) {
	anchorWatchMu.RLock()
	active := anchorWatchState != nil
	anchorWatchMu.RUnlock()
	if !active || !hasUsableVesselPosition(lat, lon) {
		return
	}
	if globalPassageStore == nil {
		log.Printf("anchor trail: %v, fix not recorded", errAnchorTrailNoStore)
		return
	}
	if err := globalPassageStore.appendAnchorTrailPoint(at.UTC().Truncate(time.Millisecond), lat, lon); err != nil {
		log.Printf("anchor trail: %v", err)
	}
}

// resetAnchorTrail starts a fresh trail (drop, moved anchor) or ends it
// (raise). The caller fails the operation when it returns an error, so the
// watch and its trail never disagree silently.
func resetAnchorTrail(restart bool) error {
	if globalPassageStore == nil {
		return errAnchorTrailNoStore
	}
	return globalPassageStore.resetAnchorTrail(restart, time.Now().UTC().Truncate(time.Millisecond))
}

// startRestoredAnchorTrail runs once at startup, after the watch is restored:
// a watch that was already running when the trail table was introduced gets
// its trail started now.
func startRestoredAnchorTrail() error {
	anchorWatchMu.RLock()
	active := anchorWatchState != nil
	anchorWatchMu.RUnlock()
	if !active {
		return nil
	}
	if globalPassageStore == nil {
		return errAnchorTrailNoStore
	}
	return globalPassageStore.startAnchorTrailIfAbsent(time.Now().UTC().Truncate(time.Millisecond))
}

// GET /api/anchor-watch/trails/self?since=<RFC3339>
// Returns the swing trail since the anchor was set or last moved.
// trail_started_at changes whenever the trail restarts, so a client that
// sees it change drops the points it holds.
func getSelfTrailHandler(c echo.Context) error {
	var since time.Time
	if sinceStr := c.QueryParam("since"); sinceStr != "" {
		var err error
		since, err = time.Parse(time.RFC3339, sinceStr)
		if err != nil {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid since timestamp"})
		}
	}

	anchorWatchMu.RLock()
	active := anchorWatchState != nil
	anchorWatchMu.RUnlock()
	if !active {
		return c.JSON(http.StatusOK, map[string]any{"points": []trackPoint{}, "trail_started_at": nil})
	}
	if globalPassageStore == nil {
		log.Printf("anchor trail: %v", errAnchorTrailNoStore)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "anchor trail unavailable: track store is not open"})
	}
	points, started, err := globalPassageStore.anchorTrailSince(since)
	if err != nil {
		log.Printf("anchor trail: %v", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "anchor trail unavailable: could not read the trail"})
	}
	wire := toWire(points)
	if wire == nil {
		wire = []trackPoint{}
	}
	var startedAt any
	if !started.IsZero() {
		startedAt = started.Format(time.RFC3339Nano)
	}
	return c.JSON(http.StatusOK, map[string]any{"points": wire, "trail_started_at": startedAt})
}
