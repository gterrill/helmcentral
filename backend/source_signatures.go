package main

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// source_signatures.go decides which SignalK instances are real ones of an
// equipment kind. A bus can publish a numbered instance that carries one stray
// figure (propulsion.0 with only exhaustTemperature) beside the instance that
// carries everything (propulsion.port): offering the first as "an engine" lets
// the operator assign an item to a source that no feature reads.
//
// The signature of a kind is the one or two leaves a feature depends on. An
// instance qualifies when any leaf of its kind's signature is in the live tree
// or has been logged to InfluxDB within sourceHistoryLookbackDays. Logged
// history is what lets an engine that is switched off, and so absent from the
// live tree, still be offered (and an existing assignment still be judged).

// sourceSignature is the signature of one kind. label is how the operator
// words it ("RPM"); leaves are alternatives, any one of which is enough, as
// paths below the source.
type sourceSignature struct {
	label  string
	leaves []string
}

// sourceSignatures is the signature of every kind discovery offers. Chosen
// from what the readers use and what this boat's real instances carry:
//
//	engine      revolutions: what makes it an engine (running detection, passage
//	            estimate, anomaly series); fuel.rate and runTime are supporting.
//	battery     voltage: every battery figure and alarm starts here.
//	alternator  current: the Alternators tile and the charging checks.
//	solar       panelPower, or power on controllers that name it that.
//	tank        currentLevel: the tank tile and level alarms.
//	generator   state, or runtime: the Generator tile and overnight model.
//	charger     current: charger output, shore-charger checks.
//	inverter    AC output power, under the three names inverters publish it.
var sourceSignatures = map[sourceGroup]sourceSignature{
	sourceEngine:     {"RPM", []string{"revolutions"}},
	sourceBattery:    {"voltage", []string{"voltage"}},
	sourceAlternator: {"output current", []string{"current"}},
	sourceSolar:      {"panel power", []string{"panelPower", "power"}},
	sourceTank:       {"level", []string{"currentLevel"}},
	sourceGenerator:  {"state", []string{"state", "runtime"}},
	sourceCharger:    {"output current", []string{"current"}},
	sourceInverter:   {"AC output power", []string{"ac.output.power", "acout.power", "acOutputPower"}},
}

// Signature states of a discovery row.
const (
	signatureCarried = "carried"
	signatureMissing = "missing"
	signatureUnknown = "unknown"
)

// sourceHistoryLookbackDays is how far back the one-time InfluxDB seed reads.
const sourceHistoryLookbackDays = 90

// sourceRecord is what Helmcentral has seen the boat report under the
// equipment sources (source_registry.go): the last time each path was seen
// (Last) and, per source, the last time anything under it was (Active). Since
// is when Helmcentral began watching this vessel, Seeded says the record was
// started from InfluxDB history, and Covers says the record spans enough time
// to call a source that never carried its signature "missing" (a seed, or
// sourceRecordMinTracking of watching).
//
// Active is what tells "missing" from "no record": propulsion.0 was seen
// carrying exhaust temperature and never revolutions, so it is alive and
// wrong; a source never seen at all (laid up, or not yet watched long enough)
// is merely silent and must never be flagged.
type sourceRecord struct {
	Last   map[string]time.Time
	Active map[string]time.Time
	Since  time.Time
	Seeded bool
	Covers bool
	// Err is why the record could not be read; while set nothing is served
	// from it and every unowned source judged against it is unknown.
	Err error
	// Now is when the record was built, which dates the listing lookback.
	Now time.Time
}

// sourceRecordMinTracking is how long Helmcentral must have watched a vessel
// without a seed before it will call a source "missing" its signature. An
// engine that is switched off for a few days would otherwise look like one
// that never reports RPM; a week of watching has normally seen the boat run.
const sourceRecordMinTracking = 7 * 24 * time.Hour

// sourceListLookback is how long ago a source that is not reporting now may
// have been seen and still be offered. A device that was replaced years ago
// should not be offered forever; judging a source missing or unknown may
// still use the whole record.
const sourceListLookback = 90 * 24 * time.Hour

// activeSources derives, per source, the latest time any of its paths was seen.
func activeSources(last map[string]time.Time) map[string]time.Time {
	active := map[string]time.Time{}
	for path, at := range last {
		source, ok := sourceOfPath(path)
		if !ok {
			continue
		}
		if prev, seen := active[source]; !seen || at.After(prev) {
			active[source] = at
		}
	}
	return active
}

// sourceOfPath is the source (propulsion.port, tanks.fuel.2) a path belongs
// to: the shortest leading part of the path that is one.
func sourceOfPath(path string) (string, bool) {
	parts := strings.Split(path, ".")
	for n := 2; n <= 3 && n < len(parts); n++ {
		prefix := strings.Join(parts[:n], ".")
		if _, _, ok := parseSource(prefix); ok {
			return prefix, true
		}
	}
	return "", false
}

// signaturePaths lists the signature paths of source.
func signaturePaths(group sourceGroup, source string) []string {
	sig := sourceSignatures[group]
	paths := make([]string, 0, len(sig.leaves))
	for _, leaf := range sig.leaves {
		paths = append(paths, source+"."+leaf)
	}
	return paths
}

// lastSeenAt is the latest time any signature path of source was seen.
func (h sourceRecord) lastSeenAt(group sourceGroup, source string) (time.Time, bool) {
	var latest time.Time
	found := false
	for _, p := range signaturePaths(group, source) {
		if at, ok := h.Last[p]; ok && (!found || at.After(latest)) {
			latest, found = at, true
		}
	}
	return latest, found
}

// seenInstances lists the instance ids of group whose signature path was seen
// within sourceListLookback of when the record was built (all of them when the
// record carries no build time).
func (h sourceRecord) seenInstances(group sourceGroup) []string {
	seen := map[string]bool{}
	var ids []string
	for path, at := range h.Last {
		if !h.Now.IsZero() && h.Now.Sub(at) > sourceListLookback {
			continue
		}
		for _, leaf := range sourceSignatures[group].leaves {
			rest, ok := strings.CutSuffix(path, "."+leaf)
			if !ok {
				continue
			}
			g, instance, ok := parseSource(rest)
			if ok && g == group && !seen[instance] {
				seen[instance] = true
				ids = append(ids, instance)
			}
		}
	}
	return ids
}

// liveCarriesSignature reports whether the live tree holds a signature path of
// source.
func liveCarriesSignature(t telemetryReader, group sourceGroup, source string) bool {
	for _, p := range signaturePaths(group, source) {
		if t.Sample(p).Present {
			return true
		}
	}
	return false
}

// signatureState judges an instance: carried when live or recorded; missing
// only when the record covers enough time and shows the source alive without
// ever carrying the signature; unknown otherwise (the record is too young, or
// the source was never seen, so nothing can be said against it).
func signatureState(live bool, h sourceRecord, group sourceGroup, source string) string {
	if live {
		return signatureCarried
	}
	if _, ok := h.lastSeenAt(group, source); ok {
		return signatureCarried
	}
	if h.Err != nil {
		return signatureUnknown
	}
	if _, active := h.Active[source]; active && h.Covers {
		return signatureMissing
	}
	return signatureUnknown
}

// historyFluxRegex is the anchored measurement regex matching every path under
// every equipment source shape (propulsion.<id>., electrical.batteries.<id>.,
// tanks.<category>.<id>. and so on). Reading every path, not only signature
// leaves, is what lets a source that is alive but never carried its signature
// be told from one that logged nothing.
func historyFluxRegex() string {
	var alts []string
	for _, group := range sourceGroupOrder {
		for _, s := range sourceShapes {
			if s.group == group {
				alts = append(alts, strings.TrimSuffix(strings.TrimPrefix(s.re.String(), "^"), "$"))
			}
		}
	}
	return `^(` + strings.Join(alts, "|") + `)\.`
}

// buildInfluxSourceHistoryFlux is the query for the last time each path under
// the equipment sources was logged. An anchored regex on the measurement
// pushes down to storage; on the boat it returns about 270 paths over 90 days
// in well under a second.
func buildInfluxSourceHistoryFlux(bucket, field string, lookbackDays int) (string, error) {
	bucketLiteral, err := fluxStringLiteral(bucket)
	if err != nil {
		return "", fmt.Errorf("bucket: %w", err)
	}
	fieldLiteral, err := fluxStringLiteral(field)
	if err != nil {
		return "", fmt.Errorf("field: %w", err)
	}
	// last() twice for the reason buildInfluxLastRecordedFlux explains: the
	// first per raw series (pushed down), the second per measurement after a
	// chronological sort.
	return fmt.Sprintf(
		"from(bucket: %s)\n"+
			"  |> range(start: -%dd)\n"+
			"  |> filter(fn: (r) => r._field == %s and r._measurement =~ /%s/)\n"+
			"  |> last()\n"+
			// _value is dropped before the group(), as in the earliest
			// query: a path logged as both float and string would collide.
			"  |> keep(columns: [\"_measurement\", \"_time\"])\n"+
			"  |> group(columns: [\"_measurement\"])\n"+
			"  |> sort(columns: [\"_time\"])\n"+
			"  |> last(column: \"_time\")",
		bucketLiteral, lookbackDays, fieldLiteral, historyFluxRegex(),
	), nil
}

// buildInfluxSourceEarliestFlux finds the oldest equipment point inside the
// lookback: how far back the seed's knowledge of the boat reaches.
func buildInfluxSourceEarliestFlux(bucket, field string, lookbackDays int) (string, error) {
	bucketLiteral, err := fluxStringLiteral(bucket)
	if err != nil {
		return "", fmt.Errorf("bucket: %w", err)
	}
	fieldLiteral, err := fluxStringLiteral(field)
	if err != nil {
		return "", fmt.Errorf("field: %w", err)
	}
	return fmt.Sprintf(
		"from(bucket: %s)\n"+
			"  |> range(start: -%dd)\n"+
			"  |> filter(fn: (r) => r._field == %s and r._measurement =~ /%s/)\n"+
			"  |> first()\n"+
			// Only _time crosses the group(): equipment paths hold both
			// float and string values (an engine's state), and one table
			// cannot hold _value as both.
			"  |> keep(columns: [\"_time\"])\n"+
			"  |> group()\n"+
			"  |> sort(columns: [\"_time\"])\n"+
			"  |> first(column: \"_time\")",
		bucketLiteral, lookbackDays, fieldLiteral, historyFluxRegex(),
	), nil
}

// sourceHistoryTimeout bounds the one seed query.
const sourceHistoryTimeout = 8 * time.Second

// currentSourceRecord is what Helmcentral has seen the boat report. It is a
// variable so handler tests can supply a record without a running registry.
var currentSourceRecord = func() sourceRecord {
	if globalSourceRegistry == nil {
		return sourceRecord{}
	}
	return globalSourceRegistry.record()
}

// queryInfluxSourceHistory returns the last time each path under the equipment
// sources was logged within sourceHistoryLookbackDays, and the time of the
// oldest such point. It runs once per vessel, to seed the registry, and never
// at request time.
func queryInfluxSourceHistory() (sourceHistory, error) {
	client, org, bucket, ok := newInfluxClient()
	if !ok {
		return sourceHistory{}, fmt.Errorf("influxdb is not configured")
	}
	field := trimEnvValue(getEnv("INFLUX_DEPTH_FIELD", "value"))
	flux, err := buildInfluxSourceHistoryFlux(bucket, field, sourceHistoryLookbackDays)
	if err != nil {
		return sourceHistory{}, err
	}
	earliestFlux, err := buildInfluxSourceEarliestFlux(bucket, field, sourceHistoryLookbackDays)
	if err != nil {
		return sourceHistory{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), sourceHistoryTimeout)
	defer cancel()
	result, err := client.QueryAPI(org).Query(ctx, flux)
	if err != nil {
		return sourceHistory{}, err
	}
	defer result.Close()
	out := sourceHistory{Last: map[string]time.Time{}}
	for result.Next() {
		rec := result.Record()
		out.Last[rec.Measurement()] = rec.Time()
	}
	if result.Err() != nil {
		return sourceHistory{}, result.Err()
	}
	if len(out.Last) == 0 {
		return out, nil
	}
	first, err := client.QueryAPI(org).Query(ctx, earliestFlux)
	if err != nil {
		return sourceHistory{}, fmt.Errorf("earliest equipment point: %w", err)
	}
	defer first.Close()
	for first.Next() {
		out.Earliest = first.Record().Time()
	}
	if first.Err() != nil {
		return sourceHistory{}, fmt.Errorf("earliest equipment point: %w", first.Err())
	}
	return out, nil
}
