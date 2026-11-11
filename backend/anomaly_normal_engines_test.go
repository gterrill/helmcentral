package main

import (
	"strings"
	"testing"
	"time"
)

var engineGateBase = time.Date(2026, 10, 20, 8, 0, 0, 0, time.UTC)

// steadyEngine returns n consecutive minutes of one engine at rpm and 80 degC.
func steadyEngine(n int, rpm float64) []normalMinute {
	out := make([]normalMinute, n)
	for i := range out {
		out[i] = normalMinute{At: engineGateBase.Add(time.Duration(i) * time.Minute), Values: map[string]float64{
			"revolutions": rpm / 60, "temperature": 353.15,
		}}
	}
	return out
}

func gateOf(minutes []normalMinute) []bool { return normalEnginesSubsystem().Gate(minutes) }

func TestEngineGate_OneEngineAt1900QualifiesFromAboutMinuteTen(t *testing.T) {
	gate := gateOf(steadyEngine(30, 1900))
	for i, ok := range gate {
		want := i >= 10
		if ok != want {
			t.Errorf("minute %d: gate = %v, want %v", i, ok, want)
		}
	}
}

func TestEngineGate_ASixtyRpmStepWaitsOutTheSteadyWindow(t *testing.T) {
	minutes := steadyEngine(40, 1900)
	for i := 20; i < len(minutes); i++ {
		minutes[i].Values["revolutions"] = 1960.0 / 60
	}
	gate := gateOf(minutes)
	for i := 20; i <= 23; i++ {
		if gate[i] {
			t.Errorf("minute %d: the last five minutes span 60 rpm, the gate must not hold", i)
		}
	}
	if !gate[19] || !gate[24] {
		t.Errorf("steady before the step (%v) and five minutes after it (%v) must hold", gate[19], gate[24])
	}
}

func TestEngineGate_AnExactlyFiftyRpmStepStillHolds(t *testing.T) {
	minutes := steadyEngine(30, 1900)
	for i := 20; i < len(minutes); i++ {
		minutes[i].Values["revolutions"] = 1950.0 / 60
	}
	for i, ok := range gateOf(minutes) {
		if i >= 10 && !ok {
			t.Errorf("minute %d: a 50 rpm change is within the steady band", i)
		}
	}
}

func TestEngineGate_AMissingMinuteRestartsTheRunningClock(t *testing.T) {
	minutes := steadyEngine(30, 1900)
	// Minutes 30 to 39 are missing, ten minutes of no data: a longer gap than the
	// run tolerates, so the next stretch is a new run.
	tail := steadyEngine(30, 1900)
	for i := range tail {
		tail[i].At = engineGateBase.Add(time.Duration(40+i) * time.Minute)
	}
	minutes = append(minutes, tail...)
	gate := gateOf(minutes)
	if !gate[29] {
		t.Fatal("the stretch before the gap held")
	}
	for i := 30; i < 40; i++ {
		if gate[i] {
			t.Errorf("index %d (ten minutes after the gap): the engine has not been running ten minutes since", i)
		}
	}
	if !gate[40] {
		t.Error("ten minutes after the gap the gate holds again")
	}
}

func TestEngineGate_FailsEachCondition(t *testing.T) {
	cases := map[string]func(m *normalMinute){
		"under 900 rpm":     func(m *normalMinute) { m.Values["revolutions"] = 800.0 / 60 },
		"coolant under 65C": func(m *normalMinute) { m.Values["temperature"] = 330 },
		"no rpm":            func(m *normalMinute) { delete(m.Values, "revolutions") },
		"no coolant":        func(m *normalMinute) { delete(m.Values, "temperature") },
	}
	for name, mutate := range cases {
		minutes := steadyEngine(30, 1900)
		for i := range minutes {
			mutate(&minutes[i])
		}
		for i, ok := range gateOf(minutes) {
			if ok {
				t.Errorf("%s: minute %d held", name, i)
			}
		}
	}
}

func TestEngineRegime(t *testing.T) {
	sub := normalEnginesSubsystem()
	s := normalSubject{ID: "port"}
	key, label, ok := sub.Regime(s, normalMinute{Values: map[string]float64{"revolutions": 1900.0 / 60}})
	if !ok || key != "port/rpm1800" || label != "1,800 to 2,000 rpm" {
		t.Fatalf("got %q %q %v", key, label, ok)
	}
	key, label, _ = sub.Regime(s, normalMinute{Values: map[string]float64{"revolutions": 2000.0 / 60}})
	if key != "port/rpm2000" || label != "2,000 to 2,200 rpm" {
		t.Fatalf("a band's lower edge belongs to it: %q %q", key, label)
	}
	if _, _, ok := sub.Regime(s, normalMinute{Values: map[string]float64{}}); ok {
		t.Fatal("a minute with no rpm has no band")
	}
}

func TestEngineAlarmOwns(t *testing.T) {
	sub := normalEnginesSubsystem()
	subjects := map[string]normalSubject{}
	for _, s := range sub.Subjects(vesselView{Engines: []vesselEngineSetting{{Instance: "port"}, {Instance: "starboard"}, {Instance: "0"}}}) {
		subjects[s.ID] = s
	}
	cases := []struct {
		path string
		want string // the engine it belongs to, "" for none
	}{
		{"propulsion.port.oilPressure", "port"},
		{"notifications.propulsion.starboard.temperature", "starboard"},
		{"helmcentral.anomaly.engines.port.oilPressureResidual", "port"},
		{"notifications.helmcentral.anomaly.engines.starboard.temperatureResidual", "starboard"},
		{"helmcentral.anomaly.propulsion.0.exhaustTemperature", "0"},
		{"electrical.batteries.512.voltage", ""},
		{"navigation.speedOverGround", ""},
		{"propulsion", ""},
		{"propulsion.portside.oilPressure", ""},
	}
	for _, c := range cases {
		got := ""
		for id, s := range subjects {
			if normalAlarmBelongs(s, c.path) {
				got = id
			}
		}
		if got != c.want {
			t.Errorf("%q belongs to %q, want %q", c.path, got, c.want)
		}
	}
}

func TestEngineSubjects(t *testing.T) {
	vessel := vesselView{Engines: []vesselEngineSetting{{Instance: "port", Name: "Port"}, {Instance: "", Name: "Nameless"}, {Instance: "starboard", Name: "Starboard engine"}}}
	subs := normalEnginesSubsystem().Subjects(vessel)
	if len(subs) != 2 || subs[0].ID != "port" || subs[1].ID != "starboard" {
		t.Fatalf("subjects = %+v", subs)
	}
	if subs[0].Name != "Port engine" || subs[1].Name != "Starboard engine" {
		t.Fatalf("names = %q, %q", subs[0].Name, subs[1].Name)
	}
	keys := map[string]normalFeature{}
	for _, f := range subs[0].Features {
		keys[f.Key] = f
	}
	for _, want := range []string{"revolutions", "temperature", "oilPressure", "boostPressure", "engineLoad", "fuel.rate", "transmission.oilPressure", "transmission.oilTemperature", "exhaustTemperature"} {
		f, ok := keys[want]
		if !ok {
			t.Errorf("no feature %s", want)
			continue
		}
		if f.Path != "propulsion.port."+want {
			t.Errorf("%s path = %q", want, f.Path)
		}
		if f.SIUnit == "" {
			t.Errorf("%s has no SI unit", want)
		}
	}
	if keys["revolutions"].Explain {
		t.Error("rpm conditions the minute and is never the top reading")
	}
	if !keys["oilPressure"].Explain || !keys["exhaustTemperature"].Explain {
		t.Error("other readings may be named")
	}
	if !keys["revolutions"].Required || !keys["temperature"].Required || keys["oilPressure"].Required {
		t.Error("only the gate's two readings are required")
	}
}

// Two engines whose items carry the same model name must not read the same
// anywhere the learned normal names an engine.
func TestEngineSubjects_EnginesWithTheSameItemNameAreToldApartByPosition(t *testing.T) {
	two := normalEngineSubjects(vesselView{Engines: []vesselEngineSetting{
		{Instance: "port", Name: "Cummins QSB 6.7"}, {Instance: "starboard", Name: "Cummins QSB 6.7"},
	}})
	if len(two) != 2 || two[0].Name != "Port Cummins QSB 6.7 engine" || two[1].Name != "Starboard Cummins QSB 6.7 engine" {
		t.Fatalf("names = %q, %q", two[0].Name, two[1].Name)
	}
	numbered := normalEngineSubjects(vesselView{Engines: []vesselEngineSetting{
		{Instance: "0", Name: "Cummins QSB 6.7"}, {Instance: "1", Name: "Cummins QSB 6.7"},
	}})
	if numbered[0].Name == numbered[1].Name || numbered[0].Name != "Engine 0 Cummins QSB 6.7 engine" {
		t.Fatalf("numbered instances: %q, %q", numbered[0].Name, numbered[1].Name)
	}
	// Names that already differ stay as the operator wrote them.
	apart := normalEngineSubjects(vesselView{Engines: []vesselEngineSetting{
		{Instance: "port", Name: "Port"}, {Instance: "starboard", Name: "Starboard"},
	}})
	if apart[0].Name != "Port engine" || apart[1].Name != "Starboard engine" {
		t.Fatalf("names = %q, %q", apart[0].Name, apart[1].Name)
	}
	// A single engine is not given a position it does not need.
	one := normalEngineSubjects(vesselView{Engines: []vesselEngineSetting{{Instance: "port", Name: "Cummins QSB 6.7"}}})
	if one[0].Name != "Cummins QSB 6.7 engine" {
		t.Fatalf("name = %q", one[0].Name)
	}
}

// The status summary, the readings list and the watch line all use those names.
func TestEngineNames_ReachTheSummaryAndTheLines(t *testing.T) {
	subs := normalEngineSubjects(vesselView{Engines: []vesselEngineSetting{
		{Instance: "port", Name: "Cummins QSB 6.7"}, {Instance: "starboard", Name: "Cummins QSB 6.7"},
	}})
	model := normalSubsystemModel{TrainedAt: trainNow, Source: normalSource{Kind: normalSourceMainBucket},
		Subjects: map[string]normalSubjectModel{subs[0].ID: {Name: subs[0].Name}, subs[1].ID: {Name: subs[1].Name}}}
	s := buildNormalStatus(normalModelFile{Subsystems: map[string]normalSubsystemModel{normalSubsystemEngines: model}}, nil, nil, nil, nil, time.UTC)
	if len(statusEngines(s)) != 2 || statusEngines(s)[0].Name != "Port Cummins QSB 6.7 engine" || statusEngines(s)[1].Name != "Starboard Cummins QSB 6.7 engine" {
		t.Fatalf("engines = %+v", statusEngines(s))
	}
	st := oilStretch(time.Date(2026, 11, 10, 2, 10, 0, 0, time.UTC), 30)
	st.SubjectName = subs[1].Name
	v := mustDescribe(t, st, "metric", time.UTC, st.Start)
	if !strings.HasPrefix(v.Summary, "Starboard Cummins QSB 6.7 engine oil pressure ran low") {
		t.Fatalf("line = %q", v.Summary)
	}
}

func TestEngineSubjects_OneEngineIsASubject(t *testing.T) {
	subs := normalEnginesSubsystem().Subjects(vesselView{Engines: []vesselEngineSetting{{Instance: "0", Name: ""}}})
	if len(subs) != 1 || subs[0].Name != "Engine 0" {
		t.Fatalf("subjects = %+v", subs)
	}
}
