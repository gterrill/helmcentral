// ukmo-lightning's run discovery, window lookup and cell lookup, deliberately
// free of any dependency on github.com/extism/go-pdk so this file and its
// tests build and run under the plain host Go toolchain. main.go holds the
// Extism wrappers and the HTTP getter.
package main

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	// lightningBucketURL is the Met Office's public, anonymous bucket (CC BY-SA
	// 4.0). It is the only host this plugin may reach.
	lightningBucketURL = "https://met-office-global-gridded-probabilities.s3.eu-west-2.amazonaws.com"
	lightningKeyRoot   = "global-gridded-probabilities"

	windowLength = 3 * time.Hour
	// lastWindowOffset is where a run ends: 116 three-hour windows after the
	// run time. A run directory appears before its files do, so a run is only
	// complete once the file for this offset exists.
	lastWindowOffset = 348 * time.Hour

	// maxWindowsPerCall keeps one call inside the host's 60 second background
	// budget: every window costs a download of the whole grid and a decode of
	// 4.9 MB.
	maxWindowsPerCall = 8
)

// getFunc fetches a URL and returns its status and body. An error means the
// request itself failed; a non-2xx status is the caller's to judge.
type getFunc func(url string) (status int, body []byte, err error)

type lightningWindow struct {
	Start       string  `json:"start"`
	End         string  `json:"end"`
	Probability float64 `json:"probability"`
}

type fetchLightningInput struct {
	Lat  float64 `json:"lat"`
	Lon  float64 `json:"lon"`
	From string  `json:"from"`
	To   string  `json:"to"`
	// Run, when the host gives it, is the run to read: the host found it on the
	// first call of a refresh, so the rest skip the S3 listings.
	Run string `json:"run"`
}

type fetchLightningOutput struct {
	Run     string            `json:"run"`
	Windows []lightningWindow `json:"windows"`
}

// gridCell returns the row and column of the grid cell holding the point.
// Latitude 90 falls in the northernmost row; longitude may be given as
// -180..180 or 0..360, and 180 and -180 are the same meridian.
func gridCell(lat, lon float64) (row, col int, err error) {
	if math.IsNaN(lat) || math.IsNaN(lon) || math.IsInf(lat, 0) || math.IsInf(lon, 0) {
		return 0, 0, fmt.Errorf("position %v, %v is not a finite number", lat, lon)
	}
	if lat < -90 || lat > 90 {
		return 0, 0, fmt.Errorf("latitude %v is outside -90..90", lat)
	}
	if lon < -180 || lon > 360 {
		return 0, 0, fmt.Errorf("longitude %v is outside -180..360", lon)
	}
	row = int(math.Floor((lat + 90) / gridLatStep))
	if row >= gridRows {
		row = gridRows - 1
	}
	x := math.Mod(lon+180, 360)
	if x < 0 {
		x += 360
	}
	col = int(math.Floor(x / gridLonStep))
	if col >= gridCols {
		col = gridCols - 1
	}
	return row, col, nil
}

// windowKey is the object key of the 3-hour window ending at end in the run.
func windowKey(run, end time.Time) string {
	const stamp = "20060102T1504Z"
	return fmt.Sprintf("%s/%s/%s-B%s-lightning_flash_accumulation-PT03H.nc",
		lightningKeyRoot, run.UTC().Format("2006/01/02/T1504Z"), end.UTC().Format(stamp), run.UTC().Format(stamp))
}

func listURL(query string) string {
	return lightningBucketURL + "/?list-type=2&" + query
}

func getOK(get getFunc, url string) ([]byte, error) {
	body, found, err := getOKOr404(get, url)
	if err == nil && !found {
		return nil, fmt.Errorf("GET %s returned 404", url)
	}
	return body, err
}

// getOKOr404 is getOK except that a 404 is an answer, not a failure: found is
// false and the error is nil. Any other non-2xx status is an error.
func getOKOr404(get getFunc, url string) (body []byte, found bool, err error) {
	status, body, err := get(url)
	if err != nil {
		return nil, false, fmt.Errorf("GET %s: %w", url, err)
	}
	if status == 404 {
		return nil, false, nil
	}
	if status < 200 || status > 299 {
		return nil, false, fmt.Errorf("GET %s returned %d", url, status)
	}
	return body, true, nil
}

// xmlValues returns the text of every <tag>...</tag> in body, in order. The
// S3 listings are flat and machine-written, so this is all they need.
func xmlValues(body, tag string) []string {
	var out []string
	open, closing := "<"+tag+">", "</"+tag+">"
	for {
		i := strings.Index(body, open)
		if i < 0 {
			return out
		}
		body = body[i+len(open):]
		j := strings.Index(body, closing)
		if j < 0 {
			return out
		}
		out = append(out, body[:j])
		body = body[j+len(closing):]
	}
}

// listRuns returns the run times that have a directory under the given UTC
// day, newest first. Runs are every six hours from 00:00.
func listRuns(get getFunc, day time.Time) ([]time.Time, error) {
	day = day.UTC()
	prefix := fmt.Sprintf("%s/%s/", lightningKeyRoot, day.Format("2006/01/02"))
	body, err := getOK(get, listURL("delimiter=/&prefix="+prefix))
	if err != nil {
		return nil, err
	}
	text := string(body)
	if v := xmlValues(text, "IsTruncated"); len(v) != 1 || v[0] != "false" {
		return nil, fmt.Errorf("listing of %s is truncated or malformed", prefix)
	}
	var runs []time.Time
	for _, block := range xmlValues(text, "CommonPrefixes") {
		for _, p := range xmlValues(block, "Prefix") {
			dir := strings.TrimSuffix(strings.TrimPrefix(p, prefix), "/")
			if len(dir) != 6 || dir[0] != 'T' || dir[5] != 'Z' || dir[3:5] != "00" {
				return nil, fmt.Errorf("unexpected run directory %q under %s", p, prefix)
			}
			hour, err := strconv.Atoi(dir[1:3])
			if err != nil || hour < 0 || hour > 23 || hour%6 != 0 {
				return nil, fmt.Errorf("unexpected run directory %q under %s", p, prefix)
			}
			runs = append(runs, time.Date(day.Year(), day.Month(), day.Day(), hour, 0, 0, 0, time.UTC))
		}
	}
	sort.Slice(runs, func(i, j int) bool { return runs[i].After(runs[j]) })
	return runs, nil
}

// runComplete reports whether the run's last window file exists.
func runComplete(get getFunc, run time.Time) (bool, error) {
	key := windowKey(run, run.Add(lastWindowOffset))
	body, err := getOK(get, listURL("prefix="+strings.TrimSuffix(key, ".nc")))
	if err != nil {
		return false, err
	}
	text := string(body)
	counts := xmlValues(text, "KeyCount")
	if len(counts) != 1 {
		return false, fmt.Errorf("listing for %s has no KeyCount", key)
	}
	switch counts[0] {
	case "0":
		return false, nil
	case "1":
		for _, k := range xmlValues(text, "Key") {
			if k == key {
				return true, nil
			}
		}
		return false, fmt.Errorf("listing for %s returned a different key", key)
	}
	return false, fmt.Errorf("listing for %s has KeyCount %s", key, counts[0])
}

// latestCompleteRun finds the newest run whose last window exists, looking at
// today's runs then yesterday's (UTC). The bucket is not listed in full: a
// run holds over a thousand keys.
func latestCompleteRun(get getFunc, now time.Time) (time.Time, error) {
	now = now.UTC()
	for back := 0; back <= 1; back++ {
		runs, err := listRuns(get, now.AddDate(0, 0, -back))
		if err != nil {
			return time.Time{}, err
		}
		for _, run := range runs {
			done, err := runComplete(get, run)
			if err != nil {
				return time.Time{}, err
			}
			if done {
				return run, nil
			}
		}
	}
	return time.Time{}, fmt.Errorf("no complete lightning run found for %s or the day before", now.Format("2006-01-02"))
}

// parseRun reads a run time the host passed in. Runs are every six hours from
// 00:00 UTC, so anything else is not a run of this source.
func parseRun(s string) (time.Time, error) {
	run, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("run: %w", err)
	}
	run = run.UTC()
	if run.Minute() != 0 || run.Second() != 0 || run.Nanosecond() != 0 || run.Hour()%6 != 0 {
		return time.Time{}, fmt.Errorf("run %s is not a six-hourly run time", s)
	}
	return run, nil
}

// fetchLightning returns the probability at the point for each 3-hour window
// of the run that ends in (from, to]: the run in.Run names, or if it is empty
// the latest complete run.
//
// A window whose file is missing from a complete run is left out, never
// filled from its neighbours. Any other failure fails the call.
func fetchLightning(get getFunc, now time.Time, in fetchLightningInput) (fetchLightningOutput, error) {
	from, err := time.Parse(time.RFC3339, in.From)
	if err != nil {
		return fetchLightningOutput{}, fmt.Errorf("from: %w", err)
	}
	to, err := time.Parse(time.RFC3339, in.To)
	if err != nil {
		return fetchLightningOutput{}, fmt.Errorf("to: %w", err)
	}
	if !to.After(from) {
		return fetchLightningOutput{}, fmt.Errorf("to %s is not after from %s", in.To, in.From)
	}
	row, col, err := gridCell(in.Lat, in.Lon)
	if err != nil {
		return fetchLightningOutput{}, err
	}

	var run time.Time
	if in.Run != "" {
		run, err = parseRun(in.Run)
	} else {
		run, err = latestCompleteRun(get, now)
	}
	if err != nil {
		return fetchLightningOutput{}, err
	}

	var ends []time.Time
	for end := run.Add(windowLength); !end.After(run.Add(lastWindowOffset)); end = end.Add(windowLength) {
		if end.After(from) && !end.After(to) {
			ends = append(ends, end)
		}
	}
	if len(ends) > maxWindowsPerCall {
		return fetchLightningOutput{}, fmt.Errorf("%d windows asked for, at most %d fit in one call", len(ends), maxWindowsPerCall)
	}

	out := fetchLightningOutput{Run: run.UTC().Format(time.RFC3339), Windows: []lightningWindow{}}
	for _, end := range ends {
		key := windowKey(run, end)
		start := end.Add(-windowLength)
		body, found, err := getOKOr404(get, lightningBucketURL+"/"+key)
		if err != nil {
			return fetchLightningOutput{}, err
		}
		if !found {
			continue
		}
		grid, err := parseLightningGrid(body)
		if err != nil {
			return fetchLightningOutput{}, fmt.Errorf("%s: %w", key, err)
		}
		if grid.end != end.Unix() || grid.start != start.Unix() {
			return fetchLightningOutput{}, fmt.Errorf("%s covers %d..%d, expected %d..%d", key, grid.start, grid.end, start.Unix(), end.Unix())
		}
		out.Windows = append(out.Windows, lightningWindow{
			Start:       start.UTC().Format(time.RFC3339),
			End:         end.UTC().Format(time.RFC3339),
			Probability: float64(grid.at(row, col)),
		})
	}
	return out, nil
}
