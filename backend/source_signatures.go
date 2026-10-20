package main

import (
	"context"
	"fmt"
	"log"
	"regexp"
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

// sourceHistory is what the boat has logged for the signature paths: the last
// time each was recorded. Err is set when it could not be read, in which case
// nothing may be concluded from Last.
type sourceHistory struct {
	Last map[string]time.Time
	Err  error
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

// signatureState judges an instance: carried when live or logged, missing when
// history was read and holds none, unknown when history could not be read and
// the live tree does not carry it.
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
	return signatureMissing
}

// signatureFluxRegex is the anchored measurement regex matching every
// signature path of every kind.
func signatureFluxRegex() string {
	var alts []string
	for _, group := range sourceGroupOrder {
		var shape *regexp.Regexp
		for _, s := range sourceShapes {
			if s.group == group {
				shape = s.re
			}
		}
		prefix := strings.TrimSuffix(strings.TrimPrefix(shape.String(), "^"), "$")
		var leaves []string
		for _, leaf := range sourceSignatures[group].leaves {
			leaves = append(leaves, regexp.QuoteMeta(leaf))
		}
		alts = append(alts, prefix+`\.(`+strings.Join(leaves, "|")+")")
	}
	return "^(" + strings.Join(alts, "|") + ")$"
}

// buildInfluxSourceSignatureFlux is the query for the last time each signature
// path was logged. An anchored regex on the measurement pushes down to
// storage, so it stays cheap over the whole lookback (about half a second on
// the boat, 90 days).
func buildInfluxSourceSignatureFlux(bucket, field string, lookbackDays int) (string, error) {
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
		bucketLiteral, lookbackDays, fieldLiteral, signatureFluxRegex(),
	), nil
}

// sourceHistoryTTL is how long a successful read of the history is reused.
// Discovery and the setup status are read together and often; the answer
// changes only when an instrument first logs a signature path.
const sourceHistoryTTL = time.Minute

var sourceHistoryCache struct {
	mu   sync.Mutex
	at   time.Time
	last map[string]time.Time
}

// loadSourceHistory reads the logged signature history. It is a variable so
// handler tests can supply history without an InfluxDB. A failure is returned
// in Err, never replaced by an empty history, and is not cached.
var loadSourceHistory = readSourceHistory

func readSourceHistory() sourceHistory {
	sourceHistoryCache.mu.Lock()
	defer sourceHistoryCache.mu.Unlock()
	if sourceHistoryCache.last != nil && time.Since(sourceHistoryCache.at) < sourceHistoryTTL {
		return sourceHistory{Last: sourceHistoryCache.last}
	}
	last, err := queryInfluxSourceHistory()
	if err != nil {
		log.Printf("source history: %v", err)
		return sourceHistory{Err: err}
	}
	sourceHistoryCache.at, sourceHistoryCache.last = time.Now(), last
	return sourceHistory{Last: last}
}

// queryInfluxSourceHistory returns the last time each signature path was
// logged within sourceHistoryLookbackDays.
func queryInfluxSourceHistory() (map[string]time.Time, error) {
	client, org, bucket, ok := newInfluxClient()
	if !ok {
		return nil, fmt.Errorf("influxdb is not configured")
	}
	field := trimEnvValue(getEnv("INFLUX_DEPTH_FIELD", "value"))
	flux, err := buildInfluxSourceSignatureFlux(bucket, field, sourceHistoryLookbackDays)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
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
