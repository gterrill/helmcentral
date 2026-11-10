package main

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

var minutesStart = time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)

func TestBuildNormalMinutesFlux_OneQueryForSeveralReadingsKeepingTheMeasurementAndSource(t *testing.T) {
	flux, err := buildNormalMinutesFlux("SignalK_1m", "value", []string{"propulsion.port.oilPressure", "propulsion.port.temperature"}, minutesStart, minutesStart.Add(24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`from(bucket: "SignalK_1m")`,
		// An OR chain of equalities is pushed down to storage. contains() is not.
		`filter(fn: (r) => (r._measurement == "propulsion.port.oilPressure" or r._measurement == "propulsion.port.temperature") and r._field == "value")`,
		`aggregateWindow(every: 1m, fn: mean, createEmpty: false, timeSrc: "_start")`,
		`keep(columns: ["_time", "_value", "_measurement", "source"])`,
		`time(v: "2026-10-01T00:00:00Z")`,
		`time(v: "2026-10-02T00:00:00Z")`,
	} {
		if !strings.Contains(flux, want) {
			t.Errorf("flux lacks %s:\n%s", want, flux)
		}
	}
	if strings.Contains(flux, "contains(") {
		t.Error("contains() is not pushed down to storage and scans every series")
	}
	if strings.Contains(flux, "r.source ==") {
		t.Error("the query must read every source, not filter one")
	}
}

func TestBuildNormalMinutesFlux_RefusesFluxInterpolationInBucketOrPath(t *testing.T) {
	hour := minutesStart.Add(time.Hour)
	if _, err := buildNormalMinutesFlux(`b${x}`, "value", []string{"p"}, minutesStart, hour); err == nil {
		t.Error("a bucket with $ must be refused")
	}
	if _, err := buildNormalMinutesFlux("b", "value", []string{"ok", `p{r}`}, minutesStart, hour); err == nil {
		t.Error("a path with { must be refused, wherever it is in the list")
	}
	if _, err := buildNormalMinutesFlux("b", "value", nil, minutesStart, hour); err == nil {
		t.Error("no paths is an error, not an empty query")
	}
	flux, err := buildNormalMinutesFlux(`odd"bucket`, "value", []string{"p"}, minutesStart, hour)
	if err != nil || !strings.Contains(flux, `from(bucket: "odd\"bucket")`) {
		t.Errorf("a quote must be escaped, not break out: %v\n%s", err, flux)
	}
}

func TestChooseNormalSource(t *testing.T) {
	cases := []struct {
		name       string
		configured bool
		minute     string
		scoring    bool
		want       normalSource
		wantErr    error
	}{
		{"no influx", false, "SignalK_1m", false, normalSource{}, errNormalNoInflux},
		{"training with a minute bucket", true, "SignalK_1m", false, normalSource{Kind: normalSourceMinuteBucket, Bucket: "SignalK_1m"}, nil},
		{"training with the bucket blank rolls up the main one", true, "", false, normalSource{Kind: normalSourceMainBucket}, nil},
		// The minute bucket lags up to an hour, so a past window is always read
		// from the main bucket.
		{"scoring ignores the minute bucket", true, "SignalK_1m", true, normalSource{Kind: normalSourceMainBucket}, nil},
	}
	for _, c := range cases {
		got, err := chooseNormalSource(c.configured, c.minute, c.scoring)
		if !errors.Is(err, c.wantErr) || got != c.want {
			t.Errorf("%s: got %+v, %v; want %+v, %v", c.name, got, err, c.want, c.wantErr)
		}
	}
}

func TestNormalSourceText(t *testing.T) {
	if got := normalSourceText(normalSource{Kind: normalSourceMinuteBucket, Bucket: "SignalK_1m"}); got != `one-minute bucket "SignalK_1m"` {
		t.Errorf("got %q", got)
	}
	if got := normalSourceText(normalSource{Kind: normalSourceMainBucket}); got != "main bucket" {
		t.Errorf("got %q", got)
	}
}

// One read of the settings gives the minute bucket with the rest of the section.
func TestLoadInfluxConfig_CarriesTheMinuteBucketFromOneRead(t *testing.T) {
	withSeededSecretsStore(t, map[string]string{"INFLUXDB_TOKEN": "tok"})
	path := writeInfluxSettingsFixture(t, "influxdb:\n  enabled: true\n  url: http://localhost:8086\n  org: myorg\n  bucket: mybucket\n")
	cfg, ok, err := loadInfluxConfig(path)
	if err != nil || !ok || cfg.MinuteBucket != "" || cfg.Bucket != "mybucket" || cfg.Token != "tok" {
		t.Fatalf("a settings file from before the field reads blank: %+v %v %v", cfg, ok, err)
	}
	path = writeInfluxSettingsFixture(t, "influxdb:\n  enabled: true\n  url: http://localhost:8086\n  org: myorg\n  bucket: mybucket\n  minute_bucket:  SignalK_1m \n")
	cfg, ok, err = loadInfluxConfig(path)
	if err != nil || !ok || cfg.MinuteBucket != "SignalK_1m" {
		t.Fatalf("cfg %+v ok %v err %v", cfg, ok, err)
	}
	src, err := resolveNormalSource(path, false)
	if err != nil || src != (normalSource{Kind: normalSourceMinuteBucket, Bucket: "SignalK_1m"}) {
		t.Fatalf("source %+v err %v", src, err)
	}
	if _, err := resolveNormalSource(writeInfluxSettingsFixture(t, "influxdb:\n  enabled: false\n"), false); !errors.Is(err, errNormalNoInflux) {
		t.Fatalf("disabled InfluxDB is %v", err)
	}
}

// A named bucket that fails is an error that names it. It never reads the main
// bucket instead.
func TestQueryNormalMinutes_ANamedBucketThatFailsIsAnErrorAndNeverFallsBack(t *testing.T) {
	var asked []normalSource
	defer swapNormalMinutesQuery(func(_ context.Context, src normalSource, _ []string, _, _ time.Time) ([]normalMinuteRow, error) {
		asked = append(asked, src)
		return nil, errors.New("bucket not found")
	})()
	_, err := queryNormalMinutes(context.Background(), "settings.yaml", normalSource{Kind: normalSourceMinuteBucket, Bucket: "SignalK_1m"}, []string{"p"}, minutesStart, minutesStart.Add(time.Hour))
	if err == nil || err.Error() != `the one-minute bucket "SignalK_1m" could not be read for 1 Oct: bucket not found` {
		t.Fatalf("err = %v", err)
	}
	for _, s := range asked {
		if s.Kind != normalSourceMinuteBucket {
			t.Fatalf("asked the %s as well: %+v", s.Kind, asked)
		}
	}
}

// The error says which chunk and which source failed, so the status line can be
// acted on.
func TestQueryNormalMinutes_AMainBucketFailureNamesTheChunk(t *testing.T) {
	calls := 0
	defer swapNormalMinutesQuery(func(context.Context, normalSource, []string, time.Time, time.Time) ([]normalMinuteRow, error) {
		calls++
		if calls < 3 {
			return nil, nil
		}
		return nil, errors.New("timeout")
	})()
	_, err := queryNormalMinutes(context.Background(), "settings.yaml", normalSource{Kind: normalSourceMainBucket}, []string{"p"}, minutesStart, minutesStart.Add(5*24*time.Hour))
	if err == nil || err.Error() != "the main bucket could not be read for 3 Oct to 4 Oct: timeout" {
		t.Fatalf("err = %v", err)
	}
}

// Every reading of an engine goes in one query per chunk, and the rows come back
// split by path.
func TestQueryNormalMinutes_OneQueryPerChunkForAllTheReadingsSplitByPath(t *testing.T) {
	calls := 0
	defer swapNormalMinutesQuery(func(_ context.Context, _ normalSource, paths []string, from, _ time.Time) ([]normalMinuteRow, error) {
		calls++
		var rows []normalMinuteRow
		for _, p := range paths {
			rows = append(rows, normalMinuteRow{Path: p, Source: "s", At: from, Value: 1})
		}
		return rows, nil
	})()
	paths := []string{"a", "b", "c", "d", "e", "f", "g", "h", "i"}
	got, err := queryNormalMinutes(context.Background(), "settings.yaml", normalSource{Kind: normalSourceMainBucket}, paths, minutesStart, minutesStart.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("%d queries for nine readings, want 1", calls)
	}
	for _, p := range paths {
		if len(got[p]) != 1 {
			t.Errorf("%s has %d rows", p, len(got[p]))
		}
	}
}

type chunkSpan struct{ from, to time.Time }

func readChunks(t *testing.T, src normalSource, window time.Duration) []chunkSpan {
	t.Helper()
	var spans []chunkSpan
	defer swapNormalMinutesQuery(func(_ context.Context, _ normalSource, _ []string, from, to time.Time) ([]normalMinuteRow, error) {
		spans = append(spans, chunkSpan{from, to})
		return nil, nil
	})()
	if _, err := queryNormalMinutes(context.Background(), "settings.yaml", src,[]string{"p"}, minutesStart, minutesStart.Add(window)); err != nil {
		t.Fatal(err)
	}
	return spans
}

// The raw main bucket holds a point a second, so a chunk of it is a day. A
// chunk of a bucket already at one minute is eight days. Either way a tiny tail
// joins the chunk before it.
func TestQueryNormalMinutes_ChunksFollowTheSource(t *testing.T) {
	day := 24 * time.Hour
	main := normalSource{Kind: normalSourceMainBucket}
	minute := normalSource{Kind: normalSourceMinuteBucket, Bucket: "SignalK_1m"}

	if got := readChunks(t, main, day+15*time.Minute); len(got) != 1 {
		t.Errorf("main bucket, a day and a lead-in: %d queries, want 1", len(got))
	}
	got := readChunks(t, main, 7*day+15*time.Minute)
	if len(got) != 7 {
		t.Fatalf("main bucket, 7 days and a lead-in: %d queries, want 7", len(got))
	}
	for i, s := range got {
		if s.to.Sub(s.from) > day+time.Hour {
			t.Errorf("main chunk %d is %v long", i, s.to.Sub(s.from))
		}
	}
	if got := readChunks(t, main, 90*day); len(got) != 90 {
		t.Errorf("main bucket, 90 days: %d queries, want 90", len(got))
	}

	if got := readChunks(t, minute, 7*day+15*time.Minute); len(got) != 1 {
		t.Errorf("minute bucket, 7 days and a lead-in: %d queries, want 1", len(got))
	}
	if got := readChunks(t, minute, 8*day+2*time.Hour); len(got) != 1 || got[0].to.Sub(got[0].from) != 8*day+2*time.Hour {
		t.Errorf("a tail under a day joins the last chunk: %+v", got)
	}
}

func TestQueryNormalMinutes_ChunksAreEightDaysOnTheMinuteBucketAndJoinUp(t *testing.T) {
	day := 24 * time.Hour
	read := func(window time.Duration) []chunkSpan {
		return readChunks(t, normalSource{Kind: normalSourceMinuteBucket, Bucket: "SignalK_1m"}, window)
	}
	got := read(90 * day)
	if len(got) != 12 {
		t.Fatalf("90 days: %d queries, want 12 (eleven of 8 days and one of 2)", len(got))
	}
	for i, s := range got {
		if i > 0 && !s.from.Equal(got[i-1].to) {
			t.Errorf("chunk %d starts at %v, the last ended %v", i, s.from, got[i-1].to)
		}
		if s.to.Sub(s.from) > 9*day {
			t.Errorf("chunk %d is %v long", i, s.to.Sub(s.from))
		}
	}
	if !got[0].from.Equal(minutesStart) || !got[11].to.Equal(minutesStart.Add(90*day)) {
		t.Errorf("chunks cover %v to %v", got[0].from, got[11].to)
	}
}

// ctx is checked between chunks, so a stopped trainer stops reading.
func TestQueryNormalMinutes_AStoppedContextEndsTheReadBetweenChunks(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	calls := 0
	defer swapNormalMinutesQuery(func(context.Context, normalSource, []string, time.Time, time.Time) ([]normalMinuteRow, error) {
		calls++
		cancel()
		return nil, nil
	})()
	_, err := queryNormalMinutes(ctx, "settings.yaml", normalSource{Kind: normalSourceMainBucket}, []string{"p"}, minutesStart, minutesStart.Add(40*24*time.Hour))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want the cancellation, not a bucket error", err)
	}
	if calls != 1 {
		t.Fatalf("%d queries ran after the cancel", calls)
	}
}

func TestQueryNormalMinutes_TheContextReachesTheQuery(t *testing.T) {
	type key struct{}
	ctx := context.WithValue(context.Background(), key{}, "mark")
	var seen any
	defer swapNormalMinutesQuery(func(c context.Context, _ normalSource, _ []string, _, _ time.Time) ([]normalMinuteRow, error) {
		seen = c.Value(key{})
		return nil, nil
	})()
	if _, err := queryNormalMinutes(ctx, "settings.yaml", normalSource{Kind: normalSourceMainBucket}, []string{"p"}, minutesStart, minutesStart.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if seen != "mark" {
		t.Fatal("the caller's context was not passed to the query")
	}
}
