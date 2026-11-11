package main

import (
	"context"
	"os"
	"testing"
	"time"
)

// swapNormalMinutesQuery replaces the one-chunk Influx read for a test and
// returns the function that restores it. The connection is a stand-in too, so a
// test needs no InfluxDB settings.
func swapNormalMinutesQuery(fn func(ctx context.Context, src normalSource, paths []string, from, to time.Time) ([]normalMinuteRow, error)) func() {
	oldRun, oldOpen := runNormalMinutesQuery, openNormalInflux
	openNormalInflux = func(string) (normalInflux, error) { return normalInflux{}, nil }
	runNormalMinutesQuery = func(ctx context.Context, _ normalInflux, src normalSource, paths []string, from, to time.Time) ([]normalMinuteRow, error) {
		return fn(ctx, src, paths, from, to)
	}
	return func() { runNormalMinutesQuery, openNormalInflux = oldRun, oldOpen }
}

// minutesFrom answers a multi-path read from per-path rows in [start, stop).
func minutesFrom(feed map[string][]normalMinuteRow, paths []string, start, stop time.Time) map[string][]normalMinuteRow {
	out := map[string][]normalMinuteRow{}
	for _, p := range paths {
		for _, r := range feed[p] {
			if !r.At.Before(start) && r.At.Before(stop) {
				out[p] = append(out[p], r)
			}
		}
	}
	return out
}

func writeNormalTestFile(path, content string) error {
	return os.WriteFile(path, []byte(content), 0o644)
}

func readNormalTestFile(path string) (string, error) {
	b, err := os.ReadFile(path)
	return string(b), err
}

// scoreOnly scores one subsystem alone, as the report and the tool do for the
// subsystems a boat has: the inputs are read, then the subsystem is judged.
func scoreOnly(ctx context.Context, sub normalSubsystem, deps normalScoreDeps, since, until time.Time, subjectFilter string, answers []normalAnswer) unusualResult {
	res := newUnusualResult(since, until)
	in, kind, err := loadUnusualInputs(deps)
	if err != nil {
		res.ProblemKind, res.Problem = kind, firstErrorLine(err)
		return res
	}
	return scoreSubsystem(ctx, sub, deps, in, res, subjectFilter, answers)
}

// withVessel makes the vessel the boat has, for the checks of ignored sensors.
func withVessel(t *testing.T, v vesselView) {
	t.Helper()
	old := normalPathVessel
	normalPathVessel = func() (vesselView, error) { return v, nil }
	t.Cleanup(func() { normalPathVessel = old })
}
