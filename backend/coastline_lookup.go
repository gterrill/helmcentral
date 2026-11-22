package main

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// ── reading the offline coastline around a position (ADR 0203, ADR 0300) ───
//
// Callers read only what is held on board. They never reach for Overpass: when
// the coastline they need is not there, they say exactly why.

// usableOwnShipFix reports the boat's position when it can be trusted. A
// coordinate-range check is not enough (the -1,-1 sentinel pair and a
// critical GNSS alert are both in range), so this is the same pair of checks
// the other background workers use.
func usableOwnShipFix(state vesselStateData, err error) (lat, lon float64, ok bool) {
	if err != nil || state.GNSSCriticalAlert || !hasUsableVesselPosition(state.Latitude, state.Longitude) {
		return 0, 0, false
	}
	return state.Latitude, state.Longitude, true
}

// coastlineDownloadHint ends every error about a square that is not held, so
// Mate's reply tells the operator where to ask for it.
const coastlineDownloadHint = "Squares can be downloaded in Settings → Offline coastline."

// coastlineProblem says what kept a lookup from the coastline, without wording
// it: the shelter estimate (shelterError) and the logbook's fetch check
// (coastlineReason) each word their own.
type coastlineProblem string

const (
	// coastlineNotOnInstall is no coastline store on this install.
	coastlineNotOnInstall coastlineProblem = "not on this install"
	// coastlineSettingUnreadable is a failure to read the on/off setting.
	coastlineSettingUnreadable coastlineProblem = "setting unreadable"
	coastlineOff               coastlineProblem = "off"
	// coastlineInland is a position whose own square is inland, well clear of the sea.
	coastlineInland coastlineProblem = "inland"
	// coastlineSquaresMissing is one or more squares within reach that are not held.
	coastlineSquaresMissing coastlineProblem = "squares missing"
)

// missingSquare is a square a lookup needs and does not hold, and why: "still
// downloading", "outside coverage", "download failed: <reason>" or "not downloaded".
type missingSquare struct {
	Label string
	State string
}

func (m missingSquare) String() string { return fmt.Sprintf("%s (%s)", m.Label, m.State) }

// joinMissing lists the squares as "Label (state), Label (state)".
func joinMissing(missing []missingSquare) string {
	parts := make([]string, len(missing))
	for i, m := range missing {
		parts[i] = m.String()
	}
	return strings.Join(parts, ", ")
}

// coastlineLookupError is a failed coastline lookup, as codes: what the caller
// tells the operator is the caller's own wording, read from Problem and Missing.
type coastlineLookupError struct {
	Problem coastlineProblem
	Missing []missingSquare
	Err     error
}

// Error is for logs and tests; no operator sees it.
func (e *coastlineLookupError) Error() string {
	switch {
	case e.Err != nil:
		return fmt.Sprintf("offline coastline: %s: %v", e.Problem, e.Err)
	case len(e.Missing) > 0:
		return fmt.Sprintf("offline coastline: %s: %s", e.Problem, joinMissing(e.Missing))
	}
	return "offline coastline: " + string(e.Problem)
}
func (e *coastlineLookupError) Unwrap() error { return e.Err }

// coastlineSetupError words a lookup that failed before any square was read,
// for the readers that say it alike (the shelter estimate and the route). Any
// other error comes back as it was.
func coastlineSetupError(err error) error {
	var ce *coastlineLookupError
	if !errors.As(err, &ce) {
		return err
	}
	switch ce.Problem {
	case coastlineNotOnInstall:
		return errors.New("Offline coastline is not available on this install")
	case coastlineSettingUnreadable:
		return fmt.Errorf("could not read the Offline coastline setting: %w", ce.Err)
	case coastlineOff:
		return errors.New("Offline coastline is off — turn it on in Settings → Offline coastline")
	}
	return err
}

// offlineCoastlineReader is what the Mate tool needs of the offline coastline:
// the switch, the squares held, and the boat's position (only to tell a square
// still on its way from one that will never come).
type offlineCoastlineReader struct {
	Enabled func() (bool, error)
	Store   *coastlineStore
	Boat    func() (lat, lon float64, ok bool)
	Now     func() time.Time
}

// shorelineSet is the shoreline around a position, with the date of the
// oldest square it came from.
type shorelineSet struct {
	Lines  []shorelineLine
	Oldest time.Time
}

// coverage is the squares the boat is (or will be) downloading, to tell "still
// downloading" from "not covered". Nil when the boat has no position. A failure
// to work coverage out is returned, to be reported only if a square is actually
// missing: with everything held, coverage does not matter.
func (r *offlineCoastlineReader) coverage() (map[coverageSquare]bool, error) {
	if r.Boat == nil {
		return nil, nil
	}
	bLat, bLon, ok := r.Boat()
	if !ok {
		return nil, nil
	}
	wanted, _, err := coverageWanted(bLat, bLon)
	if err != nil {
		return nil, fmt.Errorf("could not work out which squares the boat is downloading: %w", err)
	}
	in := make(map[coverageSquare]bool, len(wanted))
	for _, sq := range wanted {
		in[sq] = true
	}
	return in, nil
}

// aroundKey names the squares a position reads and the one it sits in: two
// positions with the same key read the same shoreline and fail alike.
type aroundKey struct {
	squares string
	own     coverageSquare
}

type aroundResult struct {
	set shorelineSet
	err error
}

// coastlinePlan is what reading the shoreline around a position needs, worked out
// from the squares' records alone: the squares, and a stamp of the state each is
// in. It holds no geometry, so a page can ask whether a stored figure still
// matches the coastline without reading a single line.
type coastlinePlan struct {
	// Needed is the squares within reach, in the order the store reads them.
	Needed []coverageSquare
	// Stamp changes when any ready square among them is downloaded again,
	// replaced or removed, or one that was missing arrives.
	Stamp string
	key   aroundKey
}

type planResult struct {
	plan coastlinePlan
	err  error
}

// plan checks that the shoreline within shelterMaxFetchNm of a position can be
// read, and names it (coastlinePlan), without reading any line. The checks are
// those around makes: the coastline must be on, and every square within reach
// held, except an inland one that was never downloaded, which holds no coastline
// by definition. A plan, or the reason there is none, is kept per key, so the
// spells that share their squares check them once.
func (l *coastlineLoader) plan(lat, lon float64) (coastlinePlan, error) {
	if err := l.ready(); err != nil {
		return coastlinePlan{}, err
	}
	needed, err := squaresWithin(lat, lon, shelterMaxFetchNm)
	if err != nil {
		return coastlinePlan{}, err
	}
	own, err := squareContaining(lat, lon)
	if err != nil {
		return coastlinePlan{}, err
	}
	key := aroundKey{squares: fmt.Sprint(sortedSquares(needed)), own: own}
	if got, ok := l.plans[key]; ok {
		return got.plan, got.err
	}
	p, err := l.checkHeld(needed, own)
	p.key = key
	l.plans[key] = planResult{p, err}
	return p, err
}

// around is the shoreline within shelterMaxFetchNm of a position, read through
// the loader so that squares and records are read once however many positions
// are asked about. The squares are those plan checks. A set, or the reason there
// is none, is kept per key, so the spells of a passage that share their squares
// read them once.
func (l *coastlineLoader) around(lat, lon float64) (shorelineSet, error) {
	p, err := l.plan(lat, lon)
	if err != nil {
		return shorelineSet{}, err
	}
	if got, ok := l.sets[p.key]; ok {
		return got.set, got.err
	}
	set, err := l.linesFor(p.Needed)
	l.sets[p.key] = aroundResult{set, err}
	return set, err
}

func (l *coastlineLoader) checkHeld(needed []coverageSquare, own coverageSquare) (coastlinePlan, error) {
	var missing []missingSquare
	var coverageErr error
	for _, sq := range needed {
		if rec, held := l.byKey[sq]; held && rec.Status == coastlineStatusReady {
			continue
		}
		if squareInland(sq) {
			// By definition no sea lies within 20 nm of it, so it holds no
			// coastline and no ray from water reaches it. Only the position's
			// own square being inland is worth saying.
			if sq == own {
				return coastlinePlan{}, &coastlineLookupError{Problem: coastlineInland}
			}
			continue
		}
		// Coverage is worked out once, the first time a square is found missing.
		var inCoverage map[coverageSquare]bool
		inCoverage, coverageErr = l.coverage()
		missing = append(missing, missingSquare{Label: sq.Label(), State: l.missingState(sq, inCoverage)})
	}
	if len(missing) > 0 {
		// A failure to work coverage out is the real problem, not "outside coverage".
		if coverageErr != nil {
			return coastlinePlan{}, coverageErr
		}
		return coastlinePlan{}, &coastlineLookupError{Problem: coastlineSquaresMissing, Missing: missing}
	}
	var ready []coastlineSquareRecord
	for _, sq := range sortedSquares(needed) {
		if rec, held := l.byKey[sq]; held && rec.Status == coastlineStatusReady {
			ready = append(ready, rec)
		}
	}
	return coastlinePlan{Needed: needed, Stamp: stampOfRecords(ready)}, nil
}

// missingState says why a square that is not held is not: a failed download, one
// on its way, or one the boat will never download.
func (l *coastlineLoader) missingState(sq coverageSquare, inCoverage map[coverageSquare]bool) string {
	rec, held := l.byKey[sq]
	switch {
	case held && rec.Status == coastlineStatusFailed:
		return fmt.Sprintf("download failed: %s", rec.LastError)
	case inCoverage[sq]:
		return "still downloading"
	default:
		return "outside coverage"
	}
}

// linesFor is the lines of the squares that are held and ready, in the store's
// order, and the date of the oldest square. An inland square that is not ready
// (never downloaded, still downloading or failed) holds no coastline, as inBox
// reads it.
func (l *coastlineLoader) linesFor(squares []coverageSquare) (shorelineSet, error) {
	oldest := time.Time{}
	out, err := joinSquareLines(squares, func(sq coverageSquare) ([]shorelineLine, error) {
		rec, held := l.byKey[sq]
		if !held || rec.Status != coastlineStatusReady {
			return nil, nil
		}
		lines, err := l.squareLines(sq)
		if err != nil {
			return nil, err
		}
		if d := rec.DownloadedAt; oldest.IsZero() || d.Before(oldest) {
			oldest = d
		}
		return lines, nil
	})
	if err != nil {
		return shorelineSet{}, err
	}
	return shorelineSet{Lines: out, Oldest: oldest}, nil
}
