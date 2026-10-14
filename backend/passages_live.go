package main

import (
	"log"
	"math"
	"sort"
	"strings"
	"sync"
	"time"
)

// The live seam between the SignalK snapshot and the track point recorder.
// buildTrackSample turns one poll tick into a trackSample; it reads through the
// same alarmReader the alarm engine and the derived paths use, so there is no
// second SignalK client.
//
// Every measurement older than trackReadingMaxAge is absent (ADR 0173
// decision 3). The generator state is the one exception: the GX publishes it
// only when it changes, so an hours-old "stopped" is still the state, not a
// stale reading.

const (
	trackSOGPath         = "navigation.speedOverGround"
	trackCOGPath         = "navigation.courseOverGroundTrue"
	trackWaterTankPrefix = "tanks.freshWater."
	trackDetectEvery     = time.Minute
)

func freshReading(s alarmSample, now time.Time) (float64, bool) {
	if !s.Present {
		return 0, false
	}
	age := alarmSampleAge(s, now)
	if age < 0 || age > trackReadingMaxAge.Seconds() {
		return 0, false
	}
	return s.Value, true
}

func freshPtr(read alarmReader, path string, now time.Time, scale float64) *float64 {
	v, ok := freshReading(read(path), now)
	if !ok {
		return nil
	}
	v *= scale
	return &v
}

// trackFuelRateLph is the total fuel rate across every engine. An engine that
// is switched off burns nothing and says so by going quiet, which is a known
// zero. An engine whose rate is merely old, or a boat that publishes no rate at
// all, leaves the total absent.
func trackFuelRateLph(read alarmReader, ratePaths []string, now time.Time) *float64 {
	if len(ratePaths) == 0 {
		return nil
	}
	var total float64
	for _, path := range ratePaths {
		sample := read(path)
		if sample.EngineOff {
			continue
		}
		v, ok := freshReading(sample, now)
		if !ok {
			return nil
		}
		total += v * 3600000 // m3/s to L/h
	}
	return &total
}

// trackFreshWaterLitres sums level x capacity over every freshWater tank. One
// tank with an old level leaves the sum absent rather than short.
func trackFreshWaterLitres(read alarmReader, tree map[string]any, now time.Time) *float64 {
	ids := map[string]bool{}
	for _, entry := range collectSignalKPaths(tree) {
		if strings.HasPrefix(entry.Path, trackWaterTankPrefix) && strings.HasSuffix(entry.Path, ".currentLevel") {
			ids[strings.TrimSuffix(strings.TrimPrefix(entry.Path, trackWaterTankPrefix), ".currentLevel")] = true
		}
	}
	if len(ids) == 0 {
		return nil
	}
	sorted := make([]string, 0, len(ids))
	for id := range ids {
		sorted = append(sorted, id)
	}
	sort.Strings(sorted)

	var litres float64
	for _, id := range sorted {
		level, ok := freshReading(read(trackWaterTankPrefix+id+".currentLevel"), now)
		capacity := read(trackWaterTankPrefix + id + ".capacity")
		if !ok || !capacity.Present {
			return nil
		}
		litres += level * capacity.Value * 1000 // m3 to L
	}
	return &litres
}

// buildTrackSample reads one tick. houseSoCPath is empty when no house bank is
// chosen in Vessel settings; the state of charge is then absent.
func buildTrackSample(snapshot *signalKSnapshot, state vesselStateData, houseBankPath string, anchorSetAt *time.Time, now time.Time) trackSample {
	read := derivedAwareAlarmReader(snapshot)
	tree := snapshot.selfTree()

	s := trackSample{
		At:  now,
		Lat: state.Latitude, Lon: state.Longitude,
		FixOK:       !state.GNSSCriticalAlert && state.PositionLastUpdateAge >= 0 && state.PositionLastUpdateAge <= trackReadingMaxAge.Seconds(),
		SOGKts:      freshPtr(read, trackSOGPath, now, metersPerSecondToKnots),
		TWSKts:      freshPtr(read, windSpeedTruePath, now, metersPerSecondToKnots),
		FuelLph:     trackFuelRateLph(read, fuelRatePaths(tree), now),
		WaterL:      trackFreshWaterLitres(read, tree, now),
		AnchorSetAt: anchorSetAt,
	}
	if cog := freshPtr(read, trackCOGPath, now, 180/math.Pi); cog != nil {
		deg := math.Mod(*cog+360, 360)
		s.COGDeg = &deg
	}
	if houseBankPath != "" {
		s.SoCPct = freshPtr(read, houseBankPath+".capacity.stateOfCharge", now, 100)
	}
	if gen := strings.TrimSpace(state.GeneratorState); gen != "" {
		s.GenState = &gen
	}
	return s
}

var (
	liveTrackMu       sync.Mutex
	globalTrackRecord *trackRecorder
	lastTrackDetectAt time.Time
	lastVesselLogErr  string
)

// observeLiveTrack is called from the server-owned poll tick (sampleTracks).
// A storage failure is logged loudly and the tick carries on: the poll must
// not stop feeding the other recorders because the log write failed.
func observeLiveTrack(snapshot *signalKSnapshot, state vesselStateData, settingsPath string, now time.Time) {
	if globalPassageStore == nil || globalTrackRecord == nil {
		return
	}

	houseBank := ""
	if vessel, err := loadVesselSettings(settingsPath); err != nil {
		if msg := err.Error(); msg != lastVesselLogErr {
			lastVesselLogErr = msg
			log.Printf("passages: house bank unavailable, state of charge not recorded: %v", err)
		}
	} else if vessel.HouseBank != nil {
		houseBank = strings.TrimSpace(vessel.HouseBank.Path)
	}

	var anchorSetAt *time.Time
	anchorWatchMu.RLock()
	if anchorWatchState != nil && !anchorWatchState.SetAt.IsZero() {
		t := anchorWatchState.SetAt.UTC()
		anchorSetAt = &t
	}
	anchorWatchMu.RUnlock()

	sample := buildTrackSample(snapshot, state, houseBank, anchorSetAt, now)

	liveTrackMu.Lock()
	defer liveTrackMu.Unlock()
	if _, err := globalTrackRecord.Observe(sample); err != nil {
		log.Printf("passages: %v", err)
		return
	}
	if now.Sub(lastTrackDetectAt) >= trackDetectEvery {
		lastTrackDetectAt = now
		if err := runPassageDetection(globalPassageStore); err != nil {
			log.Printf("passages: detection failed: %v", err)
		}
	}
}

// startPassageRecording opens the recorder over store and installs it for the
// poll tick.
func startPassageRecording(store *passageStore) error {
	rec, err := newTrackRecorder(store)
	if err != nil {
		return err
	}
	globalPassageStore = store
	startPassageNaming(store)
	startMarinaLookups(store)
	globalTrackRecord = rec
	return nil
}
