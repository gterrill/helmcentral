package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// insertFixedIDAlarmRuleForTest stores rule under the id it carries, without a
// seed marker, as a build that died part way through a set would have left it.
func insertFixedIDAlarmRuleForTest(t *testing.T, rule alarmRule) (alarmRule, error) {
	t.Helper()
	if err := validateAlarmRule(&rule); err != nil {
		return alarmRule{}, err
	}
	err := globalDocumentStore.writeAlarmRules("test insert", func(tx *sql.Tx, now time.Time) error {
		rule.CreatedAt, rule.UpdatedAt = now, now
		return insertAlarmRuleTx(tx, rule)
	})
	return rule, err
}

func TestAlarmRulesSurviveReopeningTheStore(t *testing.T) {
	withTempAlarmRules(t)
	level := 7.5
	rule := validRule()
	rule.AlarmLevelValue = &level
	rule.CapacityPath = "tanks.fuel.0.capacity"
	rule.Notify = []string{"ntfy"}
	rule.EscalateAfterSeconds = 90
	created, err := createAlarmRule(rule)
	if err != nil {
		t.Fatal(err)
	}

	store := globalDocumentStore
	reopened, err := newDocumentStore(store.path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer reopened.Close()
	var got alarmRule
	if err := reopened.Read(func(q sqlQueryer) error {
		var err error
		got, err = alarmRuleByIDQ(q, created.ID)
		return err
	}); err != nil {
		t.Fatalf("read back: %v", err)
	}
	if got.Label != created.Label || got.Value != created.Value || got.AlarmLevelValue == nil || *got.AlarmLevelValue != level ||
		got.CapacityPath != rule.CapacityPath || len(got.Notify) != 1 || len(got.Methods) != 2 ||
		got.EscalateAfterSeconds != 90 || !got.CreatedAt.Equal(created.CreatedAt) || !got.UpdatedAt.Equal(created.UpdatedAt) {
		t.Fatalf("stored rule differs:\n got %+v\nwant %+v", got, created)
	}
}

func TestAlarmRuleTxVariantsRollBackAndRefreshAfterCommit(t *testing.T) {
	withTempAlarmRules(t)
	store := globalDocumentStore
	now := time.Now().UTC()
	rule := validRule()
	rule.ID, rule.CreatedAt, rule.UpdatedAt = "tx-rule", now, now

	boom := errors.New("boom")
	err := store.RunTx("test", func(tx *sql.Tx, _ time.Time) error {
		if err := insertAlarmRuleTx(tx, rule); err != nil {
			return err
		}
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("got %v", err)
	}
	if err := refreshAlarmRules(store); err != nil {
		t.Fatal(err)
	}
	if _, ok := getAlarmRule("tx-rule"); ok {
		t.Fatal("a rolled back insert must leave no rule")
	}

	if err := store.RunTx("test", func(tx *sql.Tx, _ time.Time) error { return insertAlarmRuleTx(tx, rule) }); err != nil {
		t.Fatal(err)
	}
	if _, ok := getAlarmRule("tx-rule"); ok {
		t.Fatal("the engine's copy only changes when someone refreshes it")
	}
	if err := refreshAlarmRules(store); err != nil {
		t.Fatal(err)
	}
	if _, ok := getAlarmRule("tx-rule"); !ok {
		t.Fatal("expected the committed rule after a refresh")
	}
}

func TestUpdateAlarmRuleKeepsOriginAndCreationTime(t *testing.T) {
	withTempAlarmRules(t)
	if err := seedHeavyWeatherRules(); err != nil {
		t.Fatal(err)
	}
	rules := listAlarmRules()
	if len(rules) == 0 {
		t.Fatal("expected seeded rules")
	}
	seeded := rules[0]
	edit := seeded
	edit.Value = seeded.Value + 1
	edit.SeededBy = "" // a payload cannot claim or drop an origin
	if _, err := updateAlarmRule(seeded.ID, edit); err != nil {
		t.Fatal(err)
	}
	got, _ := getAlarmRule(seeded.ID)
	if got.SeededBy != heavyWeatherSeedMarker || !got.CreatedAt.Equal(seeded.CreatedAt) || got.Value != seeded.Value+1 {
		t.Fatalf("got %+v", got)
	}
}

func TestSeedersStampTheirRulesAndRecordTheMarkerTogether(t *testing.T) {
	withTempAlarmRules(t)
	for _, seed := range []struct {
		marker string
		run    func() error
	}{
		{heavyWeatherSeedMarker, seedHeavyWeatherRules},
		{forecastWarningsSeedMarker, seedForecastWarningsRules},
		{lawOfStormsSeedMarker, seedLawOfStormsRules},
	} {
		if err := seed.run(); err != nil {
			t.Fatalf("%s: %v", seed.marker, err)
		}
		if err := seed.run(); err != nil { // a second boot adds nothing
			t.Fatalf("%s again: %v", seed.marker, err)
		}
	}
	if err := seedAnomalySet(anomalyBaseSeedMarker, anomalyBaseSeedRules()); err != nil {
		t.Fatal(err)
	}

	counts := map[string]int{}
	for _, r := range listAlarmRules() {
		counts[r.SeededBy]++
	}
	for marker, want := range map[string]int{
		heavyWeatherSeedMarker:     len(heavyWeatherSeedRules(time.Now())),
		forecastWarningsSeedMarker: len(forecastWarningsSeedRules()),
		lawOfStormsSeedMarker:      len(lawOfStormsSeedRules()),
		anomalyBaseSeedMarker:      len(anomalyBaseSeedRules()),
	} {
		if counts[marker] != want || want == 0 {
			t.Errorf("%s: %d stamped, want %d", marker, counts[marker], want)
		}
	}
	if counts[""] != 0 {
		t.Errorf("%d seeded rules carry no origin", counts[""])
	}

	var markers int
	if err := globalDocumentStore.db.QueryRow(`SELECT COUNT(*) FROM alarm_rule_seeded_sets`).Scan(&markers); err != nil || markers != 4 {
		t.Fatalf("markers %d, %v", markers, err)
	}

	op := validRule()
	created, _ := createAlarmRule(op)
	if created.SeededBy != "" {
		t.Errorf("an operator rule has no origin, got %q", created.SeededBy)
	}
}

func TestADeletedSeededRuleStaysDeletedAcrossAReload(t *testing.T) {
	withTempAlarmRules(t)
	if err := seedLawOfStormsRules(); err != nil {
		t.Fatal(err)
	}
	for _, r := range listAlarmRules() {
		if err := deleteAlarmRule(r.ID); err != nil {
			t.Fatal(err)
		}
	}
	if err := loadAlarmRules(); err != nil {
		t.Fatal(err)
	}
	if err := seedLawOfStormsRules(); err != nil {
		t.Fatal(err)
	}
	if n := len(listAlarmRules()); n != 0 {
		t.Fatalf("the set came back: %d rules", n)
	}
}

func TestAlarmRulesFailLoudlyWithoutAStore(t *testing.T) {
	prev := globalDocumentStore
	globalDocumentStore = nil
	t.Cleanup(func() { globalDocumentStore = prev })
	if _, err := createAlarmRule(validRule()); err == nil || !strings.Contains(err.Error(), "record store") {
		t.Fatalf("got %v", err)
	}
	if err := loadAlarmRules(); err == nil {
		t.Fatal("loading without a store must fail")
	}
}

// ---- the one-time import of alarm-rules.json ----

func writeLegacyRules(t *testing.T, path string, v any) {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestImportLegacyAlarmRulesMovesRulesAndMarkersThenRenamesTheFile(t *testing.T) {
	path := withTempAlarmRules(t)
	created := time.Date(2026, 3, 1, 9, 30, 0, 123456789, time.UTC)
	updated := created.Add(48 * time.Hour)
	level := 5.0

	storms := lawOfStormsSeedRules()
	heavy := heavyWeatherSeedRules(time.Now())
	forecast := forecastWarningsSeedRules()
	fileRules := []*alarmRule{
		{ID: "op-1", Label: "Bilge high", Path: "electrical.bilge.0", Op: alarmOpAbove, Value: 0.5, State: alarmStateAlarm, Enabled: true,
			Methods: []string{"sound"}, Notify: []string{"ntfy"}, AlarmLevelValue: &level, CapacityPath: "x.capacity",
			CreatedAt: created, UpdatedAt: updated},
	}
	// Seeded rules as an older build wrote them: a fresh UUID, the seeder's definition.
	for i, set := range [][]alarmRule{storms, heavy, forecast} {
		r := set[0]
		r.ID = []string{"uuid-storm", "uuid-heavy", "uuid-forecast"}[i]
		r.CreatedAt, r.UpdatedAt = created, created
		fileRules = append(fileRules, &r)
	}
	// An operator's edited copy of a seeded rule keeps its definition identity
	// (label, path, op), so it is still recognised.
	fileRules[1].Value += 3
	// The anomaly set is recognised by its fixed ids.
	anomaly := anomalyBaseSeedRules()[0]
	anomaly.CreatedAt, anomaly.UpdatedAt = created, created
	fileRules = append(fileRules, &anomaly)
	// Fixed-id anomaly rules from every sub-set are settled by id alone.
	fixed := map[string]string{}
	for _, set := range []struct {
		marker string
		rules  []alarmRule
	}{
		{anomalyFrozenSeedMarker(), anomalyFrozenSeedRules()},
		{anomalyBatterySeedMarker(), anomalyBatterySeedRules()},
		{anomalyEngineSeedMarker("port"), anomalyEngineSeedRules("port", "Port")[:2]},
		{anomalyEngineStumbleSeedMarker("Starboard"), anomalyEngineStumbleSeedRules("Starboard", "Starboard")},
	} {
		for _, r := range set.rules {
			r.CreatedAt, r.UpdatedAt = created, created
			fileRules = append(fileRules, &r)
			fixed[r.ID] = set.marker
		}
	}
	// A rule from an older seeder definition (another label) stays the operator's.
	old := forecast[1]
	old.ID, old.Label = "uuid-old-definition", "Forecast wind warning (old wording)"
	old.CreatedAt, old.UpdatedAt = created, created
	fileRules = append(fileRules, &old)
	// A rule whose label the operator changed cannot be told from their own.
	renamed := storms[1]
	renamed.ID, renamed.Label = "uuid-renamed", "My own storm wording"
	renamed.CreatedAt, renamed.UpdatedAt = created, created
	fileRules = append(fileRules, &renamed)

	writeLegacyRules(t, path, alarmRulesFile{
		Rules:      fileRules,
		SeededSets: []string{lawOfStormsSeedMarker, heavyWeatherSeedMarker, forecastWarningsSeedMarker, anomalyBaseSeedMarker},
	})
	wantMarkers := 4

	n, err := importLegacyAlarmRules(globalDocumentStore, path, time.Now().UTC())
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if n != len(fileRules) {
		t.Fatalf("imported %d, want %d", n, len(fileRules))
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("the file must be renamed away: %v", err)
	}
	if _, err := os.Stat(path + ".imported"); err != nil {
		t.Fatalf("expected %s.imported: %v", path, err)
	}

	if err := loadAlarmRules(); err != nil {
		t.Fatal(err)
	}
	op, ok := getAlarmRule("op-1")
	if !ok || op.SeededBy != "" || !op.CreatedAt.Equal(created) || !op.UpdatedAt.Equal(updated) ||
		op.AlarmLevelValue == nil || *op.AlarmLevelValue != level || op.CapacityPath != "x.capacity" || op.Notify[0] != "ntfy" {
		t.Fatalf("operator rule not carried over intact: %+v", op)
	}
	for id, want := range map[string]string{
		"uuid-storm": lawOfStormsSeedMarker, "uuid-heavy": heavyWeatherSeedMarker, "uuid-forecast": forecastWarningsSeedMarker,
		anomalyImpossibleRuleID: anomalyBaseSeedMarker, "uuid-renamed": "", "uuid-old-definition": "",
	} {
		got, ok := getAlarmRule(id)
		if !ok || got.SeededBy != want {
			t.Errorf("%s: seeded_by %q (found %v), want %q", id, got.SeededBy, ok, want)
		}
	}
	for id, want := range fixed {
		if got, ok := getAlarmRule(id); !ok || got.SeededBy != want {
			t.Errorf("%s: seeded_by %q (found %v), want %q", id, got.SeededBy, ok, want)
		}
	}
	if !containsString(alarmRulesSeededSets, legacyImportMarker) || len(alarmRulesSeededSets) != wantMarkers+1 {
		t.Errorf("markers %v", alarmRulesSeededSets)
	}
	// Seeding after the import adds nothing: the markers came across.
	before := len(listAlarmRules())
	if err := seedLawOfStormsRules(); err != nil {
		t.Fatal(err)
	}
	if len(listAlarmRules()) != before {
		t.Error("the Law of Storms set was seeded again after the import")
	}
}

func TestImportLegacyAlarmRulesWithNoFileDoesNothing(t *testing.T) {
	path := withTempAlarmRules(t)
	n, err := importLegacyAlarmRules(globalDocumentStore, path, time.Now())
	if err != nil || n != 0 {
		t.Fatalf("got %d, %v", n, err)
	}
}

func TestImportLegacyAlarmRulesRefusesAMalformedFileAndLeavesItAlone(t *testing.T) {
	path := withTempAlarmRules(t)
	for name, body := range map[string]string{
		"not json":                    `{"rules": [`,
		"empty id":                    `{"rules":[{"id":"","label":"x","path":"a.b","op":"above","state":"warn"}]}`,
		"duplicate id":                `{"rules":[{"id":"a","label":"x","path":"a.b","op":"above","state":"warn"},{"id":"a","label":"y","path":"a.b","op":"above","state":"warn"}]}`,
		"unknown operator":            `{"rules":[{"id":"a","label":"x","path":"a.b","op":"sideways","state":"warn"}]}`,
		"unknown state":               `{"rules":[{"id":"a","label":"x","path":"a.b","op":"above","state":"loud"}]}`,
		"ignored sensors left behind": `{"rules":[],"ignored_sensors":["venus:512"]}`,
	} {
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := importLegacyAlarmRules(globalDocumentStore, path, time.Now()); err == nil {
			t.Errorf("%s: expected an error", name)
		}
		if got, _ := os.ReadFile(path); string(got) != body {
			t.Errorf("%s: the file must be left as it was", name)
		}
		if n := len(listAlarmRules()); n != 0 {
			t.Errorf("%s: %d rules imported from a refused file", name, n)
		}
		var stored int
		_ = globalDocumentStore.db.QueryRow(`SELECT COUNT(*) FROM alarm_rules`).Scan(&stored)
		if stored != 0 {
			t.Errorf("%s: %d rows written by a refused import", name, stored)
		}
	}
}

func TestImportLegacyAlarmRulesIsSafeToRepeatAfterACrashBeforeTheRename(t *testing.T) {
	path := withTempAlarmRules(t)
	writeLegacyRules(t, path, alarmRulesFile{
		Rules:      []*alarmRule{{ID: "a", Label: "x", Path: "a.b", Op: alarmOpAbove, State: alarmStateWarn, Value: 1, CreatedAt: time.Now(), UpdatedAt: time.Now()}},
		SeededSets: []string{heavyWeatherSeedMarker},
	})
	if _, err := importLegacyAlarmRules(globalDocumentStore, path, time.Now()); err != nil {
		t.Fatal(err)
	}
	// The operator edits the rule, then the crash puts the old file back.
	if err := os.Rename(path+".imported", path); err != nil {
		t.Fatal(err)
	}
	if err := loadAlarmRules(); err != nil {
		t.Fatal(err)
	}
	edit, _ := getAlarmRule("a")
	edit.Value = 99
	if _, err := updateAlarmRule("a", edit); err != nil {
		t.Fatal(err)
	}
	if _, err := importLegacyAlarmRules(globalDocumentStore, path, time.Now()); err != nil {
		t.Fatalf("repeat: %v", err)
	}
	if got, _ := getAlarmRule("a"); got.Value != 99 {
		t.Fatalf("the repeat overwrote the database with the file: value %v", got.Value)
	}
}

func TestImportLegacyAlarmRulesRefusesToOverwriteAnEarlierImportedFile(t *testing.T) {
	path := withTempAlarmRules(t)
	if err := os.WriteFile(path+".imported", []byte(`{"rules":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	writeLegacyRules(t, path, alarmRulesFile{})
	if _, err := importLegacyAlarmRules(globalDocumentStore, path, time.Now()); err == nil || !strings.Contains(err.Error(), ".imported") {
		t.Fatalf("got %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("the file must stay: %v", err)
	}
}

func TestImportLegacyAlarmRulesRunsAfterTheIgnoredSensorMove(t *testing.T) {
	path := withTempAlarmRules(t)
	store := withTempIgnoreStore(t)
	body := `{"rules":[{"id":"a","label":"x","path":"a.b","op":"above","state":"warn","value":1,"created_at":"2026-03-01T00:00:00Z","updated_at":"2026-03-01T00:00:00Z"}],"ignored_sensors":["venus:512"]}`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := moveLegacyIgnoredSensors(store, path, time.Now()); err != nil {
		t.Fatal(err)
	}
	if n, err := importLegacyAlarmRules(globalDocumentStore, filepath.Clean(path), time.Now()); err != nil || n != 1 {
		t.Fatalf("got %d, %v", n, err)
	}
	if !store.has(ignoredSensorsList, "venus:512") {
		t.Fatal("the ignored sensor was lost")
	}
}

func TestRepeatImportAfterALostRenameDoesNotBringBackDeletedRules(t *testing.T) {
	path := withTempAlarmRules(t)
	now := time.Now()
	writeLegacyRules(t, path, alarmRulesFile{Rules: []*alarmRule{
		{ID: "keep", Label: "k", Path: "a.b", Op: alarmOpAbove, State: alarmStateWarn, CreatedAt: now, UpdatedAt: now},
		{ID: "drop", Label: "d", Path: "a.c", Op: alarmOpAbove, State: alarmStateWarn, CreatedAt: now, UpdatedAt: now},
	}})
	if _, err := importLegacyAlarmRules(globalDocumentStore, path, now); err != nil {
		t.Fatal(err)
	}
	if err := deleteAlarmRule("drop"); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(path+".imported", path); err != nil {
		t.Fatal(err)
	}
	if _, err := importLegacyAlarmRules(globalDocumentStore, path, now); err != nil {
		t.Fatalf("repeat: %v", err)
	}
	if _, ok := getAlarmRule("drop"); ok {
		t.Fatal("a deleted rule came back from the restored file")
	}
	if _, err := os.Stat(path + ".imported"); err != nil {
		t.Fatalf("the repeat must still finish the rename: %v", err)
	}
}

func TestImportIgnoresABlankIgnoredSensorList(t *testing.T) {
	path := withTempAlarmRules(t)
	if err := os.WriteFile(path, []byte(`{"rules":[],"ignored_sensors":["", "  "]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := importLegacyAlarmRules(globalDocumentStore, path, time.Now()); err != nil {
		t.Fatalf("a blank-only list holds nothing to lose: %v", err)
	}
}

func TestACommittedWriteWhoseReloadFailsSaysItWasSaved(t *testing.T) {
	withTempAlarmRules(t)
	err := globalDocumentStore.writeAlarmRules("break", func(tx *sql.Tx, _ time.Time) error {
		_, err := tx.Exec(`DROP TABLE alarm_rule_seeded_sets`)
		return err
	})
	if !errors.Is(err, errAlarmRulesNotReloaded) || !strings.Contains(err.Error(), "was saved") {
		t.Fatalf("got %v", err)
	}
	rec := alarmRuleWriteStatus(err, 400)
	if rec != 500 {
		t.Fatalf("a saved-but-not-reloaded write is a server fault, got %d", rec)
	}
	if alarmRuleWriteStatus(errors.New("other"), 400) != 400 {
		t.Fatal("other errors keep the handler's status")
	}
}

func TestAnEmptyNotifyListRoundTripsAsItWas(t *testing.T) {
	withTempAlarmRules(t)
	now := time.Now().UTC()
	for id, notify := range map[string][]string{"nil": nil, "empty": {}, "some": {"ntfy"}} {
		r := validRule()
		r.ID, r.Notify, r.CreatedAt, r.UpdatedAt = id, notify, now, now
		if err := globalDocumentStore.writeAlarmRules("t", func(tx *sql.Tx, _ time.Time) error { return insertAlarmRuleTx(tx, r) }); err != nil {
			t.Fatal(err)
		}
		got, _ := getAlarmRule(id)
		if (notify == nil) != (got.Notify == nil) || len(got.Notify) != len(notify) {
			t.Errorf("%s: wrote %#v, read %#v", id, notify, got.Notify)
		}
		a, _ := json.Marshal(r)
		b, _ := json.Marshal(got)
		if !strings.Contains(string(a), `"notify"`) == strings.Contains(string(b), `"notify"`) && string(a) != string(b) {
			t.Errorf("%s: JSON differs:\n%s\n%s", id, a, b)
		}
	}
}

func TestRoleBoundSyncsWriteOnlyWhenSomethingMoves(t *testing.T) {
	withTempAlarmRules(t)
	store := globalDocumentStore
	writes := 0
	store.lockWaitHook = func() { writes++ }
	t.Cleanup(func() { store.lockWaitHook = nil })

	bound := validRule()
	bound.Enabled = false
	bound.Path, bound.PathRole = "", alarmPathRoleHouseBankSoC
	created, err := createAlarmRule(bound)
	if err != nil {
		t.Fatal(err)
	}
	literal := validRule()
	literal.Path = "electrical.batteries.9.capacity.stateOfCharge"
	if _, err := createAlarmRule(literal); err != nil {
		t.Fatal(err)
	}

	writes = 0
	if err := syncRoleBoundRules(vesselView{}); err != nil { // want "" == path "", nothing moves
		t.Fatal(err)
	}
	if err := adoptHouseBankRules("electrical.batteries.nothing.here"); err != nil {
		t.Fatal(err)
	}
	if writes != 0 {
		t.Fatalf("%d store writes with nothing to move", writes)
	}
	got, _ := getAlarmRule(created.ID)
	if !got.UpdatedAt.Equal(created.UpdatedAt) {
		t.Fatal("an untouched rule's version moved")
	}

	if err := adoptHouseBankRules(literal.Path); err != nil {
		t.Fatal(err)
	}
	if writes == 0 {
		t.Fatal("adopting a matching rule must write")
	}
}
