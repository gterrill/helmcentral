package main

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
)

// Production wiring for the passage plan's assessment (plans_assessment.go):
// the readers Mate's tools use, called without a model between them.

// planForecastMemoTTL is how long a point's forecast is reused between
// assessments: an edit to one stop asks again only for the places it moved. It
// is a little longer than the page's refresh (FORECAST_REFRESH_SECONDS, 600), so
// the refresh finds it kept rather than racing its expiry.
const planForecastMemoTTL = 12 * time.Minute

// planForecastFetchTimeout bounds one shared read of a point's forecast, which
// runs apart from the request that started it.
const planForecastFetchTimeout = 60 * time.Second

// planForecastPoints is the forecast read at every point of every plan's legs,
// kept for planForecastMemoTTL.
var planForecastPoints = newPlanForecastMemo(planForecastMemoTTL)

// planForecastMemo keeps each point's forecast, so the legs a plan edit did not
// touch are not read again. A point is keyed as the assessor keys it, with the
// providers and the zone its hours are read in. A failed read is never kept, and
// readers that miss on the same point at once share one read.
type planForecastMemo struct {
	mu     sync.Mutex
	ttl    time.Duration
	m      map[string]planForecastMemoEntry
	swept  time.Time
	flight singleflight.Group
}

type planForecastMemoEntry struct {
	at time.Time
	f  passageForecast
}

func newPlanForecastMemo(ttl time.Duration) *planForecastMemo {
	return &planForecastMemo{ttl: ttl, m: map[string]planForecastMemoEntry{}}
}

// wrap returns fc with its reads going through the memo; now is the time of the
// assessment asking, which stamps what it stores.
func (m *planForecastMemo) wrap(fc forecastSource, zone string, now time.Time) forecastSource {
	inner := fc.Fetch
	fc.Fetch = func(ctx context.Context, lat, lon float64) (passageForecast, error) {
		key := fc.Provider + "|" + fc.WaveProvider + "|" + zone + "|" + passagePointKey(passageRoutePoint{Lat: lat, Lon: lon})
		if f, ok := m.kept(key, now); ok {
			return f, nil
		}
		// The shared read belongs to every reader waiting on it, not to the one that
		// started it: it runs on a context detached from that caller, with its own
		// bound, and each caller leaves early on its own context.
		ch := m.flight.DoChan(key, func() (any, error) {
			// A reader that waited on an earlier flight finds what it kept.
			if f, ok := m.kept(key, now); ok {
				return f, nil
			}
			fetchCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), planForecastFetchTimeout)
			defer cancel()
			f, err := inner(fetchCtx, lat, lon)
			if err != nil {
				return nil, err
			}
			m.keep(key, now, f)
			return f, nil
		})
		select {
		case <-ctx.Done():
			return passageForecast{}, ctx.Err()
		case r := <-ch:
			if r.Err != nil {
				return passageForecast{}, r.Err
			}
			return r.Val.(passageForecast), nil
		}
	}
	return fc
}

// kept is the point's forecast when it was read within the TTL of now.
func (m *planForecastMemo) kept(key string, now time.Time) (passageForecast, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.m[key]
	if ok && now.Sub(e.at) < m.ttl && !now.Before(e.at) {
		return e.f, true
	}
	return passageForecast{}, false
}

// keep stores a forecast. The expired ones are cleared at most once a TTL, not on
// every miss.
func (m *planForecastMemo) keep(key string, now time.Time, f passageForecast) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if now.Sub(m.swept) >= m.ttl {
		for k, old := range m.m {
			if now.Sub(old.at) >= m.ttl {
				delete(m.m, k)
			}
		}
		m.swept = now
	}
	m.m[key] = planForecastMemoEntry{at: now, f: f}
}

// productionPlanAssessDeps wires the configured providers, the boat's recorded
// speed, the fair-tide ranking and the modelled current.
func productionPlanAssessDeps(settingsPath string) planAssessDeps {
	d := assistantProductionToolDeps(settingsPath)
	return planAssessDeps{
		now:    d.now,
		limits: d.passageLimits,
		sources: func(zone func() (string, error), now time.Time) (passageSources, error) {
			src, err := d.passageSources("plan assessment", zone, now)
			if err != nil {
				return src, err
			}
			zoneName, err := zone()
			if err != nil {
				return passageSources{}, err
			}
			src.fc = planForecastPoints.wrap(src.fc, zoneName, now)
			return src, nil
		},
		speed: func(ctx context.Context) (float64, error) {
			now := d.now()
			return planCruisingSpeed.get(now, struct{}{}, func() (float64, error) {
				return recordedCruisingSpeed(ctx, globalPassageStore, now)
			})
		},
		progress: func(ctx context.Context, p plan) ([]string, error) {
			first, err := time.Parse(planDateLayout, p.Stops[0].Arrive)
			if err != nil {
				return nil, fmt.Errorf("the first arrival %q is not a date: %w", p.Stops[0].Arrive, err)
			}
			// A day either side covers the zones the plan's dates are kept in.
			since := first.AddDate(0, 0, -1)
			ends, err := planPassageEnds.get(d.now(), since.Format(planDateLayout), func() ([]planPassageEnd, error) {
				return readPassageEnds(ctx, globalPassageStore, since)
			})
			if err != nil {
				return nil, err
			}
			return planProgressOf(p, ends)
		},
		underway: func(ctx context.Context) (*planUnderway, error) {
			return readUnderway(ctx, globalPassageStore)
		},
		position: func() (float64, float64, error) {
			state, err := tideVesselState()
			return boatOwnPosition(state, err)
		},
		suggested: func(planID int64, legKey, day string, t time.Time) (time.Time, error) {
			return globalDocumentStore.FirstSuggestion(planID, legKey, day, t)
		},
		keptSuggestion: func(planID int64, legKey, day string, loc *time.Location) (time.Time, bool, error) {
			return globalDocumentStore.KeptSuggestion(planID, legKey, day, loc)
		},
		sun:          sunTimes,
		dawn:         civilDawn,
		departures:   d.planDepartures,
		currentAlong: d.planCurrentAlong,
		warnings: func(now time.Time) (planWarningsReader, error) {
			provider, id, err := resolveForecastWarningsProvider(settingsPath)
			if err != nil {
				return nil, err
			}
			return planWarningsFrom(provider, id, now), nil
		},
	}
}

// planWarningsFrom reads warnings through the same check the warnings fetch loop
// makes, so a provider that is only serving a stale cache is an error here too,
// never "no warnings in force".
func planWarningsFrom(provider forecastWarningsProvider, id string, now time.Time) planWarningsReader {
	return func(lat, lon float64) (forecastWarningsBundle, error) {
		bundle, err := fetchCheckedWarnings(provider, id, lat, lon, now)
		if err != nil {
			return forecastWarningsBundle{}, fmt.Errorf("forecast warnings provider %q unavailable: %s", id, firstErrorLine(err))
		}
		return bundle, nil
	}
}

const (
	// The ETA speed is the median of the passages Helmcentral recorded in the last
	// planSpeedMonths months. A passage under planSpeedMinNm or planSpeedMinUnderway
	// is a marina move or a drift, not a cruise, and is left out.
	planSpeedMonths      = 12
	planSpeedMinNm       = 3.0
	planSpeedMinUnderway = 30 * time.Minute
)

// planReadTTL is how long a read of the passage record is reused. It moves only
// when a passage is recorded or edited, and a few minutes' lag is harmless. The
// passage writes are many and scattered, so the lag is bounded by this alone.
const planReadTTL = 10 * time.Minute

var (
	// planCruisingSpeed is the recorded speed the plan assessment reads.
	planCruisingSpeed planReadCache[struct{}, float64]
	// planPassageEnds is where the recorded passages ended, by the day they are
	// read since.
	planPassageEnds planReadCache[string, []planPassageEnd]
)

// planReadCache keeps what a read of the passage record returned for planReadTTL,
// so it does not run on every request. A failed read is never kept. The zero
// value is ready.
type planReadCache[K comparable, V any] struct {
	mu sync.Mutex
	m  map[K]planReadEntry[V]
}

type planReadEntry[V any] struct {
	at time.Time
	v  V
}

func (c *planReadCache[K, V]) get(now time.Time, key K, read func() (V, error)) (V, error) {
	c.mu.Lock()
	e, ok := c.m[key]
	c.mu.Unlock()
	if ok && now.Sub(e.at) < planReadTTL && !now.Before(e.at) {
		return e.v, nil
	}
	v, err := read()
	if err != nil {
		var zero V
		return zero, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.m == nil {
		c.m = map[K]planReadEntry[V]{}
	}
	// A key is a day or nothing, so the map stays small.
	for k, old := range c.m {
		if now.Sub(old.at) >= planReadTTL {
			delete(c.m, k)
		}
	}
	c.m[key] = planReadEntry[V]{at: now, v: v}
	return v, nil
}

func (c *planReadCache[K, V]) drop() {
	c.mu.Lock()
	c.m = nil
	c.mu.Unlock()
}

// recordedCruisingSpeed is the speed a plan's ETA is worked out at: the median,
// over the visible passages that began in the last twelve months and ran at least
// 3 nm and 30 minutes under way, of each passage's distance over its time under
// way. It reads Helmcentral's own passage record, not the engine history. With
// no such passage there is no speed, and no default is made up.
func recordedCruisingSpeed(ctx context.Context, store *passageStore, now time.Time) (float64, error) {
	since := now.AddDate(0, -planSpeedMonths, 0)
	records, err := visiblePassagesSince(ctx, store, since)
	if err != nil {
		return 0, err
	}
	var speeds []float64
	for _, r := range records {
		if r.Start.Before(since) || r.Start.After(now) {
			continue
		}
		// A passage with no stored figures is worked out from its track, which is
		// slow: stop once the request is over.
		if err := ctx.Err(); err != nil {
			return 0, fmt.Errorf("the passage record was not read: %w", err)
		}
		stats, err := store.PassageStatsFor(r)
		if err != nil {
			return 0, fmt.Errorf("the passage record could not be read: %w", err)
		}
		underway := time.Duration(stats.UnderwaySeconds * float64(time.Second))
		if stats.DistanceNM < planSpeedMinNm || underway < planSpeedMinUnderway {
			continue
		}
		speeds = append(speeds, stats.DistanceNM/underway.Hours())
	}
	if len(speeds) == 0 {
		return 0, fmt.Errorf("no recorded passages yet")
	}
	sort.Float64s(speeds)
	mid := len(speeds) / 2
	if len(speeds)%2 == 1 {
		return speeds[mid], nil
	}
	return (speeds[mid-1] + speeds[mid]) / 2, nil
}

// planDepartures ranks the departures in a leg's window as plan_tidal_departure
// does, on the Open-Meteo modelled current along the route, and hands them back
// as they are, not as the tool's JSON. Where the model has none for the run there
// is no ranking, and that is the error: the plan has no flood direction to time
// the tide by.
func (d assistantToolDeps) planDepartures(ctx context.Context, q planDepartureQuery) ([]tidalDeparture, error) {
	if len(q.Route) < 2 {
		return nil, fmt.Errorf("the leg has no route")
	}
	start, next := q.Route[0], q.Route[1]
	ranking, err := d.rankTidalDepartures(ctx, tidalRankQuery{
		Lat: start.Lat, Lon: start.Lon,
		CourseDeg: bearingDeg(start.Lat, start.Lon, next.Lat, next.Lon),
		Hours:     q.Hours,
		Route:     q.Route,
		Earliest:  q.Earliest, Latest: q.Latest,
		Now: d.now(),
	})
	var none errTidalNothingToRank
	if errors.As(err, &none) {
		return nil, fmt.Errorf("the Open-Meteo model has no current for the run: %s", none.reason)
	}
	if err != nil {
		return nil, err
	}
	return ranking.departures()
}

// planCurrentAlong is the modelled total current along a leg's course for a
// departure at dep, as the ranking reads it: the mean along the course, positive
// with the boat. It is for a departure the operator set; a suggested one is read
// with the ranking.
func (d assistantToolDeps) planCurrentAlong(ctx context.Context, q planDepartureQuery, dep time.Time) (planCurrentReading, error) {
	if len(q.Route) < 2 {
		return planCurrentReading{Why: "the leg has no route"}, nil
	}
	start, next := q.Route[0], q.Route[1]
	duration := tidalRunDuration(q.Hours)
	n := int(math.Ceil(float64(duration) / float64(assistantTidalSampleStep)))
	samples, err := tidalSamples(q.Route, start.Lat, start.Lon, bearingDeg(start.Lat, start.Lon, next.Lat, next.Lon), n)
	if err != nil {
		return planCurrentReading{}, err
	}
	cur, err := d.fetchTidalCurrents(ctx, samples, d.now(), dep.Add(duration))
	if err != nil {
		return planCurrentReading{}, err
	}
	distNm := make([]float64, len(samples))
	for k, s := range samples {
		distNm[k] = s.distNm
	}
	return currentReading(alongCourse(cur, samples, distNm, dep, duration), cur, samples), nil
}
