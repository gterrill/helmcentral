package main

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
)

func cameraBody(tms int64, extra string) string {
	b := `{"t_ms":` + strconv.FormatInt(tms, 10) + `,"mode":"follow","zoom":14,"pitch":60,"pace":1`
	if extra != "" {
		b += "," + extra
	}
	return b + "}"
}

func TestCameraShotCreateListPatchDelete(t *testing.T) {
	store := withPassageStore(t)
	seedPassage(t, store, passageEpoch, passageEpoch.Add(2*time.Hour))
	at := passageEpoch.Add(time.Hour).UnixMilli()

	code, body := send(t, createCameraShotHandler, http.MethodPost, cameraBody(at, `"bearing_offset_deg":90`))
	if code != http.StatusCreated {
		t.Fatal(code, body)
	}
	shot := body["shot"].(map[string]any)
	id := shot["id"].(string)
	if id == "" || shot["mode"] != "follow" || shot["bearing_offset_deg"] != float64(90) || shot["t_ms"] != float64(at) {
		t.Fatalf("%v", shot)
	}
	// An earlier shot lists first.
	if code, body = send(t, createCameraShotHandler, http.MethodPost, `{"t_ms":`+strconv.FormatInt(at-60000, 10)+`,"mode":"orbit","zoom":13,"pitch":50,"pace":0.5,"orbit_deg_per_s":-12}`); code != http.StatusCreated {
		t.Fatal(code, body)
	}
	got, err := store.ListCameraShots(0, 0)
	if err != nil || len(got) != 2 || got[0].Mode != "orbit" || got[1].ID != id || got[0].OrbitDegPerS != -12 || got[0].Pace != 0.5 {
		t.Fatalf("%+v %v", got, err)
	}
	if code, body = send(t, patchCameraShotHandler, http.MethodPatch, `{"zoom":15.5,"mode":"overview","pace":0.25}`, "id", id); code != 200 {
		t.Fatal(code, body)
	}
	s := body["shot"].(map[string]any)
	if s["zoom"] != 15.5 || s["mode"] != "overview" || s["pace"] != 0.25 || s["pitch"] != float64(60) {
		t.Fatalf("%v", s)
	}
	if code, body = send(t, deleteCameraShotHandler, http.MethodDelete, "", "id", id); code != 200 {
		t.Fatal(code, body)
	}
	if code, _ = send(t, deleteCameraShotHandler, http.MethodDelete, "", "id", id); code != 404 {
		t.Errorf("second delete: %d", code)
	}
	if code, _ = send(t, patchCameraShotHandler, http.MethodPatch, `{"zoom":3}`, "id", id); code != 404 {
		t.Errorf("patch gone: %d", code)
	}
}

func TestCameraShotValidation(t *testing.T) {
	store := withPassageStore(t)
	seedPassage(t, store, passageEpoch, passageEpoch.Add(2*time.Hour))
	at := passageEpoch.Add(time.Hour).UnixMilli()
	cases := map[string]string{
		"mode":         strings.Replace(cameraBody(at, ""), `"follow"`, `"spin"`, 1),
		"zoom high":    strings.Replace(cameraBody(at, ""), `"zoom":14`, `"zoom":22.5`, 1),
		"zoom low":     strings.Replace(cameraBody(at, ""), `"zoom":14`, `"zoom":-1`, 1),
		"pitch":        strings.Replace(cameraBody(at, ""), `"pitch":60`, `"pitch":71`, 1),
		"pace":         strings.Replace(cameraBody(at, ""), `"pace":1`, `"pace":2`, 1),
		"offset":       cameraBody(at, `"bearing_offset_deg":181`),
		"orbit":        cameraBody(at, `"orbit_deg_per_s":91`),
		"outside":      cameraBody(passageEpoch.Add(5*time.Hour).UnixMilli(), ""),
		"missing zoom": `{"t_ms":` + strconv.FormatInt(at, 10) + `,"mode":"follow","pitch":60,"pace":1}`,
		"unknown":      cameraBody(at, `"id":"x"`),
	}
	for name, b := range cases {
		if code, body := send(t, createCameraShotHandler, http.MethodPost, b); code != http.StatusBadRequest {
			t.Errorf("%s: %d %v", name, code, body)
		}
	}
	if n, _ := store.ListCameraShots(0, 0); len(n) != 0 {
		t.Errorf("rejected shots were stored: %d", len(n))
	}
	// Patch is validated the same way and leaves the shot unchanged.
	_, body := send(t, createCameraShotHandler, http.MethodPost, cameraBody(at, ""))
	id := body["shot"].(map[string]any)["id"].(string)
	for _, b := range []string{`{"pace":3}`, `{"pitch":-1}`, `{"mode":"x"}`, `{"t_ms":1}`, `{"nope":1}`} {
		if code, _ := send(t, patchCameraShotHandler, http.MethodPatch, b, "id", id); code != 400 {
			t.Errorf("patch %s: %d", b, code)
		}
	}
	got, _ := store.ListCameraShots(0, 0)
	if got[0].Pace != 1 || got[0].Pitch != 60 {
		t.Errorf("%+v", got[0])
	}
}

// Shots are keyed by real time, so splitting, merging or trimming the passage
// leaves them where they are.
func TestCameraShotsSurviveSplitMergeAndTrim(t *testing.T) {
	store := withPassageStore(t)
	p := seedPassage(t, store, passageEpoch, passageEpoch.Add(4*time.Hour))
	at := passageEpoch.Add(3 * time.Hour).UnixMilli()
	if code, body := send(t, createCameraShotHandler, http.MethodPost, cameraBody(at, "")); code != 201 {
		t.Fatal(code, body)
	}
	first, second, err := store.SplitPassage(p.ID, passageEpoch.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := store.ListCameraShots(0, 0); len(got) != 1 {
		t.Fatalf("after split: %d", len(got))
	}
	if _, err := store.MergePassages(first.ID, second.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.TrimPassage(first.ID, passageEpoch, passageEpoch.Add(2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if got, _ := store.ListCameraShots(0, 0); len(got) != 1 {
		t.Fatalf("after trim: %d", len(got))
	}
}

func TestListCameraShotsRange(t *testing.T) {
	store := withPassageStore(t)
	seedPassage(t, store, passageEpoch, passageEpoch.Add(4*time.Hour))
	for _, h := range []int{1, 2, 3} {
		if code, body := send(t, createCameraShotHandler, http.MethodPost, cameraBody(passageEpoch.Add(time.Duration(h)*time.Hour).UnixMilli(), "")); code != 201 {
			t.Fatal(code, body)
		}
	}
	from := passageEpoch.Add(90 * time.Minute).UnixMilli()
	to := passageEpoch.Add(150 * time.Minute).UnixMilli()
	got, err := store.ListCameraShots(from, to)
	if err != nil || len(got) != 1 {
		t.Fatalf("%+v %v", got, err)
	}
	code, body := send(t, listCameraShotsHandler, http.MethodGet, "")
	if code != 200 || len(body["shots"].([]any)) != 3 {
		t.Fatal(code, body)
	}
	e := echo.New()
	for query, want := range map[string]int{"?from=bad": 400, "?to=1.5": 400, "?from=" + strconv.FormatInt(from, 10) + "&to=" + strconv.FormatInt(to, 10): 200} {
		rec := httptest.NewRecorder()
		c := e.NewContext(httptest.NewRequest(http.MethodGet, "/x"+query, nil), rec)
		if err := listCameraShotsHandler(c); err != nil || rec.Code != want {
			t.Errorf("%s: %d %v", query, rec.Code, err)
		}
		if want == 200 && strings.Count(rec.Body.String(), `"id"`) != 1 {
			t.Errorf("range body %s", rec.Body.String())
		}
	}
}

func TestCameraShotRoutesAreAuthTiered(t *testing.T) {
	want := map[string]apiTier{
		"GET /api/passages/camera-shots":        tierRead,
		"POST /api/passages/camera-shots":       tierWrite,
		"PATCH /api/passages/camera-shots/:id":  tierWrite,
		"DELETE /api/passages/camera-shots/:id": tierWrite,
	}
	for _, r := range passageRoutes() {
		k := r.Method + " " + r.Path
		if w, ok := want[k]; ok {
			if r.Tier != w {
				t.Errorf("%s tier %v", k, r.Tier)
			}
			delete(want, k)
		}
	}
	for k := range want {
		t.Errorf("missing route %s", k)
	}
}
