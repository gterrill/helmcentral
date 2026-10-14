package main

// snapshotAlarmReader reads arbitrary dotted SignalK paths out of the delta
// stream snapshot (ADR 0037).
//
// This is the point of the stream work: rules can name any path the server
// publishes, not only the handful the fetchers were hand-wired to.
func snapshotAlarmReader(snapshot *signalKSnapshot) alarmReader {
	return alarmReaderFromView(snapshot.selfTelemetry())
}

// alarmReaderFromView is snapshotAlarmReader over a telemetry view the
// caller already holds, so a caller that also needs the view for something
// else (computeDerivedPaths finds fuel paths in it; buildGaugeValuesPayload
// shares one with it) does not pay for a second whole-tree copy
// (backend-perf-audit.md Tier 1 #2). The view does the tree walk; this only
// narrows a leaf to the numeric sample a rule evaluates (ADR 0178).
func alarmReaderFromView(t telemetryReader) alarmReader {
	return func(path string) alarmSample {
		sample := t.Sample(path)
		if !sample.Present {
			return alarmSample{}
		}

		// A leaf can exist but hold null: the Victron alternator.0 service
		// publishes that for voltage/current while the engine is off. Only a
		// numeric leaf counts as present, so null never reads as -1.
		value, ok := numericTelemetryValue(sample.Value)
		if !ok {
			return alarmSample{}
		}

		// LastSeen stays arrival time: the stale-data check has always timed
		// when a leaf last arrived, not the source's own clock.
		return alarmSample{Value: value, Present: true, LastSeen: sample.Received}
	}
}
