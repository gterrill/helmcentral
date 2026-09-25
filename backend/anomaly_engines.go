package main

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"sort"
	"time"
)

// anomaly_engines.go implements the "engine differentials" anomaly
// detector: for two or more configured engines running matched, comparing
// each engine's coolant/oil-pressure/boost/load/transmission readings
// against its peers, with a per-rpm-bucket offset learned from Influx
// history so a boat whose port engine always runs a little warmer than
// starboard does not get a permanent false alarm for it. See the
// anomaly-detection plan for full cycle context.
//
// Signal K revolutions are in Hz (1 Hz = 60 rpm); every rpm threshold below
// is stated in Hz for that reason, with the rpm figure from the plan in a
// comment.

// --- Twin gate ---------------------------------------------------------

const (
	twinGateMinRPMHz         = 900.0 / 60.0  // 900 rpm
	twinGateMaxRPMSpreadHz   = 50.0 / 60.0   // 50 rpm
	twinGateMinCoolantK      = 65.0 + 273.15 // 65 degC
	twinGateMinRunFor        = 10 * time.Minute
	twinGateRPMSteadyFor     = 60 * time.Second
	twinGateCoolantSteadyFor = 300 * time.Second
	// twinIdleRPMHz is the "engine is actually running" cutoff used to track
	// continuous runtime (RunningFor) from a raw rpm series: well below the
	// gate's own 900 rpm qualifying threshold, so a dip during a shift
	// doesn't look like a fresh start.
	twinIdleRPMHz = 60.0 / 60.0 // 60 rpm
)

// engineTwinState is one engine's reading for the twin gate, all measured
// at the same instant. RPMSteady/CoolantSteady are windowed conditions
// (held within tolerance for twinGateRPMSteadyFor / above threshold for
// twinGateCoolantSteadyFor) the caller resolves -- the live detector
// goroutine tracks them the same way anomaly_sensor_health.go's
// frozenWindow is built up tick by tick, since computeDerivedPathsFromTree
// cannot itself hold rolling-window state. twinGateHolds itself judges one
// instant's already-summarised state, nothing more.
type engineTwinState struct {
	Name          string
	RPMHz         float64
	CoolantK      float64
	RunningFor    time.Duration
	RPMSteady     bool
	CoolantSteady bool
}

// twinGateHolds reports whether the engine differential detector's gate
// holds across every engine in engines: every engine at or above 900 rpm
// and all within 50 rpm of each other, rpm and coolant each steady over
// their own window, every coolant at or above 65 degC, and every engine
// running for at least 10 minutes. Fewer than two engines never holds --
// there is nothing to differ against.
func twinGateHolds(engines []engineTwinState) bool {
	if len(engines) < 2 {
		return false
	}

	minRPM, maxRPM := math.Inf(1), math.Inf(-1)
	for _, e := range engines {
		if e.RPMHz < twinGateMinRPMHz {
			return false
		}
		if e.CoolantK < twinGateMinCoolantK {
			return false
		}
		if e.RunningFor < twinGateMinRunFor {
			return false
		}
		if !e.RPMSteady || !e.CoolantSteady {
			return false
		}
		if e.RPMHz < minRPM {
			minRPM = e.RPMHz
		}
		if e.RPMHz > maxRPM {
			maxRPM = e.RPMHz
		}
	}
	return maxRPM-minRPM <= twinGateMaxRPMSpreadHz
}

// --- Residual arithmetic -------------------------------------------------

// residualQuantity is (this engine's reading - the median of its peers')
// at one instant, less that engine's own learned offset for the current
// rpm bucket. ok is false when no learned offset is available (the plan's
// "the residual is absent if the bucket has under 30 qualifying minutes" --
// engineBaselineOffsetFor only ever returns a bucket that already cleared
// that count, via learnTwinBaseline's own aggregateBuckets).
func residualQuantity(ownValue float64, peerValues []float64, learnedOffset float64, learnedOk bool) (float64, bool) {
	if !learnedOk || len(peerValues) == 0 {
		return 0, false
	}
	return (ownValue - median(peerValues)) - learnedOffset, true
}

func median(values []float64) float64 {
	sorted := append([]float64(nil), values...)
	sort.Float64s(sorted)
	n := len(sorted)
	if n == 0 {
		return 0
	}
	if n%2 == 1 {
		return sorted[n/2]
	}
	return (sorted[n/2-1] + sorted[n/2]) / 2
}

func medianAbsoluteDeviation(values []float64, center float64) float64 {
	deviations := make([]float64, len(values))
	for i, v := range values {
		deviations[i] = math.Abs(v - center)
	}
	return median(deviations)
}

// --- Baseline learning ----------------------------------------------------

const (
	// engineBaselineMinMinutes is the plan's "the residual is absent if the
	// bucket has under 30 qualifying minutes".
	engineBaselineMinMinutes = 30
	// engineBaselineBucketRPM is the plan's "200 rpm bucket" width.
	engineBaselineBucketRPM = 200
	// engineBaselineLookbackDays is the plan's "14 days of 1m Influx data".
	engineBaselineLookbackDays = 14
	// engineBaselineMinRunMinutes is the plan's "at least 5 steady minutes
	// per run": a minute only counts toward learning once the gate (minus
	// its own steadiness sub-clauses, which this run-length check subsumes)
	// has held for this many consecutive 1m samples ending at that minute.
	// 5 minutes is also comfortably above twinGateCoolantSteadyFor's 300s,
	// so satisfying this run length satisfies both steadiness windows at
	// once.
	engineBaselineMinRunMinutes = 5
)

// engineBaselineBucket is one rpm bucket's learned offset for one engine's
// one quantity. RPMBucket is the bucket's lower bound in rpm (not Hz), for
// readability in the persisted JSON file.
type engineBaselineBucket struct {
	RPMBucket int     `json:"rpm_bucket"`
	Median    float64 `json:"median"`
	MAD       float64 `json:"mad"`
	Minutes   int     `json:"minutes"`
}

// engineBaseline is data/anomaly-twin-baseline.json's whole shape: per
// engine, per quantity path suffix (e.g. "temperature", "oilPressure"), the
// learned rpm-bucket offsets.
type engineBaseline struct {
	Engines      map[string]map[string][]engineBaselineBucket `json:"engines"`
	ComputedAt   time.Time                                    `json:"computed_at"`
	LookbackDays int                                          `json:"lookback_days"`
}

// engineSeries is one engine's raw telemetry for one quantity's learning
// window, in queryInfluxPathRange's own []telemetryPoint shape: rpm and
// coolant (the gate's own inputs) plus the one quantity being learned. All
// three series, and every engine's, must share the same length and the same
// per-index timestamps -- exactly what fetching every path over the same
// start/stop/every gives (the same same-tick assumption
// vesselFuelEconomyWithAge already makes of its own inputs elsewhere in
// this package). A caller with genuinely gappy Influx data must re-align
// onto a common grid before calling in here; this function fails fast on a
// mismatch rather than silently comparing two different minutes.
type engineSeries struct {
	Name     string
	RPM      []telemetryPoint
	Coolant  []telemetryPoint
	Quantity []telemetryPoint
}

// learnTwinBaseline aggregates a learning window's worth of already-fetched
// per-engine telemetry into a per-engine, per-rpm-bucket learned offset. At
// every minute where the twin gate holds continuously for at least
// engineBaselineMinRunMinutes samples, each engine's residual against the
// median of its peers is bucketed by rpm and reduced to a median, MAD and
// minute count; a bucket under engineBaselineMinMinutes is dropped, not
// zeroed.
// bucketToMinutes turns a raw telemetryPoint series into a minute-keyed
// map, truncating each timestamp down to its minute and keeping the last
// value seen for any minute two points collide on. A real Influx fetch
// carries both gaps (missing minutes) and duplicate rows (most often at a
// 7-day chunk boundary, depending on the upstream query's own inclusivity
// at that edge) -- a duplicate is the same underlying reading arriving
// twice, not two different readings, so the fix is to collapse it, not to
// average or reject it.
func bucketToMinutes(points []telemetryPoint) map[time.Time]float64 {
	out := make(map[time.Time]float64, len(points))
	for _, p := range points {
		out[p.Timestamp.Truncate(time.Minute)] = p.Value
	}
	return out
}

// learnTwinBaseline aggregates a learning window's worth of already-fetched
// per-engine telemetry into a per-engine, per-rpm-bucket learned offset.
//
// Each engine's rpm, coolant and the quantity being learned are fetched as
// three independent Influx queries (fetchInfluxRangeChunked, one call per
// path), so they do not share one cadence or one set of gaps -- two
// engines' series, or even one engine's own rpm versus its oilPressure,
// rarely line up index-for-index. This buckets every series down to the
// minute (bucketToMinutes) and joins on that bucket instead of on array
// position: a minute only ever qualifies once every engine's rpm, coolant
// AND quantity all have a reading for it. At every minute where the twin
// gate holds continuously for at least engineBaselineMinRunMinutes
// consecutive *qualifying* minutes (a real time gap between two qualifying
// minutes breaks the run, the same as an idle rpm sample would -- two
// readings either side of five minutes of missing data are not a single
// steady run), each engine's residual against the median of its peers is
// bucketed by rpm and reduced to a median, MAD and minute count; a bucket
// under engineBaselineMinMinutes is dropped, not zeroed.
func learnTwinBaseline(series []engineSeries) (map[string][]engineBaselineBucket, error) {
	if len(series) < 2 {
		return nil, fmt.Errorf("learnTwinBaseline: needs at least two engines, got %d", len(series))
	}

	type engineMinutes struct {
		rpm, coolant, value map[time.Time]float64
	}
	perEngine := make([]engineMinutes, len(series))
	for i, s := range series {
		perEngine[i] = engineMinutes{
			rpm:     bucketToMinutes(s.RPM),
			coolant: bucketToMinutes(s.Coolant),
			value:   bucketToMinutes(s.Quantity),
		}
	}

	// A minute only counts when every engine has all three quantities for
	// it -- the join. Candidates come from the first engine's rpm minutes;
	// any engine's map would do, since the loop below still requires every
	// other engine (and this one's own coolant/value) to agree.
	var minutes []time.Time
	for minute := range perEngine[0].rpm {
		ok := true
		for _, e := range perEngine {
			if _, has := e.rpm[minute]; !has {
				ok = false
				break
			}
			if _, has := e.coolant[minute]; !has {
				ok = false
				break
			}
			if _, has := e.value[minute]; !has {
				ok = false
				break
			}
		}
		if ok {
			minutes = append(minutes, minute)
		}
	}
	sort.Slice(minutes, func(i, j int) bool { return minutes[i].Before(minutes[j]) })
	n := len(minutes)

	runningFor := make([][]time.Duration, len(series))
	for e := range series {
		runningFor[e] = runningDurationsAt(minutes, perEngine[e].rpm, twinIdleRPMHz)
	}

	qualifies := make([]bool, n)
	for i, minute := range minutes {
		states := make([]engineTwinState, len(series))
		for e, s := range series {
			states[e] = engineTwinState{
				Name: s.Name, RPMHz: perEngine[e].rpm[minute], CoolantK: perEngine[e].coolant[minute],
				RunningFor: runningFor[e][i],
				// Steadiness is folded into the run-length check below
				// rather than judged per-minute here.
				RPMSteady: true, CoolantSteady: true,
			}
		}
		qualifies[i] = twinGateHolds(states)
	}
	steady := steadyRunAt(minutes, qualifies, engineBaselineMinRunMinutes)

	perEngineSamples := make(map[string][]baselineSample, len(series))

	for i, minute := range minutes {
		if !steady[i] {
			continue
		}
		for e, s := range series {
			var peers []float64
			for e2 := range series {
				if e2 == e {
					continue
				}
				peers = append(peers, perEngine[e2].value[minute])
			}
			if len(peers) == 0 {
				continue
			}
			residual := perEngine[e].value[minute] - median(peers)
			perEngineSamples[s.Name] = append(perEngineSamples[s.Name], baselineSample{bucket: rpmBucket(perEngine[e].rpm[minute]), residual: residual})
		}
	}

	result := make(map[string][]engineBaselineBucket, len(perEngineSamples))
	for name, samples := range perEngineSamples {
		result[name] = aggregateBaselineBuckets(samples)
	}
	return result, nil
}

// runningDurationsAt is runningDurations' counterpart over an already-joined
// minute sequence: for each minute, how long the engine has been
// continuously above idleHz ending at that minute, resetting not only on a
// below-idle reading but also across a real time gap between one qualifying
// minute and the next (a missing stretch of data is not evidence the engine
// kept running through it). minutes must be sorted ascending.
func runningDurationsAt(minutes []time.Time, rpmByMinute map[time.Time]float64, idleHz float64) []time.Duration {
	out := make([]time.Duration, len(minutes))
	var runSince time.Time
	running := false
	for i, minute := range minutes {
		gapped := i > 0 && minute.Sub(minutes[i-1]) > time.Minute
		if rpmByMinute[minute] <= idleHz || gapped {
			running = false
		}
		if !running {
			running = true
			runSince = minute
		}
		out[i] = minute.Sub(runSince)
	}
	return out
}

// steadyRun reports, for each index, whether qualifies has held true for at
// least minRun consecutive samples ending at that index.
// steadyRunAt is steadyRun's counterpart over an already-joined minute
// sequence: for each minute, whether qualifies has held true for at least
// minRun consecutive *qualifying* minutes ending there, where "consecutive"
// also requires the minutes themselves to be one real minute apart -- a
// gap between two qualifying minutes breaks the run the same way a
// non-qualifying minute would. minutes must be sorted ascending and the
// same length as qualifies.
func steadyRunAt(minutes []time.Time, qualifies []bool, minRun int) []bool {
	out := make([]bool, len(qualifies))
	run := 0
	for i, q := range qualifies {
		gapped := i > 0 && minutes[i].Sub(minutes[i-1]) > time.Minute
		if gapped {
			run = 0
		}
		if q {
			run++
		} else {
			run = 0
		}
		out[i] = run >= minRun
	}
	return out
}

// rpmBucket returns hz's rpm bucket, as the bucket's lower bound in rpm.
func rpmBucket(hz float64) int {
	rpm := hz * 60
	return int(math.Floor(rpm/engineBaselineBucketRPM)) * engineBaselineBucketRPM
}

// baselineSample is one qualifying minute's residual for one engine at one
// rpm bucket, the aggregation unit learnTwinBaseline builds and
// aggregateBaselineBuckets reduces.
type baselineSample struct {
	bucket   int
	residual float64
}

func aggregateBaselineBuckets(samples []baselineSample) []engineBaselineBucket {
	byBucket := map[int][]float64{}
	for _, s := range samples {
		byBucket[s.bucket] = append(byBucket[s.bucket], s.residual)
	}

	buckets := make([]int, 0, len(byBucket))
	for b := range byBucket {
		buckets = append(buckets, b)
	}
	sort.Ints(buckets)

	var out []engineBaselineBucket
	for _, b := range buckets {
		values := byBucket[b]
		if len(values) < engineBaselineMinMinutes {
			continue
		}
		med := median(values)
		out = append(out, engineBaselineBucket{
			RPMBucket: b,
			Median:    med,
			MAD:       medianAbsoluteDeviation(values, med),
			Minutes:   len(values),
		})
	}
	return out
}

// engineBaselineOffsetFor looks up the learned offset for rpmHz's bucket. ok
// is false when no bucket was learned for it (never enough qualifying
// minutes, or no baseline at all).
func engineBaselineOffsetFor(buckets []engineBaselineBucket, rpmHz float64) (float64, bool) {
	want := rpmBucket(rpmHz)
	for _, b := range buckets {
		if b.RPMBucket == want {
			return b.Median, true
		}
	}
	return 0, false
}

// --- Persistence: data/anomaly-twin-baseline.json --------------------------

func anomalyTwinBaselinePath() string {
	return cacheFilePath("ANOMALY_TWIN_BASELINE_FILE", "data/anomaly-twin-baseline.json")
}

// loadEngineBaseline reads the persisted baseline. A missing file is a
// fresh install (no baseline learned yet): it returns the zero value with
// no error, exactly like readSettings' own "missing file, empty map, no
// error" contract. A file that exists but fails to parse is corrupt, not
// absent, and fails fast -- the plan's "a corrupt baseline file is logged
// as an error and treated as no baseline, never as zero offsets": the
// caller must not mistake "could not read this" for "learned zero offset
// everywhere", which would silently mute every twin residual instead of
// leaving them absent.
func loadEngineBaseline(path string) (engineBaseline, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return engineBaseline{}, nil
		}
		return engineBaseline{}, fmt.Errorf("reading %s: %w", path, err)
	}

	var b engineBaseline
	if err := json.Unmarshal(raw, &b); err != nil {
		return engineBaseline{}, fmt.Errorf("parsing %s: %w", path, err)
	}
	return b, nil
}

func saveEngineBaseline(path string, b engineBaseline) error {
	return writeJSONFileAtomic(path, b)
}
