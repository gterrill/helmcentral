package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"math"
	"strings"
	"time"
)

// The fetch figures the sea sweep keeps (ADR 0300, ADR 0299). Reading the shore
// around a spell costs far more than a page can spend, so the hourly sweep does
// it once for each position and stores what the rays found, keyed by the squares
// of coastline it read. A page only reads the rows, and never a line of the
// coastline.

const (
	// seaFetchSpellBudget is the most geometries (one for each position and state
	// of its squares) one sweep builds. The first sweep after an upgrade has every
	// passage to fill; the rest wait for the next sweep, oldest passage first.
	seaFetchSpellBudget = 2000
	// fetchPosScale rounds a position to a millionth of a degree for its key. Rays
	// are cast from the rounded position, so a key names what was computed.
	fetchPosScale = 1e6
)

// fetchRayKey names one stored row: the position (millionths of a degree), the
// kind of ray (wind, calm or swell), the bearing in tenths of a degree (0 for a
// calm), and the stamp of the squares the shore was read from.
type fetchRayKey struct {
	LatE6, LonE6 int
	Kind         string
	BearingDD    int
	Squares      string
}

// fetchRay is one stored row. DistM is the mean (wind) or longest (calm) fetch,
// and is nil when the shore is open, the position ashore, or the row is a swell's.
type fetchRay struct {
	fetchRayKey
	OnLand, Open, Reef bool
	DistM              *float64
	ComputedMs         int64
}

func createSeaFetchSchema(db *sql.DB) error {
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS sea_fetch_rays (
		lat_e6 INTEGER NOT NULL,
		lon_e6 INTEGER NOT NULL,
		kind TEXT NOT NULL,
		bearing_dd INTEGER NOT NULL,
		squares TEXT NOT NULL,
		on_land INTEGER NOT NULL,
		open INTEGER NOT NULL,
		reef INTEGER NOT NULL,
		dist_m REAL,
		computed_ms INTEGER NOT NULL,
		PRIMARY KEY (lat_e6, lon_e6, kind, bearing_dd, squares)
	)`); err != nil {
		return fmt.Errorf("create sea fetch schema: %w", err)
	}
	return nil
}

// StoreFetchRays keeps the rows in one transaction. A row already held is left
// as it is: its key names the squares it was computed from.
func (s *passageStore) StoreFetchRays(rows []fetchRay) error {
	if len(rows) == 0 {
		return nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("store sea fetch: %w", err)
	}
	defer tx.Rollback()
	for _, r := range rows {
		var dist any
		if r.DistM != nil {
			dist = *r.DistM
		}
		if _, err := tx.Exec(`INSERT OR IGNORE INTO sea_fetch_rays
			(lat_e6, lon_e6, kind, bearing_dd, squares, on_land, open, reef, dist_m, computed_ms)
			VALUES (?,?,?,?,?,?,?,?,?,?)`,
			r.LatE6, r.LonE6, r.Kind, r.BearingDD, r.Squares, r.OnLand, r.Open, r.Reef, dist, r.ComputedMs); err != nil {
			return fmt.Errorf("store sea fetch: %w", err)
		}
	}
	return tx.Commit()
}

// FetchRayKeys is the key of every stored row, for the sweep to tell what it has
// already worked out.
func (s *passageStore) FetchRayKeys() (map[fetchRayKey]bool, error) {
	rows, err := s.db.Query(`SELECT lat_e6, lon_e6, kind, bearing_dd, squares FROM sea_fetch_rays`)
	if err != nil {
		return nil, fmt.Errorf("read sea fetch: %w", err)
	}
	defer rows.Close()
	out := map[fetchRayKey]bool{}
	for rows.Next() {
		var k fetchRayKey
		if err := rows.Scan(&k.LatE6, &k.LonE6, &k.Kind, &k.BearingDD, &k.Squares); err != nil {
			return nil, fmt.Errorf("read sea fetch: %w", err)
		}
		out[k] = true
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read sea fetch: %w", err)
	}
	return out, nil
}

// FetchRaysWithin reads, in one query, every stored row at a position inside the
// bounds (millionths of a degree), whatever squares it was computed from. The
// caller picks the rows whose key it wants out of the answer.
func (s *passageStore) FetchRaysWithin(minLat, maxLat, minLon, maxLon int) (map[fetchRayKey]fetchRay, error) {
	rows, err := s.db.Query(`SELECT lat_e6, lon_e6, kind, bearing_dd, squares, on_land, open, reef, dist_m, computed_ms
		FROM sea_fetch_rays WHERE lat_e6 BETWEEN ? AND ? AND lon_e6 BETWEEN ? AND ?`, minLat, maxLat, minLon, maxLon)
	if err != nil {
		return nil, fmt.Errorf("read sea fetch: %w", err)
	}
	defer rows.Close()
	out := map[fetchRayKey]fetchRay{}
	for rows.Next() {
		var r fetchRay
		var dist sql.NullFloat64
		if err := rows.Scan(&r.LatE6, &r.LonE6, &r.Kind, &r.BearingDD, &r.Squares, &r.OnLand, &r.Open, &r.Reef, &dist, &r.ComputedMs); err != nil {
			return nil, fmt.Errorf("read sea fetch: %w", err)
		}
		if dist.Valid {
			d := dist.Float64
			r.DistM = &d
		}
		out[r.fetchRayKey] = r
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read sea fetch: %w", err)
	}
	return out, nil
}

// fetchPos is a spell's position as its rows are keyed: millionths of a degree,
// and the rounded position in degrees that rays are cast from.
func fetchPos(sp feltSpell) (latE6, lonE6 int, lat, lon float64) {
	latE6 = int(math.Round(*sp.MidLat * fetchPosScale))
	lonE6 = int(math.Round(*sp.MidLon * fetchPosScale))
	return latE6, lonE6, float64(latE6) / fetchPosScale, float64(lonE6) / fetchPosScale
}

// rayRequest is one row a spell needs: a kind of ray along a bearing.
type rayRequest struct {
	Kind      string
	BearingDD int
}

// fetchRequests are the rows the page will ask for to judge a spell, given the
// hindcast hour behind it: a wind wave needs the wind kept with the spell (a calm
// takes the one row for every bearing, any other wind the row for the direction
// it came from), and a swell the row for the quarter it comes from. A spell the
// page will not check, for want of a wind or a direction, has no wind row to work
// out.
func fetchRequests(sp feltSpell, hour seaHindcastRow) []rayRequest {
	var out []rayRequest
	if hour.WindWave != nil && sp.WindKept && sp.WindKts != nil {
		switch {
		case *sp.WindKts < fetchCalmKts:
			out = append(out, rayRequest{Kind: fetchKindCalm})
		case sp.WindFromDeg != nil:
			out = append(out, rayRequest{Kind: fetchKindWind, BearingDD: bearingDD(*sp.WindFromDeg)})
		}
	}
	if hour.Swell != nil {
		out = append(out, rayRequest{Kind: fetchKindSwell, BearingDD: bearingDD(hour.Swell.FromDeg)})
	}
	return out
}

// seaFetchSummary is what one run did.
type seaFetchSummary struct {
	// Built is the geometries built (one for each position and state of its
	// squares), Rows the rows stored, Squares the squares read from the store.
	Built, Rows, Squares int
	// Failed is the geometries that could not be read or stored, and
	// PassagesFailed the passages whose figures could not be read.
	Failed, PassagesFailed int
	// Skipped is why the run did nothing, when it did nothing.
	Skipped string
	// Stopped is why the run ended before it had looked at every passage.
	Stopped seaStop
}

type fetchGeoKey struct {
	latE6, lonE6 int
	stamp        string
}

// runSeaFetch works out, for every spell that has a sea state, what the shore
// says of it, and stores what is missing (ADR 0300). The spells are looked at
// passage by passage, oldest first, with one coastline loader shared by all, so a
// square is read once however many passages lie over it. A spell is worked out
// once for each position, each state of its squares, and each wind direction or
// swell direction; a spell that has its rows costs only a lookup.
//
// It never guesses: with the coastline off or not on this install, it logs why
// and stores nothing, and a spell whose squares are not all held is left alone,
// since the page words why (a missing square, an inland position). It builds at
// most seaFetchSpellBudget geometries and honours ctx; the next sweep carries on.
// Only runSeaPass calls it, so two never overlap.
func runSeaFetch(ctx context.Context, store *passageStore, coast func() *offlineCoastlineReader) seaFetchSummary {
	return runSeaFetchWithin(ctx, store, coast, seaFetchSpellBudget)
}

// runSeaFetchWithin is runSeaFetch with the budget given, so a test can use a
// small one.
func runSeaFetchWithin(ctx context.Context, store *passageStore, coast func() *offlineCoastlineReader, budget int) seaFetchSummary {
	var sum seaFetchSummary
	if ctx.Err() != nil {
		sum.Stopped = seaStopped
		return sum
	}
	records, err := store.allPassageRecords()
	if err != nil {
		log.Printf("passages: sea fetch skipped: %v; nothing stored, it tries again at the next hourly sweep", err)
		sum.Skipped = err.Error()
		return sum
	}
	loader := coast().newLoader()
	var existing map[fetchRayKey]bool
	// ready is checked, and the stored keys read, when the first spell needs work,
	// so a sweep with nothing to do says nothing.
	started := false
	start := func() bool {
		if started {
			return sum.Skipped == ""
		}
		started = true
		if err := loader.ready(); err != nil {
			sum.Skipped = coastlineReason(err)
			log.Printf("passages: sea fetch skipped: %s; nothing stored, it tries again at the next hourly sweep", sum.Skipped)
			return false
		}
		if existing, err = store.FetchRayKeys(); err != nil {
			sum.Skipped = err.Error()
			log.Printf("passages: sea fetch skipped: %v; nothing stored, it tries again at the next hourly sweep", err)
			return false
		}
		return true
	}

	var failures []string
passages:
	for _, rec := range records {
		if ctx.Err() != nil {
			sum.Stopped = seaStopped
			break
		}
		f, err := store.PassageFiguresFor(rec)
		if err != nil {
			log.Printf("passages: sea fetch: passage %s left out: %v", rec.ID, err)
			sum.PassagesFailed++
			continue
		}
		seas := store.seaLookupFor(f.Stats.FeltSpells)
		if seas != nil && seas.err != nil {
			log.Printf("passages: sea fetch: passage %s left out: its hindcast rows could not be read: %v", rec.ID, seas.err)
			sum.PassagesFailed++
			continue
		}
		if seas == nil {
			continue
		}
		geos := map[fetchGeoKey]*fetchGeometry{}
		failedGeos := map[fetchGeoKey]bool{}
		var out []fetchRay
		stopped := seaRanOn
	spells:
		for _, sp := range f.Stats.FeltSpells {
			if ctx.Err() != nil {
				stopped = seaStopped
				break
			}
			k, ok := sp.seaKey()
			hour, held := seas.rows[k]
			if !ok || !held || hour.MissingReason != "" {
				continue
			}
			requests := fetchRequests(sp, hour)
			if len(requests) == 0 {
				continue
			}
			if !start() {
				return sum
			}
			latE6, lonE6, lat, lon := fetchPos(sp)
			plan, err := loader.plan(lat, lon)
			if err != nil {
				// The page words it (a square missing, the position inland).
				continue
			}
			var todo []fetchRayKey
			for _, r := range requests {
				key := fetchRayKey{LatE6: latE6, LonE6: lonE6, Kind: r.Kind, BearingDD: r.BearingDD, Squares: plan.Stamp}
				if !existing[key] {
					todo = append(todo, key)
				}
			}
			if len(todo) == 0 {
				continue
			}
			gk := fetchGeoKey{latE6, lonE6, plan.Stamp}
			if failedGeos[gk] {
				continue
			}
			geo := geos[gk]
			if geo == nil {
				if sum.Built >= budget {
					stopped = seaStopBudget
					break spells
				}
				set, err := loader.linesFor(plan.Needed)
				if err != nil {
					failedGeos[gk] = true
					sum.Failed++
					noteFirst(&failures, err.Error())
					continue
				}
				geo = newFetchGeometry(lat, lon, set.Lines)
				geos[gk] = geo
				sum.Built++
			}
			now := time.Now().UnixMilli()
			for _, key := range todo {
				res := geo.ray(key.Kind, key.BearingDD)
				row := fetchRay{fetchRayKey: key, OnLand: res.OnLand, Open: res.Open, Reef: res.Reef, ComputedMs: now}
				if key.Kind != fetchKindSwell && !res.OnLand && !res.Open {
					d := res.DistM
					row.DistM = &d
				}
				out = append(out, row)
				existing[key] = true
			}
		}
		if err := store.StoreFetchRays(out); err != nil {
			log.Printf("passages: sea fetch: passage %s: %v", rec.ID, err)
			sum.Failed++
			noteFirst(&failures, err.Error())
		} else {
			sum.Rows += len(out)
		}
		if stopped != seaRanOn {
			sum.Stopped = stopped
			break passages
		}
	}
	sum.Squares = len(loader.lines)
	if sum.Built > 0 || sum.Failed > 0 || sum.Stopped != seaRanOn || sum.PassagesFailed > 0 {
		line := fmt.Sprintf("passages: sea fetch: worked out %d positions, %d figures, %d positions failed", sum.Built, sum.Rows, sum.Failed)
		if len(failures) > 0 {
			line += " (" + strings.Join(failures, "; ") + "); they are tried again at the next hourly sweep"
		}
		if sum.PassagesFailed > 0 {
			line += fmt.Sprintf(", %d passages left out", sum.PassagesFailed)
		}
		if sum.Stopped != seaRanOn {
			line += ", " + string(sum.Stopped)
		}
		log.Print(line)
	}
	return sum
}

// runSeaPass is one pass of the sea sweep: the hindcast is filled first, so a
// spell whose hour was fetched this pass has its shore worked out in the same
// pass.
func runSeaPass(ctx context.Context, store *passageStore, resolve func() (waveProvider, string, error), coast func() *offlineCoastlineReader, now time.Time) {
	runSeaHindcast(ctx, store, resolve, now)
	runSeaFetch(ctx, store, coast)
}

// ── what the page reads ──────────────────────────────────────────────────

// fetchLookup judges the sea of each spell of one passage against the stored
// figures. It reads the coastline setting and the squares' records once, never a
// line of coastline, and keeps nothing between page reads.
type fetchLookup struct {
	loader  *coastlineLoader
	rows    map[fetchRayKey]fetchRay
	rowsErr error
}

// newFetchView reads the stored rows for a passage's spells in one query. A nil
// reader is a coastline that is not on this install, and every spell says so.
func newFetchView(r *offlineCoastlineReader, store *passageStore, spells []feltSpell) *fetchLookup {
	f := &fetchLookup{loader: r.newLoader()}
	var minLat, maxLat, minLon, maxLon int
	found := false
	for _, sp := range spells {
		if !sp.hasPosition() {
			continue
		}
		latE6, lonE6, _, _ := fetchPos(sp)
		if !found {
			minLat, maxLat, minLon, maxLon, found = latE6, latE6, lonE6, lonE6, true
			continue
		}
		minLat, maxLat = min(minLat, latE6), max(maxLat, latE6)
		minLon, maxLon = min(minLon, lonE6), max(maxLon, lonE6)
	}
	if found {
		if f.rows, f.rowsErr = store.FetchRaysWithin(minLat, maxLat, minLon, maxLon); f.rowsErr != nil {
			log.Printf("passages: %v", f.rowsErr)
		}
	}
	return f
}

// judge applies the fetch check to a spell's sea from the stored figures. A spell
// the sweep has not yet worked out reads "not worked out yet".
func (f *fetchLookup) judge(sp feltSpell, sea *spellSea) {
	if !sp.hasPosition() {
		return
	}
	judgeSea(sp, sea, func() (rayFunc, string) {
		latE6, lonE6, lat, lon := fetchPos(sp)
		plan, err := f.loader.plan(lat, lon)
		if err != nil {
			return nil, coastlineReason(err)
		}
		return func(kind string, bearingDeg float64) (rayResult, string) {
			if f.rowsErr != nil {
				return rayResult{}, fetchStoredUnreadable
			}
			dd := 0
			if kind != fetchKindCalm {
				dd = bearingDD(bearingDeg)
			}
			row, ok := f.rows[fetchRayKey{LatE6: latE6, LonE6: lonE6, Kind: kind, BearingDD: dd, Squares: plan.Stamp}]
			if !ok {
				return rayResult{}, fetchNotWorkedOut
			}
			res := rayResult{OnLand: row.OnLand, Open: row.Open, Reef: row.Reef}
			if row.DistM != nil {
				res.DistM = *row.DistM
			}
			return res, ""
		}, ""
	})
}
