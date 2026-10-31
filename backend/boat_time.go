package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"os"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ringsaturn/tzf"
)

// Boat time comes from position (ADR 0230). The zone at a latitude and
// longitude is looked up in the timezone-boundary-builder data that tzf
// embeds: a civil zone near land (Australia/Sydney, with its daylight saving),
// a nautical Etc/GMT zone offshore. A fixed offset taken from longitude, which
// this replaced, was wrong in New South Wales every summer and in South
// Australia all year.

var (
	zoneFinderOnce sync.Once
	zoneFinderVal  tzf.F
	zoneFinderErr  error
)

// zoneFinder builds tzf's default finder on first use, never at boot: the
// index costs about 30 MB of heap and a second or so on a Pi 5, which a
// process that never needs a zone should not pay.
func zoneFinder() (tzf.F, error) {
	zoneFinderOnce.Do(func() {
		zoneFinderVal, zoneFinderErr = tzf.NewDefaultFinder()
		if zoneFinderErr == nil {
			finderReady.Store(true)
		}
	})
	return zoneFinderVal, zoneFinderErr
}

// placeZone is a zone: the location to format times in and the IANA name to
// hand to an upstream API.
type placeZone struct {
	Loc  *time.Location
	Name string
}

var zoneLocations sync.Map // IANA name -> *time.Location

func loadZoneLocation(name string) (*time.Location, error) {
	if v, ok := zoneLocations.Load(name); ok {
		return v.(*time.Location), nil
	}
	loc, err := time.LoadLocation(name)
	if err != nil {
		return nil, fmt.Errorf("time zone %q is not in the embedded zone database: %w", name, err)
	}
	zoneLocations.Store(name, loc)
	return loc, nil
}

// zoneAt is the zone at a position: where a place keeps its own time. An
// empty answer from the finder is an error, never a quiet UTC.
func zoneAt(lat, lon float64) (placeZone, error) {
	if !validLatLon(lat, lon) {
		return placeZone{}, fmt.Errorf("no time zone for position %.4f, %.4f: out of range", lat, lon)
	}
	f, err := zoneFinder()
	if err != nil {
		return placeZone{}, fmt.Errorf("the time zone index could not be built: %w", err)
	}
	name := f.GetTimezoneName(lon, lat)
	if name == "" {
		return placeZone{}, fmt.Errorf("no time zone found for position %.4f, %.4f", lat, lon)
	}
	loc, err := loadZoneLocation(name)
	if err != nil {
		return placeZone{}, err
	}
	return placeZone{Loc: loc, Name: name}, nil
}

// zoneAtName rebuilds a zone from a stored IANA name.
func zoneAtName(name string) (placeZone, error) {
	loc, err := loadZoneLocation(name)
	if err != nil {
		return placeZone{}, err
	}
	return placeZone{Loc: loc, Name: name}, nil
}

var utcZone = placeZone{Loc: time.UTC, Name: "UTC"}

func offsetText(seconds int) string {
	sign := "+"
	if seconds < 0 {
		sign, seconds = "-", -seconds
	}
	h, m := seconds/3600, (seconds%3600)/60
	if m == 0 {
		return fmt.Sprintf("UTC%s%d", sign, h)
	}
	return fmt.Sprintf("UTC%s%d:%02d", sign, h, m)
}

// Label is how an operator says the zone at the instant being shown:
// "AEDT (UTC+11)" in a Sydney summer, "UTC+11" offshore where the zone has no
// name of its own. It is computed at `at` because daylight saving makes the
// answer depend on the date.
func (z placeZone) Label(at time.Time) string {
	abbr, off := at.In(z.Loc).Zone()
	if z.Loc == time.UTC || (off == 0 && abbr == "UTC") {
		return "UTC"
	}
	text := offsetText(off)
	if abbr != "" && !strings.ContainsAny(abbr[:1], "+-0123456789") && abbr != "UTC" {
		return abbr + " (" + text + ")"
	}
	return text
}

// Describe is the zone for Mate's prompt: the IANA name and the offset at
// the instant, "Australia/Brisbane, UTC+10".
func (z placeZone) Describe(at time.Time) string {
	if z.Loc == time.UTC {
		return "UTC"
	}
	_, off := at.In(z.Loc).Zone()
	return z.Name + ", " + offsetText(off)
}

// boatTime is the zone the boat keeps. LastFix is set when it comes from the
// last trusted fix because there is no usable one now. NoFix is set when no
// fix has ever been seen, so the zone is UTC. Err is set when the stored
// last-fix zone could not be read; the zone is UTC then and the caller can
// say why.
type boatTime struct {
	placeZone
	LastFix bool
	NoFix   bool
	Err     error
}

// Source names where the zone came from, for the vessel stream.
func (b boatTime) Source() string {
	switch {
	case b.NoFix:
		return "none"
	case b.LastFix:
		return "last_fix"
	default:
		return "fix"
	}
}

// Qualifier is the words that follow the zone when it is not a live one.
func (b boatTime) Qualifier() string {
	switch {
	case b.NoFix:
		return "no position yet"
	case b.LastFix:
		return "last fix"
	default:
		return ""
	}
}

func boatZoneFilePath() string {
	return cacheFilePath("BOAT_ZONE_FILE", "data/boat_zone.json")
}

type storedBoatZone struct {
	Zone string `json:"zone"`
}

// The last-fix rule (ADR 0230) lives here and only here. With no usable
// own-ship fix the boat keeps the zone of the last trusted one, which is
// remembered across restarts in a small file. It is logged once each time it
// starts to apply. Before any fix has ever been seen the zone is UTC.
var boatZoneMemory struct {
	mu          sync.Mutex
	loaded      bool
	zone        string // IANA name of the last trusted fix's zone
	loadErr     error
	loggedFall  bool
	loggedWait  bool // the zone index was still loading when a good fix came in
	loggedError string
	loggedWrite string
}

func resetBoatZoneState() {
	boatZoneMemory.mu.Lock()
	defer boatZoneMemory.mu.Unlock()
	boatZoneMemory.loaded = false
	boatZoneMemory.zone = ""
	boatZoneMemory.loadErr = nil
	boatZoneMemory.loggedFall = false
	boatZoneMemory.loggedWait = false
	boatZoneMemory.loggedError = ""
	boatZoneMemory.loggedWrite = ""
	zoneCells.mu.Lock()
	zoneCells.m = nil
	zoneCells.mu.Unlock()
}

// loadBoatZoneLocked reads the stored zone once. A missing file means no fix
// has ever been seen; anything else unreadable is kept as an error.
func loadBoatZoneLocked() {
	if boatZoneMemory.loaded {
		return
	}
	boatZoneMemory.loaded = true
	path := boatZoneFilePath()
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return
	}
	if err != nil {
		boatZoneMemory.loadErr = fmt.Errorf("could not read the last-fix time zone %s: %w", path, err)
		return
	}
	if len(raw) == 0 {
		return
	}
	var s storedBoatZone
	if err := json.Unmarshal(raw, &s); err != nil {
		boatZoneMemory.loadErr = fmt.Errorf("the last-fix time zone file %s is not valid: %w", path, err)
		return
	}
	if _, err := loadZoneLocation(s.Zone); err != nil {
		boatZoneMemory.loadErr = fmt.Errorf("the last-fix time zone file %s names an unknown zone: %w", path, err)
		return
	}
	boatZoneMemory.zone = s.Zone
}

// Positions are rounded to 0.01 degrees (about a kilometre) before the zone is
// looked up, so a frame that arrives every second costs a map read. A boat
// within a kilometre of a zone boundary may be read as on the wrong side for as
// long as it sits there; the next fix beyond that distance corrects it.
type zoneCellKey struct{ lat, lon int }

const zoneCellCap = 512

var zoneCells struct {
	mu sync.RWMutex
	m  map[zoneCellKey]placeZone
}

// finderReady is set once the zone index has been built.
var finderReady atomic.Bool

// finderStarted is set when a background build of the zone index has been
// started, so a busy caller starts it once and not on every call.
var finderStarted atomic.Bool

// startZoneFinder builds the zone index in the background, once. It reports
// whether this call started the build.
func startZoneFinder() bool {
	if !finderStarted.CompareAndSwap(false, true) {
		return false
	}
	go zoneFinder()
	return true
}

func zoneAtCached(lat, lon float64) (placeZone, error) {
	key := zoneCellKey{int(math.Round(lat * 100)), int(math.Round(lon * 100))}
	zoneCells.mu.RLock()
	z, ok := zoneCells.m[key]
	zoneCells.mu.RUnlock()
	if ok {
		return z, nil
	}
	z, err := zoneAt(lat, lon)
	if err != nil {
		return placeZone{}, err
	}
	zoneCells.mu.Lock()
	if zoneCells.m == nil || len(zoneCells.m) >= zoneCellCap {
		zoneCells.m = make(map[zoneCellKey]placeZone, zoneCellCap)
	}
	zoneCells.m[key] = z
	zoneCells.mu.Unlock()
	return z, nil
}

// rememberedZone is the stored last-fix zone, if any, and any problem reading it.
func rememberedZone() (string, error) {
	boatZoneMemory.mu.Lock()
	defer boatZoneMemory.mu.Unlock()
	loadBoatZoneLocked()
	return boatZoneMemory.zone, boatZoneMemory.loadErr
}

// boatTimeAt is the boat's time zone. fixOK says whether lat and lon are a
// usable own-ship fix; when it is, the zone is looked up and remembered, and
// when it is not the last remembered zone applies.
//
// The lookup runs outside the lock. A caller that already has a remembered
// zone is never held up by the first build of the zone index: until the index
// is ready it gets the remembered zone, marked as the last fix, and the index
// builds in the background.
func boatTimeAt(lat, lon float64, fixOK bool) boatTime {
	if fixOK {
		if !finderReady.Load() {
			if zone, _ := rememberedZone(); zone != "" {
				startZoneFinder()
				return boatTimeLastFix(true)
			}
		}
		z, err := zoneAtCached(lat, lon)
		if err == nil {
			rememberBoatZone(z.Name)
			return boatTime{placeZone: z}
		}
		// A usable position with no zone is a data problem: say so, then
		// apply the last-fix rule rather than guess.
		boatZoneMemory.mu.Lock()
		logBoatZoneErrorLocked(err)
		boatZoneMemory.mu.Unlock()
	}
	return boatTimeLastFix(false)
}

// rememberBoatZone records a live fix's zone. A failed write still updates the
// zone held in memory, so the boat's clock is right until the next restart,
// and each distinct write failure is logged once.
func rememberBoatZone(name string) {
	boatZoneMemory.mu.Lock()
	defer boatZoneMemory.mu.Unlock()
	loadBoatZoneLocked()
	boatZoneMemory.loggedFall = false
	if boatZoneMemory.zone == name && boatZoneMemory.loadErr == nil {
		return
	}
	boatZoneMemory.zone = name
	boatZoneMemory.loadErr = nil
	if err := writeJSONFileAtomic(boatZoneFilePath(), storedBoatZone{Zone: name}); err != nil {
		if msg := err.Error(); msg != boatZoneMemory.loggedWrite {
			boatZoneMemory.loggedWrite = msg
			log.Printf("boat time: could not remember the zone %s across a restart: %v", name, err)
		}
		return
	}
	boatZoneMemory.loggedWrite = ""
}

// boatTimeLastFix is the last-fix rule: the remembered zone, or UTC when no
// fix has ever been seen. indexLoading says the position is good and the
// remembered zone is used only while the zone index builds, so the log does
// not blame the fix.
func boatTimeLastFix(indexLoading bool) boatTime {
	boatZoneMemory.mu.Lock()
	defer boatZoneMemory.mu.Unlock()
	loadBoatZoneLocked()

	if boatZoneMemory.loadErr != nil {
		logBoatZoneErrorLocked(boatZoneMemory.loadErr)
		return boatTime{placeZone: utcZone, NoFix: true, Err: boatZoneMemory.loadErr}
	}
	if boatZoneMemory.zone == "" {
		if !boatZoneMemory.loggedFall {
			boatZoneMemory.loggedFall = true
			log.Printf("boat time: no position fix has been seen yet, so times are in UTC")
		}
		return boatTime{placeZone: utcZone, NoFix: true}
	}
	z, err := zoneAtName(boatZoneMemory.zone)
	if err != nil {
		logBoatZoneErrorLocked(err)
		return boatTime{placeZone: utcZone, NoFix: true, Err: err}
	}
	switch {
	case indexLoading:
		if !boatZoneMemory.loggedWait {
			boatZoneMemory.loggedWait = true
			log.Printf("boat time: the time zone index is still loading, so times stay in %s, the zone of the last fix", z.Name)
		}
	case !boatZoneMemory.loggedFall:
		boatZoneMemory.loggedFall = true
		log.Printf("boat time: no usable position fix, so times stay in %s, the zone of the last fix", z.Name)
	}
	return boatTime{placeZone: z, LastFix: true}
}

func logBoatZoneErrorLocked(err error) {
	if msg := err.Error(); msg != boatZoneMemory.loggedError {
		boatZoneMemory.loggedError = msg
		log.Printf("boat time: %v", err)
	}
}

// boatTimeForState applies the checks every other background worker applies
// to an own-ship fix (usableOwnShipFix): the -1,-1 sentinel and a critical
// GNSS alert are not a fix.
func boatTimeForState(state vesselStateData, err error) boatTime {
	lat, lon, ok := usableOwnShipFix(state, err)
	return boatTimeAt(lat, lon, ok)
}

// LabelAt is the zone as an operator says it at the instant, with the words
// "last fix" when it is not a live one: "AEDT (UTC+11)", "AEST (UTC+10, last
// fix)", "UTC (no position yet)".
func (b boatTime) LabelAt(at time.Time) string {
	return b.qualify(b.Label(at))
}

// LabelSpan labels the zone for a set of instants shown together. Daylight
// saving can change the label between them, so each distinct label is named in
// time order: "AEST (UTC+10) then AEDT (UTC+11)".
func (b boatTime) LabelSpan(ats ...time.Time) string {
	sorted := append([]time.Time(nil), ats...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Before(sorted[j]) })
	var labels []string
	for _, at := range sorted {
		if at.IsZero() {
			continue
		}
		l := b.Label(at)
		if len(labels) == 0 || labels[len(labels)-1] != l {
			labels = append(labels, l)
		}
	}
	if len(labels) == 0 {
		return b.qualify(b.Label(time.Now()))
	}
	if len(labels) == 1 {
		return b.qualify(labels[0])
	}
	return b.qualify(strings.Join(labels, " then "))
}

func (b boatTime) qualify(label string) string {
	q := b.Qualifier()
	switch {
	case q == "":
		return label
	case strings.HasSuffix(label, ")"):
		return strings.TrimSuffix(label, ")") + ", " + q + ")"
	default:
		return label + " (" + q + ")"
	}
}

// PromptClock is the zone as Mate's clock line words it:
// "Australia/Brisbane, UTC+10", "Australia/Brisbane, UTC+10, last fix",
// "UTC, no position yet".
func (b boatTime) PromptClock(at time.Time) string {
	desc := b.Describe(at)
	if q := b.Qualifier(); q != "" {
		return desc + ", " + q
	}
	return desc
}

// boatTime is the boat's clock for a Mate tool. A tool with no vessel-state
// reader still gets the last-fix rule, never a silent UTC.
func (d assistantToolDeps) boatTime() boatTime {
	if d.vesselState == nil {
		return boatTimeAt(0, 0, false)
	}
	state, err := d.vesselState()
	return boatTimeForState(state, err)
}

// zoneAbbreviation is the short name of loc at t for a clock: "AEDT" where the
// zone has one, "UTC+12" where tzdata only has an offset ("+12" offshore).
func zoneAbbreviation(t time.Time, loc *time.Location) string {
	name, off := t.In(loc).Zone()
	if name != "" && !strings.ContainsAny(name[:1], "+-0123456789") {
		return name
	}
	return offsetText(off)
}

// clockWithZone is t on loc's wall clock with the zone named: layout is a Go
// layout without a zone element.
func clockWithZone(t time.Time, loc *time.Location, layout string) string {
	return t.In(loc).Format(layout) + " " + zoneAbbreviation(t, loc)
}
