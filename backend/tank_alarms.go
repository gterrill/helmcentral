package main

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"os"
	"strings"
	"sync"

	"github.com/labstack/echo/v4"
)

// Tank level alarms (ADR 0214).
//
// One set of levels per tank drives three things: the colours on the Tanks
// tile, the colours on the Engine Cluster's fuel bars, and the alarms. The
// levels are a percentage of full. A kind has defaults, set in Settings, and
// a tank can override either level or switch its alarms off on its equipment
// page. Only a tank that has an equipment item gets alarms.
//
// Like the gauge-zone alarms (ADR 0050), the rules are derived at evaluation
// time rather than written into alarm-rules.json, so editing a level changes
// the alarm and the tile together and there is nothing to keep in sync.

const (
	tankAlarmRuleIDPrefix = "tank:"

	// A level hovering at a threshold is the reason people switch alarms off,
	// and a tank sloshes. The level must stay past the threshold this long to
	// raise, and travel this far back to clear. Fixed, not settings.
	tankAlarmDwellSeconds = 60
	tankAlarmClearPoints  = 5.0

	// The Tanks tile calls a tank stale after this long without an update
	// (frontend STALE_AFTER_SECONDS); an alarm must not be raised from a
	// reading the tile itself will not show as current.
	tankAlarmStaleAfterSeconds = 120

	tankKindFuel  = "fuel"
	tankKindWater = "water"
	tankKindWaste = "waste"
)

// tankLevelThresholds are one tank's warn and alarm levels, in percent of full.
type tankLevelThresholds struct {
	Warn  float64 `json:"warn"`
	Alarm float64 `json:"alarm"`
}

// tankAlarmDefaults are the levels each kind of tank uses unless its item says
// otherwise.
type tankAlarmDefaults struct {
	Fuel  tankLevelThresholds `json:"fuel"`
	Water tankLevelThresholds `json:"water"`
	Waste tankLevelThresholds `json:"waste"`
}

// builtinTankAlarmDefaults are generic: nothing here is a particular boat's.
func builtinTankAlarmDefaults() tankAlarmDefaults {
	return tankAlarmDefaults{
		Fuel:  tankLevelThresholds{Warn: 20, Alarm: 10},
		Water: tankLevelThresholds{Warn: 20, Alarm: 10},
		Waste: tankLevelThresholds{Warn: 80, Alarm: 90},
	}
}

func (d tankAlarmDefaults) forKind(kind string) tankLevelThresholds {
	switch kind {
	case tankKindFuel:
		return d.Fuel
	case tankKindWaste:
		return d.Waste
	default:
		return d.Water
	}
}

// tankLowIsBad is false for waste: a full holding tank is the bad one.
func tankLowIsBad(kind string) bool { return kind != tankKindWaste }

// validateTankLevels checks one pair: both inside 0-100, and the alarm level
// further into the bad side than the warn level.
func validateTankLevels(kind string, t tankLevelThresholds) error {
	for _, v := range []float64{t.Warn, t.Alarm} {
		if math.IsNaN(v) || v < 0 || v > 100 {
			return fmt.Errorf("tank levels are percentages between 0 and 100")
		}
	}
	// Both levels clear 5 points back on the good side, so the clear point has
	// to stay inside the scale.
	for _, v := range []float64{t.Warn, t.Alarm} {
		if tankLowIsBad(kind) && v > 100-tankAlarmClearPoints {
			return fmt.Errorf("levels for a low-bad tank can be at most %g percent, so the alarm can clear %g points above them", 100-tankAlarmClearPoints, tankAlarmClearPoints)
		}
		if !tankLowIsBad(kind) && v < tankAlarmClearPoints {
			return fmt.Errorf("levels for a waste tank must be at least %g percent, so the alarm can clear %g points below them", tankAlarmClearPoints, tankAlarmClearPoints)
		}
	}
	if tankLowIsBad(kind) && t.Alarm >= t.Warn {
		return fmt.Errorf("the alarm level must be lower than the warn level, because a low tank is the bad one")
	}
	if !tankLowIsBad(kind) && t.Alarm <= t.Warn {
		return fmt.Errorf("the alarm level must be higher than the warn level, because a full tank is the bad one")
	}
	return nil
}

func (d tankAlarmDefaults) validate() error {
	for _, k := range []struct {
		kind, name string
		t          tankLevelThresholds
	}{{tankKindFuel, "fuel", d.Fuel}, {tankKindWater, "fresh water", d.Water}, {tankKindWaste, "waste", d.Waste}} {
		if err := validateTankLevels(k.kind, k.t); err != nil {
			return fmt.Errorf("%s tanks: %w", k.name, err)
		}
	}
	return nil
}

// resolveTankLevels is the pair a tank uses: its own figure where set, else its
// kind's default. alarmsOff is returned beside it because a tank with alarms
// off still has levels to colour by.
func resolveTankLevels(kind string, defaults tankAlarmDefaults, item sourceItem) (levels tankLevelThresholds, alarmsOff bool) {
	levels = defaults.forKind(kind)
	if item.TankWarnPercent > 0 {
		levels.Warn = item.TankWarnPercent
	}
	if item.TankAlarmPercent > 0 {
		levels.Alarm = item.TankAlarmPercent
	}
	return levels, item.TankAlarmsOff
}

// validateTankOverrides checks the figures on an item about to be saved. Only a
// tank may carry them, and the pair that results once blanks take the current
// defaults has to be the right way round.
func validateTankOverrides(item *equipmentItem, group sourceGroup) error {
	has := item.TankWarnPercent != 0 || item.TankAlarmPercent != 0 || item.TankAlarmsOff
	if group != sourceTank {
		if has {
			return fmt.Errorf("tank alarm levels belong to a tank item, and this one is not mapped to a tank")
		}
		return nil
	}
	if item.TankWarnPercent < 0 || item.TankAlarmPercent < 0 {
		return fmt.Errorf("tank levels cannot be negative")
	}
	if item.TankWarnPercent == 0 && item.TankAlarmPercent == 0 {
		return nil
	}
	_, instance, _ := parseSource(item.SignalKSource)
	kind := tankKindOfInstance(instance)
	levels, _ := resolveTankLevels(kind, currentTankAlarmDefaults(), sourceItem{TankWarnPercent: item.TankWarnPercent, TankAlarmPercent: item.TankAlarmPercent})
	return validateTankLevels(kind, levels)
}

// tankKindOfInstance maps a tank source instance ("fuel.0") to its kind, the
// same mapping the Tanks tile uses.
func tankKindOfInstance(instance string) string {
	category, _, _ := strings.Cut(instance, ".")
	return tankKindFromCategory(category)
}

// ── defaults store ───────────────────────────────────────────────────────────

// The defaults live in their own file for the same reason alarm rules do:
// POST /api/settings rebuilds its sections wholesale and would drop a new key.
func tankAlarmDefaultsFilePath() string {
	return cacheFilePath("TANK_ALARM_DEFAULTS_FILE", "data/tank-alarm-defaults.json")
}

var (
	tankAlarmDefaultsMu    sync.RWMutex
	tankAlarmDefaultsState = builtinTankAlarmDefaults()
)

func currentTankAlarmDefaults() tankAlarmDefaults {
	tankAlarmDefaultsMu.RLock()
	defer tankAlarmDefaultsMu.RUnlock()
	return tankAlarmDefaultsState
}

// loadTankAlarmDefaults reads the file. A missing file is a boat that has
// never changed them and reads as the built-in levels; a file that exists but
// cannot be read or fails validation is an error, never the built-in levels.
func loadTankAlarmDefaults() error {
	data, err := os.ReadFile(tankAlarmDefaultsFilePath())
	if os.IsNotExist(err) {
		tankAlarmDefaultsMu.Lock()
		tankAlarmDefaultsState = builtinTankAlarmDefaults()
		tankAlarmDefaultsMu.Unlock()
		return nil
	}
	if err != nil {
		return fmt.Errorf("reading tank alarm defaults: %w", err)
	}
	var loaded tankAlarmDefaults
	if err := json.Unmarshal(data, &loaded); err != nil {
		return fmt.Errorf("parsing tank alarm defaults: %w", err)
	}
	if err := loaded.validate(); err != nil {
		return fmt.Errorf("tank alarm defaults: %w", err)
	}
	tankAlarmDefaultsMu.Lock()
	tankAlarmDefaultsState = loaded
	tankAlarmDefaultsMu.Unlock()
	return nil
}

// tanksLeftBackwards names the tanks whose resolved pair would be out of range
// or the wrong way round under defaults. A default is only saved if this is
// empty, so no tank silently loses its alarms and colours to a pair nobody chose.
func tanksLeftBackwards(defaults tankAlarmDefaults) ([]string, error) {
	items, err := listSourceItems()
	if err != nil {
		return nil, fmt.Errorf("cannot check the tanks against these levels, the equipment items are unreadable: %w", err)
	}
	var names []string
	for _, item := range items {
		if item.Group != sourceTank {
			continue
		}
		kind := tankKindOfInstance(item.Instance)
		levels, _ := resolveTankLevels(kind, defaults, item)
		if validateTankLevels(kind, levels) != nil {
			names = append(names, item.Name)
		}
	}
	return names, nil
}

func saveTankAlarmDefaults(next tankAlarmDefaults) error {
	if err := next.validate(); err != nil {
		return err
	}
	names, err := tanksLeftBackwards(next)
	if err != nil {
		return err
	}
	if len(names) > 0 {
		return fmt.Errorf("these levels would leave %s with an alarm level on the wrong side of its warn level, or out of range. Change that tank's own levels first", strings.Join(names, ", "))
	}
	tankAlarmDefaultsMu.Lock()
	defer tankAlarmDefaultsMu.Unlock()
	if err := writeJSONFileAtomic(tankAlarmDefaultsFilePath(), next); err != nil {
		return err
	}
	tankAlarmDefaultsState = next
	return nil
}

// ── derived rules ────────────────────────────────────────────────────────────

func isTankDerivedAlarmRuleID(id string) bool {
	return strings.HasPrefix(id, tankAlarmRuleIDPrefix)
}

// tankDerivedAlarmRules is one rule per tank that has an equipment item and
// has not had its alarms switched off. The rule is a single alarm with two
// levels (Value is the warn level, AlarmLevelValue the alarm level), so a tank
// escalates and de-escalates as one alarm: it has one card, one log entry and
// one notification path, and acknowledging it acknowledges the tank. A tank
// whose resolved pair is backwards (a hand-edited defaults file) raises nothing
// and says so in the log.
//
// The result is reused while the items and defaults it was built from are the
// same, since every card refresh and every ownership check asks for it.
func tankDerivedAlarmRules() []alarmRule {
	items, err := listSourceItems()
	noteVesselRead("tank alarms", err, "tank alarms: could not read the equipment items, none are raised: %v")
	defaults := currentTankAlarmDefaults()

	tankRuleMemo.mu.Lock()
	defer tankRuleMemo.mu.Unlock()
	if err == nil && tankRuleMemo.valid && sameSourceItems(tankRuleMemo.items, items) && tankRuleMemo.defaults == defaults {
		return tankRuleMemo.rules
	}

	var rules []alarmRule
	for _, item := range items {
		if item.Group != sourceTank {
			continue
		}
		kind := tankKindOfInstance(item.Instance)
		levels, off := resolveTankLevels(kind, defaults, item)
		if off {
			continue
		}
		pairErr := validateTankLevels(kind, levels)
		noteVesselRead("tank alarms:"+item.ID, pairErr, "tank alarms: "+item.Name+" raises none: %v")
		if pairErr != nil {
			continue
		}
		op, word := alarmOpBelow, "Low"
		if !tankLowIsBad(kind) {
			op, word = alarmOpAbove, "High"
		}
		alarmLevel := levels.Alarm / 100
		rules = append(rules, alarmRule{
			ID:                tankAlarmRuleIDPrefix + item.ID,
			Enabled:           true,
			Path:              item.Source + ".currentLevel",
			CapacityPath:      item.Source + ".capacity",
			Label:             item.Name + " " + word,
			Op:                op,
			Value:             levels.Warn / 100,
			AlarmLevelValue:   &alarmLevel,
			Hysteresis:        tankAlarmClearPoints / 100,
			DwellSeconds:      tankAlarmDwellSeconds,
			StaleAfterSeconds: tankAlarmStaleAfterSeconds,
			State:             alarmStateWarn,
			Methods:           []string{"visual", "sound"},
		})
	}
	if err == nil {
		tankRuleMemo.valid, tankRuleMemo.items, tankRuleMemo.defaults, tankRuleMemo.rules = true, items, defaults, rules
	}
	return rules
}

var tankRuleMemo struct {
	mu       sync.Mutex
	valid    bool
	items    []sourceItem
	defaults tankAlarmDefaults
	rules    []alarmRule
}

// sameSourceItems compares by value, so a cache refresh that reads the same
// rows does not rebuild the rules and an edit that changes one does.
func sameSourceItems(a, b []sourceItem) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// derivedAlarmRules is every rule Helmcentral derives rather than stores: gauge
// zones (ADR 0050) and tank levels (ADR 0214).
func derivedAlarmRules() []alarmRule {
	zones := zoneDerivedAlarmRules()
	return append(zones[:len(zones):len(zones)], tankDerivedAlarmRules()...)
}

// tankAlarmBody is the card body and the notification text: the level, then
// the volume when the tank's capacity is known. level and capacity are SI (a
// ratio and cubic metres).
func tankAlarmBody(level float64, capacity *float64) string {
	body := fmt.Sprintf("%d%%", int(math.Round(level*100)))
	if capacity != nil && *capacity > 0 {
		body += fmt.Sprintf(" · %d L", int(math.Round(level**capacity*1000)))
	}
	return body
}

// withTankAlarmBodies sets every tank card's body from the live level, on the
// server, so the card, the notification text and Mate read the same words
// (ADR 0157). The volume is in litres, the one volume unit the app uses
// (operatorUnit in alarm_units.go).
func withTankAlarmBodies(statuses []alarmStatus) []alarmStatus {
	for i := range statuses {
		if isTankDerivedAlarmRuleID(statuses[i].RuleID) {
			statuses[i].Message = tankAlarmBody(statuses[i].Value, statuses[i].Capacity)
		}
	}
	return statuses
}

// tankLevelPercent is the percentage a reading of currentLevel means. SignalK
// publishes a ratio, but some senders publish 0-100, so a value over 1 is
// already a percentage. The Tanks tile and the alarms both read it through
// here, so they cannot disagree. A negative reading is not a level.
func tankLevelPercent(raw float64) (percent float64, ok bool) {
	if raw < 0 || math.IsNaN(raw) {
		return 0, false
	}
	if raw <= 1 {
		raw *= 100
	}
	return math.Max(0, math.Min(100, roundTo1(raw))), true
}

// tankLevelAlarmReader makes the alarm engine read a tank's currentLevel as the
// tile does: a ratio of full, whichever scale the sender used.
func tankLevelAlarmReader(base alarmReader) alarmReader {
	return func(path string) alarmSample {
		sample := base(path)
		if !sample.Present || !strings.HasPrefix(path, "tanks.") || !strings.HasSuffix(path, ".currentLevel") {
			return sample
		}
		percent, ok := tankLevelPercent(sample.Value)
		if !ok {
			return alarmSample{}
		}
		sample.Value = percent / 100
		return sample
	}
}

// ── tank levels on the Tanks payload ─────────────────────────────────────────

// annotateTankLevels puts each listed tank's resolved levels beside its
// reading, so the Tanks tile and the fuel bars colour from the same numbers the
// alarms use. A listed tank always has an item (the tile lists no other), so a
// tank that has none here lost it between the two reads and gets the kind's
// defaults. Unreadable items are an error: without them the alarms are not
// running either, so the tile must not colour by defaults as if they were.
func annotateTankLevels(tanks []tankLevelData) error {
	items, err := listSourceItems()
	if err != nil {
		return fmt.Errorf("tank levels unavailable, the equipment items could not be read: %w", err)
	}
	bySource := make(map[string]sourceItem, len(items))
	for _, item := range items {
		bySource[item.Source] = item
	}
	defaults := currentTankAlarmDefaults()
	for i := range tanks {
		levels, off := resolveTankLevels(tanks[i].Kind, defaults, bySource["tanks."+tanks[i].ID])
		tanks[i].WarnPercent, tanks[i].AlarmPercent, tanks[i].AlarmsOff = levels.Warn, levels.Alarm, off
	}
	return nil
}

// ── HTTP ─────────────────────────────────────────────────────────────────────

func getTankAlarmDefaultsHandler(c echo.Context) error {
	return c.JSON(http.StatusOK, currentTankAlarmDefaults())
}

func setTankAlarmDefaultsHandler(c echo.Context) error {
	var next tankAlarmDefaults
	if err := c.Bind(&next); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request payload"})
	}
	if err := saveTankAlarmDefaults(next); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, currentTankAlarmDefaults())
}
