package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func createTestSavedPlace(t *testing.T, env *proposalEnv, name string, lat, lon float64) savedPlace {
	t.Helper()
	var p savedPlace
	err := env.docs.RunTx("test create", func(tx *sql.Tx, now time.Time) error {
		var err error
		p, err = createSavedPlaceTx(tx, savedPlace{Name: name, Lat: lat, Lon: lon}, now)
		return err
	})
	if err != nil {
		t.Fatalf("create %q: %v", name, err)
	}
	return p
}

func TestSavedPlaceKey_IgnoresCasePunctuationAndIslandAbbreviations(t *testing.T) {
	same := []string{"Thomas Island South", "thomas  island south", "Thomas Is. South", "THOMAS ISL SOUTH", " Thomas Island - South "}
	want := savedPlaceKey(same[0])
	for _, n := range same {
		if got := savedPlaceKey(n); got != want {
			t.Errorf("%q: key %q, want %q", n, got, want)
		}
	}
	if savedPlaceKey("Thomas Island") == want {
		t.Error("a name that is only part of another must not match it")
	}
}

func TestSavedPlaces_StoreCreateListUpdateDelete(t *testing.T) {
	env := newProposalEnv(t)
	p := createTestSavedPlace(t, env, "Thomas Island South", -20.5446, 149.1189)
	createTestSavedPlace(t, env, "Brampton Island - West Bay", -20.8163, 149.2615)

	var list []savedPlace
	if err := env.docs.Read(func(q sqlQueryer) error {
		var err error
		list, err = listSavedPlacesQ(q)
		return err
	}); err != nil || len(list) != 2 || list[0].Name != "Brampton Island - West Bay" {
		t.Fatalf("list: %v %+v", err, list)
	}

	env.advance()
	err := env.docs.RunTx("test update", func(tx *sql.Tx, now time.Time) error {
		p.Name, p.Lat = "Thomas Is South", -20.55
		_, err := updateSavedPlaceTx(tx, p, now)
		return err
	})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	got, _ := savedPlaceByIDQ(env.docs.db, p.ID)
	if got.Name != "Thomas Is South" || got.Lat != -20.55 || !got.UpdatedAt.After(got.CreatedAt) {
		t.Fatalf("update not stored: %+v", got)
	}

	if err := env.docs.RunTx("test delete", func(tx *sql.Tx, _ time.Time) error { return deleteSavedPlaceTx(tx, p.ID) }); err != nil {
		t.Fatal(err)
	}
	if _, err := savedPlaceByIDQ(env.docs.db, p.ID); err != errSavedPlaceNotFound {
		t.Fatalf("expected not found after delete, got %v", err)
	}
}

func TestSavedPlaces_NamesAreUniqueIgnoringCase(t *testing.T) {
	env := newProposalEnv(t)
	createTestSavedPlace(t, env, "Thomas Island South", -20.5, 149.1)
	err := env.docs.RunTx("dup", func(tx *sql.Tx, now time.Time) error {
		_, err := createSavedPlaceTx(tx, savedPlace{Name: "thomas island south", Lat: -20, Lon: 149}, now)
		return err
	})
	if err == nil || !strings.Contains(err.Error(), "name:") || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("expected a name error, got %v", err)
	}
}

func TestSavedPlaces_ValidationRefusesBadPositions(t *testing.T) {
	for name, p := range map[string]savedPlace{
		"empty name": {Name: " ", Lat: 1, Lon: 1},
		"lat":        {Name: "A", Lat: 91, Lon: 1},
		"lon":        {Name: "A", Lat: 1, Lon: 181},
	} {
		if err := validateSavedPlace(&p); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

func TestSavedPlaces_ProposalCreatesThenUpdates(t *testing.T) {
	env := newProposalEnv(t)
	p := env.propose(t, `{"operations":[{"type":"saved_place","action":"create","fields":{"name":"Thomas Island South","lat":-20.5446,"lon":149.118933}}]}`)
	if d := p.Ops[0].Description; !strings.Contains(d, "Thomas Island South") || !strings.Contains(d, "20°32.676' S") || !strings.Contains(d, "149°07.136' E") {
		t.Fatalf("description should read in degrees and minutes, got %q", d)
	}
	if _, err := env.apply(p.ID); err != nil {
		t.Fatalf("apply: %v", err)
	}
	got, err := savedPlaceByNameQ(env.docs.db, "thomas island south")
	if err != nil || got.Lat != -20.5446 {
		t.Fatalf("expected the place saved: %v %+v", err, got)
	}

	env.advance()
	up := env.propose(t, `{"operations":[{"type":"saved_place","action":"update","id":"`+got.ID+`","fields":{"lat":-20.55,"lon":149.12}}]}`)
	if !strings.Contains(up.Ops[0].Description, "Move saved place Thomas Island South") {
		t.Fatalf("description: %q", up.Ops[0].Description)
	}
	if _, err := env.apply(up.ID); err != nil {
		t.Fatalf("apply update: %v", err)
	}
	got, _ = savedPlaceByIDQ(env.docs.db, got.ID)
	if got.Lat != -20.55 || got.Lon != 149.12 {
		t.Fatalf("expected the move stored: %+v", got)
	}
}

func TestSavedPlaces_ProposalCardShowsOneDegreesAndMinutesPosition(t *testing.T) {
	env := newProposalEnv(t)
	p := env.propose(t, `{"operations":[{"type":"saved_place","action":"create","fields":{"name":"X Bay","lat":-20.5,"lon":149.25}}]}`)
	after := p.Ops[0].After
	if after["position"] != "20°30.000' S 149°15.000' E" || after["lat"] != nil {
		t.Fatalf("card fields: %+v", after)
	}
}

func TestSavedPlacesREST_ListPatchDelete(t *testing.T) {
	env := newProposalEnv(t)
	p := createTestSavedPlace(t, env, "Thomas Island South", -20.5, 149.1)

	c, rec := watchRequestTo(http.MethodGet, "/api/saved-places", "")
	if err := listSavedPlacesHandler(c); err != nil || rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Thomas Island South") {
		t.Fatalf("list: %v %d %s", err, rec.Code, rec.Body)
	}

	c, rec = watchRequestTo(http.MethodPatch, "/api/saved-places/"+p.ID, `{"name":"Thomas South","lat":-20.6}`, "id", p.ID)
	if err := patchSavedPlaceHandler(c); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("patch: %v %d %s", err, rec.Code, rec.Body)
	}
	got, _ := savedPlaceByIDQ(env.docs.db, p.ID)
	if got.Name != "Thomas South" || got.Lat != -20.6 || got.Lon != 149.1 {
		t.Fatalf("patch should change only what it names: %+v", got)
	}

	c, rec = watchRequestTo(http.MethodPatch, "/api/saved-places/"+p.ID, `{"lat":120}`, "id", p.ID)
	if err := patchSavedPlaceHandler(c); err != nil || rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "lat") {
		t.Fatalf("a bad latitude is a 400: %d %s", rec.Code, rec.Body)
	}

	c, rec = watchRequestTo(http.MethodDelete, "/api/saved-places/"+p.ID, "", "id", p.ID)
	if err := deleteSavedPlaceHandler(c); err != nil || rec.Code != http.StatusNoContent {
		t.Fatalf("delete: %v %d", err, rec.Code)
	}
	c, rec = watchRequestTo(http.MethodDelete, "/api/saved-places/"+p.ID, "", "id", p.ID)
	if err := deleteSavedPlaceHandler(c); err != nil || rec.Code != http.StatusNotFound {
		t.Fatalf("second delete is a 404: %d", rec.Code)
	}
}

func TestSavePlaceTool_ProposesCreateThenUpdateWithOldAndNew(t *testing.T) {
	env := newProposalEnv(t)
	ctx := context.Background()
	run := func(args string) assistantProposal {
		raw, _, err := env.deps.execute(ctx, "save_place", json.RawMessage(args))
		if err != nil {
			t.Fatalf("save_place: %v", err)
		}
		var res assistantProposeResult
		if err := json.Unmarshal([]byte(raw), &res); err != nil {
			t.Fatal(err)
		}
		return res.Proposal
	}

	p := run(`{"name":"Thomas Island South","lat":-20.5446,"lon":149.118933}`)
	if len(p.Ops) != 1 || p.Ops[0].Action != changeCreate {
		t.Fatalf("expected a create, got %+v", p.Ops)
	}
	if got, _ := listSavedPlacesQ(env.docs.db); len(got) != 0 {
		t.Fatal("save_place must not write")
	}
	env.saveOnMessage(t, "Save it?", p)
	if _, err := env.apply(p.ID); err != nil {
		t.Fatal(err)
	}

	up := run(`{"name":"thomas island south","lat":-20.55,"lon":149.12}`)
	if up.Ops[0].Action != changeUpdate || up.Ops[0].Before["position"] != "20°32.676' S 149°07.136' E" || up.Ops[0].After["position"] != "20°33.000' S 149°07.200' E" {
		t.Fatalf("expected an update showing old and new, got %+v", up.Ops[0])
	}
}

func TestSavePlaceTool_RefusesAnImpossiblePosition(t *testing.T) {
	env := newProposalEnv(t)
	_, _, err := env.deps.execute(context.Background(), "save_place", json.RawMessage(`{"name":"X","lat":-200,"lon":149}`))
	if err == nil || !strings.Contains(err.Error(), "lat") {
		t.Fatalf("expected a lat error, got %v", err)
	}
}

func TestFindPlaces_SavedPlaceAnswersWithoutAskingTheProvider(t *testing.T) {
	env := newProposalEnv(t)
	createTestSavedPlace(t, env, "Thomas Island South", -20.5446, 149.1189)
	provider := &fakeSearchPlacesProvider{id: "fake-places"}
	deps := findPlacesDeps(-20.1, 149.1, provider)
	deps.documents = env.deps.documents

	raw, _, err := deps.execute(context.Background(), "find_places", json.RawMessage(`{"query":"Thomas Is. South"}`))
	if err != nil {
		t.Fatal(err)
	}
	var res assistantFindPlacesResult
	if err := json.Unmarshal([]byte(raw), &res); err != nil {
		t.Fatal(err)
	}
	if provider.callCount() != 0 {
		t.Errorf("a saved hit should not call the provider")
	}
	if res.Search != "saved" || len(res.Results) != 1 || res.Results[0].Name != "Thomas Island South" || res.Results[0].Source != "saved" {
		t.Fatalf("expected the saved place, got %+v", res)
	}
	if res.Results[0].StraightLineNm == nil || *res.Results[0].StraightLineNm <= 0 || res.Results[0].Lat != -20.5446 {
		t.Errorf("expected distance and position: %+v", res.Results[0])
	}
	if got := assistantFindPlacesLogSuffix("find_places", raw); got != " (search=saved, results=1)" {
		t.Errorf("log suffix %q", got)
	}
}

func TestFindPlaces_NoSavedHitStillAsksTheProvider(t *testing.T) {
	env := newProposalEnv(t)
	createTestSavedPlace(t, env, "Thomas Island South", -20.5446, 149.1189)
	provider := &fakeSearchPlacesProvider{id: "fake-places", result: placeSearchResult{Search: "exact", RadiusNm: 100,
		Results: []placeSearchMatch{{Name: "Shag Cove", Kind: "bay", Lat: -20.11, Lon: 149.1}}}}
	deps := findPlacesDeps(-20.1, 149.1, provider)
	deps.documents = env.deps.documents

	raw, _, err := deps.execute(context.Background(), "find_places", json.RawMessage(`{"query":"Shag Cove"}`))
	if err != nil {
		t.Fatal(err)
	}
	if provider.callCount() != 1 || !strings.Contains(raw, `"Shag Cove"`) || strings.Contains(raw, `"saved"`) {
		t.Fatalf("expected the provider's answer, calls=%d raw=%s", provider.callCount(), raw)
	}
}

func TestFindPlaces_SavedPlaceFoundWithoutAVesselPosition(t *testing.T) {
	env := newProposalEnv(t)
	createTestSavedPlace(t, env, "Thomas Island South", -20.5446, 149.1189)
	provider := &fakeSearchPlacesProvider{id: "fake-places"}
	deps := findPlacesDeps(0, 0, provider)
	deps.vesselState = func() (vesselStateData, error) { return vesselStateData{Latitude: -1, Longitude: -1}, nil }
	deps.documents = env.deps.documents

	raw, _, err := deps.execute(context.Background(), "find_places", json.RawMessage(`{"query":"Thomas Island South"}`))
	if err != nil {
		t.Fatal(err)
	}
	if provider.callCount() != 0 || !strings.Contains(raw, `"search":"saved"`) || !strings.Contains(raw, `"lat":-20.5446`) {
		t.Fatalf("expected the saved place, calls=%d raw=%s", provider.callCount(), raw)
	}
	if strings.Contains(raw, "straight_line") || strings.Contains(raw, `"centre"`) {
		t.Fatalf("distance, bearing and centre must be left out without a position: %s", raw)
	}
	var res assistantFindPlacesResult
	if err := json.Unmarshal([]byte(raw), &res); err != nil || len(res.Results) != 1 {
		t.Fatalf("decode: %v %s", err, raw)
	}
}

func TestSavedPlacesREST_Create(t *testing.T) {
	env := newProposalEnv(t)
	c, rec := watchRequestTo(http.MethodPost, "/api/saved-places", `{"name":"  Thomas  Island South ","lat":-20.5446,"lon":149.1189,"note":"Quiet in a southerly"}`)
	if err := createSavedPlaceHandler(c); err != nil || rec.Code != http.StatusCreated {
		t.Fatalf("create: %v %d %s", err, rec.Code, rec.Body)
	}
	got, err := savedPlaceByNameQ(env.docs.db, "thomas island south")
	if err != nil || got.Name != "Thomas Island South" || got.Note != "Quiet in a southerly" || got.Lat != -20.5446 {
		t.Fatalf("stored: %v %+v", err, got)
	}

	c, rec = watchRequestTo(http.MethodPost, "/api/saved-places", `{"name":"thomas is. south","lat":-20,"lon":149}`)
	if err := createSavedPlaceHandler(c); err != nil || rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "already exists") {
		t.Fatalf("a duplicate name is a 409 saying so: %d %s", rec.Code, rec.Body)
	}

	for name, body := range map[string]string{
		"missing lat": `{"name":"A","lon":149}`,
		"bad lat":     `{"name":"A","lat":95,"lon":149}`,
		"no name":     `{"name":" ","lat":1,"lon":1}`,
		"unknown":     `{"name":"A","lat":1,"lon":1,"colour":"red"}`,
	} {
		c, rec = watchRequestTo(http.MethodPost, "/api/saved-places", body)
		if err := createSavedPlaceHandler(c); err != nil || rec.Code != http.StatusBadRequest {
			t.Errorf("%s: want 400, got %d %s", name, rec.Code, rec.Body)
		}
	}
}

func TestSavedPlacesREST_RenameToATakenNameIsAConflict(t *testing.T) {
	env := newProposalEnv(t)
	createTestSavedPlace(t, env, "Thomas Island South", -20.5, 149.1)
	other := createTestSavedPlace(t, env, "Brampton West", -20.8, 149.2)
	c, rec := watchRequestTo(http.MethodPatch, "/api/saved-places/"+other.ID, `{"name":"thomas island south"}`, "id", other.ID)
	if err := patchSavedPlaceHandler(c); err != nil || rec.Code != http.StatusConflict {
		t.Fatalf("want 409, got %d %s", rec.Code, rec.Body)
	}
}

func TestMCPFindPlaces_ReturnsASavedPlace(t *testing.T) {
	store := newTestMCPStore(t)
	deps := contextTestDeps(t)
	provider := &fakeSearchPlacesProvider{id: "fake-places"}
	placeSearchCache.reset()
	deps.vesselState = func() (vesselStateData, error) {
		return vesselStateData{Latitude: -20.1, Longitude: 149.1}, nil
	}
	deps.placeNames = func() (placeNameProvider, string, error) { return provider, provider.ID(), nil }
	if err := globalDocumentStore.RunTx("seed", func(tx *sql.Tx, now time.Time) error {
		_, err := createSavedPlaceTx(tx, savedPlace{Name: "Thomas Island South", Lat: -20.5446, Lon: 149.1189}, now)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	svc := newMCPService(store, func() assistantToolDeps { return deps })
	e := echo.New()
	e.Any("/mcp", svc.Handle)
	srv := httptest.NewServer(e)
	t.Cleanup(srv.Close)
	env := &mcpTestEnv{store: store, server: srv, deps: deps}

	_, secret, _ := store.CreateToken("t", []string{"find_places"})
	res, err := env.connect(t, secret).CallTool(context.Background(), &mcp.CallToolParams{Name: "find_places", Arguments: map[string]any{"query": "Thomas Is. South"}})
	if err != nil || res.IsError {
		t.Fatal(err, res)
	}
	text := res.Content[0].(*mcp.TextContent).Text
	if !strings.Contains(text, `"search":"saved"`) || !strings.Contains(text, "Thomas Island South") || provider.callCount() != 0 {
		t.Fatalf("an outside assistant should get the saved place, calls=%d: %s", provider.callCount(), text)
	}
}

func TestSavedPlaces_CardShowsTheWholePositionWhenOnlyLatChanges(t *testing.T) {
	env := newProposalEnv(t)
	p := createTestSavedPlace(t, env, "Thomas Island South", -20.5446, 149.118933)
	up := env.propose(t, `{"operations":[{"type":"saved_place","action":"update","id":"`+p.ID+`","fields":{"lat":-20.55}}]}`)
	if got := up.Ops[0].After["position"]; got != "20°33.000' S 149°07.136' E" {
		t.Fatalf("after position %v", got)
	}
	if got := up.Ops[0].Before["position"]; got != "20°32.676' S 149°07.136' E" {
		t.Fatalf("before position %v", got)
	}
}

func TestSavePlaceTool_AMoveKeepsTheStoredName(t *testing.T) {
	env := newProposalEnv(t)
	createTestSavedPlace(t, env, "Thomas Island South", -20.5446, 149.118933)
	raw, _, err := env.deps.execute(context.Background(), "save_place", json.RawMessage(`{"name":"thomas is. south","lat":-20.55,"lon":149.12}`))
	if err != nil {
		t.Fatal(err)
	}
	var res assistantProposeResult
	if err := json.Unmarshal([]byte(raw), &res); err != nil {
		t.Fatal(err)
	}
	if len(res.Proposal.Ops) != 1 || res.Proposal.Ops[0].Action != changeUpdate {
		t.Fatalf("expected an update: %+v", res.Proposal.Ops)
	}
	if n, ok := res.Proposal.Ops[0].After["name"]; ok && n != "Thomas Island South" {
		t.Fatalf("a move must not rename the place: %v", n)
	}
}

func TestSavePlaceTool_SamePositionIsAlreadySavedAndOnlyANewNoteIsProposed(t *testing.T) {
	env := newProposalEnv(t)
	createTestSavedPlace(t, env, "Thomas Island South", -20.5446, 149.118933)
	raw, _, err := env.deps.execute(context.Background(), "save_place", json.RawMessage(`{"name":"Thomas Island South","lat":-20.5446,"lon":149.118933}`))
	if err != nil {
		t.Fatalf("an already saved place is a plain answer, not an error: %v", err)
	}
	if !strings.Contains(raw, "already saved") || strings.Contains(raw, `"proposal"`) {
		t.Fatalf("expected a plain already-saved result, got %s", raw)
	}

	raw, _, err = env.deps.execute(context.Background(), "save_place", json.RawMessage(`{"name":"Thomas Island South","lat":-20.5446,"lon":149.118933,"note":"Quiet in a southerly"}`))
	if err != nil {
		t.Fatal(err)
	}
	var res assistantProposeResult
	if err := json.Unmarshal([]byte(raw), &res); err != nil {
		t.Fatal(err)
	}
	if len(res.Proposal.Ops) != 1 || res.Proposal.Ops[0].After["note"] != "Quiet in a southerly" {
		t.Fatalf("expected a note change, got %s", raw)
	}
}

func TestFindPlaces_NoRecordStoreIsAnError(t *testing.T) {
	provider := &fakeSearchPlacesProvider{id: "fake-places"}
	deps := findPlacesDeps(-20.1, 149.1, provider)
	deps.documents = func() *documentStore { return nil }
	if _, _, err := deps.execute(context.Background(), "find_places", json.RawMessage(`{"query":"Thomas Island South"}`)); err == nil {
		t.Fatal("a missing record store must be reported, not skipped")
	}
	deps.documents = nil
	if _, _, err := deps.execute(context.Background(), "find_places", json.RawMessage(`{"query":"Thomas Island South"}`)); err == nil {
		t.Fatal("no record store seam must be reported, not skipped")
	}
}

func TestSavedPlaces_ANameKeyCollisionOnWriteIsNameTaken(t *testing.T) {
	env := newProposalEnv(t)
	createTestSavedPlace(t, env, "Thomas Island South", -20.5, 149.1)
	_, err := env.docs.db.Exec(`INSERT INTO saved_places (id, name, name_key, lat, lon, note, created_at, updated_at)
		VALUES ('x', 'Thomas Island South', ?, 0, 0, '', 0, 0)`, savedPlaceKey("Thomas Island South"))
	if err == nil {
		t.Fatal("expected the unique constraint to fire")
	}
	if got := savedPlaceWriteError(err); !errors.Is(got, errSavedPlaceNameTaken) {
		t.Fatalf("want errSavedPlaceNameTaken, got %v", got)
	}
	if got := savedPlaceWriteError(errors.New("disk full")); errors.Is(got, errSavedPlaceNameTaken) {
		t.Fatal("other errors must pass through")
	}
}
