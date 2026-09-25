package main

import (
	"bufio"
	"encoding/json"
	"os"
	"testing"
	"time"
)

// anomalyDeltaFixturePath is the ~10 minute real delta stream captured off
// Pikorua on 2026-09-25, carrying $source for battery.512, vebus.276 and
// alternator.0/1 (see AGENTS.md's Fallback/Test-First policies: fixtures
// used for behaviour assertions must be captured from the real server, not
// assumed).
const anomalyDeltaFixturePath = "testdata/anomaly/signalk-deltas-2026-09-25.ndjson"

// replayAnomalyDeltaFixture reads the captured NDJSON delta stream line by
// line and applies each delta to a fresh snapshot at that delta's own
// carried timestamp (not wall-clock), the same way signalk_stream.go
// consumes a live connection's hello frame and deltas. It returns the
// populated snapshot plus the latest timestamp seen across every delta, so
// a caller can evaluate a check "as of" the fixture's own last moment
// rather than the real now().
func replayAnomalyDeltaFixture(t *testing.T) (snapshot *signalKSnapshot, lastAt time.Time) {
	t.Helper()

	f, err := os.Open(anomalyDeltaFixturePath)
	if err != nil {
		t.Fatalf("opening %s: %v", anomalyDeltaFixturePath, err)
	}
	defer f.Close()

	snapshot = newSignalKSnapshot()
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var delta signalKDelta
		if err := json.Unmarshal(line, &delta); err != nil {
			t.Fatalf("parsing delta line: %v", err)
		}

		if delta.Self != "" {
			snapshot.setSelfContext(delta.Self)
			continue
		}
		if len(delta.Updates) == 0 {
			continue
		}

		at := lastAt
		for _, u := range delta.Updates {
			if u.Timestamp == "" {
				continue
			}
			parsed, err := time.Parse(time.RFC3339, u.Timestamp)
			if err != nil {
				continue
			}
			if parsed.After(at) {
				at = parsed
			}
		}
		if at.IsZero() {
			t.Fatalf("delta line carried no usable timestamp: %s", line)
		}

		snapshot.applyDelta(delta, at)
		if at.After(lastAt) {
			lastAt = at
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scanning %s: %v", anomalyDeltaFixturePath, err)
	}
	if lastAt.IsZero() {
		t.Fatalf("fixture carried no timestamped deltas")
	}

	return snapshot, lastAt
}
