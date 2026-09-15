package main

import (
	"context"
	"log"
	"math"
	"sync"
	"time"
)

// Server-side anchor auto-raise (ADR 0099).
//
// The auto-close that used to run in the browser (use-anchor-watch-auto-close.ts)
// had no single decision-maker: every open tab evaluated the same telemetry
// independently, so dropping the hook and backing down to set it could have
// three screens each decide, within a second of each other, that the boat
// had left and DELETE the watch - which is exactly what happened on the boat
// (see docs/adr/0099). This watcher is the one place that decision gets made
// now, on the server's own fresh telemetry, on a ticker independent of any
// browser being open at all.
const (
	// autoRaiseSustainSeconds is how long every condition below must hold
	// continuously before the watch is auto-raised. Long enough that
	// backing down to set the hook - which stops the boat within a few
	// seconds once the rode snubs - never sustains long enough to fire.
	autoRaiseSustainSeconds = 15

	// autoRaiseMinSOGKts is the speed-over-ground floor. This, not a longer
	// fixed grace period, is what actually tells backing down (SOG falls
	// back toward zero once the rode snubs) apart from getting under way
	// (SOG keeps climbing) - a boat can sit outside radius+buffer for a
	// while at anchor in a strong current or mid-reposition, so distance
	// and time alone aren't enough.
	autoRaiseMinSOGKts = 3.0

	autoRaiseCheckInterval = 5 * time.Second

	// autoRaiseReasonEnginesRunning is the (currently only) reason code
	// recorded on a successful auto-raise and surfaced to clients via
	// last_auto_raise. Named rather than inlined so a future second rule,
	// if one is ever added, doesn't have to guess at the string this one
	// already shipped.
	autoRaiseReasonEnginesRunning = "engines_running"
)

// autoRaiseSample is everything the watcher needs about current conditions,
// gathered in one fetchSignalKVesselState call so check() reasons about a
// single consistent instant rather than several separate reads that could
// each observe a different one.
//
// PositionOK and GNSSCritical are kept as two separate fields, mirroring
// anchorDragWatcher's position() return (alarm_anchor.go), rather than
// folded into one bool here: it is what lets a test pin "the -1,-1 sentinel
// never fires" and "a critical GNSS fix never fires" as two independent
// conditions instead of one combined guess.
type autoRaiseSample struct {
	Lat, Lon float64
	// PositionOK is hasUsableVesselPosition(Lat, Lon): the coordinate-range
	// and -1,-1/0,0-sentinel check alone.
	PositionOK   bool
	GNSSCritical bool
	// SOGKts is speedOverGroundKts as fetchSignalKVesselState reports it:
	// -1 means unavailable, which this feature treats as NOT eligible, not
	// as "assume stationary" or "assume moving."
	SOGKts float64
	// Engine0RPM/Engine1RPM are readEngineRPM's own values: -1 for both "no
	// reading at all" and "the reading is older than defaultRPMMaxAge."
	Engine0RPM float64
	Engine1RPM float64
}

type anchorAutoRaiseWatcher struct {
	// enabled, anchor, sample and raise are injectable so tests drive this
	// watcher without touching settings.yaml, package-level anchor state or
	// the network - same shape as anchorDragWatcher (alarm_anchor.go).
	enabled func() bool
	anchor  func() *anchorWatchData
	sample  func() (autoRaiseSample, bool) // bool: whether the read succeeded at all
	// raise attempts to raise the anchor watch this watcher evaluated -
	// expectedSetAt is that watch's session identity (anchorWatchData.SetAt,
	// which stays fixed across a reposition, per the comment on setAnchorWatch)
	// at the moment check() decided to raise, not necessarily the moment
	// raise() actually runs. w.sample() makes a real network round trip
	// earlier in the same tick, which is a real window for a concurrent
	// operator action (Raise, then Drop a new anchorage) to replace
	// anchorWatchState before raise() executes. raise must re-confirm,
	// under anchorLifecycleMu, that the current watch is still that same
	// session before touching anything - raised is false, with a nil
	// error, when it finds a different (or no) session, which check()
	// treats as "skip, don't record, reset" rather than a failure.
	raise func(expectedSetAt time.Time) (raised bool, err error)

	// conditionsSince is when every condition most recently started holding
	// continuously; the zero time means it is not currently holding.
	// raiseAttempted latches once raise() has been called for this
	// continuous window, so a failed raise is logged once and not retried
	// every tick while conditions keep holding - it only gets another
	// chance once conditions have dropped and re-armed.
	conditionsSince time.Time
	raiseAttempted  bool
}

func newAnchorAutoRaiseWatcher() *anchorAutoRaiseWatcher {
	return &anchorAutoRaiseWatcher{
		enabled: func() bool {
			settingsPath := getEnv("SETTINGS_FILE", "../settings.yaml")
			settings, err := readSettings(settingsPath)
			if err != nil {
				// Fail-fast, per AGENTS.md: an unreadable settings file must
				// never be treated as "the feature is on."
				return false
			}
			return buildSettingsPayload(settings).Anchor.AutoRaiseOnMotoring
		},
		anchor: func() *anchorWatchData {
			anchorWatchMu.RLock()
			defer anchorWatchMu.RUnlock()
			return anchorWatchState
		},
		sample: func() (autoRaiseSample, bool) {
			state, err := fetchSignalKVesselState()
			if err != nil {
				return autoRaiseSample{}, false
			}
			return autoRaiseSample{
				Lat:          state.Latitude,
				Lon:          state.Longitude,
				PositionOK:   hasUsableVesselPosition(state.Latitude, state.Longitude),
				GNSSCritical: state.GNSSCriticalAlert,
				SOGKts:       state.SpeedOverGroundKts,
				Engine0RPM:   state.Engine0RPM,
				Engine1RPM:   state.Engine1RPM,
			}, true
		},
		raise: func(expectedSetAt time.Time) (bool, error) {
			anchorLifecycleMu.Lock()
			defer anchorLifecycleMu.Unlock()

			// Fresh read, under the lock that also guards Drop/reposition/
			// PATCH/Raise: the anchor() read at the top of check() is stale
			// by the time we get here (sample() has since made a real
			// network round trip), so this is the one read that actually
			// matters. A different session - a different SetAt, or none at
			// all - means the watch this watcher was timing is gone, and
			// whatever is here now never met the conditions itself.
			anchorWatchMu.RLock()
			current := anchorWatchState
			anchorWatchMu.RUnlock()
			if current == nil || !current.SetAt.Equal(expectedSetAt) {
				return false, nil
			}

			if err := raiseAnchorWatch(); err != nil {
				return false, err
			}
			return true, nil
		},
	}
}

func engineRunning(rpm float64) bool {
	return rpm > 0 && !math.IsInf(rpm, 0)
}

// check evaluates one tick. now is injected so tests control the sustain
// window without sleeping - same pattern as anchorDragWatcher.check.
func (w *anchorAutoRaiseWatcher) check(now time.Time) {
	anchor := w.anchor()
	if anchor == nil {
		// The watch was raised (by us or by the operator) or never dropped.
		w.reset()
		return
	}

	if !w.enabled() {
		w.reset()
		return
	}

	sample, ok := w.sample()
	if !ok {
		// Missing telemetry means no auto-raise, never an assumed value.
		w.reset()
		return
	}

	engines := engineRunning(sample.Engine0RPM) || engineRunning(sample.Engine1RPM)

	positionValid := sample.PositionOK && !sample.GNSSCritical
	var distance float64
	outsideRadius := false
	if positionValid {
		distance = haversineMeters(anchor.Lat, anchor.Lon, sample.Lat, sample.Lon)
		// Same buffer as the drag watcher (alarm_anchor.go's
		// anchorDragBufferMeters, the frontend's old DRAG_BUFFER_METERS): a
		// vessel still swinging on its rode must never trip this.
		outsideRadius = distance > anchor.RadiusMeters+anchorDragBufferMeters
	}

	// -1 (unavailable) is always below autoRaiseMinSOGKts, so this
	// comparison alone already treats missing SOG as ineligible - no
	// separate sentinel check needed.
	fastEnough := sample.SOGKts >= autoRaiseMinSOGKts

	hold := engines && positionValid && outsideRadius && fastEnough

	if !hold {
		w.reset()
		return
	}

	if w.conditionsSince.IsZero() {
		w.conditionsSince = now
	}

	if w.raiseAttempted {
		// Already tried for this continuous window - do not retry in a
		// tight loop while conditions stay true; wait for them to drop and
		// re-arm instead.
		return
	}

	if now.Sub(w.conditionsSince) < autoRaiseSustainSeconds*time.Second {
		return
	}

	w.raiseAttempted = true
	log.Printf("anchor auto-raise: conditions sustained %ds - rpm0=%.0f rpm1=%.0f distance=%.0fm radius=%.0fm sog=%.1fkt: raising",
		autoRaiseSustainSeconds, sample.Engine0RPM, sample.Engine1RPM, distance, anchor.RadiusMeters, sample.SOGKts)

	raised, err := w.raise(anchor.SetAt)
	if err != nil {
		// Not retried while hold stays true (raiseAttempted above). The
		// watch stays active and outside radius+buffer, which is exactly
		// the condition the anchor-drag alarm (alarm_anchor.go) already
		// watches for, so this failure does not leave the operator with no
		// signal at all - it leaves them with the existing, already-tested
		// drag alarm instead of a second, redundant notification path.
		log.Printf("anchor auto-raise: FAILED to raise: %v (will not retry until conditions drop and re-arm; the anchor-drag alarm covers the boat remaining outside the watch radius)", err)
		return
	}
	if !raised {
		// The anchor watch changed session (a different SetAt, or none at
		// all) between check()'s read of it and raise() actually running -
		// see the comment on the raise field. This is not our session
		// anymore, so nothing was raised and nothing is recorded. Reset,
		// not latch: whatever is active now (if anything) is a fresh
		// session that hasn't been timed at all, and w.anchor() will pick
		// it up fresh on the next tick.
		log.Printf("anchor auto-raise: skipped - the anchor watch changed to a different session before the raise ran; not raising, resetting")
		w.reset()
		return
	}

	recordAutoRaise(now, autoRaiseReasonEnginesRunning)
	log.Printf("anchor auto-raise: raised - engines running, under way outside the watch radius")
}

func (w *anchorAutoRaiseWatcher) reset() {
	w.conditionsSince = time.Time{}
	w.raiseAttempted = false
}

func startAnchorAutoRaiseWatcher(ctx context.Context, interval time.Duration) {
	watcher := newAnchorAutoRaiseWatcher()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			watcher.check(now.UTC())
		}
	}
}

// anchorAutoRaiseRecord is the evidence clients need to explain an automatic
// raise they didn't ask for - "at" so a client can tell whether it has
// already shown this one, "reason" so the toast can say why.
type anchorAutoRaiseRecord struct {
	At     time.Time
	Reason string
}

var (
	autoRaiseMu   sync.RWMutex
	lastAutoRaise *anchorAutoRaiseRecord
)

func recordAutoRaise(at time.Time, reason string) {
	autoRaiseMu.Lock()
	defer autoRaiseMu.Unlock()
	lastAutoRaise = &anchorAutoRaiseRecord{At: at, Reason: reason}
}

func getLastAutoRaise() *anchorAutoRaiseRecord {
	autoRaiseMu.RLock()
	defer autoRaiseMu.RUnlock()
	return lastAutoRaise
}

// withLastAutoRaise adds last_auto_raise to a GET /api/anchor-watch response
// when the server has ever auto-raised a watch (in this process's lifetime -
// there is no persistence, so a restart clears it same as the drag alarm's
// own in-memory state does). Every client already polls this endpoint
// (use-anchor-watch.ts), so this is the least invasive way for each of them
// to learn a raise happened without any of them having decided it
// themselves - the same problem the old browser-side auto-close had.
func withLastAutoRaise(resp map[string]any) map[string]any {
	if record := getLastAutoRaise(); record != nil {
		resp["last_auto_raise"] = map[string]any{
			"at":     record.At.Format(time.RFC3339),
			"reason": record.Reason,
		}
	}
	return resp
}
