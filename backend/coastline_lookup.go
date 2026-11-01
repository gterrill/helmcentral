package main

import (
	"fmt"
	"strings"
	"time"
)

// ── reading the offline coastline for a shelter estimate (ADR 0203) ───────
//
// anchorage_details reads only what is held on board. It never reaches for
// Overpass: when the coastline it needs is not there, it says exactly why.

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

func (r *offlineCoastlineReader) ShorelineAround(lat, lon float64) (shorelineSet, error) {
	if r == nil || r.Store == nil {
		return shorelineSet{}, fmt.Errorf("Offline coastline is not available on this install")
	}
	enabled, err := r.Enabled()
	if err != nil {
		return shorelineSet{}, fmt.Errorf("could not read the Offline coastline setting: %w", err)
	}
	if !enabled {
		return shorelineSet{}, fmt.Errorf("Offline coastline is off — turn it on in Settings → Offline coastline")
	}

	needed, err := squaresWithin(lat, lon, shelterMaxFetchNm)
	if err != nil {
		return shorelineSet{}, err
	}
	own, err := squareContaining(lat, lon)
	if err != nil {
		return shorelineSet{}, err
	}
	recs, err := r.Store.Records()
	if err != nil {
		return shorelineSet{}, err
	}
	byKey := make(map[coverageSquare]coastlineSquareRecord, len(recs))
	for _, rec := range recs {
		byKey[rec.Square] = rec
	}

	// The squares the boat is (or will be) downloading, to tell "still
	// downloading" from "not covered".
	var inCoverage map[coverageSquare]bool
	var boatErr error
	if r.Boat != nil {
		if bLat, bLon, ok := r.Boat(); ok {
			wanted, _, werr := coverageWanted(bLat, bLon)
			if werr != nil {
				// Reported only if a square is actually missing: with
				// everything held, coverage does not matter.
				boatErr = fmt.Errorf("could not work out which squares the boat is downloading: %w", werr)
			}
			inCoverage = make(map[coverageSquare]bool, len(wanted))
			for _, sq := range wanted {
				inCoverage[sq] = true
			}
		}
	}

	describe := func(sq coverageSquare) string {
		rec, held := byKey[sq]
		switch {
		case held && rec.Status == coastlineStatusFailed:
			return fmt.Sprintf("download failed: %s", rec.LastError)
		case inCoverage[sq]:
			return "still downloading"
		default:
			return "outside coverage"
		}
	}

	var missing []string
	ownMissing := false
	for _, sq := range needed {
		if rec, held := byKey[sq]; held && rec.Status == coastlineStatusReady {
			continue
		}
		if squareInland(sq) {
			// By definition no sea lies within 20 nm of it, so it holds no
			// coastline and no ray from water reaches it. Only the position's
			// own square being inland is worth saying.
			if sq == own {
				return shorelineSet{}, fmt.Errorf("%.4f, %.4f is inland, well clear of the sea, so there is no anchorage shelter to estimate there", lat, lon)
			}
			continue
		}
		if sq == own {
			ownMissing = true
		}
		missing = append(missing, fmt.Sprintf("%s (%s)", sq.Label(), describe(sq)))
	}
	if len(missing) > 0 {
		if boatErr != nil {
			return shorelineSet{}, boatErr
		}
		if ownMissing {
			switch describe(own) {
			case "outside coverage":
				return shorelineSet{}, fmt.Errorf("%.4f, %.4f is outside Offline coastline coverage: Helmcentral holds the squares within %.0f nm of the boat and any it downloaded earlier (%s is not among them)",
					lat, lon, coastlineCoverageRadiusNm, own.Label())
			case "still downloading":
				return shorelineSet{}, fmt.Errorf("Offline coastline coverage for this area is still downloading (%s); try again later", own.Label())
			default:
				return shorelineSet{}, fmt.Errorf("Offline coastline coverage for this area is not available: %s: %s", own.Label(), describe(own))
			}
		}
		return shorelineSet{}, fmt.Errorf("shelter at %.4f, %.4f needs neighbouring Offline coastline squares that are not held: %s", lat, lon, strings.Join(missing, ", "))
	}

	// Inland squares that were never downloaded hold no coastline.
	var used []coverageSquare
	for _, sq := range needed {
		if _, held := byKey[sq]; held || !squareInland(sq) {
			used = append(used, sq)
		}
	}
	lines, err := r.Store.Lines(used)
	if err != nil {
		return shorelineSet{}, err
	}
	oldest := time.Time{}
	for _, sq := range used {
		if d := byKey[sq].DownloadedAt; oldest.IsZero() || d.Before(oldest) {
			oldest = d
		}
	}
	return shorelineSet{Lines: lines, Oldest: oldest}, nil
}
