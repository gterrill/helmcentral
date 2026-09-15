package main

import (
	"encoding/json"
	"io"
	"net/http"
)

// accessLogSkipWriter wraps the real log-capture writer and drops
// successful, low-signal access-log lines before they ever reach it: GET or
// HEAD requests that came back 2xx or 304. Those are exactly what the
// boat's access log was mostly made of - every tile, poll and asset request
// - and measured at two thirds of /api/logs/stream's bytes (backend perf
// audit Tier 3), flooding both the 2,000-entry ring buffer and the stream
// with lines an operator investigating a real problem gets no value from.
// Everything else - any error status, or any non-GET/HEAD method - still
// gets through unconditionally.
//
// middleware.LoggerConfig's own Skipper runs before next(c) executes
// (labstack/echo/v4/middleware/logger.go), so it cannot see the response
// status - by the time a request is known to be a successful GET, the real
// logger middleware has already run next(c), computed status/latency, and
// formatted its line, then calls Output.Write with the result. Wrapping
// Output instead of trying to use Skipper (main.go's middleware.
// LoggerWithConfig call) is what lets this decide after the handler runs.
// It parses just the two fields it needs from that line rather than
// reimplementing the formatter, which happens to work because
// DefaultLoggerConfig.Format's output is genuine JSON per line (see its own
// doc comment: "logs in JSON format").
type accessLogSkipWriter struct {
	underlying io.Writer
}

func (w accessLogSkipWriter) Write(p []byte) (int, error) {
	var line struct {
		Method string `json:"method"`
		Status int    `json:"status"`
	}
	// A line that fails to decode is forwarded rather than dropped: losing
	// log lines by accident (a future Format change this no longer
	// understands) is worse than a few extra noisy ones getting through,
	// per the fallback policy - this must never be the thing that silently
	// eats real log output.
	if err := json.Unmarshal(p, &line); err == nil {
		successful := line.Status == http.StatusNotModified || (line.Status >= 200 && line.Status < 300)
		getOrHead := line.Method == http.MethodGet || line.Method == http.MethodHead
		if successful && getOrHead {
			// Swallowed: report the full length written so the real Echo
			// logger middleware, which only checks the returned error, never
			// sees what looks like a short write.
			return len(p), nil
		}
	}
	return w.underlying.Write(p)
}
