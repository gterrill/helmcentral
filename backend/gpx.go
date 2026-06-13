package main

import "time"

// Legacy no-op: GPX file ingestion has been removed in favor of SignalK tracks plugin data.
func queryGPXPositionTrailRange(start, end time.Time) []trailPoint {
	return nil
}
