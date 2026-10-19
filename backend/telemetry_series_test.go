package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
)

var seriesTestNow = time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)

// stubSeries wires the series handler to canned data. means maps a path to its
// bucketed means; a path absent from the map has no raw samples at all.
func stubSeries(t *testing.T, configured bool, means map[string][]telemetryPoint) {
	t.Helper()
	oldIO, oldCfg, oldNow := telemetrySeriesIO, telemetrySeriesConfigured, telemetrySeriesNow
	t.Cleanup(func() { telemetrySeriesIO, telemetrySeriesConfigured, telemetrySeriesNow = oldIO, oldCfg, oldNow })
	telemetrySeriesConfigured = func() bool { return configured }
	telemetrySeriesNow = func() time.Time { return seriesTestNow }
	telemetrySeriesIO = pathHistoryIO{
		stat: func(_ context.Context, path, _ string, _, _ time.Time, _, aggFn string) ([]telemetryPoint, error) {
			if aggFn != "mean" {
				t.Errorf("series must only query mean, got %q", aggFn)
			}
			return means[path], nil
		},
		firstLast: func(_ context.Context, path, _ string, _, _ time.Time) (time.Time, time.Time, bool, error) {
			t.Errorf("series must not run the first/last query (path %s)", path)
			return time.Time{}, time.Time{}, false, nil
		},
	}
}

func seriesGet(t *testing.T, paths []string, from, to string) *httptest.ResponseRecorder {
	t.Helper()
	q := url.Values{}
	for _, p := range paths {
		q.Add("path", p)
	}
	q.Set("from", from)
	q.Set("to", to)
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/telemetry/series?"+q.Encode(), nil)
	rec := httptest.NewRecorder()
	if err := telemetrySeriesHandler(e.NewContext(req, rec)); err != nil {
		t.Fatalf("handler: %v", err)
	}
	return rec
}

func TestTelemetrySeriesRejectsBadArgs(t *testing.T) {
	stubSeries(t, true, nil)
	const from, to = "2026-10-18T00:00:00Z", "2026-10-19T00:00:00Z"
	five := []string{"a.a", "b.b", "c.c", "d.d", "e.e"}
	cases := []struct {
		name     string
		paths    []string
		from, to string
	}{
		{"no paths", nil, from, to},
		{"five paths", five, from, to},
		{"blank path", []string{" "}, from, to},
		{"flux interpolation", []string{"${x}"}, from, to},
		{"bad from", []string{"a.b"}, "yesterday", to},
		{"bad to", []string{"a.b"}, from, ""},
		{"from equals to", []string{"a.b"}, from, from},
		{"from after to", []string{"a.b"}, to, from},
		{"span over 60 days", []string{"a.b"}, "2026-08-01T00:00:00Z", "2026-10-19T00:00:00Z"},
		{"to in the future", []string{"a.b"}, "2026-10-19T00:00:00Z", "2026-10-19T13:00:00Z"},
	}
	for _, tc := range cases {
		rec := seriesGet(t, tc.paths, tc.from, tc.to)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: expected 400, got %d (%s)", tc.name, rec.Code, rec.Body)
			continue
		}
		var body map[string]string
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || body["error"] == "" {
			t.Errorf("%s: expected a plain error message, got %s", tc.name, rec.Body)
		}
	}
}

func TestTelemetrySeriesRequiresInflux(t *testing.T) {
	stubSeries(t, false, nil)
	rec := seriesGet(t, []string{"a.b"}, "2026-10-18T00:00:00Z", "2026-10-19T00:00:00Z")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 without InfluxDB, got %d", rec.Code)
	}
}

func TestTelemetrySeriesBucketPerSpan(t *testing.T) {
	data := map[string][]telemetryPoint{"a.b": {{Timestamp: seriesTestNow.Add(-time.Hour), Value: 1}}}
	cases := []struct {
		from, to, want string
	}{
		{"2026-10-19T11:30:00Z", "2026-10-19T12:00:00Z", "1m"},
		{"2026-10-19T09:00:00Z", "2026-10-19T12:00:00Z", "1m"},
		{"2026-10-19T00:00:00Z", "2026-10-19T12:00:00Z", "2m"},
		{"2026-10-18T12:00:00Z", "2026-10-19T12:00:00Z", "5m"},
		{"2026-10-16T12:00:00Z", "2026-10-19T12:00:00Z", "15m"},
		{"2026-10-12T12:00:00Z", "2026-10-19T12:00:00Z", "30m"},
		{"2026-09-19T12:00:00Z", "2026-10-19T12:00:00Z", "2h"},
		{"2026-08-20T12:00:00Z", "2026-10-19T12:00:00Z", "6h"},
	}
	for _, tc := range cases {
		stubSeries(t, true, data)
		rec := seriesGet(t, []string{"a.b"}, tc.from, tc.to)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s..%s: %d %s", tc.from, tc.to, rec.Code, rec.Body)
		}
		var resp telemetrySeriesResponse
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		if resp.Bucket != tc.want {
			t.Errorf("%s..%s: bucket %q, want %q", tc.from, tc.to, resp.Bucket, tc.want)
		}
	}
}

func TestTelemetrySeriesMultiPathShape(t *testing.T) {
	t0 := time.Date(2026, 10, 18, 0, 0, 0, 0, time.UTC)
	stubSeries(t, true, map[string][]telemetryPoint{
		"electrical.batteries.house.voltage": {{Timestamp: t0, Value: 13.2}, {Timestamp: t0.Add(30 * time.Minute), Value: 13.4}},
		"environment.depth.belowKeel":        {{Timestamp: t0, Value: 4.5}},
	})
	rec := seriesGet(t, []string{"electrical.batteries.house.voltage", "environment.depth.belowKeel"},
		"2026-10-18T00:00:00Z", "2026-10-19T00:00:00Z")
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d %s", rec.Code, rec.Body)
	}
	var resp telemetrySeriesResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.From != "2026-10-18T00:00:00Z" || resp.To != "2026-10-19T00:00:00Z" || resp.Bucket != "5m" {
		t.Errorf("envelope: %+v", resp)
	}
	if len(resp.Series) != 2 || resp.Series[0].Path != "electrical.batteries.house.voltage" || resp.Series[1].Path != "environment.depth.belowKeel" {
		t.Fatalf("series order/paths: %+v", resp.Series)
	}
	v := resp.Series[0]
	if len(v.Points) != 2 || v.Points[1].T != "2026-10-18T00:30:00Z" || v.Points[1].Mean != 13.4 {
		t.Errorf("points: %+v", v.Points)
	}
	// Buckets cover the whole requested range, so the silence after the last
	// sample is reported as a gap running to "to".
	if n := len(v.Gaps); n == 0 || v.Gaps[n-1].From != "2026-10-18T00:35:00Z" || v.Gaps[n-1].To != "2026-10-19T00:00:00Z" {
		t.Errorf("gaps: %+v", v.Gaps)
	}
	if strings.Contains(rec.Body.String(), `"gaps":null`) {
		t.Errorf("gaps must be an array, got null: %s", rec.Body)
	}
}

func TestTelemetrySeriesNoDataIsPerSeries(t *testing.T) {
	stubSeries(t, true, map[string][]telemetryPoint{
		"a.ok": {{Timestamp: seriesTestNow.Add(-time.Hour), Value: 1}},
	})
	rec := seriesGet(t, []string{"a.ok", "b.never"}, "2026-10-18T12:00:00Z", "2026-10-19T12:00:00Z")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 with one series found, got %d %s", rec.Code, rec.Body)
	}
	var resp telemetrySeriesResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Series) != 2 || !resp.Series[0].Found || resp.Series[1].Found || resp.Series[1].Path != "b.never" {
		t.Fatalf("series: %+v", resp.Series)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"found":false`) || strings.Contains(body, `"points":null`) || strings.Contains(body, `"gaps":null`) {
		t.Errorf("a series without data needs found:false and empty arrays: %s", body)
	}
}

func TestTelemetrySeriesNoDataAnywhereIs404NamingEveryPath(t *testing.T) {
	stubSeries(t, true, nil)
	rec := seriesGet(t, []string{"b.never", "c.never"}, "2026-10-18T12:00:00Z", "2026-10-19T12:00:00Z")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "b.never") || !strings.Contains(body, "c.never") {
		t.Errorf("body must name the paths: %s", body)
	}
}

func TestTelemetrySeriesQueriesPathsConcurrentlyButBounded(t *testing.T) {
	stubSeries(t, true, nil)
	var mu sync.Mutex
	inflight, peak := 0, 0
	reached := make(chan struct{}, 8)
	release := make(chan struct{})
	telemetrySeriesIO.stat = func(ctx context.Context, _, _ string, _, _ time.Time, _, _ string) ([]telemetryPoint, error) {
		mu.Lock()
		inflight++
		if inflight > peak {
			peak = inflight
		}
		mu.Unlock()
		reached <- struct{}{}
		select {
		case <-release:
		case <-ctx.Done():
		}
		mu.Lock()
		inflight--
		mu.Unlock()
		return []telemetryPoint{{Timestamp: seriesTestNow.Add(-time.Hour), Value: 1}}, nil
	}
	go func() {
		// Wait until two paths are in flight at once, then let everything go.
		<-reached
		<-reached
		close(release)
	}()
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		done <- seriesGet(t, []string{"a.a", "b.b", "c.c", "d.d"}, "2026-10-18T12:00:00Z", "2026-10-19T12:00:00Z")
	}()
	select {
	case rec := <-done:
		if rec.Code != http.StatusOK {
			t.Fatalf("got %d %s", rec.Code, rec.Body)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("paths were queried one after another")
	}
	if peak < 2 || peak > telemetrySeriesMaxPaths {
		t.Errorf("peak concurrency %d, want 2..%d", peak, telemetrySeriesMaxPaths)
	}
}

func TestTelemetrySeriesDeadlineIs504(t *testing.T) {
	stubSeries(t, true, nil)
	old := telemetrySeriesTimeout
	telemetrySeriesTimeout = 50 * time.Millisecond
	t.Cleanup(func() { telemetrySeriesTimeout = old })
	telemetrySeriesIO.stat = func(ctx context.Context, _, _ string, _, _ time.Time, _, _ string) ([]telemetryPoint, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	rec := seriesGet(t, []string{"a.b"}, "2026-10-18T12:00:00Z", "2026-10-19T12:00:00Z")
	if rec.Code != http.StatusGatewayTimeout {
		t.Fatalf("expected 504, got %d %s", rec.Code, rec.Body)
	}
	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || body["error"] == "" {
		t.Errorf("expected a plain message: %s", rec.Body)
	}
}

func TestTelemetrySeriesReportsTheFirstErrorByPathOrder(t *testing.T) {
	stubSeries(t, true, nil)
	telemetrySeriesIO.stat = func(_ context.Context, path, _ string, _, _ time.Time, _, _ string) ([]telemetryPoint, error) {
		if path == "a.first" {
			time.Sleep(50 * time.Millisecond) // finishes after the other path has already failed
		}
		return nil, errors.New("boom " + path)
	}
	for i := 0; i < 3; i++ {
		rec := seriesGet(t, []string{"a.first", "b.second"}, "2026-10-18T12:00:00Z", "2026-10-19T12:00:00Z")
		if rec.Code != http.StatusBadGateway || !strings.Contains(rec.Body.String(), "boom a.first") {
			t.Fatalf("expected the error of the first path, got %d %s", rec.Code, rec.Body)
		}
	}
}

func TestTelemetrySeriesClampsToNow(t *testing.T) {
	stubSeries(t, true, nil)
	var gotStop time.Time
	telemetrySeriesIO.stat = func(_ context.Context, _, _ string, _, stop time.Time, _, _ string) ([]telemetryPoint, error) {
		gotStop = stop
		return []telemetryPoint{{Timestamp: seriesTestNow.Add(-30 * time.Minute), Value: 1}}, nil
	}
	rec := seriesGet(t, []string{"a.b"}, "2026-10-19T09:00:00Z", "2026-10-19T12:03:00Z")
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d %s", rec.Code, rec.Body)
	}
	var resp telemetrySeriesResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.To != "2026-10-19T12:00:00Z" {
		t.Errorf("to must report the clamped stop, got %s", resp.To)
	}
	if !gotStop.Equal(seriesTestNow) {
		t.Errorf("queried up to %v, want now", gotStop)
	}
	if g := resp.Series[0].Gaps; len(g) == 0 || g[len(g)-1].To != "2026-10-19T12:00:00Z" {
		t.Errorf("trailing gap must end at now: %+v", g)
	}
}

func TestTelemetrySeriesBucketWidthTargetsAtMost400Buckets(t *testing.T) {
	allowed := map[time.Duration]string{
		time.Minute: "1m", 2 * time.Minute: "2m", 5 * time.Minute: "5m", 10 * time.Minute: "10m",
		15 * time.Minute: "15m", 30 * time.Minute: "30m", time.Hour: "1h", 2 * time.Hour: "2h",
		3 * time.Hour: "3h", 6 * time.Hour: "6h",
	}
	for _, span := range []time.Duration{time.Minute, time.Hour, 7 * time.Hour, 24 * time.Hour, 3 * 24 * time.Hour, 30 * 24 * time.Hour, telemetrySeriesMaxSpan} {
		label, width := telemetrySeriesBucketWidth(span)
		if allowed[width] != label {
			t.Errorf("span %v: %q/%v is not on the allowlist", span, label, width)
		}
		if n := int((span + width - 1) / width); n > 400 {
			t.Errorf("span %v: %d buckets", span, n)
		}
	}
	if label, _ := telemetrySeriesBucketWidth(telemetrySeriesMaxSpan); label != "6h" {
		t.Errorf("60 days: %q, want 6h", label)
	}
}

func TestQueryPathHistoryBucketsRejectsUnknownAggregate(t *testing.T) {
	io := pathHistoryIO{
		stat: func(context.Context, string, string, time.Time, time.Time, string, string) ([]telemetryPoint, error) {
			t.Error("must not query")
			return nil, nil
		},
		firstLast: func(context.Context, string, string, time.Time, time.Time) (time.Time, time.Time, bool, error) {
			return seriesTestNow, seriesTestNow, true, nil
		},
	}
	_, err := queryPathHistoryBuckets(context.Background(), io, "a.b", "", seriesTestNow.Add(-time.Hour), seriesTestNow, []string{"median"}, false, assistantPathHistoryBucketWidth)
	if err == nil || !strings.Contains(err.Error(), "median") {
		t.Fatalf("expected an error naming the aggregate, got %v", err)
	}
}

func TestTelemetrySeriesInfluxErrorIs502(t *testing.T) {
	stubSeries(t, true, nil)
	telemetrySeriesIO.stat = func(context.Context, string, string, time.Time, time.Time, string, string) ([]telemetryPoint, error) {
		return nil, errors.New("connection refused")
	}
	rec := seriesGet(t, []string{"a.b"}, "2026-10-18T12:00:00Z", "2026-10-19T12:00:00Z")
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d", rec.Code)
	}
}
