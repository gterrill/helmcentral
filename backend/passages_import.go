package main

import (
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"sort"
	"time"
)

// GPX import (ADR 0173 decision 6). Imported points carry position and a speed
// and course worked out from successive fixes; every other reading is absent.
// They are stored with source "imported" and run through the same detector.

const (
	gpxMaxFileBytes    = 50 << 20
	gpxMaxFiles        = 50
	gpxMaxRequestBytes = 200 << 20
	// A recorded track covers the time between its points while they are no
	// further apart than this.
	importCoverageGap = 15 * time.Minute

	// importedSegment is the one segment id of imported points.
	importedSegment = 1_000_000_000
)

type gpxFix struct {
	At       time.Time
	Lat, Lon float64
	Seg      int // which trkseg of which file, for segment breaks
}

type gpxFile struct {
	Name  string
	Fixes []gpxFix
}

type gpxFileSummary struct {
	Name   string `json:"name"`
	Points int    `json:"points"`
}

type gpxImportSummary struct {
	Files               []gpxFileSummary `json:"files"`
	PointsRead          int              `json:"points_read"`
	PointsAdded         int              `json:"points_added"`
	PointsRejected      int              `json:"points_rejected"`
	PointsAlreadyHeld   int              `json:"points_already_imported"`
	PointsInRecordedLog int              `json:"points_in_recorded_log"`
	PassagesCreated     int              `json:"passages_created"`
	PassagesSkipped     int              `json:"passages_skipped"`
}

// parseGPX reads the track points of one GPX file. It uses encoding/xml, which
// never fetches an external entity or DTD, and reads at most maxBytes. A track
// point without a time is an error: its place in the log would be a guess.
func parseGPX(r io.Reader, maxBytes int64, firstSeg int) ([]gpxFix, int, error) {
	lr := &io.LimitedReader{R: r, N: maxBytes + 1}
	dec := xml.NewDecoder(lr)
	var fixes []gpxFix
	seg := firstSeg
	inTrk := false
	for {
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if lr.N <= 0 {
			return nil, seg, fmt.Errorf("file is over the %d MB limit", maxBytes>>20)
		}
		if err != nil {
			return nil, seg, fmt.Errorf("not valid GPX: %w", err)
		}
		switch el := tok.(type) {
		case xml.StartElement:
			switch el.Name.Local {
			case "trk":
				inTrk = true
			case "trkseg":
				seg++
			case "trkpt":
				if !inTrk {
					continue
				}
				var pt struct {
					Lat  *float64 `xml:"lat,attr"`
					Lon  *float64 `xml:"lon,attr"`
					Time string   `xml:"time"`
				}
				if err := dec.DecodeElement(&pt, &el); err != nil {
					return nil, seg, fmt.Errorf("bad track point: %w", err)
				}
				if pt.Lat == nil || pt.Lon == nil {
					return nil, seg, errors.New("a track point has no latitude or longitude")
				}
				if pt.Time == "" {
					return nil, seg, errors.New("a track point has no time")
				}
				t, err := time.Parse(time.RFC3339Nano, pt.Time)
				if err != nil {
					return nil, seg, fmt.Errorf("a track point has an unreadable time %q", pt.Time)
				}
				fixes = append(fixes, gpxFix{At: t.UTC(), Lat: *pt.Lat, Lon: *pt.Lon, Seg: seg})
			}
		case xml.EndElement:
			if el.Name.Local == "trk" {
				inTrk = false
			}
		}
	}
	return fixes, seg, nil
}

// importedTrackPoints orders fixes by time and turns them into track points:
// unusable positions and jumps over 40 kn are rejected, there is a single
// segment, and SOG and COG come from
// the leg to the next fix (the leg from the previous fix for the last one). A
// leg that implies under one knot is rest however long it lasted, so a
// three-hour gap with 100 m moved is a stop and does not blend into the legs
// either side.
func importedTrackPoints(fixes []gpxFix) (pts []recordedTrackPoint, rejected int) {
	sorted := append([]gpxFix(nil), fixes...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].At.Before(sorted[j].At) })

	var kept []gpxFix
	for _, f := range sorted {
		if !hasUsableVesselPosition(f.Lat, f.Lon) {
			rejected++
			continue
		}
		if n := len(kept); n > 0 {
			prev := kept[n-1]
			dt := f.At.Sub(prev.At).Seconds()
			if dt <= 0 {
				rejected++
				continue
			}
			if haversineMeters(prev.Lat, prev.Lon, f.Lat, f.Lon)/dt > trackMaxJumpKts*trackMetersPerKnot {
				rejected++
				continue
			}
		}
		kept = append(kept, f)
	}

	// One continuous track. The plugin writes nothing while the boat is at
	// rest and thins straight legs, so a long gap is either a stop or a leg
	// sailed in a straight line, never missing data. Which of the two is
	// decided by the speed the gap implies, below; the 10 minute segment gap
	// belongs to the dense recorded log only.
	pts = make([]recordedTrackPoint, 0, len(kept))
	for _, f := range kept {
		pts = append(pts, recordedTrackPoint{At: f.At, Segment: importedSegment, Lat: f.Lat, Lon: f.Lon, Source: trackSourceImported})
	}
	for i := range pts {
		var a, b int
		switch {
		case i+1 < len(pts) && pts[i+1].Segment == pts[i].Segment:
			a, b = i, i+1
		case i > 0 && pts[i-1].Segment == pts[i].Segment:
			a, b = i-1, i
		default:
			continue
		}
		dt := pts[b].At.Sub(pts[a].At).Seconds()
		sog := haversineMeters(pts[a].Lat, pts[a].Lon, pts[b].Lat, pts[b].Lon) / dt / trackMetersPerKnot
		cog := bearingDeg(pts[a].Lat, pts[a].Lon, pts[b].Lat, pts[b].Lon)
		pts[i].SOGKts, pts[i].COGDeg = &sog, &cog
	}
	return pts, rejected
}

// recordedCoverage returns the time ranges the recorded log already covers
// within [from, to].
func (s *passageStore) recordedCoverage(from, to time.Time) ([][2]time.Time, error) {
	rec, err := s.trackPoints(from.Add(-importCoverageGap), to.Add(importCoverageGap), trackSourceRecorded)
	if err != nil {
		return nil, err
	}
	var out [][2]time.Time
	for _, p := range rec {
		if n := len(out); n > 0 && p.At.Sub(out[n-1][1]) <= importCoverageGap {
			out[n-1][1] = p.At
			continue
		}
		out = append(out, [2]time.Time{p.At, p.At})
	}
	return out, nil
}

func covered(ranges [][2]time.Time, t time.Time) bool {
	for _, r := range ranges {
		if !t.Before(r[0]) && !t.After(r[1]) {
			return true
		}
	}
	return false
}

// ImportGPX stores the imported points that are not already held and not
// inside time the recorded log covers, then detects passages over the imported
// points in that range. A passage that overlaps one already stored is skipped.
func (s *passageStore) ImportGPX(files []gpxFile) (gpxImportSummary, error) {
	var sum gpxImportSummary
	var all []gpxFix
	for _, f := range files {
		sum.Files = append(sum.Files, gpxFileSummary{Name: f.Name, Points: len(f.Fixes)})
		sum.PointsRead += len(f.Fixes)
		all = append(all, f.Fixes...)
	}
	pts, rejected := importedTrackPoints(all)
	sum.PointsRejected = rejected
	if len(pts) == 0 {
		return sum, nil
	}
	from, to := pts[0].At, pts[len(pts)-1].At

	cover, err := s.recordedCoverage(from, to)
	if err != nil {
		return sum, err
	}
	held, err := s.trackPoints(from, to, trackSourceImported)
	if err != nil {
		return sum, err
	}
	have := make(map[int64]bool, len(held))
	for _, h := range held {
		have[h.At.UnixMilli()] = true
	}

	tx, err := s.db.Begin()
	if err != nil {
		return sum, fmt.Errorf("begin import: %w", err)
	}
	defer tx.Rollback()
	for _, p := range pts {
		switch {
		case covered(cover, p.At):
			sum.PointsInRecordedLog++
		case have[p.At.UnixMilli()]:
			sum.PointsAlreadyHeld++
		default:
			if err := insertTrackPoint(tx, p); err != nil {
				return sum, err
			}
			sum.PointsAdded++
		}
	}
	if err := tx.Commit(); err != nil {
		return sum, fmt.Errorf("commit import: %w", err)
	}

	imported, err := s.trackPoints(from, to, trackSourceImported)
	if err != nil {
		return sum, err
	}
	det := detectLoggedPassages(imported)
	found := det.Closed
	if det.Open != nil && det.Open.End.Sub(det.Open.Departed) >= passageMinDuration && det.Open.DistanceNM >= passageMinDistanceNM {
		// the file ended while under way; keep the part that was recorded
		found = append(found, *det.Open)
	}
	for _, lp := range found {
		overlaps, err := s.PassageOverlaps(lp.Start, lp.End)
		if err != nil {
			return sum, err
		}
		if overlaps {
			sum.PassagesSkipped++
			continue
		}
		rec, err := s.InsertPassage(passageRecord{Start: lp.Start, End: lp.End})
		if err != nil {
			return sum, err
		}
		announcePassage(rec.ID)
		sum.PassagesCreated++
	}
	return sum, nil
}
