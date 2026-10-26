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

func saveTankAlarmDefaults(next tankAlarmDefaults) error {
	if err := next.validate(); err != nil {
		return err
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

// tankDerivedAlarmRules is one warn rule and one alarm rule per tank that has
// an equipment item and has not had its alarms switched off. A tank whose
// resolved pair is backwards (a default changed under an override) raises
// nothing and says so in the log.
func tankDerivedAlarmRules() []alarmRule {
	items, err := listSourceItems()
	noteVesselRead("tank alarms", err, "tank alarms: could not read the equipment items, none are raised: %v")
	defaults := currentTankAlarmDefaults()

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
		for _, level := range []struct {
			name    string
			percent float64
			state   string
		}{{"warn", levels.Warn, alarmStateWarn}, {"alarm", levels.Alarm, alarmStateAlarm}} {
			rules = append(rules, alarmRule{
				ID:                tankAlarmRuleIDPrefix + item.ID + ":" + level.name,
				Enabled:           true,
				Path:              item.Source + ".currentLevel",
				CapacityPath:      item.Source + ".capacity",
				Label:             item.Name + " " + word,
				Op:                op,
				Value:             level.percent / 100,
				Hysteresis:        tankAlarmClearPoints / 100,
				DwellSeconds:      tankAlarmDwellSeconds,
				StaleAfterSeconds: tankAlarmStaleAfterSeconds,
				State:             level.state,
				Methods:           []string{"visual", "sound"},
			})
		}
	}
	return rules
}

// derivedAlarmRules is every rule Helmcentral derives rather than stores: gauge
// zones (ADR 0050) and tank levels (ADR 0214).
func derivedAlarmRules() []alarmRule {
	return append(zoneDerivedAlarmRules(), tankDerivedAlarmRules()...)
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

// collapseTankAlarms leaves one card per tank. A tank holds a warn rule and an
// alarm rule on one path, both live below the alarm level; the card is the
// worse of the two, and an acknowledged warn gives way to the alarm the moment
// it raises, whatever the operator did to the warn.
func collapseTankAlarms(statuses []alarmStatus) []alarmStatus {
	best := map[string]int{}
	for i, s := range statuses {
		if !isTankDerivedAlarmRuleID(s.RuleID) {
			continue
		}
		if j, seen := best[s.Path]; !seen || alarmStateRank[s.State] > alarmStateRank[statuses[j].State] {
			best[s.Path] = i
		}
	}
	out := make([]alarmStatus, 0, len(statuses))
	for i, s := range statuses {
		if isTankDerivedAlarmRuleID(s.RuleID) && best[s.Path] != i {
			continue
		}
		out = append(out, s)
	}
	return out
}

// ── tank levels on the Tanks payload ─────────────────────────────────────────

// annotateTankLevels puts each listed tank's resolved levels beside its
// reading, so the Tanks tile and the fuel bars colour from the same numbers the
// alarms use. A listed tank always has an item (the tile lists no other), so a
// tank that has none here lost it between the two reads and gets the kind's
// defaults.
func annotateTankLevels(tanks []tankLevelData) {
	items, err := listSourceItems()
	noteVesselRead("tank levels", err, "tanks: could not read the equipment items, tank levels use the defaults: %v")
	bySource := make(map[string]sourceItem, len(items))
	for _, item := range items {
		bySource[item.Source] = item
	}
	defaults := currentTankAlarmDefaults()
	for i := range tanks {
		levels, off := resolveTankLevels(tanks[i].Kind, defaults, bySource["tanks."+tanks[i].ID])
		tanks[i].WarnPercent, tanks[i].AlarmPercent, tanks[i].AlarmsOff = levels.Warn, levels.Alarm, off
	}
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
