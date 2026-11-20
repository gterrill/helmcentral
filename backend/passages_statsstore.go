package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"sync"
	"time"
)

// A passage's figures are worked out from its track points once and kept on
// its row. They are cleared whenever the passage's range or the points inside
// it change (trim, split, merge, an import into its range), and worked out
// again the next time they are read.

// passageFigures is what is kept on the row: the stats, and the longitude of
// the first point the page uses for the boat's own time zone.
type passageFigures struct {
	// Version is the shape the figures were worked out in. Rows stored in an
	// older shape are worked out again from the points when next read.
	Version  int          `json:"version"`
	Stats    passageStats `json:"stats"`
	StartLon *float64     `json:"start_lon"`
	// StartLat is the latitude of the first point, kept with StartLon so the
	// passage's time zone can be looked up from the pair.
	StartLat *float64 `json:"start_lat"`
	// Timezone is the IANA zone at the start point, kept with the figures so a
	// list does not look it up again for every passage on every request. It is
	// worked out when the figures are, and once for a row stored before it was
	// kept. Empty when the position has no zone.
	Timezone string `json:"timezone,omitempty"`
}

// passageFiguresVersion 2 added the time of each maximum and the motion maxima.
const passageFiguresVersion = 2

// PassageStatsFor is the stored stats alone.
func (s *passageStore) PassageStatsFor(rec passageRecord) (passageStats, error) {
	f, err := s.PassageFiguresFor(rec)
	return f.Stats, err
}

// PassageFiguresFor returns the stored figures, computing and storing them
// first when the row has none.
func (s *passageStore) PassageFiguresFor(rec passageRecord) (passageFigures, error) {
	var raw sql.NullString
	var cached *passageFigures
	if err := s.db.QueryRow(`SELECT stats_json FROM passages WHERE id = ?`, rec.ID).Scan(&raw); err != nil {
		return passageFigures{}, fmt.Errorf("read passage stats: %w", err)
	}
	if raw.Valid {
		var f passageFigures
		if err := json.Unmarshal([]byte(raw.String), &f); err == nil {
			// Rows stored before the start latitude was kept have a longitude
			// and no latitude; they are worked out again below.
			if f.Version >= passageFiguresVersion && (f.StartLat != nil || f.StartLon == nil) {
				if f.Timezone == "" && f.StartLat != nil && f.StartLon != nil {
					// Stored before the zone was kept: look it up this once
					// and keep it.
					if name, _ := f.lookupStartZone(rec.ID); name != "" {
						f.Timezone = name
						s.storeFigures(rec, f)
					}
				}
				return f, nil
			}
			cached = &f
		}
	}
	pts, err := s.TrackPointsBetween(rec.Start, rec.End)
	if err != nil {
		return passageFigures{}, err
	}
	if cached != nil && len(pts) == 0 {
		// Nothing to work it out from; the stored figures stand.
		return *cached, nil
	}
	st := passageFigures{Version: passageFiguresVersion, Stats: computePassageStats(pts), StartLon: startLon(pts), StartLat: startLat(pts)}
	st.Timezone, _ = st.lookupStartZone(rec.ID)
	if err := s.storeFigures(rec, st); err != nil {
		return st, err
	}
	return st, nil
}

// Sideways g at the helm (ADR 0297): how hard the roll throws someone standing
// at the helm, worked out whenever a passage is read from the operator's helm
// height and the roll acceleration the recorder stored (ADR 0295). It is never
// stored, so changing the height changes every passage. The roll axis is taken
// to lie at the waterline, which over-reads for a boat that rolls about a point
// higher up and under-reads for one that rolls about a lower one.

// passageSidewaysG is a passage's largest sideways g at the helm and when it
// was recorded. With no figure, Reason says why.
type passageSidewaysG struct {
	PeakG  *float64 `json:"peak_g"`
	PeakAt *int64   `json:"peak_at"`
	Reason string   `json:"reason,omitempty"`
}

const (
	sidewaysReasonNoHeight      = "Helm height isn't set. Enter it under Settings, Vessel."
	sidewaysReasonUnreadable    = "The helm height couldn't be read, so sideways g isn't shown."
	sidewaysReasonNoMotion      = "No motion was recorded on this passage: the boat had no attitude sensor, or its motion feed wasn't reaching Helmcentral."
	sidewaysReasonSlowFeed      = "The attitude feed was below 8 Hz on this passage, too slow to work out sideways g."
	sidewaysReasonImported      = "This passage was imported, so it has no motion figures."
	sidewaysReasonBeforeCapture = "This passage was recorded before 10 Hz capture, so it has roll peaks but no sideways g."
	sidewaysReasonNoAccel       = "No point on this passage has a roll acceleration, though motion was recorded."
)

// passageSidewaysGFor works the figures out for a passage's points from the
// helm height now on record: helm height in metres times roll acceleration in
// radians per second squared, over g. As in the stored stats, the first point's
// figures describe the interval before the passage, so it is left out. A height
// that cannot be read is logged and travels as the reason, so the passage is
// still returned.
func passageSidewaysGFor(id string, pts []recordedTrackPoint) passageSidewaysG {
	helmHeightM, err := currentHelmHeight()
	if err != nil {
		if helmHeightLog.allow() {
			log.Printf("passages: helm height for %s: %v (repeats are not logged for an hour)", id, err)
		}
		return passageSidewaysG{Reason: sidewaysReasonUnreadable}
	}
	if helmHeightM == nil {
		return passageSidewaysG{Reason: sidewaysReasonNoHeight}
	}
	var out passageSidewaysG
	for i, p := range pts {
		if i == 0 || p.RollAccelPeakDps2 == nil {
			continue
		}
		g := *helmHeightM * (*p.RollAccelPeakDps2 * math.Pi / 180) / gravityMPerS2
		raiseMax(&out.PeakG, &out.PeakAt, &g, p.At)
	}
	if out.PeakG == nil {
		out.Reason = noSidewaysReason(pts)
	}
	return out
}

// noSidewaysReason says why no point has a roll acceleration, from what the
// points kept, leaving out the first as the peak loop does: imported points
// carry no motion at all; stored attitude rates mostly under the floor mean the
// feed was too slow; roll peaks without a rate were recorded before 10 Hz capture; nothing
// at all means no attitude sensor or motion feed while it was recorded.
func noSidewaysReason(pts []recordedTrackPoint) string {
	if len(pts) > 0 {
		pts = pts[1:] // the first window is before the passage, as in the peak loop
	}
	var rated, slow int
	var peaks, imported bool
	for _, p := range pts {
		if p.Source == trackSourceImported {
			imported = true
		}
		if p.MotionRateHz != nil {
			rated++
			if *p.MotionRateHz < motionMinRateHz {
				slow++
			}
		}
		peaks = peaks || p.RollPeakDeg != nil
	}
	switch {
	case imported:
		return sidewaysReasonImported
	case rated > 0 && slow*2 > rated:
		return sidewaysReasonSlowFeed
	case rated > 0:
		return sidewaysReasonNoAccel
	case peaks:
		return sidewaysReasonBeforeCapture
	default:
		return sidewaysReasonNoMotion
	}
}

// helmHeightLog lets a failure to read the helm height reach the log once an
// hour, not on every passage read.
var helmHeightLog = &onceAnHour{}

// onceAnHour answers allow true at most once an hour. now is the clock, and
// time.Now when nil; tests give it their own.
type onceAnHour struct {
	mu   sync.Mutex
	now  func() time.Time
	last time.Time
}

func (o *onceAnHour) allow() bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	now := time.Now()
	if o.now != nil {
		now = o.now()
	}
	if !o.last.IsZero() && now.Sub(o.last) < time.Hour {
		return false
	}
	o.last = now
	return true
}

// currentHelmHeight is the operator's helm height, nil when blank.
func currentHelmHeight() (*float64, error) {
	if globalDocumentStore == nil {
		return nil, fmt.Errorf("the vessel details store is not running")
	}
	v, err := globalDocumentStore.GetVesselParticulars()
	if err != nil {
		return nil, err
	}
	return v.HelmHeightM, nil
}

// storeFigures writes the figures to the row, only if the range is still the
// one they were worked out for, so a trim racing this read cannot leave stale
// figures behind.
func (s *passageStore) storeFigures(rec passageRecord, f passageFigures) error {
	buf, err := json.Marshal(f)
	if err != nil {
		return err
	}
	if _, err := s.db.Exec(`UPDATE passages SET stats_json = ? WHERE id = ? AND start_ms = ? AND end_ms = ?`,
		string(buf), rec.ID, rec.Start.UnixMilli(), rec.End.UnixMilli()); err != nil {
		return fmt.Errorf("store passage stats: %w", err)
	}
	return nil
}

// ListPassageSummaries is the visible passages, newest first, with their
// stored figures.
func (s *passageStore) ListPassageSummaries() ([]passageSummary, error) {
	records, err := s.ListPassages()
	if err != nil {
		return nil, err
	}
	out := make([]passageSummary, 0, len(records))
	for _, r := range records {
		f, err := s.PassageFiguresFor(r)
		if err != nil {
			return nil, err
		}
		tz, reason := f.startTimezone(r.ID)
		out = append(out, passageSummary{passageRecord: r, Stats: f.Stats, Timezone: tz, TimezoneError: reason})
	}
	return out, nil
}
