package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"time"
)

// This file is the list_passages tool (ADR 0165). Asked about "the last few
// passages", Mate had no way to turn that into time windows, so it never
// reached the logged telemetry that holds the answer. list_passages reads the
// vessel's own speed-over-ground history and returns the recent periods the
// boat was underway; Mate then calls get_path_history once per window.

const (
	// assistantPassagesBucket is the InfluxDB aggregation window. 5 minutes
	// is fine enough to see a short run and a brief slow-down, and keeps a
	// 60-day query to about 17,000 points, well inside the 6 s query timeout.
	assistantPassagesBucket = 5 * time.Minute
	assistantPassagesEvery  = "5m"

	// assistantPassagesMinSOGKts is the underway threshold. It is the same
	// 3 kn the anchor auto-raise uses to say the boat is moving under its
	// own power (autoRaiseMinSOGKts), so both features agree on "underway".
	assistantPassagesMinSOGKts = autoRaiseMinSOGKts

	// assistantPassagesMergeGap: a slow-down or missing data shorter than
	// this inside a run (a lock, a bridge, a crossing wake) stays one passage.
	assistantPassagesMergeGap = 30 * time.Minute

	// assistantPassagesMinDuration drops shorter runs: shifting berth or
	// moving the boat in a marina is not a passage.
	assistantPassagesMinDuration = 15 * time.Minute

	assistantPassagesDefaultDaysBack = 14
	assistantPassagesMaxDaysBack     = 60
	assistantPassagesDefaultLimit    = 5
	assistantPassagesMaxLimit        = 20
)

type detectedPassage struct {
	Start      time.Time
	End        time.Time
	DistanceNm float64
	MeanSOGKts float64
	MaxSOGKts  float64
}

// detectPassages turns time-ordered SOG points (m/s, each the mean of the
// bucket ending at its timestamp) into passages, oldest first.
func detectPassages(sog []telemetryPoint, bucket time.Duration) []detectedPassage {
	var out []detectedPassage
	var cur *detectedPassage
	var sumKts float64
	var n int
	var last time.Time

	flush := func() {
		if cur == nil {
			return
		}
		if cur.End.Sub(cur.Start) >= assistantPassagesMinDuration {
			cur.MeanSOGKts = sumKts / float64(n)
			out = append(out, *cur)
		}
		cur = nil
	}

	for _, p := range sog {
		kts := p.Value * metersPerSecondToKnots
		if kts < assistantPassagesMinSOGKts {
			continue
		}
		if cur != nil && p.Timestamp.Sub(last) > assistantPassagesMergeGap {
			flush()
		}
		if cur == nil {
			cur = &detectedPassage{Start: p.Timestamp.Add(-bucket)}
			sumKts, n = 0, 0
		}
		cur.End = p.Timestamp
		cur.DistanceNm += kts * bucket.Hours()
		cur.MaxSOGKts = math.Max(cur.MaxSOGKts, kts)
		sumKts += kts
		n++
		last = p.Timestamp
	}
	flush()
	return out
}

// mergeSOGSources folds the range query's output into one time-ordered
// series. The query does not group by source, so each receiver publishing
// SOG comes back as its own block; buckets sharing a timestamp are averaged.
func mergeSOGSources(points []telemetryPoint) []telemetryPoint {
	sorted := append([]telemetryPoint(nil), points...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Timestamp.Before(sorted[j].Timestamp) })
	var out []telemetryPoint
	var sum float64
	var n int
	for i, p := range sorted {
		sum += p.Value
		n++
		if i == len(sorted)-1 || !sorted[i+1].Timestamp.Equal(p.Timestamp) {
			out = append(out, telemetryPoint{Timestamp: p.Timestamp, Value: sum / float64(n)})
			sum, n = 0, 0
		}
	}
	return out
}

type assistantListPassagesArgs struct {
	DaysBack *int `json:"days_back"`
	Limit    *int `json:"limit"`
}

type assistantPassageJSON struct {
	Start         string  `json:"start"`
	End           string  `json:"end"`
	DurationHours float64 `json:"duration_hours"`
	DistanceNm    float64 `json:"distance_nm"`
	MeanSOGKts    float64 `json:"mean_sog_kts"`
	MaxSOGKts     float64 `json:"max_sog_kts"`
}

func (d assistantToolDeps) executeListPassages(ctx context.Context, raw json.RawMessage) (string, error) {
	var args assistantListPassagesArgs
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &args); err != nil {
			return "", fmt.Errorf("parse list_passages arguments: %w", err)
		}
	}
	days := assistantPassagesDefaultDaysBack
	if args.DaysBack != nil && *args.DaysBack > 0 {
		days = *args.DaysBack
	}
	if days > assistantPassagesMaxDaysBack {
		days = assistantPassagesMaxDaysBack
	}
	limit := assistantPassagesDefaultLimit
	if args.Limit != nil && *args.Limit > 0 {
		limit = *args.Limit
	}
	if limit > assistantPassagesMaxLimit {
		limit = assistantPassagesMaxLimit
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}

	stop := d.now()
	start := stop.AddDate(0, 0, -days)
	sog, err := d.influxRange("navigation.speedOverGround", start, stop, assistantPassagesEvery)
	if err != nil {
		return "", fmt.Errorf("list_passages: %w", err)
	}

	result := map[string]any{
		"window_start":      start.UTC().Format(time.RFC3339),
		"window_end":        stop.UTC().Format(time.RFC3339),
		"underway_above_kn": assistantPassagesMinSOGKts,
	}
	if len(sog) == 0 {
		result["status"] = "no_speed_data"
		result["note"] = fmt.Sprintf("No speed-over-ground was logged in the last %d days, so no passages can be found.", days)
		result["passages"] = []assistantPassageJSON{}
		result["found"] = 0
		b, _ := json.Marshal(result)
		return string(b), nil
	}

	found := detectPassages(mergeSOGSources(sog), assistantPassagesBucket)
	sort.Slice(found, func(i, j int) bool { return found[i].Start.After(found[j].Start) })
	total := len(found)
	if len(found) > limit {
		found = found[:limit]
	}
	passages := make([]assistantPassageJSON, 0, len(found))
	for _, p := range found {
		passages = append(passages, assistantPassageJSON{
			Start:         p.Start.UTC().Format(time.RFC3339),
			End:           p.End.UTC().Format(time.RFC3339),
			DurationHours: round1(p.End.Sub(p.Start).Hours()),
			DistanceNm:    round1(p.DistanceNm),
			MeanSOGKts:    round1(p.MeanSOGKts),
			MaxSOGKts:     round1(p.MaxSOGKts),
		})
	}
	result["passages"] = passages
	result["found"] = total
	result["note"] = "Most recent first. A passage is a run at or above the underway speed; slow-downs under 30 minutes are part of it. distance_nm is integrated from logged speed, so approximate."
	b, err := json.Marshal(result)
	if err != nil {
		return "", fmt.Errorf("list_passages: %w", err)
	}
	return string(b), nil
}
