package main

import (
	"database/sql"
	"errors"
	"log"
	"time"
)

// Naming a passage's ends from the place-names plugin selected in settings
// (ADR 0101). It runs after a passage is stored, off the poll tick, because
// the plugin may call out over the network. A plugin that has no answer, or
// fails, leaves the name empty; the operator can type one. A name the operator
// has edited, or already filled in, is never replaced.

// passageCreated is told the id of every passage the detector or an import
// stores. Nil in tests unless one is checking it.
var passageCreated func(id string)

func announcePassage(id string) {
	if passageCreated != nil {
		passageCreated(id)
	}
}

func (s *passageStore) firstPointFrom(t time.Time) (*recordedTrackPoint, error) {
	return s.onePoint(`WHERE t_ms >= ? ORDER BY t_ms, id LIMIT 1`, t)
}

func (s *passageStore) lastPointUntil(t time.Time) (*recordedTrackPoint, error) {
	return s.onePoint(`WHERE t_ms <= ? ORDER BY t_ms DESC, id DESC LIMIT 1`, t)
}

func (s *passageStore) onePoint(tail string, t time.Time) (*recordedTrackPoint, error) {
	p, err := scanTrackPoint(s.db.QueryRow(`SELECT `+trackPointColumns+` FROM track_points `+tail, t.UnixMilli()))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// FillPassageNames asks lookup for the places at a passage's start and end and
// stores any answer that is not empty into a name that is still empty, and only
// while the passage is unedited.
func (s *passageStore) FillPassageNames(id string, lookup func(lat, lon float64) string) error {
	rec, err := s.GetPassage(id)
	if err != nil {
		return err
	}
	if rec.Edited {
		return nil
	}
	if rec.FromName == "" {
		if p, err := s.firstPointFrom(rec.Start); err != nil {
			return err
		} else if p != nil {
			if name := lookup(p.Lat, p.Lon); name != "" {
				if _, err := s.db.Exec(`UPDATE passages SET from_name = ? WHERE id = ? AND edited = 0 AND from_name = ''`, name, id); err != nil {
					return err
				}
			}
		}
	}
	if rec.ToName == "" {
		if p, err := s.lastPointUntil(rec.End); err != nil {
			return err
		} else if p != nil {
			if name := lookup(p.Lat, p.Lon); name != "" {
				if _, err := s.db.Exec(`UPDATE passages SET to_name = ? WHERE id = ? AND edited = 0 AND to_name = ''`, name, id); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// startPassageNaming runs one worker that names passages as they are announced.
func startPassageNaming(store *passageStore) {
	queue := make(chan string, 256)
	passageCreated = func(id string) {
		select {
		case queue <- id:
		default:
			log.Printf("passages: naming queue full, passage %s left unnamed", id)
		}
	}
	go func() {
		for id := range queue {
			err := store.FillPassageNames(id, func(lat, lon float64) string {
				return resolveAndCachePlaceName(placeNameProviderResolve, lat, lon)
			})
			if err != nil {
				log.Printf("passages: naming %s: %v", id, err)
			}
		}
	}()
}
