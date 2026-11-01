package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func depthOf(v float64) *float64 { return &v }

func createDepthPlace(t *testing.T, env *proposalEnv, name string, lat, lon float64, depth *float64, source, date string) savedPlace {
	t.Helper()
	var p savedPlace
	err := env.docs.RunTx("test create", func(tx *sql.Tx, now time.Time) error {
		var err error
		p, err = createSavedPlaceTx(tx, savedPlace{Name: name, Lat: lat, Lon: lon, DepthM: depth, DepthSource: source, DepthDate: date}, now)
		return err
	})
	if err != nil {
		t.Fatalf("create %q: %v", name, err)
	}
	return p
}

func TestSavedPlaceDepth_StoredReplacedAndCleared(t *testing.T) {
	env := newProposalEnv(t)
	p := createDepthPlace(t, env, "Butterfly Bay", -20.07, 148.92, depthOf(4), "charted", "2026-11-01")
	got, err := savedPlaceByIDQ(env.docs.db, p.ID)
	if err != nil || got.DepthM == nil || *got.DepthM != 4 || got.DepthSource != "charted" || got.DepthDate != "2026-11-01" {
		t.Fatalf("stored: %v %+v", err, got)
	}

	// A new depth replaces the old with its own source and date.
	err = env.docs.RunTx("replace", func(tx *sql.Tx, now time.Time) error {
		got.DepthM, got.DepthSource, got.DepthDate = depthOf(5.5), "sounded", "2026-11-09"
		_, err := updateSavedPlaceTx(tx, got, now)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	got, _ = savedPlaceByIDQ(env.docs.db, p.ID)
	if *got.DepthM != 5.5 || got.DepthSource != "sounded" || got.DepthDate != "2026-11-09" {
		t.Fatalf("replaced: %+v", got)
	}

	err = env.docs.RunTx("clear", func(tx *sql.Tx, now time.Time) error {
		got.DepthM, got.DepthSource, got.DepthDate = nil, "", ""
		_, err := updateSavedPlaceTx(tx, got, now)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	got, _ = savedPlaceByIDQ(env.docs.db, p.ID)
	if got.DepthM != nil || got.DepthSource != "" || got.DepthDate != "" {
		t.Fatalf("cleared: %+v", got)
	}
}

func TestSavedPlaceDepth_ValidationIsAGroup(t *testing.T) {
	env := newProposalEnv(t)
	cases := map[string]savedPlace{
		"source missing":        {DepthM: depthOf(4), DepthDate: "2026-11-01"},
		"source unknown":        {DepthM: depthOf(4), DepthSource: "guessed", DepthDate: "2026-11-01"},
		"date missing":          {DepthM: depthOf(4), DepthSource: "charted"},
		"date not a date":       {DepthM: depthOf(4), DepthSource: "charted", DepthDate: "yesterday"},
		"source without depth":  {DepthSource: "charted", DepthDate: "2026-11-01"},
		"date without depth":    {DepthDate: "2026-11-01"},
		"absurdly deep":         {DepthM: depthOf(5000), DepthSource: "charted", DepthDate: "2026-11-01"},
		"drying more than 10 m": {DepthM: depthOf(-30), DepthSource: "charted", DepthDate: "2026-11-01"},
	}
	for name, p := range cases {
		p.Name, p.Lat, p.Lon = "Spot "+name, -20, 149
		err := env.docs.RunTx("create", func(tx *sql.Tx, now time.Time) error {
			_, err := createSavedPlaceTx(tx, p, now)
			return err
		})
		if err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
	// A drying height is a valid depth.
	createDepthPlace(t, env, "Drying spot", -20, 149, depthOf(-0.8), "charted", "2026-11-01")
}

func TestSavedPlaceDepth_ExistingTableGainsTheColumns(t *testing.T) {
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "old.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE saved_places (id TEXT PRIMARY KEY, name TEXT NOT NULL, name_key TEXT NOT NULL UNIQUE,
		lat REAL NOT NULL, lon REAL NOT NULL, note TEXT NOT NULL DEFAULT '', created_at INTEGER NOT NULL, updated_at INTEGER NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO saved_places VALUES ('a','Old Spot','old spot',-20,149,'',1,1)`); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ { // twice: the migration is safe to repeat
		if err := createSavedPlacesSchema(db); err != nil {
			t.Fatal(err)
		}
	}
	p, err := savedPlaceByIDQ(db, "a")
	if err != nil || p.Name != "Old Spot" || p.DepthM != nil || p.DepthSource != "" {
		t.Fatalf("old row after migration: %v %+v", err, p)
	}
}

func TestSavedPlaceDepthREST_SetReplaceAndClear(t *testing.T) {
	env := newProposalEnv(t)
	c, rec := watchRequestTo(http.MethodPost, "/api/saved-places", `{"name":"Cove","lat":-20,"lon":149,"depth_m":3.5,"depth_source":"charted","depth_date":"2026-11-01"}`)
	if err := createSavedPlaceHandler(c); err != nil || rec.Code != http.StatusCreated {
		t.Fatalf("create: %v %d %s", err, rec.Code, rec.Body)
	}
	var created savedPlace
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil || created.DepthM == nil || *created.DepthM != 3.5 {
		t.Fatalf("created %v: %s", err, rec.Body)
	}

	patch := func(body string) (int, string) {
		c, rec := watchRequestTo(http.MethodPatch, "/api/saved-places/"+created.ID, body, "id", created.ID)
		if err := patchSavedPlaceHandler(c); err != nil {
			t.Fatal(err)
		}
		return rec.Code, rec.Body.String()
	}
	if code, body := patch(`{"depth_m":4.2,"depth_source":"charted","depth_date":"2026-11-02"}`); code != http.StatusOK {
		t.Fatalf("replace: %d %s", code, body)
	}
	got, _ := savedPlaceByIDQ(env.docs.db, created.ID)
	if *got.DepthM != 4.2 || got.DepthDate != "2026-11-02" {
		t.Fatalf("replaced: %+v", got)
	}
	// A rename alone leaves the depth be.
	if code, body := patch(`{"name":"Cove Two"}`); code != http.StatusOK {
		t.Fatalf("rename: %d %s", code, body)
	}
	if got, _ = savedPlaceByIDQ(env.docs.db, created.ID); got.DepthM == nil || *got.DepthM != 4.2 {
		t.Fatalf("a rename must not touch the depth: %+v", got)
	}
	// A new figure needs its own source and date.
	if code, _ := patch(`{"depth_m":9}`); code != http.StatusBadRequest {
		t.Errorf("depth without a source and date: want 400, got %d", code)
	}
	if code, _ := patch(`{"depth_source":"sounded"}`); code != http.StatusBadRequest {
		t.Errorf("a source without a depth: want 400, got %d", code)
	}
	if code, body := patch(`{"depth_m":null}`); code != http.StatusOK {
		t.Fatalf("clear: %d %s", code, body)
	}
	if got, _ = savedPlaceByIDQ(env.docs.db, created.ID); got.DepthM != nil || got.DepthSource != "" || got.DepthDate != "" {
		t.Fatalf("cleared: %+v", got)
	}
}

func TestSavePlaceTool_CarriesAStatedDepthAndShowsTheOldOneBeingReplaced(t *testing.T) {
	env := newProposalEnv(t)
	env.deps.today = time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	run := func(args string) assistantProposal {
		t.Helper()
		raw, _, err := env.deps.execute(context.Background(), "save_place", json.RawMessage(args))
		if err != nil {
			t.Fatalf("save_place: %v", err)
		}
		var res assistantProposeResult
		if err := json.Unmarshal([]byte(raw), &res); err != nil {
			t.Fatal(err)
		}
		return res.Proposal
	}

	p := run(`{"name":"Butterfly Bay","lat":-20.0765,"lon":148.925,"depth_m":4}`)
	if got := p.Ops[0].After["depth"]; got != "4.0 m at datum (charted 2026-11-01)" {
		t.Fatalf("the proposal must show the depth, source and date: %+v", p.Ops[0].After)
	}
	if !strings.Contains(p.Ops[0].Description, "Butterfly Bay") {
		t.Errorf("description %q", p.Ops[0].Description)
	}
	env.saveOnMessage(t, "Save it?", p)
	if _, err := env.apply(p.ID); err != nil {
		t.Fatal(err)
	}
	saved, _ := savedPlaceByNameQ(env.docs.db, "butterfly bay")
	if saved.DepthM == nil || *saved.DepthM != 4 || saved.DepthSource != "charted" || saved.DepthDate != "2026-11-01" {
		t.Fatalf("stored: %+v", saved)
	}

	// A different figure: the confirm step shows the old and the new.
	env.deps.today = time.Date(2026, 11, 8, 0, 0, 0, 0, time.UTC)
	up := run(`{"name":"butterfly bay","lat":-20.0765,"lon":148.925,"depth_m":5.5}`)
	op := up.Ops[0]
	if op.Action != changeUpdate || op.Before["depth"] != "4.0 m at datum (charted 2026-11-01)" || op.After["depth"] != "5.5 m at datum (charted 2026-11-08)" {
		t.Fatalf("expected the old and new depth, got before %+v after %+v", op.Before, op.After)
	}
	if !strings.Contains(op.Description, "5.5 m at datum") {
		t.Errorf("description %q", op.Description)
	}

	// The same figure again changes nothing.
	env.saveOnMessage(t, "Save it?", up)
	if _, err := env.apply(up.ID); err != nil {
		t.Fatal(err)
	}
	raw, _, err := env.deps.execute(context.Background(), "save_place", json.RawMessage(`{"name":"Butterfly Bay","lat":-20.0765,"lon":148.925,"depth_m":5.5}`))
	if err != nil || !strings.Contains(raw, "already_saved") {
		t.Fatalf("the same position and depth is already saved: %v %s", err, raw)
	}
}

func TestSavePlaceTool_ADepthNeedsTheOperatorsDate(t *testing.T) {
	env := newProposalEnv(t)
	env.deps.today = time.Time{}
	_, _, err := env.deps.execute(context.Background(), "save_place", json.RawMessage(`{"name":"Cove","lat":-20,"lon":149,"depth_m":3}`))
	if err == nil || !strings.Contains(err.Error(), "date") {
		t.Fatalf("want an error naming the missing date, got %v", err)
	}
}

func TestSavedPlaceProposal_ClearsADepthWithNull(t *testing.T) {
	env := newProposalEnv(t)
	p := createDepthPlace(t, env, "Cove", -20, 149, depthOf(3), "charted", "2026-11-01")
	prop := env.propose(t, `{"operations":[{"type":"saved_place","action":"update","id":"`+p.ID+`","fields":{"depth_m":null}}]}`)
	if prop.Ops[0].Before["depth"] != "3.0 m at datum (charted 2026-11-01)" || prop.Ops[0].After["depth"] != "none" {
		t.Fatalf("before %+v after %+v", prop.Ops[0].Before, prop.Ops[0].After)
	}
	if _, err := env.apply(prop.ID); err != nil {
		t.Fatal(err)
	}
	if got, _ := savedPlaceByIDQ(env.docs.db, p.ID); got.DepthM != nil {
		t.Fatalf("cleared: %+v", got)
	}
}
