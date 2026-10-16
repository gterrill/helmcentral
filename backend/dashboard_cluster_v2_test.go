package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// Finding: the caps dropped (a corner holds one row, a rail two tanks), and a
// cluster saved under the old caps made every later save of its page fail
// validation. Loading brings it within them, logged, the way telltales are moved.
func TestLoadTrimsAnOldClusterWithinTheCaps(t *testing.T) {
	setupDashboardPagesTest(t)
	path := filepath.Join(t.TempDir(), "dashboard-pages.json")
	t.Setenv("DASHBOARD_PAGES_FILE", path)
	body := `{"equipment_converted":true,"pages":[{"id":"p1","name":"Underway","position":0,"created_at":"2026-01-01T00:00:00Z","updated_at":"2026-01-01T00:00:00Z","widgets":[
	  {"id":"cluster:old00001","x":0,"y":0,"w":6,"h":14,"cluster":{"title":"Port",
	    "ring":{"suffix":"revolutions","label":"RPM","display":"radial","quantity":"frequency","unit":"rpm"},
	    "centre":{"suffix":"runTime","label":"Hours","display":"numeric","quantity":"duration","unit":"h"},
	    "corners":[{"label":"Temps","rows":[
	      {"suffix":"temperature","label":"Coolant","display":"numeric","quantity":"temperature","unit":"C"},
	      {"suffix":"transmission.oilTemperature","label":"Gearbox","display":"numeric","quantity":"temperature","unit":"C"}]}],
	    "fuel":{"side":"left","bars":[{"tankId":"a"},{"tankId":"b"},{"tankId":"c"}]}}}]}]}`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	loadDashboardPages()

	dashboardPagesMu.RLock()
	defer dashboardPagesMu.RUnlock()
	page := dashboardPagesState["p1"]
	if page == nil {
		t.Fatal("page was not loaded")
	}
	cl := page.Widgets[0].Cluster
	if len(cl.Corners[0].Rows) != 1 || cl.Corners[0].Rows[0].Suffix != "temperature" {
		t.Fatalf("a corner keeps its first row: %+v", cl.Corners[0].Rows)
	}
	if len(cl.Fuel.Bars) != 2 || cl.Fuel.Bars[1].TankID != "b" {
		t.Fatalf("a rail keeps its first two tanks: %+v", cl.Fuel.Bars)
	}
	if msg := validateDashboardWidgets(page.Widgets, ""); msg != "" {
		t.Fatalf("the trimmed page must be editable again, got %q", msg)
	}
}

// A telltale keeps the alarm id it was issued, wherever it sat in the old list:
// the coolant reading is not always the first.
func TestMovedTelltalesKeepTheirIssuedAlarmIDs(t *testing.T) {
	held := func(suffix string) dashboardGaugeConfig {
		g := zoneGaugeConfig(0, 120, gaugeZone{From: 100, To: 120, State: alarmStateAlarm})
		g.Path, g.Suffix = "", suffix
		return *g
	}
	cl := &dashboardClusterConfig{Title: "Port", Telltales: []dashboardGaugeConfig{
		held("exhaustTemperature"), held("transmission.oilTemperature"), held("temperature"),
	}}
	moveLegacyTelltales(cl)
	if cl.Arcs.Left == nil || cl.Arcs.Left.ZoneIndex == nil || *cl.Arcs.Left.ZoneIndex != 20 {
		t.Fatalf("the coolant was telltale 2, so index 20: %+v", cl.Arcs.Left)
	}
	if cl.Arcs.Right == nil || cl.Arcs.Right.ZoneIndex == nil || *cl.Arcs.Right.ZoneIndex != 19 {
		t.Fatalf("the gearbox was telltale 1, so index 19: %+v", cl.Arcs.Right)
	}

	engine := newTileItem(t, "Port engine", "propulsion.port", "")
	cl.EquipmentID = engine
	cl.Ring = dashboardGaugeConfig{Suffix: "revolutions", Display: "radial", Quantity: "frequency", Unit: "rpm"}
	cl.Centre = dashboardGaugeConfig{Suffix: "runTime", Display: "numeric", Quantity: "duration", Unit: "h"}
	withPages(t, clusterWidget("cluster:abcd1234", cl))
	ids := map[string]bool{}
	for _, r := range zoneDerivedAlarmRules() {
		ids[r.ID] = true
	}
	for _, want := range []string{"zone:cluster:abcd1234:20:0", "zone:cluster:abcd1234:19:0"} {
		if !ids[want] {
			t.Fatalf("expected the issued id %q, got %v", want, ids)
		}
	}
}

// The backend's footer order is the frontend's CLUSTER_FOOTER_SLOTS, with no
// gap in it. One fixture is read by both tests, so neither can drift.
func TestFooterSlotOrderMatchesTheSharedFixture(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "cluster_footer_slots.json"))
	if err != nil {
		t.Fatal(err)
	}
	var want []string
	if err := json.Unmarshal(raw, &want); err != nil {
		t.Fatal(err)
	}

	var tags []string
	typ := reflect.TypeOf(dashboardClusterFooter{})
	for i := 0; i < typ.NumField(); i++ {
		tags = append(tags, strings.Split(typ.Field(i).Tag.Get("json"), ",")[0])
	}
	if !reflect.DeepEqual(tags, want) {
		t.Fatalf("struct field order %v differs from the fixture %v", tags, want)
	}

	footer := &dashboardClusterFooter{}
	v := reflect.ValueOf(footer).Elem()
	for i := 0; i < v.NumField(); i++ {
		v.Field(i).Set(reflect.ValueOf(&dashboardGaugeConfig{Label: fmt.Sprint(i)}))
	}
	slots := footer.slots()
	if len(slots) != len(want) {
		t.Fatalf("slots() has %d entries for %d footer slots (no gaps)", len(slots), len(want))
	}
	for i, slot := range slots {
		if slot == nil || slot.Label != fmt.Sprint(i) {
			t.Fatalf("slots()[%d] is not the field %q: %+v", i, want[i], slot)
		}
	}
}
