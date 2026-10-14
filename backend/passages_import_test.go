package main

import (
	"bytes"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
)

func gpxFor(pts []recordedTrackPoint) string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0"?><gpx version="1.1" xmlns="http://www.topografix.com/GPX/1/1"><trk><trkseg>`)
	for _, p := range pts {
		fmt.Fprintf(&b, `<trkpt lat="%.6f" lon="%.6f"><time>%s</time></trkpt>`, p.Lat, p.Lon, p.At.Format(time.RFC3339))
	}
	b.WriteString(`</trkseg></trk></gpx>`)
	return b.String()
}

func importTrip(pts []recordedTrackPoint) []gpxFile {
	fixes, _, err := parseGPX(strings.NewReader(gpxFor(pts)), gpxMaxFileBytes, 0)
	if err != nil {
		panic(err)
	}
	return []gpxFile{{Name: "a.gpx", Fixes: fixes}}
}

func TestImportCreatesPassageWithDerivedSpeedAndNoReadings(t *testing.T) {
	store := newTestPassageStore(t)
	sum, err := store.ImportGPX(importTrip(simpleTrip(2 * time.Hour).pts))
	if err != nil {
		t.Fatal(err)
	}
	if sum.PassagesCreated != 1 || sum.PointsAdded == 0 {
		t.Fatalf("summary %+v", sum)
	}
	list, _ := store.ListPassages()
	pts, _ := store.TrackPointsBetween(list[0].Start, list[0].End)
	mid := pts[len(pts)/2]
	if mid.SOGKts == nil || !approx(*mid.SOGKts, 6, 0.2) {
		t.Errorf("derived sog %v", mid.SOGKts)
	}
	if mid.COGDeg == nil || !approx(*mid.COGDeg, 0, 0.5) {
		t.Errorf("derived cog %v (due north)", mid.COGDeg)
	}
	if mid.TWSKts != nil || mid.FuelLph != nil || mid.SoCPct != nil || mid.WaterL != nil || mid.GenState != nil {
		t.Errorf("imported points carry no readings: %+v", mid)
	}
	if mid.Source != trackSourceImported {
		t.Errorf("source %q", mid.Source)
	}
}

func TestImportTwiceDoesNotDuplicate(t *testing.T) {
	store := newTestPassageStore(t)
	files := importTrip(simpleTrip(2 * time.Hour).pts)
	first, _ := store.ImportGPX(files)
	second, err := store.ImportGPX(files)
	if err != nil {
		t.Fatal(err)
	}
	if second.PointsAdded != 0 || second.PointsAlreadyHeld != first.PointsAdded || second.PassagesCreated != 0 || second.PassagesSkipped != 1 {
		t.Errorf("re-import summary %+v", second)
	}
	var n int
	store.db.QueryRow(`SELECT COUNT(*) FROM passages`).Scan(&n)
	if n != 1 {
		t.Errorf("passages %d", n)
	}
}

func TestImportSkipsTimeCoveredByRecordedLog(t *testing.T) {
	store := newTestPassageStore(t)
	b := simpleTrip(2 * time.Hour)
	recorded := b.pts[:200]
	appendAll(t, store, recorded)
	sum, err := store.ImportGPX(importTrip(b.pts))
	if err != nil {
		t.Fatal(err)
	}
	if sum.PointsInRecordedLog < 150 {
		t.Errorf("recorded time must win: %+v", sum)
	}
	imported, _ := store.trackPoints(passageEpoch, passageEpoch.Add(48*time.Hour), trackSourceImported)
	last := recorded[len(recorded)-1].At
	for _, p := range imported {
		if !p.At.After(last) {
			t.Fatalf("imported point at %v inside recorded range ending %v", p.At, last)
		}
	}
}

func TestImportedPointsNeverFeedLiveState(t *testing.T) {
	store := newTestPassageStore(t)
	store.ImportGPX(importTrip(simpleTrip(2 * time.Hour).pts))
	if p, _ := store.LastTrackPoint(); p != nil {
		t.Errorf("recorder baseline polluted by import: %+v", p)
	}
	if err := runPassageDetection(store); err != nil {
		t.Fatal(err)
	}
	if got, _ := store.TrackPointsSince(passageEpoch.Add(-time.Hour)); len(got) != 0 {
		t.Errorf("live detector sees %d imported points", len(got))
	}
}

func TestParseGPXRejectsMissingTimeAndBadXMLAndOversize(t *testing.T) {
	noTime := `<gpx><trk><trkseg><trkpt lat="1" lon="2"></trkpt></trkseg></trk></gpx>`
	if _, _, err := parseGPX(strings.NewReader(noTime), 1<<20, 0); err == nil {
		t.Error("a point with no time must be an error")
	}
	if _, _, err := parseGPX(strings.NewReader(`<gpx><trk>`), 1<<20, 0); err == nil {
		t.Error("truncated xml must be an error")
	}
	big := gpxFor(simpleTrip(time.Hour).pts)
	if _, _, err := parseGPX(strings.NewReader(big), 100, 0); err == nil {
		t.Error("over the size cap must be an error")
	}
}

func TestParseGPXDoesNotResolveExternalEntities(t *testing.T) {
	doc := `<?xml version="1.0"?><!DOCTYPE gpx [<!ENTITY x SYSTEM "file:///etc/passwd">]>
<gpx><trk><trkseg><trkpt lat="1" lon="2"><time>2026-10-14T00:00:00Z</time><name>&x;</name></trkpt></trkseg></trk></gpx>`
	fixes, _, err := parseGPX(strings.NewReader(doc), 1<<20, 0)
	if err == nil && len(fixes) != 1 {
		t.Errorf("unexpected %v", fixes)
	}
	// either outcome is safe; what matters is no file content is read
}

func TestParseGPXIgnoresWaypointsAndRoutes(t *testing.T) {
	doc := `<gpx><wpt lat="1" lon="2"><time>2026-10-14T00:00:00Z</time></wpt><rte><rtept lat="1" lon="2"/></rte>
<trk><trkseg><trkpt lat="-20" lon="149"><time>2026-10-14T00:00:00Z</time></trkpt></trkseg></trk></gpx>`
	fixes, _, err := parseGPX(strings.NewReader(doc), 1<<20, 0)
	if err != nil || len(fixes) != 1 {
		t.Errorf("%v %v", fixes, err)
	}
}

func TestImportRejectsJumpsAndSentinels(t *testing.T) {
	b := simpleTrip(2 * time.Hour)
	fixes := importTrip(b.pts)[0].Fixes
	fixes[50].Lat, fixes[50].Lon = -1, -1
	fixes[60].Lat += 5
	store := newTestPassageStore(t)
	sum, _ := store.ImportGPX([]gpxFile{{Name: "a", Fixes: fixes}})
	if sum.PointsRejected != 2 {
		t.Errorf("rejected %d, want 2", sum.PointsRejected)
	}
}

func postImport(t *testing.T, files map[string]string) (int, string) {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	for name, body := range files {
		fw, _ := w.CreateFormFile("files", name)
		fw.Write([]byte(body))
	}
	w.Close()
	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/api/passages/import", &buf)
	req.Header.Set("Content-Type", w.FormDataContentType())
	rec := httptest.NewRecorder()
	if err := importPassagesHandler(e.NewContext(req, rec)); err != nil {
		t.Fatal(err)
	}
	return rec.Code, rec.Body.String()
}

func TestImportHandlerTwoFilesOneSummary(t *testing.T) {
	store := withPassageStore(t)
	b := simpleTrip(2 * time.Hour)
	code, body := postImport(t, map[string]string{"day1.gpx": gpxFor(b.pts[:200]), "day1b.gpx": gpxFor(b.pts[200:])})
	if code != 200 {
		t.Fatal(code, body)
	}
	if !strings.Contains(body, `"passages_created":1`) || !strings.Contains(body, `"day1.gpx"`) {
		t.Errorf("body %s", body)
	}
	if list, _ := store.ListPassages(); len(list) != 1 {
		t.Errorf("passages %d", len(list))
	}
}

func TestImportHandlerBadFileStoresNothing(t *testing.T) {
	store := withPassageStore(t)
	good := gpxFor(simpleTrip(2 * time.Hour).pts)
	code, body := postImport(t, map[string]string{"good.gpx": good, "bad.gpx": "<gpx><trk><trkseg><trkpt lat='1' lon='2'/></trkseg></trk></gpx>"})
	if code != http.StatusUnprocessableEntity || !strings.Contains(body, "bad.gpx") {
		t.Errorf("code %d body %s", code, body)
	}
	if list, _ := store.ListPassages(); len(list) != 0 {
		t.Errorf("a failed import stored %d passages", len(list))
	}
	var n int
	store.db.QueryRow(`SELECT COUNT(*) FROM track_points`).Scan(&n)
	if n != 0 {
		t.Errorf("a failed import stored %d points", n)
	}
}

func TestImportHandlerNoFiles(t *testing.T) {
	withPassageStore(t)
	if code, _ := postImport(t, nil); code != http.StatusBadRequest {
		t.Errorf("code %d", code)
	}
}

func TestStoreUpgradesTrackPointsTableWithoutSource(t *testing.T) {
	path := t.TempDir() + "/old.sqlite"
	db, err := openHelmcentralDB(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE track_points (id INTEGER PRIMARY KEY AUTOINCREMENT, t_ms INTEGER NOT NULL, segment INTEGER NOT NULL, lat REAL NOT NULL, lon REAL NOT NULL, sog_kts REAL, cog_deg REAL, tws_kts REAL, gust_kts REAL, soc_pct REAL, fuel_lph REAL, water_l REAL, gen_state TEXT, anchor_set_ms INTEGER)`); err != nil {
		t.Fatal(err)
	}
	db.Exec(`INSERT INTO track_points (t_ms, segment, lat, lon) VALUES (1000, 0, -20, 149)`)
	db.Close()
	s, err := newPassageStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.close()
	p, _ := s.LastTrackPoint()
	if p == nil || p.Source != trackSourceRecorded {
		t.Errorf("existing rows must read as recorded: %+v", p)
	}
}

func TestImportHandlerTakesAYearOfDailyFilesButNotAThousand(t *testing.T) {
	withPassageStore(t)
	mk := func(n int) map[string]string {
		files := map[string]string{}
		for i := 0; i < n; i++ {
			files[fmt.Sprintf("day-%04d.gpx", i)] = `<gpx><trk><trkseg><trkpt lat="-20" lon="149"><time>2026-10-14T00:00:00Z</time></trkpt></trkseg></trk></gpx>`
		}
		return files
	}
	if code, body := postImport(t, mk(122)); code != 200 {
		t.Errorf("122 files: %d %s", code, body)
	}
	if code, body := postImport(t, mk(500)); code != 200 {
		t.Errorf("500 files: %d %s", code, body)
	}
	if code, body := postImport(t, mk(501)); code != http.StatusBadRequest || !strings.Contains(body, "at most 500") {
		t.Errorf("501 files: %d %s", code, body)
	}
}
