package main

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
)

// farSquares are open-sea squares well outside the 200 nm ring around the
// harness position (-20.1, 148.9), nearest first from there.
func farSquares(t *testing.T, h *coastlineHarness) []coverageSquare {
	t.Helper()
	sqs := []coverageSquare{{-30, 160}, {-31, 160}, {-32, 160}, {-33, 160}}
	wanted, _, err := coverageWanted(h.lat, h.lon)
	if err != nil {
		t.Fatal(err)
	}
	inRing := map[coverageSquare]bool{}
	for _, w := range wanted {
		inRing[w] = true
	}
	for _, sq := range sqs {
		if squareInland(sq) || inRing[sq] {
			t.Fatalf("test square %v must be sea outside the ring", sq)
		}
	}
	return sqs
}

func downloadedAfterSteps(h *coastlineHarness, n int) []coverageSquare {
	for i := 0; i < n; i++ {
		h.m.step()
	}
	return h.src.downloads
}

func TestCoastlineRequest_DownloadsBeforeTheRing(t *testing.T) {
	h := newCoastlineHarness(t)
	far := farSquares(t, h)
	if _, err := h.m.Request([]coverageSquare{far[0], far[1]}, false); err != nil {
		t.Fatal(err)
	}
	got := downloadedAfterSteps(h, 3)
	if got[0] != far[0] || got[1] != far[1] {
		t.Fatalf("requested squares first, nearest the boat first: %v", got)
	}
	wanted, _, _ := coverageWanted(h.lat, h.lon)
	if got[2] != wanted[0] {
		t.Errorf("then the ring: %v, want %v", got[2], wanted[0])
	}
}

func TestCoastlineRequest_RequestsRunInTheOrderMade(t *testing.T) {
	h := newCoastlineHarness(t)
	far := farSquares(t, h)
	// The first request holds the farther square; the second a nearer one.
	if _, err := h.m.Request([]coverageSquare{far[3]}, false); err != nil {
		t.Fatal(err)
	}
	if _, err := h.m.Request([]coverageSquare{far[0]}, false); err != nil {
		t.Fatal(err)
	}
	got := downloadedAfterSteps(h, 2)
	if got[0] != far[3] || got[1] != far[0] {
		t.Errorf("order made, not distance: %v", got)
	}
}

func TestCoastlineRequest_WithinARequestNearestTheBoatFirst(t *testing.T) {
	h := newCoastlineHarness(t)
	far := farSquares(t, h)
	if _, err := h.m.Request([]coverageSquare{far[3], far[1], far[2], far[0]}, false); err != nil {
		t.Fatal(err)
	}
	got := downloadedAfterSteps(h, 4)
	for i := range far {
		if got[i] != far[i] {
			t.Fatalf("nearest first: %v", got)
		}
	}
}

func TestCoastlineRequest_WithNoFixFollowsSelectionOrder(t *testing.T) {
	h := newCoastlineHarness(t)
	far := farSquares(t, h)
	h.fix = false
	if _, err := h.m.Request([]coverageSquare{far[2], far[3], far[0]}, false); err != nil {
		t.Fatal(err)
	}
	got := downloadedAfterSteps(h, 3)
	if got[0] != far[2] || got[1] != far[3] || got[2] != far[0] {
		t.Errorf("selection order: %v", got)
	}
}

func TestCoastlineRequest_SurvivesANewManagerAndStore(t *testing.T) {
	path := filepath.Join(t.TempDir(), "helmcentral.sqlite")
	s1, err := newCoastlineStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if n, err := s1.AddRequests([]coverageSquare{{-30, 160}, {-31, 160}}); err != nil || n != 2 {
		t.Fatalf("add: %d %v", n, err)
	}
	s1.Close()
	s2, err := newCoastlineStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	reqs, err := s2.Requests()
	if err != nil || len(reqs) != 2 || reqs[0].Square != (coverageSquare{-30, 160}) || reqs[1].Pos != 1 {
		t.Fatalf("requests after reopen: %+v %v", reqs, err)
	}
	// A manager built over the reopened store downloads them.
	h := newCoastlineHarness(t)
	h.m.cfg.Store = s2
	if got := downloadedAfterSteps(h, 1); got[0] != (coverageSquare{-30, 160}) {
		t.Errorf("restarted manager did not resume the request: %v", got)
	}
}

func TestCoastlineRequest_DownloadedSquareBecomesOrdinary(t *testing.T) {
	h := newCoastlineHarness(t)
	far := farSquares(t, h)
	if _, err := h.m.Request([]coverageSquare{far[0]}, false); err != nil {
		t.Fatal(err)
	}
	h.m.step()
	reqs, _ := h.store.Requests()
	if len(reqs) != 0 {
		t.Errorf("request left after download: %+v", reqs)
	}
	rec, ok, _ := h.store.Record(far[0])
	if !ok || rec.Status != coastlineStatusReady {
		t.Fatalf("not held: %+v", rec)
	}
	st, _ := h.m.Status()
	for _, s := range st.Squares {
		if s.Lat == far[0].Lat && s.Lon == far[0].Lon && s.Requested {
			t.Error("a held square is no longer requested")
		}
	}
	if st.RequestedWaiting != 0 {
		t.Errorf("waiting %d", st.RequestedWaiting)
	}
}

func TestCoastlineRequest_CapsAtOneHundredToDownload(t *testing.T) {
	h := newCoastlineHarness(t)
	var sqs []coverageSquare
	for lat := -60; len(sqs) < 101; lat++ {
		for lon := -170; lon < -150 && len(sqs) < 101; lon++ {
			if sq := (coverageSquare{lat, lon}); !squareInland(sq) {
				sqs = append(sqs, sq)
			}
		}
	}
	if _, err := h.m.Request(sqs, false); !errors.Is(err, errCoastlineBadRequest) {
		t.Fatalf("101 squares: %v", err)
	}
	if reqs, _ := h.store.Requests(); len(reqs) != 0 {
		t.Errorf("a refused request stores nothing: %d", len(reqs))
	}
	// Held and all-land squares do not count toward the cap.
	_ = h.store.SaveSquareAsOf(sqs[0], nil, h.now, coastlineTestDataAsOf)
	res, err := h.m.Request(sqs, false)
	if err != nil || res.Requested != 100 || res.Held != 1 {
		t.Errorf("100 to download plus one held: %+v %v", res, err)
	}
}

func TestCoastlineRequest_RefusesNearTheMeridianAndPoles(t *testing.T) {
	h := newCoastlineHarness(t)
	for _, sq := range []coverageSquare{{89, 10}, {-90, 10}, {10, 180}, {10, -181}, {95, 0}} {
		if _, err := h.m.Request([]coverageSquare{sq}, false); !errors.Is(err, errCoastlineBadRequest) {
			t.Errorf("%v: %v", sq, err)
		}
	}
	if reqs, _ := h.store.Requests(); len(reqs) != 0 {
		t.Errorf("stored %v", reqs)
	}
}

func TestCoastlineRequest_SkipsAllLandAndCountsIt(t *testing.T) {
	h := newCoastlineHarness(t)
	far := farSquares(t, h)
	var inland []coverageSquare
	for lat := -60; lat < 80 && len(inland) < 3; lat++ {
		if sq := (coverageSquare{lat, 20}); squareInland(sq) {
			inland = append(inland, sq)
		}
	}
	if len(inland) < 3 {
		t.Fatal("no inland squares to test with")
	}
	res, err := h.m.Request(append(inland, far[0]), false)
	if err != nil || res.Requested != 1 || res.InlandSkipped != 3 {
		t.Fatalf("%+v %v", res, err)
	}
	reqs, _ := h.store.Requests()
	if len(reqs) != 1 || reqs[0].Square != far[0] {
		t.Errorf("stored %+v", reqs)
	}
}

func TestCoastlineRequest_AlreadyRequestedKeepsItsPlace(t *testing.T) {
	h := newCoastlineHarness(t)
	far := farSquares(t, h)
	_, _ = h.m.Request([]coverageSquare{far[3]}, false)
	res, err := h.m.Request([]coverageSquare{far[0], far[3]}, false)
	if err != nil || res.Requested != 1 || res.AlreadyRequested != 1 {
		t.Fatalf("%+v %v", res, err)
	}
	if got := downloadedAfterSteps(h, 1); got[0] != far[3] {
		t.Errorf("first request first: %v", got)
	}
}

func TestCoastlineRequest_RefusedWhileOffAndNeverSwitchesOn(t *testing.T) {
	h := newCoastlineHarness(t)
	far := farSquares(t, h)
	h.setEnabled(false)
	if _, err := h.m.Request([]coverageSquare{far[0]}, false); !errors.Is(err, errCoastlineOff) {
		t.Fatalf("off: %v", err)
	}
	if _, err := h.m.Remove([]coverageSquare{far[0]}); !errors.Is(err, errCoastlineOff) {
		t.Fatalf("off remove: %v", err)
	}
	if h.enabled {
		t.Error("a request must not turn the switch on")
	}
}

func TestCoastlineRequest_SwitchOffKeepsRequestsAndTheyResume(t *testing.T) {
	h := newCoastlineHarness(t)
	far := farSquares(t, h)
	if _, err := h.m.Request([]coverageSquare{far[0], far[1]}, false); err != nil {
		t.Fatal(err)
	}
	h.setEnabled(false)
	if h.m.step() || len(h.src.downloads) != 0 {
		t.Fatal("off downloads nothing")
	}
	st, err := h.m.Status()
	if err != nil || st.State != "off" || st.RequestedWaiting != 2 {
		t.Fatalf("off status %+v %v", st, err)
	}
	h.setEnabled(true)
	if got := downloadedAfterSteps(h, 1); got[0] != far[0] {
		t.Errorf("did not resume: %v", got)
	}
}

func TestCoastlineRequest_ClearCancelsRequests(t *testing.T) {
	h := newCoastlineHarness(t)
	far := farSquares(t, h)
	_, _ = h.m.Request([]coverageSquare{far[0]}, false)
	if err := h.m.Clear(); err != nil {
		t.Fatal(err)
	}
	if reqs, _ := h.store.Requests(); len(reqs) != 0 {
		t.Errorf("requests left: %+v", reqs)
	}
	got := downloadedAfterSteps(h, 1)
	for _, sq := range got {
		if sq == far[0] {
			t.Error("cancelled request downloaded")
		}
	}
}

func TestCoastlineRequest_FailedRequestBacksOffAndNextGoes(t *testing.T) {
	h := newCoastlineHarness(t)
	far := farSquares(t, h)
	h.src.fail[far[0]] = errors.New("bad data in this box")
	_, _ = h.m.Request([]coverageSquare{far[0], far[1]}, false)
	got := downloadedAfterSteps(h, 2)
	if got[0] != far[0] || got[1] != far[1] {
		t.Fatalf("failed one is passed over, next goes: %v", got)
	}
	st, _ := h.m.Status()
	if st.RequestedFailed != 1 || st.RequestedWaiting != 0 {
		t.Errorf("status waiting %d failed %d", st.RequestedWaiting, st.RequestedFailed)
	}
	var sawErr bool
	for _, s := range st.Squares {
		if s.Lat == far[0].Lat && s.Lon == far[0].Lon {
			sawErr = s.Requested && s.Error != ""
		}
	}
	if !sawErr {
		t.Error("failed requested square carries its reason")
	}
}

func TestCoastlineRequest_StatusMarksWaitingSquares(t *testing.T) {
	h := newCoastlineHarness(t)
	far := farSquares(t, h)
	_, _ = h.m.Request([]coverageSquare{far[0], far[1]}, false)
	st, _ := h.m.Status()
	n := 0
	for _, s := range st.Squares {
		if s.Requested {
			n++
			if s.State != "pending" {
				t.Errorf("state %q", s.State)
			}
		}
	}
	if n != 2 || st.RequestedWaiting != 2 || st.State != "downloading" {
		t.Errorf("requested %d waiting %d state %s", n, st.RequestedWaiting, st.State)
	}
}

func TestCoastlineRemove_DeletesOnlyHeldSquares(t *testing.T) {
	h := newCoastlineHarness(t)
	far := farSquares(t, h)
	_ = h.store.SaveSquareAsOf(far[0], nil, h.now, coastlineTestDataAsOf)
	_ = h.store.SaveSquareAsOf(far[1], nil, h.now, coastlineTestDataAsOf)
	n, err := h.m.Remove([]coverageSquare{far[0], far[2]})
	if err != nil || n != 1 {
		t.Fatalf("removed %d %v", n, err)
	}
	if _, ok, _ := h.store.Record(far[0]); ok {
		t.Error("far[0] still held")
	}
	if _, ok, _ := h.store.Record(far[1]); !ok {
		t.Error("an unselected square was removed")
	}
}

func TestCoastlineRemove_DropsAQueuedUpdateOfTheSquare(t *testing.T) {
	h := newCoastlineHarness(t)
	far := farSquares(t, h)
	_ = h.store.SaveSquareAsOf(far[0], nil, h.now, coastlineTestDataAsOf)
	_ = h.store.RecordEdits(far[0], 2, h.now)
	if _, err := h.m.Update(); err != nil {
		t.Fatal(err)
	}
	if _, err := h.m.Remove([]coverageSquare{far[0]}); err != nil {
		t.Fatal(err)
	}
	if st, _ := h.m.Status(); st.Updating != 0 {
		t.Errorf("queue %d", st.Updating)
	}
}

func TestCoastlineRequest_PreviewCountsAndStoresNothing(t *testing.T) {
	h := newCoastlineHarness(t)
	far := farSquares(t, h)
	var inland coverageSquare
	for lat := -60; lat < 80; lat++ {
		if sq := (coverageSquare{lat, 20}); squareInland(sq) {
			inland = sq
			break
		}
	}
	_ = h.store.SaveSquareAsOf(far[1], nil, h.now, coastlineTestDataAsOf)
	res, err := h.m.Request([]coverageSquare{far[0], far[1], inland}, true)
	if err != nil || res.Requested != 1 || res.Held != 1 || res.InlandSkipped != 1 {
		t.Fatalf("%+v %v", res, err)
	}
	if reqs, _ := h.store.Requests(); len(reqs) != 0 {
		t.Errorf("a preview stored %+v", reqs)
	}
}

func TestCoastlineRequestHandlers_StatusCodes(t *testing.T) {
	h := newCoastlineHarness(t)
	prev := globalCoastlineManager
	globalCoastlineManager = h.m
	t.Cleanup(func() { globalCoastlineManager = prev })
	e := echo.New()
	call := func(handler echo.HandlerFunc, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		ctx := e.NewContext(req, rec)
		if err := handler(ctx); err != nil {
			e.HTTPErrorHandler(err, ctx) // as the server renders a returned error
		}
		return rec
	}
	if rec := call(requestOfflineCoastlineHandler, `{"squares":["-30,160"]}`); rec.Code != http.StatusAccepted {
		t.Errorf("request: %d %s", rec.Code, rec.Body)
	}
	if rec := call(requestOfflineCoastlineHandler, `{"squares":["nope"]}`); rec.Code != http.StatusBadRequest {
		t.Errorf("bad id: %d", rec.Code)
	}
	if rec := call(requestOfflineCoastlineHandler, `{"squares":["89,10"]}`); rec.Code != http.StatusBadRequest {
		t.Errorf("pole: %d", rec.Code)
	}
	if rec := call(removeOfflineCoastlineSquaresHandler, `{"squares":["-30,160"]}`); rec.Code != http.StatusOK {
		t.Errorf("remove: %d %s", rec.Code, rec.Body)
	}
	h.setEnabled(false)
	if rec := call(requestOfflineCoastlineHandler, `{"squares":["-31,160"]}`); rec.Code != http.StatusConflict {
		t.Errorf("off: %d", rec.Code)
	}
}

func TestCoastlineRemove_WinsOverADownloadInFlight(t *testing.T) {
	h := newCoastlineHarness(t)
	far := farSquares(t, h)
	h.fix = false // no ring top-up: the refresh of the held square is the one call
	heldWithoutDataDate(t, h, far[0])
	stepWhileBlocked(t, h, func() {
		if n, err := h.m.Remove([]coverageSquare{far[0]}); err != nil || n != 1 {
			t.Errorf("remove: %d %v", n, err)
		}
	})
	if _, ok, _ := h.store.Record(far[0]); ok {
		t.Error("a download that began before Remove brought the square back")
	}
}

func TestCoastlineStatus_ServesTheSelectableBounds(t *testing.T) {
	h := newCoastlineHarness(t)
	st, err := h.m.Status()
	if err != nil {
		t.Fatal(err)
	}
	want := coastlineSelectableBounds{LatMin: -89, LatMax: 88, LonMin: -180, LonMax: 179}
	if st.Selectable != want {
		t.Errorf("selectable %+v, want %+v", st.Selectable, want)
	}
	if !coastlineSelectable(coverageSquare{88, 179}) || coastlineSelectable(coverageSquare{89, 0}) || coastlineSelectable(coverageSquare{-90, 0}) {
		t.Error("selectable limits")
	}
}

func TestCoastlineRemove_LeavesAnUnrelatedCallInFlightAlone(t *testing.T) {
	h := newCoastlineHarness(t)
	far := farSquares(t, h)
	h.fix = false
	// Records come back in lat order, so the queue is [far[1], far[0]]: the
	// worker pops far[1] (-31) first.
	for _, sq := range []coverageSquare{far[0], far[1]} {
		_ = h.store.SaveSquareAsOf(sq, nil, h.now, coastlineTestDataAsOf)
		_ = h.store.RecordEdits(sq, 3, h.now)
	}
	if _, err := h.m.Update(); err != nil {
		t.Fatal(err)
	}
	stepWhileBlocked(t, h, func() {
		if n, err := h.m.Remove([]coverageSquare{far[0]}); err != nil || n != 1 {
			t.Errorf("remove: %d %v", n, err)
		}
	})
	rec, ok, _ := h.store.Record(far[1])
	if !ok || rec.EditsSince != 0 {
		t.Errorf("the update of the other square was lost: %+v %v", rec, ok)
	}
	if _, ok, _ := h.store.Record(far[0]); ok {
		t.Error("removed square still held")
	}
}

func TestCoastlineRemove_AlsoCancelsRequests(t *testing.T) {
	h := newCoastlineHarness(t)
	far := farSquares(t, h)
	h.src.fail[far[0]] = errors.New("bad data in this box")
	_, _ = h.m.Request([]coverageSquare{far[0], far[1]}, false)
	h.m.step() // far[0] fails and keeps its request
	n, err := h.m.Remove([]coverageSquare{far[0], far[1], far[2]})
	if err != nil || n != 2 {
		t.Fatalf("removed %d %v", n, err)
	}
	if reqs, _ := h.store.Requests(); len(reqs) != 0 {
		t.Errorf("requests left: %+v", reqs)
	}
	if _, ok, _ := h.store.Record(far[0]); ok {
		t.Error("the failed row of a cancelled request is left behind")
	}
	h.m.step()
	for _, sq := range h.src.downloads[1:] {
		if sq == far[0] || sq == far[1] {
			t.Errorf("a cancelled request downloaded: %v", h.src.downloads)
		}
	}
}

func TestCoastlineRequest_RefusalsAreWordedForTheOperator(t *testing.T) {
	h := newCoastlineHarness(t)
	var sqs []coverageSquare
	for lat := -60; len(sqs) < 101; lat++ {
		for lon := -170; lon < -150 && len(sqs) < 101; lon++ {
			if sq := (coverageSquare{lat, lon}); !squareInland(sq) {
				sqs = append(sqs, sq)
			}
		}
	}
	_, err := h.m.Request(sqs, false)
	if err == nil || !strings.Contains(err.Error(), "at most 100") || !strings.Contains(err.Error(), "Select fewer") || strings.Contains(err.Error(), "bad coastline request") {
		t.Errorf("cap text: %v", err)
	}
	_, err = h.m.Request([]coverageSquare{{89, 10}}, false)
	if err == nil || strings.Contains(err.Error(), "180") || !strings.Contains(err.Error(), "pole") {
		t.Errorf("pole text: %v", err)
	}
}

func TestCoastlineRequestHandlers_RefuseAnOversizedListBeforeParsing(t *testing.T) {
	h := newCoastlineHarness(t)
	prev := globalCoastlineManager
	globalCoastlineManager = h.m
	t.Cleanup(func() { globalCoastlineManager = prev })
	e := echo.New()
	ids := strings.Repeat(`"not a square",`, coastlineMaxRequestInput+1)
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"squares":[`+ids+`"x"]}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	ctx := e.NewContext(req, rec)
	if err := requestOfflineCoastlineHandler(ctx); err != nil {
		e.HTTPErrorHandler(err, ctx)
	}
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "Select fewer") {
		t.Errorf("%d %s", rec.Code, rec.Body)
	}
}
