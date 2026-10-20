package main

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
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

// sourceHistoryLookbackDays is how far back logged history counts.
const sourceHistoryLookbackDays = 90

// sourceHistory is what the boat has logged under the equipment groups: the
// last time each path was recorded (Last), and, per source, the last time
// anything under it was recorded (Active). Err is set when it could not be
// read, in which case nothing may be concluded from the rest.
//
// Active is what tells "missing" from "no recent data": propulsion.0 logged
// exhaust temperature and never revolutions, so it is alive and wrong; a
// source with nothing logged at all (laid up, or a short retention) is merely
// silent and must never be flagged.
type sourceHistory struct {
	Last   map[string]time.Time
	Active map[string]time.Time
	Err    error
}

// newSourceHistory builds the history from the last-logged time of each path.
func newSourceHistory(last map[string]time.Time) sourceHistory {
	h := sourceHistory{Last: last, Active: map[string]time.Time{}}
	for path, at := range last {
		source, ok := sourceOfPath(path)
		if !ok {
			continue
		}
		if prev, seen := h.Active[source]; !seen || at.After(prev) {
			h.Active[source] = at
		}
	}
	return h
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

// lastLogged is the latest time any signature path of source was logged.
func (h sourceHistory) lastLogged(group sourceGroup, source string) (time.Time, bool) {
	var latest time.Time
	found := false
	for _, p := range signaturePaths(group, source) {
		if at, ok := h.Last[p]; ok && (!found || at.After(latest)) {
			latest, found = at, true
		}
	}
	return latest, found
}

// loggedInstances lists the instance ids of group that have a logged
// signature path.
func (h sourceHistory) loggedInstances(group sourceGroup) []string {
	seen := map[string]bool{}
	var ids []string
	for path := range h.Last {
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

// signatureState judges an instance: carried when live or logged; missing only
// when history was read and shows the source alive without ever carrying the
// signature; unknown when history could not be read, or the source logged
// nothing at all (no recent data, so nothing can be said against it).
func signatureState(live bool, h sourceHistory, group sourceGroup, source string) string {
	if live {
		return signatureCarried
	}
	if h.Err != nil {
		return signatureUnknown
	}
	if _, ok := h.lastLogged(group, source); ok {
		return signatureCarried
	}
	if _, active := h.Active[source]; active {
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
			"  |> group(columns: [\"_measurement\"])\n"+
			"  |> sort(columns: [\"_time\"])\n"+
			"  |> last()\n"+
			"  |> keep(columns: [\"_measurement\", \"_time\"])",
		bucketLiteral, lookbackDays, fieldLiteral, historyFluxRegex(),
	), nil
}

const (
	// sourceHistoryTTL is how long a successful read is reused. Discovery and
	// the setup status are read together and often.
	sourceHistoryTTL = time.Minute
	// sourceHistoryFailureTTL is how long a failed read is reused, so an
	// InfluxDB that is down is asked about twice a minute, not on every request.
	sourceHistoryFailureTTL = 30 * time.Second
	// sourceHistoryTimeout bounds one query.
	sourceHistoryTimeout = 8 * time.Second
)

// sourceHistoryCall is one query in flight; its waiters receive result when
// done closes.
type sourceHistoryCall struct {
	done   chan struct{}
	result sourceHistory
}

var sourceHistoryState struct {
	mu       sync.Mutex
	at       time.Time
	have     bool
	result   sourceHistory
	inflight *sourceHistoryCall
	failing  bool
}

// fetchSourceHistory and sourceHistoryNow are variables so tests can supply a
// blocking or failing loader and a clock.
var (
	fetchSourceHistory = queryInfluxSourceHistory
	sourceHistoryNow   = time.Now
)

// loadSourceHistory reads the logged history. It is a variable so handler
// tests can supply history without an InfluxDB. A failure is returned in Err,
// never replaced by an empty history.
var loadSourceHistory = readSourceHistory

// readSourceHistory is single-flight: one query at a time, the other callers
// wait for its result. A success is reused for sourceHistoryTTL and a failure
// for sourceHistoryFailureTTL. The mutex is never held across the query. A
// failure is logged once per streak, and recovery once.
func readSourceHistory() sourceHistory {
	st := &sourceHistoryState
	st.mu.Lock()
	if st.have {
		ttl := sourceHistoryTTL
		if st.result.Err != nil {
			ttl = sourceHistoryFailureTTL
		}
		if sourceHistoryNow().Sub(st.at) < ttl {
			r := st.result
			st.mu.Unlock()
			return r
		}
	}
	if call := st.inflight; call != nil {
		st.mu.Unlock()
		<-call.done
		return call.result
	}
	call := &sourceHistoryCall{done: make(chan struct{})}
	st.inflight = call
	st.mu.Unlock()

	last, err := fetchSourceHistory()
	var result sourceHistory
	if err != nil {
		result = sourceHistory{Err: err}
	} else {
		result = newSourceHistory(last)
	}

	st.mu.Lock()
	if err != nil && !st.failing {
		log.Printf("source history: %v; Discovery lists only sources reporting now", err)
	}
	if err == nil && st.failing {
		log.Printf("source history: logged history is readable again")
	}
	st.failing = err != nil
	st.at, st.have, st.result, st.inflight = sourceHistoryNow(), true, result, nil
	st.mu.Unlock()
	call.result = result
	close(call.done)
	return result
}

// queryInfluxSourceHistory returns the last time each path under the equipment
// sources was logged within sourceHistoryLookbackDays.
func queryInfluxSourceHistory() (map[string]time.Time, error) {
	client, org, bucket, ok := newInfluxClient()
	if !ok {
		return nil, fmt.Errorf("influxdb is not configured")
	}
	field := trimEnvValue(getEnv("INFLUX_DEPTH_FIELD", "value"))
	flux, err := buildInfluxSourceHistoryFlux(bucket, field, sourceHistoryLookbackDays)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), sourceHistoryTimeout)
	defer cancel()
	result, err := client.QueryAPI(org).Query(ctx, flux)
	if err != nil {
		return nil, err
	}
	defer result.Close()
	last := map[string]time.Time{}
	for result.Next() {
		rec := result.Record()
		last[rec.Measurement()] = rec.Time()
	}
	if result.Err() != nil {
		return nil, result.Err()
	}
	return last, nil
}
