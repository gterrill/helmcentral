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

// passageNamesWake nudges the naming worker after the detector or an import
// stores passages. Nil until the worker is running.
var passageNamesWake func()

func announcePassage(string) {
	if passageNamesWake != nil {
		passageNamesWake()
	}
}

const (
	passageNamingRetryAfter = 24 * time.Hour
	passageNamingScanEvery  = 10 * time.Minute
)

// PassagesNeedingNames lists passages with an empty origin or destination the
// operator has not named themselves, that have not been tried in the last
// retryAfter. Filing, hiding or moving a passage does not stop it being named.
func (s *passageStore) PassagesNeedingNames(now time.Time, retryAfter time.Duration) ([]string, error) {
	rows, err := s.db.Query(`SELECT id FROM passages
		WHERE ((from_name = '' AND from_name_edited = 0) OR (to_name = '' AND to_name_edited = 0))
		AND (name_attempted_ms IS NULL OR name_attempted_ms < ?)
		ORDER BY start_ms`, now.Add(-retryAfter).UnixMilli())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
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
// stores any answer that is not empty into a name that is still empty and that
// the operator has not edited.
func (s *passageStore) FillPassageNames(id string, lookup func(lat, lon float64) string) error {
	rec, err := s.GetPassage(id)
	if err != nil {
		return err
	}
	// Stamped first, so a lookup that fails or hangs is not tried again in a
	// tight loop; the scan retries it after the retry window.
	if _, err := s.db.Exec(`UPDATE passages SET name_attempted_ms = ? WHERE id = ?`, time.Now().UnixMilli(), id); err != nil {
		return err
	}
	if rec.FromName == "" && !rec.FromNameEdited {
		if p, err := s.firstPointFrom(rec.Start); err != nil {
			return err
		} else if p != nil {
			if name := lookup(p.Lat, p.Lon); name != "" {
				if _, err := s.db.Exec(`UPDATE passages SET from_name = ? WHERE id = ? AND from_name_edited = 0 AND from_name = ''`, name, id); err != nil {
					return err
				}
			}
		}
	}
	if rec.ToName == "" && !rec.ToNameEdited {
		if p, err := s.lastPointUntil(rec.End); err != nil {
			return err
		} else if p != nil {
			if name := lookup(p.Lat, p.Lon); name != "" {
				if _, err := s.db.Exec(`UPDATE passages SET to_name = ? WHERE id = ? AND to_name_edited = 0 AND to_name = ''`, name, id); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

type namingWorker struct {
	store  *passageStore
	lookup func(lat, lon float64) string
	every  time.Duration
	signal chan struct{}
}

func newNamingWorker(store *passageStore, lookup func(lat, lon float64) string, every time.Duration) *namingWorker {
	return &namingWorker{store: store, lookup: lookup, every: every, signal: make(chan struct{}, 1)}
}

// wake asks for a scan; wakes arriving while one is pending coalesce.
func (w *namingWorker) wake() {
	select {
	case w.signal <- struct{}{}:
	default:
	}
}

// run scans at once (this is what resumes the backlog after a restart), then
// on every wake and every tick, until done closes.
func (w *namingWorker) run(done <-chan struct{}) {
	ticker := time.NewTicker(w.every)
	defer ticker.Stop()
	for {
		w.scan()
		select {
		case <-done:
			return
		case <-w.signal:
		case <-ticker.C:
		}
	}
}

func (w *namingWorker) scan() {
	ids, err := w.store.PassagesNeedingNames(time.Now(), passageNamingRetryAfter)
	if err != nil {
		log.Printf("passages: naming scan: %v", err)
		return
	}
	for _, id := range ids {
		if err := w.store.FillPassageNames(id, w.lookup); err != nil {
			log.Printf("passages: naming %s: %v", id, err)
		}
	}
}

// startPassageNaming runs the naming worker for the life of the process.
func startPassageNaming(store *passageStore) {
	w := newNamingWorker(store, func(lat, lon float64) string {
		return resolveAndCachePlaceName(placeNameProviderResolve, lat, lon)
	}, passageNamingScanEvery)
	passageNamesWake = w.wake
	go w.run(make(chan struct{}))
}
