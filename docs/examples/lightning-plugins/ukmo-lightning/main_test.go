package main

import (
	"fmt"
	"math"
	"os"
	"strings"
	"testing"
	"time"
)

// The fixtures in testdata are real files from the Met Office bucket,
// captured on 2026-11-12 (see README.md). Reference values were read from
// the grid with h5py.
const (
	gridFixture = "testdata/20261112T0300Z-B20261112T0000Z-lightning_flash_accumulation-PT03H.nc"
	listDay     = "testdata/list-day-20261112.xml"
	listDone    = "testdata/list-last-window-T0000Z.xml"
	listPartial = "testdata/list-last-window-T0600Z.xml"
)

func readFixture(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func loadGrid(t *testing.T) *lightningGrid {
	t.Helper()
	g, err := parseLightningGrid(readFixture(t, gridFixture))
	if err != nil {
		t.Fatalf("parseLightningGrid: %v", err)
	}
	return g
}

func TestParseLightningGrid_ReferenceValues(t *testing.T) {
	g := loadGrid(t)

	for _, tc := range []struct {
		row, col int
		want     float32
	}{
		{480, 640, 0.0302734375},
		{0, 0, 0},
		{959, 1279, 0},
		{638, 366, 1.0},
		{197, 453, 0.2138671875},
	} {
		if got := g.at(tc.row, tc.col); got != tc.want {
			t.Errorf("v[%d][%d] = %v, want %v", tc.row, tc.col, got, tc.want)
		}
	}

	var sum float64
	for r := 0; r < gridRows; r++ {
		for c := 0; c < gridCols; c++ {
			sum += float64(g.at(r, c))
		}
	}
	if sum != 15708.40625 {
		t.Errorf("sum of all cells = %v, want 15708.40625", sum)
	}

	if g.start != 1794441600 || g.end != 1794452400 {
		t.Errorf("time_bnds = [%d %d], want [1794441600 1794452400]", g.start, g.end)
	}
}

func TestParseLightningGrid_ReadsTheShuffledBytesInPlace(t *testing.T) {
	g := loadGrid(t)
	if !g.shuffled {
		t.Fatal("the grid should keep the inflated, still-shuffled bytes rather than an unshuffled copy")
	}
	// at() must agree with an explicit unshuffle of the same bytes.
	plain := unshuffle(g.values, 4)
	for _, rc := range [][2]int{{480, 640}, {638, 366}, {197, 453}, {959, 1279}, {1, 1}} {
		i := rc[0]*gridCols + rc[1]
		want := math.Float32frombits(uint32(plain[i*4]) | uint32(plain[i*4+1])<<8 | uint32(plain[i*4+2])<<16 | uint32(plain[i*4+3])<<24)
		if got := g.at(rc[0], rc[1]); got != want {
			t.Errorf("at(%d,%d) = %v, unshuffled bytes say %v", rc[0], rc[1], got, want)
		}
	}
}

func TestParseLightningGrid_RejectsWhatItWasNotWrittenFor(t *testing.T) {
	good := readFixture(t, gridFixture)

	mutate := func(f func(b []byte) []byte) []byte {
		b := make([]byte, len(good))
		copy(b, good)
		return f(b)
	}

	for _, tc := range []struct {
		name string
		file []byte
		want string
	}{
		{"not hdf5", []byte("CDF\x01 plain old netcdf"), "not an HDF5 file"},
		{"truncated", good[:len(good)-1000], "truncated"},
		{"superblock version", mutate(func(b []byte) []byte { b[8] = 3; return b }), "superblock version 3"},
		{"offset size", mutate(func(b []byte) []byte { b[9] = 4; return b }), "offset size 4"},
		// The root object header sits at 0x30; its version byte follows "OHDR".
		{"object header version", mutate(func(b []byte) []byte { b[0x34] = 1; return b }), "only version 2"},
		// The chunk the grid is stored in, flipped past zlib's header.
		{"corrupt deflate stream", mutate(func(b []byte) []byte { b[10808+1000] ^= 0xFF; return b }), "deflate"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseLightningGrid(tc.file)
			if err == nil {
				t.Fatal("expected an error, got a grid")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %q does not mention %q", err, tc.want)
			}
		})
	}
}

func TestUnshuffle(t *testing.T) {
	// Two 4-byte elements, AABBCCDD and 11223344, stored shuffled.
	got := unshuffle([]byte{0xAA, 0x11, 0xBB, 0x22, 0xCC, 0x33, 0xDD, 0x44}, 4)
	want := []byte{0xAA, 0xBB, 0xCC, 0xDD, 0x11, 0x22, 0x33, 0x44}
	if string(got) != string(want) {
		t.Fatalf("unshuffle = %x, want %x", got, want)
	}
}

func TestGridCell(t *testing.T) {
	for _, tc := range []struct {
		name     string
		lat, lon float64
		row, col int
	}{
		{"centre of v[480][640]", 0.09375, 0.140625, 480, 640},
		{"centre of v[638][366]", 29.71875, -76.921875, 638, 366},
		{"centre of v[197][453]", -52.96875, -52.453125, 197, 453},
		{"south pole clamps into row 0", -90, 0, 0, 640},
		{"north pole clamps into the last row", 90, 0, 959, 640},
		{"lon 0..360 form", 29.71875, 360 - 76.921875, 638, 366},
		{"lon 180 wraps to column 0", 0, 180, 480, 0},
		{"lon -180 is column 0", 0, -180, 480, 0},
		{"just west of the antimeridian", 0, 179.99, 480, 1279},
		{"just east of the antimeridian", 0, -179.99, 480, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			row, col, err := gridCell(tc.lat, tc.lon)
			if err != nil {
				t.Fatal(err)
			}
			if row != tc.row || col != tc.col {
				t.Fatalf("gridCell(%v, %v) = (%d, %d), want (%d, %d)", tc.lat, tc.lon, row, col, tc.row, tc.col)
			}
		})
	}

	for _, bad := range [][2]float64{{91, 0}, {-91, 0}, {0, 361}, {0, -181}, {math.NaN(), 0}, {0, math.Inf(1)}} {
		if _, _, err := gridCell(bad[0], bad[1]); err == nil {
			t.Errorf("gridCell(%v, %v) accepted an impossible position", bad[0], bad[1])
		}
	}
}

func TestWindowKey(t *testing.T) {
	run := time.Date(2026, 11, 12, 0, 0, 0, 0, time.UTC)
	got := windowKey(run, run.Add(3*time.Hour))
	want := "global-gridded-probabilities/2026/11/12/T0000Z/20261112T0300Z-B20261112T0000Z-lightning_flash_accumulation-PT03H.nc"
	if got != want {
		t.Fatalf("windowKey = %s, want %s", got, want)
	}
	// The last window of the run lives in the run's directory, under its own date.
	got = windowKey(run, run.Add(lastWindowOffset))
	want = "global-gridded-probabilities/2026/11/12/T0000Z/20261126T1200Z-B20261112T0000Z-lightning_flash_accumulation-PT03H.nc"
	if got != want {
		t.Fatalf("last windowKey = %s, want %s", got, want)
	}
}

// fakeBucket serves the captured listings and, for window files, the one grid
// fixture or a 404, recording every URL it was asked for.
type fakeBucket struct {
	t        *testing.T
	requests []string
	// windows maps a window end (unix seconds) to its response status; ends not
	// listed answer 404.
	windows map[int64]int
}

func (f *fakeBucket) get(url string) (int, []byte, error) {
	f.requests = append(f.requests, url)
	switch {
	case strings.Contains(url, "delimiter=/&prefix=global-gridded-probabilities/2026/11/12/"):
		return 200, readFixture(f.t, listDay), nil
	case strings.Contains(url, "prefix=global-gridded-probabilities/2026/11/11/"):
		return 200, []byte(`<ListBucketResult><KeyCount>0</KeyCount><IsTruncated>false</IsTruncated></ListBucketResult>`), nil
	case strings.Contains(url, "list-type=2&prefix=") && strings.Contains(url, "T0000Z/20261126T1200Z-B20261112T0000Z"):
		return 200, readFixture(f.t, listDone), nil
	case strings.Contains(url, "list-type=2&prefix=") && strings.Contains(url, "T0600Z/20261126T1800Z-B20261112T0600Z"):
		return 200, readFixture(f.t, listPartial), nil
	case strings.HasSuffix(url, ".nc"):
		// Only the T0000Z run is complete, so window requests are for it.
		if !strings.Contains(url, "/T0000Z/") {
			f.t.Errorf("window requested from an incomplete run: %s", url)
		}
		validity := url[strings.LastIndex(url, "/")+1:][:14]
		end, err := time.Parse("20060102T1504Z", validity)
		if err != nil {
			f.t.Fatalf("bad validity in %s: %v", url, err)
		}
		status, ok := f.windows[end.Unix()]
		if !ok {
			return 404, []byte(`<Error><Code>NoSuchKey</Code></Error>`), nil
		}
		if status != 200 {
			return status, nil, nil
		}
		// The one real file stands in for every window; patch time_bnds so it
		// reads as this window. time_bnds is contiguous int64 data at 326188.
		b := readFixture(f.t, gridFixture)
		putInt64(b[326188:], end.Add(-3*time.Hour).Unix())
		putInt64(b[326196:], end.Unix())
		return 200, b, nil
	}
	f.t.Fatalf("unexpected request %s", url)
	return 0, nil, nil
}

func putInt64(b []byte, v int64) {
	for i := 0; i < 8; i++ {
		b[i] = byte(uint64(v) >> (8 * i))
	}
}

func TestLatestCompleteRun_SkipsTheIncompleteNewestRun(t *testing.T) {
	bucket := &fakeBucket{t: t}
	now := time.Date(2026, 11, 12, 11, 44, 0, 0, time.UTC)

	run, err := latestCompleteRun(bucket.get, now)
	if err != nil {
		t.Fatal(err)
	}
	if want := time.Date(2026, 11, 12, 0, 0, 0, 0, time.UTC); !run.Equal(want) {
		t.Fatalf("run = %s, want %s", run, want)
	}
	// T0600Z is listed first and is not complete, so T0000Z is the answer.
	last := bucket.requests[len(bucket.requests)-1]
	if !strings.Contains(last, "T0000Z/20261126T1200Z") {
		t.Fatalf("expected the T0000Z completeness check last, requests: %v", bucket.requests)
	}
	for _, r := range bucket.requests {
		if strings.Contains(r, "T0600Z") && !strings.Contains(r, "list-type=2&prefix=") {
			t.Errorf("listing the full T0600Z directory: %s", r)
		}
	}
}

func TestLatestCompleteRun_NothingComplete(t *testing.T) {
	get := func(url string) (int, []byte, error) {
		return 200, []byte(`<ListBucketResult><KeyCount>0</KeyCount><IsTruncated>false</IsTruncated></ListBucketResult>`), nil
	}
	if _, err := latestCompleteRun(get, time.Date(2026, 11, 12, 11, 44, 0, 0, time.UTC)); err == nil {
		t.Fatal("expected an error when no run exists")
	}
}

func TestFetchLightning_WindowsInRange(t *testing.T) {
	end := func(h int) int64 { return time.Date(2026, 11, 12, h, 0, 0, 0, time.UTC).Unix() }
	bucket := &fakeBucket{t: t, windows: map[int64]int{end(3): 200, end(6): 200, end(12): 200}}
	now := time.Date(2026, 11, 12, 11, 44, 0, 0, time.UTC)

	// Windows ending 03, 06, 09 and 12; 09 is missing from the run.
	// from is exclusive: the window ending exactly at 03:00 is not in (03:00, 12:00].
	out, err := fetchLightning(bucket.get, now, fetchLightningInput{
		Lat: 0.09375, Lon: 0.140625, From: "2026-11-12T02:00:00Z", To: "2026-11-12T12:00:00Z",
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.Run != "2026-11-12T00:00:00Z" {
		t.Errorf("run = %s", out.Run)
	}
	if len(out.Windows) != 3 {
		t.Fatalf("got %d windows, want 3 (09:00 is missing and must not be filled): %+v", len(out.Windows), out.Windows)
	}
	first := out.Windows[0]
	if first.Start != "2026-11-12T00:00:00Z" || first.End != "2026-11-12T03:00:00Z" || first.Probability != 0.0302734375 {
		t.Errorf("first window = %+v", first)
	}
	if out.Windows[2].End != "2026-11-12T12:00:00Z" {
		t.Errorf("last window = %+v", out.Windows[2])
	}

	// from is exclusive.
	out, err = fetchLightning(bucket.get, now, fetchLightningInput{
		Lat: 0.09375, Lon: 0.140625, From: "2026-11-12T03:00:00Z", To: "2026-11-12T06:00:00Z",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Windows) != 1 || out.Windows[0].End != "2026-11-12T06:00:00Z" {
		t.Errorf("(03:00, 06:00] gave %+v", out.Windows)
	}
}

func TestFetchLightning_GivenRunSkipsDiscovery(t *testing.T) {
	end := func(h int) int64 { return time.Date(2026, 11, 12, h, 0, 0, 0, time.UTC).Unix() }
	bucket := &fakeBucket{t: t, windows: map[int64]int{end(3): 200, end(6): 200}}
	now := time.Date(2026, 11, 12, 11, 44, 0, 0, time.UTC)

	out, err := fetchLightning(bucket.get, now, fetchLightningInput{
		Lat: 0.09375, Lon: 0.140625, From: "2026-11-12T02:00:00Z", To: "2026-11-12T06:00:00Z",
		Run: "2026-11-12T00:00:00Z",
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.Run != "2026-11-12T00:00:00Z" || len(out.Windows) != 2 {
		t.Fatalf("out = %+v", out)
	}
	for _, r := range bucket.requests {
		if !strings.HasSuffix(r, ".nc") {
			t.Errorf("a given run still listed the bucket: %s", r)
		}
	}

	for _, bad := range []string{"yesterday", "2026-11-12T01:00:00Z"} {
		_, err := fetchLightning(bucket.get, now, fetchLightningInput{
			Lat: 0, Lon: 0, From: "2026-11-12T02:00:00Z", To: "2026-11-12T06:00:00Z", Run: bad,
		})
		if err == nil || !strings.Contains(err.Error(), "run") {
			t.Errorf("run %q: err = %v, want one about the run", bad, err)
		}
	}
}

func TestFetchLightning_PastTheEndOfTheRunIsEmptyNotAnError(t *testing.T) {
	bucket := &fakeBucket{t: t}
	out, err := fetchLightning(bucket.get, time.Date(2026, 11, 12, 11, 44, 0, 0, time.UTC), fetchLightningInput{
		Lat: 0, Lon: 0, From: "2026-11-27T00:00:00Z", To: "2026-11-27T12:00:00Z",
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.Windows == nil || len(out.Windows) != 0 {
		t.Fatalf("windows = %#v, want an empty non-nil slice", out.Windows)
	}
}

func TestFetchLightning_FailsFast(t *testing.T) {
	end := time.Date(2026, 11, 12, 3, 0, 0, 0, time.UTC).Unix()
	now := time.Date(2026, 11, 12, 11, 44, 0, 0, time.UTC)
	in := fetchLightningInput{Lat: 0, Lon: 0, From: "2026-11-12T00:00:00Z", To: "2026-11-12T03:00:00Z"}

	t.Run("server error on a window", func(t *testing.T) {
		bucket := &fakeBucket{t: t, windows: map[int64]int{end: 500}}
		if _, err := fetchLightning(bucket.get, now, in); err == nil || !strings.Contains(err.Error(), "500") {
			t.Fatalf("err = %v, want a 500 failure", err)
		}
	})
	t.Run("window file is not the window asked for", func(t *testing.T) {
		bucket := &fakeBucket{t: t, windows: map[int64]int{end: 200}}
		wrong := func(url string) (int, []byte, error) {
			status, body, err := bucket.get(url)
			if strings.HasSuffix(url, ".nc") {
				putInt64(body[326196:], end+3600)
			}
			return status, body, err
		}
		if _, err := fetchLightning(wrong, now, in); err == nil || !strings.Contains(err.Error(), "covers") {
			t.Fatalf("err = %v, want a time_bnds mismatch", err)
		}
	})
	t.Run("garbage file", func(t *testing.T) {
		bucket := &fakeBucket{t: t}
		garbage := func(url string) (int, []byte, error) {
			if strings.HasSuffix(url, ".nc") {
				return 200, []byte("<html>captive portal</html>"), nil
			}
			return bucket.get(url)
		}
		if _, err := fetchLightning(garbage, now, in); err == nil {
			t.Fatal("expected an error for a file that is not HDF5")
		}
	})
	t.Run("transport error", func(t *testing.T) {
		down := func(string) (int, []byte, error) { return 0, nil, fmt.Errorf("connection refused") }
		if _, err := fetchLightning(down, now, in); err == nil {
			t.Fatal("expected an error")
		}
	})
	t.Run("too many windows for one call", func(t *testing.T) {
		bucket := &fakeBucket{t: t}
		wide := in
		wide.To = "2026-11-14T00:00:00Z"
		if _, err := fetchLightning(bucket.get, now, wide); err == nil || !strings.Contains(err.Error(), "at most") {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("bad input", func(t *testing.T) {
		bucket := &fakeBucket{t: t}
		for _, bad := range []fetchLightningInput{
			{Lat: 95, Lon: 0, From: in.From, To: in.To},
			{Lat: 0, Lon: 0, From: "yesterday", To: in.To},
			{Lat: 0, Lon: 0, From: in.To, To: in.From},
		} {
			if _, err := fetchLightning(bucket.get, now, bad); err == nil {
				t.Errorf("accepted %+v", bad)
			}
		}
		if len(bucket.requests) != 0 {
			t.Errorf("bad input still reached the network: %v", bucket.requests)
		}
	})
}
