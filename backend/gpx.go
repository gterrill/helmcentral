package main

import (
	"encoding/xml"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

func queryGPXPositionTrailRange(start, end time.Time) []trailPoint {
	if start.IsZero() || end.IsZero() || !end.After(start) {
		return nil
	}

	dir := resolveGPXTracksDir()
	if dir == "" {
		return nil
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}

	type fileCandidate struct {
		path    string
		modTime time.Time
	}

	candidates := make([]fileCandidate, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if strings.ToLower(filepath.Ext(entry.Name())) != ".gpx" {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		candidates = append(candidates, fileCandidate{
			path:    filepath.Join(dir, entry.Name()),
			modTime: info.ModTime(),
		})
	}

	if len(candidates) == 0 {
		return nil
	}

	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].modTime.Before(candidates[j].modTime)
	})

	points := make([]trailPoint, 0)
	for _, candidate := range candidates {
		filePoints, err := readGPXPointsInRange(candidate.path, start, end)
		if err != nil || len(filePoints) == 0 {
			continue
		}
		points = append(points, filePoints...)
	}

	if len(points) == 0 {
		return nil
	}

	sort.Slice(points, func(i, j int) bool {
		return points[i].Timestamp.Before(points[j].Timestamp)
	})

	return points
}

func resolveGPXTracksDir() string {
	candidates := []string{}
	if configured := trimEnvValue(getEnv("SIGNALK_GPX_TRACKS_DIR", "")); configured != "" {
		candidates = append(candidates, configured)
	}
	candidates = append(candidates,
		"/Main-Storage/data/signalk/gpx-tracks",
		"Main-Storage/data/signalk/gpx-tracks",
	)

	for _, dir := range candidates {
		info, err := os.Stat(dir)
		if err == nil && info.IsDir() {
			return dir
		}
	}

	return ""
}

func readGPXPointsInRange(path string, start, end time.Time) ([]trailPoint, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	decoder := xml.NewDecoder(file)
	result := make([]trailPoint, 0)

	for {
		token, err := decoder.Token()
		if err != nil {
			if err == io.EOF {
				break
			}
			return nil, err
		}

		startElement, ok := token.(xml.StartElement)
		if !ok || startElement.Name.Local != "trkpt" {
			continue
		}

		var trkpt struct {
			Lat  float64 `xml:"lat,attr"`
			Lon  float64 `xml:"lon,attr"`
			Time string  `xml:"time"`
		}
		if err := decoder.DecodeElement(&trkpt, &startElement); err != nil {
			continue
		}

		timestamp, ok := parseGPXTime(trkpt.Time)
		if !ok {
			continue
		}
		if timestamp.Before(start) || !timestamp.Before(end) {
			continue
		}
		if trkpt.Lat < -90 || trkpt.Lat > 90 || trkpt.Lon < -180 || trkpt.Lon > 180 {
			continue
		}

		result = append(result, trailPoint{
			Lat:       trkpt.Lat,
			Lon:       trkpt.Lon,
			Timestamp: timestamp,
		})
	}

	return result, nil
}

func parseGPXTime(raw string) (time.Time, bool) {
	v := strings.TrimSpace(raw)
	if v == "" {
		return time.Time{}, false
	}
	if t, err := time.Parse(time.RFC3339Nano, v); err == nil {
		return t.UTC(), true
	}
	if t, err := time.Parse(time.RFC3339, v); err == nil {
		return t.UTC(), true
	}
	return time.Time{}, false
}
