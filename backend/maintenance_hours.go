package main

import (
	"strings"
	"time"
)

// This file resolves the one live figure the maintenance engine
// (maintenance_status.go) needs but cannot compute itself: an item's
// current true running hours. It reuses the exact same snapshot-reading
// machinery Mate's diagnostics tools already read a path through (ADR
// 0131, alarm_reader.go/signalk_paths.go's pathAge) rather than inventing a
// second way to ask "what does this path say and how old is that" -
// AGENTS.md fail-fast: a bound path with a missing or stale value is
// reported as unknown, never guessed at from whatever was last seen.

// maintenanceHoursStaleAfter is the same staleness threshold Mate's own
// diagnostics tools and every derived path already use
// (assistantDiagnosticsStaleAfter, derivedInputMaxAge) - reused rather than
// picked afresh, so "hours unknown" here can never disagree with what a
// tile bound to the same path already shows as stale.
const maintenanceHoursStaleAfter = derivedInputMaxAge

// hourMeterReset is one row of hour_meter_resets: an operator-recorded
// meter replacement (maintenance_store.go). OldReading is the true hours
// the superseded meter/gauge showed at the moment of replacement;
// NewReading is what the fresh meter itself reads at that same moment
// (typically 0, but not assumed to be - a replacement with a used meter is
// a real case).
type hourMeterReset struct {
	OldReading float64   `json:"old_reading"`
	NewReading float64   `json:"new_reading"`
	ChangedAt  string    `json:"changed_at"`
	CreatedAt  time.Time `json:"created_at"`
}

// latestMeterOffsetHours is the figure added to the live meter's own raw
// reading so true hours keep counting across a replacement. Only the MOST
// RECENT reset matters, never a sum of every reset ever recorded: the
// operator's own OldReading at each replacement already states the
// cumulative true hours at that moment (it is what the superseded meter
// was showing, which already reflects every earlier replacement), so
// offset = latest.OldReading - latest.NewReading is the whole answer -
// true_hours = raw_live + offset, and at the instant of replacement
// raw_live == NewReading, so true_hours_at_replacement == OldReading by
// construction.
func latestMeterOffsetHours(resets []hourMeterReset) float64 {
	if len(resets) == 0 {
		return 0
	}
	latest := resets[0]
	for _, r := range resets[1:] {
		if r.CreatedAt.After(latest.CreatedAt) {
			latest = r
		}
	}
	return latest.OldReading - latest.NewReading
}

// currentEquipmentHours resolves path's live value (SignalK runTime is
// published in seconds - converted to hours here) plus offsetHours (this
// item's own latestMeterOffsetHours) into the maintenanceHourReading every
// rule on this item shares. Known is false whenever the figure cannot be
// trusted at all:
//
//   - path is blank: no hour_meter_path is bound on this item - the
//     operator records readings manually instead (spec §2), and there is
//     nothing here for the engine to read live.
//   - the bound path has never carried a value in this snapshot.
//   - the bound path's value IS present but its age exceeds
//     maintenanceHoursStaleAfter - stale, not guessed at.
//
// reader and snapshot are both injectable (the same idiom
// assistantToolDeps.signalKDiagnostics uses) so this is testable against a
// fake reader with no live SignalK connection at all; production always
// passes snapshotAlarmReader(globalSignalKSnapshot) and
// globalSignalKSnapshot itself.
func currentEquipmentHours(reader alarmReader, snapshot *signalKSnapshot, path string, offsetHours float64, now time.Time) maintenanceHourReading {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return maintenanceHourReading{}
	}

	sample := reader(trimmed)
	if !sample.Present {
		return maintenanceHourReading{}
	}

	age := pathAge(snapshot, sample, trimmed, now)
	if age < 0 {
		return maintenanceHourReading{}
	}
	if age > maintenanceHoursStaleAfter.Seconds() {
		lastGood := now.Add(-time.Duration(age * float64(time.Second)))
		return maintenanceHourReading{StaleSince: &lastGood}
	}

	hours := sample.Value/3600.0 + offsetHours
	return maintenanceHourReading{Known: true, Hours: hours}
}
