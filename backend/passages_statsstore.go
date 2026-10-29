package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
)

// A passage's figures are worked out from its track points once and kept on
// its row. They are cleared whenever the passage's range or the points inside
// it change (trim, split, merge, an import into its range), and worked out
// again the next time they are read.

// passageFigures is what is kept on the row: the stats, and the longitude of
// the first point the page uses for the boat's own time zone.
type passageFigures struct {
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
			if f.StartLat != nil || f.StartLon == nil {
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
	st := passageFigures{Stats: computePassageStats(pts), StartLon: startLon(pts), StartLat: startLat(pts)}
	st.Timezone, _ = st.lookupStartZone(rec.ID)
	if err := s.storeFigures(rec, st); err != nil {
		return st, err
	}
	return st, nil
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
