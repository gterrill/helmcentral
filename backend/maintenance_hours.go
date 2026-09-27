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
// second way to ask "what does this path say and how old is that".
//
// Amendment, 2026-09-27: an hour meter is not like an ordinary telemetry
// path. A depth or wind reading going quiet for two minutes means the
// sensor or feed died - the boat's depth didn't stop existing. An engine's
// runtime counter, by contrast, is EXPECTED to stop publishing the instant
// the engine is switched off, because there is nothing left to count: the
// meter does not "go stale," it correctly stops moving. Treating that as
// "hours unknown" (the behaviour this file had before this amendment) made
// every hours-based rule read unknown at anchor and while the engine was
// off for its own service - precisely when an operator is most likely to
// be looking. The last value this path ever reported is therefore always
// the current reading, however old; only its age is worth telling the
// operator (currentEquipmentHours' own AsOf), never used to discard it.

// hourMeterReset is one row of hour_meter_resets: an operator-recorded
// meter replacement (maintenance_store.go). OldReading is the true hours
// the superseded meter/gauge showed at the moment of replacement;
// NewReading is what the fresh meter itself reads at that same moment
// (typically 0, but not assumed to be - a replacement with a used meter is
// a real case). ChangedAt is the date the replacement actually happened,
// which is what "in force at" means for offsetInForceAt below; CreatedAt
// is only when the row was entered into Helmcentral, which a back-filled
// reset can postdate by months and plays no part in ordering.
type hourMeterReset struct {
	ID         string    `json:"id"`
	OldReading float64   `json:"old_reading"`
	NewReading float64   `json:"new_reading"`
	ChangedAt  string    `json:"changed_at"`
	CreatedAt  time.Time `json:"created_at"`
}

// offsetInForceAt is the figure that turns a GAUGE (raw meter) reading FROM
// atDate into true hours: true = gauge + offsetInForceAt(resets, atDate).
// Only the most recent reset THAT HAD ALREADY HAPPENED by atDate counts,
// never a sum of every reset ever recorded and never one dated after
// atDate - the operator's own OldReading at each replacement already
// states the cumulative true hours at that moment (it is what the
// superseded meter was showing, which already reflects every earlier
// replacement), so offset = reset.OldReading - reset.NewReading is the
// whole answer once the right reset is picked; at the instant of
// replacement raw_live == NewReading, so true_hours_at_replacement ==
// OldReading by construction.
//
// This is the ONE place gauge and true hours convert into each other
// (2026-09-27 amendment below) - a live reading (atDate = now/today) and a
// back-filled completion or "set last done" (atDate = whatever date the
// operator actually gave) share this same function so the two can never
// quietly disagree about which offset a given date should use.
//
// resets is trusted to already be ChangedAt-descending, most-recent-first
// - ListHourMeterResets' own contract (`ORDER BY changed_at DESC, id
// DESC`) - so the first entry whose ChangedAt is on or before atDate IS
// the answer; this is a single forward scan, never a second sort or
// pairwise comparison over a list the store already ordered correctly.
// ChangedAt/atDate are compared as YYYY-MM-DD strings - installDatePattern
// guarantees that shape for every ChangedAt on file, and ISO date strings
// of the same length sort lexicographically exactly the way they sort
// chronologically, the same idiom maintenance_store.go's own
// CompleteMaintenanceRule uses for its baseline-only-moves-forward check.
func offsetInForceAt(resets []hourMeterReset, atDate time.Time) float64 {
	cutoff := atDate.Format("2006-01-02")
	for _, r := range resets {
		if r.ChangedAt <= cutoff {
			return r.OldReading - r.NewReading
		}
	}
	return 0
}

// gaugeToTrueHours is offsetInForceAt spelled out at its one call shape:
// what every write of an operator-typed hours figure (completeMaintenanceRuleHandler,
// setMaintenanceRuleLastDoneHandler, the standalone log entry handlers -
// maintenance_handlers.go) actually wants. The operator always types a
// GAUGE (raw meter) reading; every stored hours figure the status engine
// later compares (last_done_hours, and a log entry's own hours next to it)
// is always TRUE hours - this is the one conversion between the two.
func gaugeToTrueHours(resets []hourMeterReset, gauge float64, atDate time.Time) float64 {
	return gauge + offsetInForceAt(resets, atDate)
}

// currentEquipmentHours resolves path's live value (SignalK runTime is
// published in seconds - converted to hours here) into the
// maintenanceHourReading every rule on this item shares: Gauge is that raw
// conversion, Hours is Gauge plus offsetHours (this item's own
// offsetInForceAt at "now") - true hours. Known is false only when there is
// nothing here for the engine to read live at all:
//
//   - path is blank: no hour_meter_path is bound on this item - the
//     operator records readings manually instead (spec §2).
//   - the bound path has never carried a value in this snapshot.
//
// A value that IS present is Known regardless of its age (this file's own
// 2026-09-27 amendment) - AsOf carries how long ago it was received, when
// the snapshot has usable timestamp evidence for it (pathAge returning -1
// is only reachable for a hand-built fixture missing every timestamp; a
// real SignalK delta always carries one), so the frontend can show "as of
// <time>" without the reading itself being discarded.
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

	gauge := sample.Value / 3600.0
	reading := maintenanceHourReading{Known: true, Hours: gauge + offsetHours, Gauge: gauge}
	if age := pathAge(snapshot, sample, trimmed, now); age >= 0 {
		asOf := now.Add(-time.Duration(age * float64(time.Second)))
		reading.AsOf = &asOf
	}
	return reading
}
