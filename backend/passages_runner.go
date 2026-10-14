package main

import "fmt"

// runPassageDetection reads the track points since the detector last stopped,
// stores any passage that has ended, and remembers where to resume. A passage
// that overlaps one already stored, hidden and edited ones included, is left
// alone: the operator's version wins (ADR 0173 decision 5).
func runPassageDetection(store *passageStore) error {
	resume, ok, err := store.DetectorResume()
	if err != nil {
		return err
	}
	if !ok {
		first, any, err := store.FirstTrackPointTime()
		if err != nil || !any {
			return err
		}
		resume = first
	}

	pts, err := store.TrackPointsSince(resume)
	if err != nil {
		return err
	}
	if len(pts) == 0 {
		return nil
	}

	det := detectLoggedPassages(pts)
	for _, lp := range det.Closed {
		overlaps, err := store.PassageOverlaps(lp.Start, lp.End)
		if err != nil {
			return err
		}
		if overlaps {
			continue
		}
		rec, err := store.InsertPassage(passageRecord{Start: lp.Start, End: lp.End})
		if err != nil {
			return fmt.Errorf("store detected passage: %w", err)
		}
		store.PassageStatsFor(rec) // warm the stored figures
		announcePassage(rec.ID)
	}
	return store.SetDetectorResume(det.Resume)
}
