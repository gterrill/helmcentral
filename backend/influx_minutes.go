package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	influxdb2 "github.com/influxdata/influxdb-client-go/v2"
)

// One-minute readings for the learned normal (ADR 0276). The same readings are
// asked for whichever bucket they come from: aggregateWindow(every: 1m) is a
// no-op on a bucket that already holds minute means and a roll-up on the main
// one, so training from either gives the same minutes.

const (
	normalSourceMinuteBucket = "minute_bucket"
	normalSourceMainBucket   = "main_bucket"

	// A chunk bounds one query, which the InfluxDB client cuts off at its own 20
	// seconds whatever our context says. A bucket already at one minute holds
	// about 11,500 points per reading in 8 days, so a 7 day report window with
	// its lead-in is one query. The raw main bucket holds a point a second,
	// about 86,000 per reading per day, so it is read a day at a time.
	normalMinuteBucketChunk = 8 * 24 * time.Hour
	normalMainBucketChunk   = 24 * time.Hour
	// A trailing chunk shorter than 1/normalMergeTailDivisor of the chunk joins
	// the one before it rather than costing a query.
	normalMergeTailDivisor = 8
	// normalMinutesTimeout is only an outer bound on one chunk. The InfluxDB
	// client cuts every request off at its own 20 seconds, which is the real limit.
	normalMinutesTimeout = 60 * time.Second
)

// normalChunkFor is the longest span one query reads from src.
func normalChunkFor(src normalSource) time.Duration {
	if src.Kind == normalSourceMinuteBucket {
		return normalMinuteBucketChunk
	}
	return normalMainBucketChunk
}

// errNormalNoInflux is what the learned normal says when there is nothing to
// learn from. It is not a failure of a bucket and is reported as itself.
var errNormalNoInflux = errors.New("InfluxDB is not set up")

// normalSource says where the minutes came from, for the saved model.
type normalSource struct {
	Kind   string `json:"kind"`
	Bucket string `json:"bucket,omitempty"`
}

// normalSourceText is the source as the operator reads it.
func normalSourceText(s normalSource) string {
	if s.Kind == normalSourceMinuteBucket {
		return `one-minute bucket "` + s.Bucket + `"`
	}
	return "main bucket"
}

// chooseNormalSource picks the bucket. Training reads the operator's one-minute
// bucket when one is set and otherwise rolls up the main bucket. Scoring a past
// window always reads the main bucket, because a downsampling task can leave the
// minute bucket up to an hour behind. No InfluxDB is an error, never a guess.
func chooseNormalSource(configured bool, minuteBucket string, scoring bool) (normalSource, error) {
	switch {
	case !configured:
		return normalSource{}, errNormalNoInflux
	case scoring || minuteBucket == "":
		return normalSource{Kind: normalSourceMainBucket}, nil
	}
	return normalSource{Kind: normalSourceMinuteBucket, Bucket: minuteBucket}, nil
}

// resolveNormalSource reads the settings once for chooseNormalSource. A secrets
// store that cannot be read is an error, not "InfluxDB is not set up".
func resolveNormalSource(settingsPath string, scoring bool) (normalSource, error) {
	cfg, ok, err := loadInfluxConfig(settingsPath)
	if err != nil {
		return normalSource{}, err
	}
	return chooseNormalSource(ok, cfg.MinuteBucket, scoring)
}

// normalMinuteRow is one reading's mean over one minute, from one source.
type normalMinuteRow struct {
	Path   string
	Source string
	At     time.Time
	Value  float64
}

// buildNormalMinutesFlux builds the query for several readings' one-minute
// means over [start, stop), keeping the measurement (the path) and the source
// tag so the rows can be split and two sources told apart. timeSrc "_start"
// labels each minute with its first instant, which is how the trainer joins
// minutes. It is a pure function so its escaping can be tested without InfluxDB.
func buildNormalMinutesFlux(bucket, field string, paths []string, start, stop time.Time) (string, error) {
	if len(paths) == 0 {
		return "", errors.New("no paths to read")
	}
	// An OR chain of equalities is pushed down to storage. contains() is not, and
	// scans every series in the bucket.
	alternatives := make([]string, len(paths))
	for i, p := range paths {
		lit, err := fluxStringLiteral(p)
		if err != nil {
			return "", fmt.Errorf("path: %w", err)
		}
		alternatives[i] = "r._measurement == " + lit
	}
	bucketLiteral, err := fluxStringLiteral(bucket)
	if err != nil {
		return "", fmt.Errorf("bucket: %w", err)
	}
	fieldLiteral, err := fluxStringLiteral(field)
	if err != nil {
		return "", fmt.Errorf("field: %w", err)
	}
	// SignalK also logs words and on/off flags, and a mean of those fails the whole
	// query, so only floats go on to be averaged (the how-to's own task does the same).
	return fmt.Sprintf(
		`import "types"`+"\n"+`from(bucket: %s) |> range(start: time(v: %q), stop: time(v: %q)) |> filter(fn: (r) => (%s) and r._field == %s) |> filter(fn: (r) => types.isType(v: r._value, type: "float")) |> aggregateWindow(every: 1m, fn: mean, createEmpty: false, timeSrc: "_start") |> keep(columns: ["_time", "_value", "_measurement", "source"])`,
		bucketLiteral, start.UTC().Format(time.RFC3339), stop.UTC().Format(time.RFC3339), strings.Join(alternatives, " or "), fieldLiteral,
	), nil
}

// normalInflux is the connection a run reads through, resolved once per run.
type normalInflux struct {
	client     influxdb2.Client
	org        string
	mainBucket string
	// close ends a connection opened for this run. It is nil for one that is shared.
	close func()
}

// openNormalInflux resolves the InfluxDB settings, secrets included, once per
// run. Settings that say InfluxDB is not used are errNormalNoInflux. A secrets
// store that cannot be read is its own error and never reads as "not set up".
// The client is the run's own, not the cache's shared one: a settings save in
// the middle of a long run would otherwise close it underneath. The caller closes it.
var openNormalInflux = func(settingsPath string) (normalInflux, error) {
	cfg, ok, err := loadInfluxConfig(settingsPath)
	if err != nil {
		return normalInflux{}, err
	}
	if !ok {
		return normalInflux{}, errNormalNoInflux
	}
	client := influxdb2.NewClient(cfg.URL, cfg.Token)
	return normalInflux{client: client, org: cfg.Org, mainBucket: cfg.Bucket, close: client.Close}, nil
}

// runNormalMinutesQuery is one chunk's I/O, a variable so a test can answer it
// without InfluxDB (the same seam idiom as fetchInfluxRangeChunked).
var runNormalMinutesQuery = func(ctx context.Context, in normalInflux, src normalSource, paths []string, from, to time.Time) ([]normalMinuteRow, error) {
	client, org, mainBucket := in.client, in.org, in.mainBucket
	bucket := mainBucket
	if src.Kind == normalSourceMinuteBucket {
		bucket = src.Bucket
	}
	field := trimEnvValue(getEnv("INFLUX_DEPTH_FIELD", "value"))
	flux, err := buildNormalMinutesFlux(bucket, field, paths, from, to)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, normalMinutesTimeout)
	defer cancel()
	result, err := client.QueryAPI(org).Query(ctx, flux)
	if err != nil {
		return nil, err
	}
	defer result.Close()

	var rows []normalMinuteRow
	for result.Next() {
		rec := result.Record()
		v, ok := rec.Value().(float64)
		if !ok {
			continue
		}
		path, _ := rec.ValueByKey("_measurement").(string)
		source, _ := rec.ValueByKey("source").(string)
		rows = append(rows, normalMinuteRow{Path: path, Source: source, At: rec.Time().UTC(), Value: v})
	}
	if result.Err() != nil {
		return nil, result.Err()
	}
	return rows, nil
}

// normalSpanText names a chunk by its dates, "3 Nov to 4 Nov", or "3 Nov" when
// it lies inside one day.
func normalSpanText(from, to time.Time) string {
	a, b := from.UTC().Format("2 Jan"), to.UTC().Format("2 Jan")
	if a == b {
		return a
	}
	return a + " to " + b
}

// queryNormalMinutes reads the one-minute means of several readings over
// [start, stop), by path, one query for all of them per chunk. A failure names
// what failed: a named one-minute bucket that cannot be read is an error that
// says so and is never answered from the main bucket (AGENTS.md, Fallback
// Policy). ctx is checked between chunks.
func queryNormalMinutes(ctx context.Context, settingsPath string, src normalSource, paths []string, start, stop time.Time) (map[string][]normalMinuteRow, error) {
	out := make(map[string][]normalMinuteRow, len(paths))
	// Chunks start and end on whole minutes: aggregateWindow labels a minute by
	// its first instant, and a partial first or last minute is a mean of part.
	start, stop = start.Truncate(time.Minute), stop.Truncate(time.Minute)
	chunk := normalChunkFor(src)
	// The connection is resolved once, on the first chunk, and every chunk of the
	// run reads through it.
	var in normalInflux
	opened := false
	for from := start; from.Before(stop); {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if !opened {
			var err error
			if in, err = openNormalInflux(settingsPath); err != nil {
				return nil, err
			}
			opened = true
			if in.close != nil {
				defer in.close()
			}
		}
		to := from.Add(chunk)
		if stop.Sub(to) < chunk/normalMergeTailDivisor {
			to = stop
		}
		rows, err := runNormalMinutesQuery(ctx, in, src, paths, from, to)
		if err != nil {
			if errors.Is(err, errNormalNoInflux) || ctx.Err() != nil {
				return nil, err
			}
			what := "the main bucket"
			if src.Kind == normalSourceMinuteBucket {
				what = fmt.Sprintf("the one-minute bucket %q", src.Bucket)
			}
			return nil, fmt.Errorf("%s could not be read for %s: %w", what, normalSpanText(from, to), err)
		}
		for _, r := range rows {
			out[r.Path] = append(out[r.Path], r)
		}
		from = to
	}
	return out, nil
}
