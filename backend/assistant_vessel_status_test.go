package main

import (
	"context"
	"encoding/json"
	"math"
	"strings"
	"testing"
	"time"
)

// The fixtures below are shaped like the live capture in
// testdata/anomaly/signalk-self-snapshot-2026-09-25.json: house bank on
// electrical.batteries.512 (SoC as a 0-1 ratio, volts, signed amps), the Venus
// totals for solar and DC load, environment.outside.pressure in pascals and
// true wind in metres per second.

var vesselStatusTestNow = time.Date(2026, 10, 28, 2, 0, 0, 0, time.UTC)

func fresh(v any) telemetrySample {
	return telemetrySample{Value: v, Present: true, Age: 3 * time.Second, AgeKnown: true}
}

func aged(v any, age time.Duration) telemetrySample {
	return telemetrySample{Value: v, Present: true, Age: age, AgeKnown: true}
}

func statusTestTelemetry() fakeTelemetry {
	return fakeTelemetry{Samples: map[string]telemetrySample{
		"electrical.batteries.512.capacity.stateOfCharge": fresh(0.73),
		"electrical.batteries.512.voltage":                fresh(26.47),
		"electrical.batteries.512.current":                fresh(-22.5),
		"electrical.venus.totalPanelPower":                fresh(338.79),
		"electrical.venus.dcPower":                        fresh(452.22),
		"environment.outside.pressure":                    fresh(102262.06),
		"tanks.fuel.2.currentLevel":                       fresh(0.18),
		"tanks.freshWater.0.currentLevel":                 fresh(0.64),
		"tanks.blackWater.0.currentLevel":                 fresh(0.55),
		"propulsion.0.revolutions":                        fresh(0.0),
		"propulsion.1.revolutions":                        fresh(13.3),
	}}
}

func statusTestDeps(t *testing.T) assistantToolDeps {
	t.Helper()
	tel := statusTestTelemetry()
	minus150 := -150.0
	return assistantToolDeps{
		now:       func() time.Time { return vesselStatusTestNow },
		telemetry: func(time.Time) (telemetryReader, error) { return tel, nil },
		vesselView: func() (vesselView, error) {
			return vesselView{
				HouseBank: &vesselHouseBankSetting{Path: "electrical.batteries.512"},
				Engines: []vesselEngineSetting{
					{Instance: "0", Name: "Port Engine"},
					{Instance: "1", Name: "Starboard Engine"},
				},
			}, nil
		},
		displayUnits: func() (string, error) { return "metric", nil },
		vesselState: func() (vesselStateData, error) {
			return vesselStateData{
				Status: "anchored", Depth: 3.3, DepthLastUpdateAge: 2,
				WindSpeedTrueKts: 13.8, WindLastUpdateAge: 2,
				Latitude: -18.2231125, Longitude: 146.21240833333334, PositionLastUpdateAge: 1,
			}, nil
		},
		trueWindGustKts: func() (float64, bool) { return 19.4, true },
		sourceItems: func() ([]sourceItem, error) {
			return []sourceItem{
				{ID: "t-fuel", Name: "Port Fuel", Source: "tanks.fuel.2", Group: sourceTank, Instance: "fuel.2"},
				{ID: "t-water", Name: "Fresh Water", Source: "tanks.freshWater.0", Group: sourceTank, Instance: "freshWater.0"},
				{ID: "t-waste", Name: "Black Water", Source: "tanks.blackWater.0", Group: sourceTank, Instance: "blackWater.0"},
			}, nil
		},
		tankDefaults: builtinTankAlarmDefaults,
		derivedPaths: func(time.Time) (map[string]*float64, map[string]float64) {
			return map[string]*float64{pressureChange3hPath: &minus150}, map[string]float64{pressureChange3hPath: 4}
		},
		engineStates:  func(time.Time) map[string]engineStateInfo { return nil },
		activeAlarms:  func() []alarmStatus { return nil },
		ignoredAlarms: func() []string { return nil },
		anomalyReading: func() (anomalyReading, bool) {
			return anomalyReading{Values: map[string]float64{}, ComputedAt: vesselStatusTestNow.Add(-time.Second)}, true
		},
		anchorWatch: func() (anchorWatchData, bool, string) { return anchorWatchData{}, false, "" },
		overnight:   func(time.Time) (overnightResult, string) { return overnightResult{}, "influxdb not configured" },
		documents:   func() *documentStore { return nil },
	}
}

func runStatus(t *testing.T, d assistantToolDeps) vesselStatusResult {
	t.Helper()
	out, err := d.execute(context.Background(), "get_vessel_status", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("get_vessel_status: %v", err)
	}
	var res vesselStatusResult
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("decode: %v\n%s", err, out)
	}
	return res
}

func near(got *float64, want, tol float64) bool {
	return got != nil && math.Abs(*got-want) <= tol
}

func TestVesselStatus_PowerFromTheHouseBankRole(t *testing.T) {
	res := runStatus(t, statusTestDeps(t))
	p := res.Power
	if !near(p.HouseBank.SocPercent.Value, 73, 0.01) || p.HouseBank.SocPercent.Unit != "%" {
		t.Errorf("soc: %+v", p.HouseBank.SocPercent)
	}
	if !near(p.HouseBank.Voltage.Value, 26.47, 0.001) || p.HouseBank.Voltage.DisplayUnit != "V" {
		t.Errorf("voltage: %+v", p.HouseBank.Voltage)
	}
	if !near(p.HouseBank.Current.Value, -22.5, 0.001) {
		t.Errorf("current: %+v", p.HouseBank.Current)
	}
	if !near(p.SolarW.Value, 338.79, 0.01) || !near(p.DCLoadW.Value, 452.22, 0.01) {
		t.Errorf("solar/dc: %+v %+v", p.SolarW, p.DCLoadW)
	}
}

func TestVesselStatus_NoHouseBankRoleIsAbsentWithAReason(t *testing.T) {
	d := statusTestDeps(t)
	d.vesselView = func() (vesselView, error) { return vesselView{}, nil }
	res := runStatus(t, d)
	for name, v := range map[string]statusValue{"soc": res.Power.HouseBank.SocPercent, "voltage": res.Power.HouseBank.Voltage, "current": res.Power.HouseBank.Current} {
		if v.Value != nil || v.Missing != "no house bank role set" {
			t.Errorf("%s: %+v", name, v)
		}
	}
	if res.Power.SolarW.Value == nil {
		t.Errorf("solar should not depend on the house bank role: %+v", res.Power.SolarW)
	}
}

func TestVesselStatus_UnresolvedHouseBankSaysSo(t *testing.T) {
	d := statusTestDeps(t)
	d.vesselView = func() (vesselView, error) { return vesselView{HouseBankUnresolved: true}, nil }
	res := runStatus(t, d)
	if !strings.Contains(res.Power.HouseBank.SocPercent.Missing, "no SignalK source") {
		t.Errorf("missing: %q", res.Power.HouseBank.SocPercent.Missing)
	}
}

func TestVesselStatus_StaleReadingIsNeverFilledFromTheOldValue(t *testing.T) {
	d := statusTestDeps(t)
	tel := statusTestTelemetry()
	tel.Samples["electrical.batteries.512.voltage"] = aged(26.47, 20*time.Minute)
	delete(tel.Samples, "electrical.venus.dcPower")
	d.telemetry = func(time.Time) (telemetryReader, error) { return tel, nil }
	res := runStatus(t, d)
	v := res.Power.HouseBank.Voltage
	if v.Value != nil || v.Display != nil || !strings.HasPrefix(v.Missing, "no fresh reading") {
		t.Errorf("voltage should be absent as stale: %+v", v)
	}
	if res.Power.DCLoadW.Value != nil || res.Power.DCLoadW.Missing == "" {
		t.Errorf("dc load: %+v", res.Power.DCLoadW)
	}
}

func TestVesselStatus_SolarSumsControllersWhenThereIsNoVenusTotal(t *testing.T) {
	d := statusTestDeps(t)
	tel := statusTestTelemetry()
	delete(tel.Samples, "electrical.venus.totalPanelPower")
	for id, w := range map[string]float64{"288": 66.36, "289": 95.36, "290": 98.52, "291": 94.09} {
		tel.Samples["electrical.solar."+id+".panelPower"] = fresh(w)
	}
	d.telemetry = func(time.Time) (telemetryReader, error) { return tel, nil }
	res := runStatus(t, d)
	if !near(res.Power.SolarW.Value, 354.33, 0.01) {
		t.Errorf("solar: %+v", res.Power.SolarW)
	}
	// One silent controller makes the sum a guess, so the total is withheld.
	tel.Samples["electrical.solar.291.panelPower"] = aged(94.09, time.Hour)
	res = runStatus(t, d)
	if res.Power.SolarW.Value != nil || !strings.Contains(res.Power.SolarW.Missing, "no fresh reading") {
		t.Errorf("solar with a silent controller: %+v", res.Power.SolarW)
	}
}

func TestVesselStatus_TanksCarryLevelAndAlarmLevels(t *testing.T) {
	d := statusTestDeps(t)
	d.activeAlarms = func() []alarmStatus {
		return []alarmStatus{{RuleID: "tank:t-fuel", Label: "Port Fuel Low", State: alarmStateWarn, Phase: alarmPhaseActive}}
	}
	res := runStatus(t, d)
	if len(res.Tanks.Tanks) != 3 {
		t.Fatalf("tanks: %+v", res.Tanks)
	}
	byName := map[string]vesselStatusTank{}
	for _, tk := range res.Tanks.Tanks {
		byName[tk.Name] = tk
	}
	fuel := byName["Port Fuel"]
	if fuel.Kind != "fuel" || !near(fuel.Level.Value, 18, 0.01) || fuel.WarnPercent != 20 || fuel.AlarmPercent != 10 || fuel.AlarmState != alarmStateWarn {
		t.Errorf("fuel: %+v", fuel)
	}
	water := byName["Fresh Water"]
	if water.Kind != "water" || !near(water.Level.Value, 64, 0.01) || water.AlarmState != "" {
		t.Errorf("water: %+v", water)
	}
	waste := byName["Black Water"]
	if waste.Kind != "waste" || waste.WarnPercent != 80 || waste.AlarmPercent != 90 {
		t.Errorf("waste: %+v", waste)
	}
}

func TestVesselStatus_TankOverridesAndAlarmsOffAreReported(t *testing.T) {
	d := statusTestDeps(t)
	d.sourceItems = func() ([]sourceItem, error) {
		return []sourceItem{{ID: "t-fuel", Name: "Port Fuel", Source: "tanks.fuel.2", Group: sourceTank, Instance: "fuel.2", TankWarnPercent: 30, TankAlarmPercent: 15, TankAlarmsOff: true}}, nil
	}
	res := runStatus(t, d)
	tk := res.Tanks.Tanks[0]
	if tk.WarnPercent != 30 || tk.AlarmPercent != 15 || !tk.AlarmsOff {
		t.Errorf("tank: %+v", tk)
	}
}

func TestVesselStatus_SilentTankAndNoTanks(t *testing.T) {
	d := statusTestDeps(t)
	tel := statusTestTelemetry()
	tel.Samples["tanks.fuel.2.currentLevel"] = aged(0.18, 10*time.Minute)
	d.telemetry = func(time.Time) (telemetryReader, error) { return tel, nil }
	res := runStatus(t, d)
	for _, tk := range res.Tanks.Tanks {
		if tk.Name == "Port Fuel" && (tk.Level.Value != nil || !strings.HasPrefix(tk.Level.Missing, "no fresh reading")) {
			t.Errorf("silent tank: %+v", tk)
		}
	}

	d.sourceItems = func() ([]sourceItem, error) { return nil, nil }
	res = runStatus(t, d)
	if len(res.Tanks.Tanks) != 0 || res.Tanks.Missing != "no tanks are set up on the Equipment page" {
		t.Errorf("no tanks: %+v", res.Tanks)
	}
}

func TestVesselStatus_SurroundingsDepthWindPressure(t *testing.T) {
	res := runStatus(t, statusTestDeps(t))
	s := res.Surroundings
	if !near(s.Depth.Value, 3.3, 0.001) || s.Depth.Unit != "m" || !near(s.Depth.Display, 3.3, 0.001) {
		t.Errorf("depth: %+v", s.Depth)
	}
	if !near(s.TrueWindSpeed.Value, 13.8/metersPerSecondToKnots, 0.001) || !near(s.TrueWindSpeed.Display, 13.8, 0.001) || s.TrueWindSpeed.DisplayUnit != "kn" {
		t.Errorf("wind: %+v", s.TrueWindSpeed)
	}
	if !near(s.TrueWindGust.Display, 19.4, 0.001) || !strings.Contains(s.TrueWindGustNote, "10 minutes") {
		t.Errorf("gust: %+v %q", s.TrueWindGust, s.TrueWindGustNote)
	}
	if !near(s.Pressure.Value, 102262.06, 0.01) || !near(s.Pressure.Display, 1022.6, 0.05) || s.Pressure.DisplayUnit != "hPa" {
		t.Errorf("pressure: %+v", s.Pressure)
	}
	if !near(s.PressureChange3h.Value, -150, 0.001) || !near(s.PressureChange3h.Display, -1.5, 0.001) || s.PressureChange3h.DisplayUnit != "hPa" {
		t.Errorf("3h change: %+v", s.PressureChange3h)
	}
}

func TestVesselStatus_ImperialDepthInFeet(t *testing.T) {
	d := statusTestDeps(t)
	d.displayUnits = func() (string, error) { return "imperial", nil }
	res := runStatus(t, d)
	if !near(res.Surroundings.Depth.Value, 3.3, 0.001) || !near(res.Surroundings.Depth.Display, 10.83, 0.01) || res.Surroundings.Depth.DisplayUnit != "ft" {
		t.Errorf("depth: %+v", res.Surroundings.Depth)
	}
}

func TestVesselStatus_PressureChangeStillFillingIsAbsentWithAReason(t *testing.T) {
	d := statusTestDeps(t)
	d.derivedPaths = func(time.Time) (map[string]*float64, map[string]float64) {
		return map[string]*float64{pressureChange3hPath: nil}, map[string]float64{pressureChange3hPath: -1}
	}
	res := runStatus(t, d)
	c := res.Surroundings.PressureChange3h
	if c.Value != nil || !strings.Contains(c.Missing, "barometer history") {
		t.Errorf("3h change: %+v", c)
	}
	if res.Surroundings.Pressure.Value == nil {
		t.Errorf("the pressure itself is still a fresh reading: %+v", res.Surroundings.Pressure)
	}
}

func TestVesselStatus_NoTrueWindIsAbsent(t *testing.T) {
	d := statusTestDeps(t)
	d.vesselState = func() (vesselStateData, error) {
		return vesselStateData{Depth: -1, DepthLastUpdateAge: -1, WindSpeedTrueKts: -1, WindLastUpdateAge: -1, Latitude: -1, Longitude: -1, PositionLastUpdateAge: -1}, nil
	}
	d.trueWindGustKts = func() (float64, bool) { return 0, false }
	res := runStatus(t, d)
	if res.Surroundings.TrueWindSpeed.Missing == "" || res.Surroundings.TrueWindGust.Missing == "" || res.Surroundings.Depth.Missing == "" {
		t.Errorf("surroundings: %+v", res.Surroundings)
	}
}

func TestVesselStatus_EnginesRunningOffAndSilent(t *testing.T) {
	d := statusTestDeps(t)
	res := runStatus(t, d)
	if len(res.Boat.Engines) != 2 {
		t.Fatalf("engines: %+v", res.Boat.Engines)
	}
	if res.Boat.Engines[0].State != "off" || res.Boat.Engines[1].State != "running" || !near(res.Boat.Engines[1].RPM.Value, 13.3*60, 0.01) {
		t.Errorf("engines: %+v", res.Boat.Engines)
	}

	// Keyed off: the engine computer is powered down so its readings stop, and
	// the engine state says so (ADR 0163).
	tel := statusTestTelemetry()
	delete(tel.Samples, "propulsion.0.revolutions")
	d.telemetry = func(time.Time) (telemetryReader, error) { return tel, nil }
	d.engineStates = func(time.Time) map[string]engineStateInfo {
		return map[string]engineStateInfo{"0": {State: engineStateOff}}
	}
	res = runStatus(t, d)
	if res.Boat.Engines[0].State != "off" {
		t.Errorf("keyed off: %+v", res.Boat.Engines[0])
	}

	// Silent with no engine state to explain it: unknown, not off.
	d.engineStates = func(time.Time) map[string]engineStateInfo { return nil }
	res = runStatus(t, d)
	if res.Boat.Engines[0].State != "" || res.Boat.Engines[0].Missing == "" {
		t.Errorf("silent: %+v", res.Boat.Engines[0])
	}

	d.vesselView = func() (vesselView, error) {
		return vesselView{HouseBank: &vesselHouseBankSetting{Path: "electrical.batteries.512"}}, nil
	}
	res = runStatus(t, d)
	if len(res.Boat.Engines) != 0 || res.Boat.EnginesMissing != "no engine items are set up" {
		t.Errorf("no engines: %+v %q", res.Boat.Engines, res.Boat.EnginesMissing)
	}
}

func TestVesselStatus_AnchorState(t *testing.T) {
	d := statusTestDeps(t)
	res := runStatus(t, d)
	if res.Boat.Anchor.Down || res.Boat.Anchor.Missing != "" {
		t.Errorf("no watch set: %+v", res.Boat.Anchor)
	}
	d.anchorWatch = func() (anchorWatchData, bool, string) {
		return anchorWatchData{Lat: -18.2226, Lon: 146.2125, RadiusMeters: 45, SetAt: vesselStatusTestNow.Add(-6 * time.Hour)}, true, ""
	}
	res = runStatus(t, d)
	a := res.Boat.Anchor
	if !a.Down || a.RadiusM != 45 || a.SetAt == "" || a.DistanceFromAnchorM == nil || *a.DistanceFromAnchorM > 100 {
		t.Errorf("anchor: %+v", a)
	}
	d.anchorWatch = func() (anchorWatchData, bool, string) {
		return anchorWatchData{}, false, "anchor_watch.json is unreadable"
	}
	res = runStatus(t, d)
	if res.Boat.Anchor.Missing != "anchor_watch.json is unreadable" {
		t.Errorf("load error: %+v", res.Boat.Anchor)
	}
}

func TestVesselStatus_AlarmsActiveAndIgnored(t *testing.T) {
	d := statusTestDeps(t)
	d.activeAlarms = func() []alarmStatus {
		return []alarmStatus{{RuleID: "x", Label: "House Bank Low", Message: "House bank 24 percent", State: alarmStateAlert, Phase: alarmPhaseActive}}
	}
	d.ignoredAlarms = func() []string { return []string{"Depth Sounder Silent"} }
	res := runStatus(t, d)
	if res.Alarms.ActiveCount != 1 || res.Alarms.Active[0].Title != "House Bank Low" || res.Alarms.Active[0].State != alarmStateAlert {
		t.Errorf("active: %+v", res.Alarms)
	}
	if res.Alarms.IgnoredCount != 1 || res.Alarms.Ignored[0] != "Depth Sounder Silent" {
		t.Errorf("ignored: %+v", res.Alarms)
	}
}

func TestVesselStatus_AnomalyCountsAndSilentSources(t *testing.T) {
	d := statusTestDeps(t)
	d.anomalyReading = func() (anomalyReading, bool) {
		return anomalyReading{
			Values: map[string]float64{anomalySensorFrozenCountPath: 1, anomalySensorOutOfRangeCountPath: 0, anomalySensorSilentSourceCountPath: 2},
			Sensors: map[string][]sensorHealthEntry{
				anomalySensorFrozenCountPath:       {{Name: "Depth", Text: "Depth reading frozen at 3.3 m"}},
				anomalySensorSilentSourceCountPath: {{Name: "Vesper Cortex", Text: "Vesper Cortex silent"}, {Name: "YachtDevices", Text: "YachtDevices silent"}},
			},
			ComputedAt: vesselStatusTestNow.Add(-time.Second),
		}, true
	}
	res := runStatus(t, d)
	a := res.Anomalies
	if a.Frozen != 1 || a.OutOfRange != 0 || a.SilentSources != 2 {
		t.Errorf("counts: %+v", a)
	}
	if len(a.SilentSourceNames) != 2 || a.SilentSourceNames[0] != "Vesper Cortex" {
		t.Errorf("names: %+v", a.SilentSourceNames)
	}
	if len(a.FrozenSensors) != 1 {
		t.Errorf("frozen: %+v", a.FrozenSensors)
	}
}

func TestVesselStatus_StaleAnomalyReadingIsAbsent(t *testing.T) {
	d := statusTestDeps(t)
	d.anomalyReading = func() (anomalyReading, bool) {
		return anomalyReading{Values: map[string]float64{anomalySensorFrozenCountPath: 1}, ComputedAt: vesselStatusTestNow.Add(-time.Minute)}, true
	}
	res := runStatus(t, d)
	if res.Anomalies.Missing == "" {
		t.Errorf("a stale detector tick must not read as zero: %+v", res.Anomalies)
	}
	d.anomalyReading = func() (anomalyReading, bool) { return anomalyReading{}, false }
	res = runStatus(t, d)
	if res.Anomalies.Missing == "" {
		t.Errorf("no tick yet: %+v", res.Anomalies)
	}
}

func pinPlannedStayClock(t *testing.T) {
	t.Helper()
	prev := plannedStayNow
	plannedStayNow = func() time.Time { return vesselStatusTestNow }
	t.Cleanup(func() { plannedStayNow = prev })
}

func plannedStayStoreForTest(t *testing.T, stay plannedStay) func() *documentStore {
	t.Helper()
	pinPlannedStayClock(t)
	store := newTestDocumentStore(t)
	if _, err := store.SetPlannedStay(stay); err != nil {
		t.Fatalf("SetPlannedStay: %v", err)
	}
	return func() *documentStore { return store }
}

func TestVesselStatus_PlannedStayWithDistanceFromTheBoat(t *testing.T) {
	d := statusTestDeps(t)
	stay := plannedStay{Name: "Magnetic Island Marina", Lat: -19.1585, Lon: 146.8508, Arrive: "2026-10-30", Depart: "2026-11-03"}
	d.documents = plannedStayStoreForTest(t, stay)
	res := runStatus(t, d)
	s := res.PlannedStay
	if s.Missing != "" || s.Name != stay.Name || s.Arrive != "2026-10-30" || s.Depart != "2026-11-03" {
		t.Fatalf("stay: %+v", s)
	}
	wantM := haversineMeters(-18.2231125, 146.21240833333334, stay.Lat, stay.Lon)
	if s.DistanceFromBoatM == nil || math.Abs(*s.DistanceFromBoatM-wantM) > 1 {
		t.Errorf("distance: %v want %v", s.DistanceFromBoatM, wantM)
	}
	if s.DistanceFromBoatNM == nil || math.Abs(*s.DistanceFromBoatNM-wantM/metersPerNauticalMile) > 0.06 {
		t.Errorf("distance nm: %v", s.DistanceFromBoatNM)
	}
	if s.BearingTrueDeg == nil || *s.BearingTrueDeg < 90 || *s.BearingTrueDeg > 180 {
		t.Errorf("bearing: %v", s.BearingTrueDeg)
	}
}

func TestVesselStatus_PlannedStayWithoutABoatPosition(t *testing.T) {
	d := statusTestDeps(t)
	d.documents = plannedStayStoreForTest(t, plannedStay{Name: "Magnetic Island Marina", Lat: -19.1585, Lon: 146.8508, Arrive: "2026-10-30", Depart: "2026-11-03"})
	d.vesselState = func() (vesselStateData, error) {
		return vesselStateData{Depth: 3.3, DepthLastUpdateAge: 2, Latitude: -1, Longitude: -1, PositionLastUpdateAge: -1, WindSpeedTrueKts: -1}, nil
	}
	res := runStatus(t, d)
	s := res.PlannedStay
	if s.Name == "" || s.DistanceFromBoatM != nil || s.DistanceMissing == "" {
		t.Errorf("stay: %+v", s)
	}
}

func TestVesselStatus_NoPlannedStayOrAnEndedOne(t *testing.T) {
	d := statusTestDeps(t)
	pinPlannedStayClock(t)
	store := newTestDocumentStore(t)
	d.documents = func() *documentStore { return store }
	res := runStatus(t, d)
	if res.PlannedStay.Missing != "no stay is planned" {
		t.Errorf("none: %+v", res.PlannedStay)
	}
	// A stay whose last day has passed is no stay at all (plannedStayActive):
	// save it while it is current, then let the clock move past it.
	if _, err := store.SetPlannedStay(plannedStay{Name: "Old", Lat: -19.1, Lon: 146.8, Arrive: "2026-10-28", Depart: "2026-10-28"}); err != nil {
		t.Fatal(err)
	}
	d.now = func() time.Time { return vesselStatusTestNow.Add(72 * time.Hour) }
	res = runStatus(t, d)
	if res.PlannedStay.Missing != "the last planned stay ended before today" || res.PlannedStay.Name != "" {
		t.Errorf("ended: %+v", res.PlannedStay)
	}
}

func TestVesselStatus_ToolFailsPlainlyWithoutLiveData(t *testing.T) {
	d := statusTestDeps(t)
	d.telemetry = func(time.Time) (telemetryReader, error) { return nil, errSignalKNoSelf }
	if _, err := d.execute(context.Background(), "get_vessel_status", json.RawMessage(`{}`)); err == nil {
		t.Fatal("expected an error when SignalK has delivered nothing")
	}
	d = statusTestDeps(t)
	d.telemetry = nil
	if _, err := d.execute(context.Background(), "get_vessel_status", json.RawMessage(`{}`)); err == nil {
		t.Fatal("expected an error when the telemetry reader is not wired")
	}
}

// ── observations ────────────────────────────────────────────────────────

func nightsResult(last float64, prior ...float64) overnightResult {
	return overnightResult{
		Basis:                         "history",
		LastNightStart:                time.Date(2026, 10, 27, 8, 40, 0, 0, time.UTC),
		LastNightRatePercentPerHour:   &last,
		PriorNightRatesPercentPerHour: prior,
	}
}

func TestOvernightDrawObservation_Threshold(t *testing.T) {
	cases := []struct {
		name string
		res  overnightResult
		want bool
	}{
		{"1.8x a steady median, five nights", nightsResult(-0.9, -0.5, -0.5, -0.4, -0.6, -0.5), true},
		{"exactly 1.5x with the extra draw above the floor", nightsResult(-0.75, -0.5, -0.5, -0.5), true},
		{"just under 1.5x", nightsResult(-0.74, -0.5, -0.5, -0.5), false},
		{"only two prior nights", nightsResult(-1.5, -0.5, -0.5), false},
		{"a big ratio on a tiny draw stays quiet", nightsResult(-0.15, -0.05, -0.05, -0.05), false},
		{"quieter than usual is not an observation", nightsResult(-0.2, -0.5, -0.5, -0.5), false},
		{"last night not usable", overnightResult{Basis: "history", PriorNightRatesPercentPerHour: []float64{-0.5, -0.5, -0.5}}, false},
	}
	for _, c := range cases {
		got := overnightDrawObservation(c.res)
		if (got != nil) != c.want {
			t.Errorf("%s: observation=%v, want %v", c.name, got, c.want)
		}
	}
}

func TestOvernightDrawObservation_StatesFactsOnly(t *testing.T) {
	o := overnightDrawObservation(nightsResult(-0.9, -0.5, -0.5, -0.4, -0.6, -0.5))
	if o == nil {
		t.Fatal("expected an observation")
	}
	if o.Kind != "overnight_house_draw" || o.NightsCompared != 5 {
		t.Errorf("observation: %+v", o)
	}
	if !near(&o.LastNightPercentPerHour, 0.9, 1e-9) || !near(&o.TypicalPercentPerHour, 0.5, 1e-9) || !near(&o.Ratio, 1.8, 0.001) {
		t.Errorf("figures: %+v", o)
	}
	for _, banned := range []string{"because", "probably", "likely", "caused"} {
		if strings.Contains(strings.ToLower(o.Summary), banned) {
			t.Errorf("summary guesses a cause (%q): %s", banned, o.Summary)
		}
	}
}

func TestVesselStatus_ObservationsAppearOnlyWhenClear(t *testing.T) {
	d := statusTestDeps(t)
	d.overnight = func(time.Time) (overnightResult, string) {
		return nightsResult(-0.9, -0.5, -0.5, -0.4, -0.6, -0.5), ""
	}
	res := runStatus(t, d)
	if len(res.Observations) != 1 || res.Observations[0].Kind != "overnight_house_draw" {
		t.Fatalf("observations: %+v", res.Observations)
	}

	d.overnight = func(time.Time) (overnightResult, string) {
		return nightsResult(-0.5, -0.5, -0.5, -0.4, -0.6, -0.5), ""
	}
	res = runStatus(t, d)
	if len(res.Observations) != 0 || len(res.ObservationsUnchecked) != 0 {
		t.Errorf("a normal night is no observation: %+v %v", res.Observations, res.ObservationsUnchecked)
	}
}

func TestVesselStatus_ObservationsNotCheckedSaysWhy(t *testing.T) {
	d := statusTestDeps(t)
	d.overnight = func(time.Time) (overnightResult, string) { return overnightResult{}, "no house bank role set" }
	res := runStatus(t, d)
	if len(res.Observations) != 0 || len(res.ObservationsUnchecked) != 1 || !strings.Contains(res.ObservationsUnchecked[0], "no house bank role set") {
		t.Errorf("unchecked: %v", res.ObservationsUnchecked)
	}

	// The history basis needs InfluxDB; anything else has no nights to compare.
	d.overnight = func(time.Time) (overnightResult, string) {
		r := "need 2 usable nights, have 0"
		return overnightResult{Basis: "none", Reason: &r}, ""
	}
	res = runStatus(t, d)
	if len(res.ObservationsUnchecked) != 1 || !strings.Contains(res.ObservationsUnchecked[0], "need 2 usable nights") {
		t.Errorf("unchecked: %v", res.ObservationsUnchecked)
	}
}

// computeOvernight must hand over last night on its own, so the comparison
// against the typical rate does not include the night being judged.
func TestComputeOvernight_ExposesLastNightApartFromThePriorOnes(t *testing.T) {
	now, windows := allLookbackWindows(t)
	hours := func(i int) float64 { return windows[i].End.Sub(windows[i].Start).Hours() }
	socSeries := concatPoints(
		syntheticSlopeSeries(windows[0], overnightSampleInterval, 65, 65+(-1.5)*hours(0)),
		syntheticSlopeSeries(windows[1], overnightSampleInterval, 65, 65+(-0.5)*hours(1)),
		syntheticSlopeSeries(windows[2], overnightSampleInterval, 65, 65+(-0.6)*hours(2)),
		syntheticSlopeSeries(windows[3], overnightSampleInterval, 65, 65+(-0.4)*hours(3)),
	)
	result := computeOvernight(now, vesselLat, vesselLon, testOvernightPaths, true, true, lookbackQuery(t, socSeries, nil, nil))
	if result.LastNightRatePercentPerHour == nil || !near(result.LastNightRatePercentPerHour, -1.5, 0.001) {
		t.Fatalf("last night: %v", result.LastNightRatePercentPerHour)
	}
	if !result.LastNightStart.Equal(windows[0].Start) {
		t.Errorf("last night start: %v want %v", result.LastNightStart, windows[0].Start)
	}
	if len(result.PriorNightRatesPercentPerHour) != 3 || !near(&result.PriorNightRatesPercentPerHour[0], -0.5, 0.001) {
		t.Errorf("prior: %v", result.PriorNightRatesPercentPerHour)
	}
	// The dawn projection's own median still covers all four usable nights.
	if result.NightsUsed != 4 {
		t.Errorf("nights used: %d", result.NightsUsed)
	}
}

func TestComputeOvernight_LastNightUnusableLeavesItNil(t *testing.T) {
	now, windows := allLookbackWindows(t)
	hours := func(i int) float64 { return windows[i].End.Sub(windows[i].Start).Hours() }
	// windows[0] has no data: last night cannot be judged, whatever came before.
	socSeries := concatPoints(
		nil,
		syntheticSlopeSeries(windows[1], overnightSampleInterval, 65, 65+(-0.5)*hours(1)),
		syntheticSlopeSeries(windows[2], overnightSampleInterval, 65, 65+(-0.6)*hours(2)),
		syntheticSlopeSeries(windows[3], overnightSampleInterval, 65, 65+(-0.4)*hours(3)),
	)
	result := computeOvernight(now, vesselLat, vesselLon, testOvernightPaths, true, true, lookbackQuery(t, socSeries, nil, nil))
	if result.LastNightRatePercentPerHour != nil || len(result.PriorNightRatesPercentPerHour) != 0 {
		t.Errorf("last night should not be judged: %v %v", result.LastNightRatePercentPerHour, result.PriorNightRatesPercentPerHour)
	}
}
