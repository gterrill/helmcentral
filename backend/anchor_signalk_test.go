package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/labstack/echo/v4"
)

func anchorPublishEnv(t *testing.T) *publishStub {
	t.Helper()
	anchorTestEnv(t, 0)
	stub := newPublishStub(t)
	withServiceAccount(t, stub)
	anchorWatchMu.Lock()
	old := anchorWatchState
	anchorWatchState = nil
	anchorWatchMu.Unlock()
	t.Cleanup(func() {
		anchorWatchMu.Lock()
		anchorWatchState = old
		anchorWatchMu.Unlock()
	})
	return stub
}

func raiseAnchor(t *testing.T) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	if err := deleteAnchorWatch(echo.New().NewContext(httptest.NewRequest(http.MethodDelete, "/api/anchor-watch", nil), rec)); err != nil {
		t.Fatal(err)
	}
	return rec
}

func TestAnchorLifecyclePublishesPositionAndExplicitNull(t *testing.T) {
	stub := anchorPublishEnv(t)
	for _, lat := range []float64{-20, -20.001} {
		code, body := postAnchorWatch(t, map[string]any{"lat": lat, "lon": 149.0})
		if code != http.StatusOK {
			t.Fatalf("drop/reposition: %d %v", code, body)
		}
	}
	if rec := raiseAnchor(t); rec.Code != http.StatusOK {
		t.Fatalf("raise: %d %s", rec.Code, rec.Body.String())
	}
	// Repeated Raise must also send null, repairing an upstream latch even
	// when Helmcentral already has no local watch.
	if rec := raiseAnchor(t); rec.Code != http.StatusOK {
		t.Fatal(rec.Body.String())
	}
	frames := stub.captured()
	if len(frames) != 4 {
		t.Fatalf("wanted four lifecycle deltas, got %d", len(frames))
	}
	for i, frame := range frames {
		var delta signalKDelta
		if err := json.Unmarshal(frame, &delta); err != nil {
			t.Fatal(err)
		}
		v := delta.Updates[0].Values[0]
		if v.Path != "navigation.anchor.position" {
			t.Fatalf("wrong path: %s", v.Path)
		}
		if i >= 2 {
			if v.Value != nil {
				t.Fatalf("raise must publish null, got %v", v.Value)
			}
		} else {
			pos, ok := v.Value.(map[string]any)
			if !ok || pos["latitude"] != []float64{-20, -20.001}[i] || pos["longitude"] != 149.0 {
				t.Fatalf("wrong coordinates: %v", v.Value)
			}
		}
	}
}

func TestAnchorPublishFailureIsExplicitAndRetainsWatch(t *testing.T) {
	stub := anchorPublishEnv(t)
	code, _ := postAnchorWatch(t, map[string]any{"lat": -20.0, "lon": 149.0})
	if code != http.StatusOK {
		t.Fatalf("initial drop: %d", code)
	}
	stub.mu.Lock()
	stub.ingest = false
	stub.mu.Unlock()
	if rec := raiseAnchor(t); rec.Code != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d", rec.Code)
	}
	if anchorWatchState == nil {
		t.Fatal("failed raise deleted local watch")
	}
	if _, err := os.Stat(anchorWatchFilePath()); err != nil {
		t.Fatalf("watch must remain on disk: %v", err)
	}
	code, body := postAnchorWatch(t, map[string]any{"lat": -21.0, "lon": 149.0})
	if code != http.StatusBadGateway {
		t.Fatalf("failed reposition must report 502, got %d %v", code, body)
	}
}

func TestAnchorPersistFailureDoesNotPublish(t *testing.T) {
	stub := anchorPublishEnv(t)
	file := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(file, []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ANCHOR_WATCH_FILE", filepath.Join(file, "anchor.json"))
	code, _ := postAnchorWatch(t, map[string]any{"lat": -20.0, "lon": 149.0})
	if code != http.StatusInternalServerError {
		t.Fatalf("expected persist failure, got %d", code)
	}
	if len(stub.captured()) != 0 {
		t.Fatal("published despite persist failure")
	}
}

func TestAnchorRaiseMustConfirmExplicitNullNotMissingPath(t *testing.T) {
	stub := anchorPublishEnv(t)
	stub.mu.Lock()
	stub.ingest = false
	stub.mu.Unlock()
	if rec := raiseAnchor(t); rec.Code != http.StatusBadGateway {
		t.Fatalf("absent path is not an accepted raise: %d", rec.Code)
	}
}

func TestAnchorPublishesBowCorrectedPosition(t *testing.T) {
	settings := anchorTestEnv(t, 8)
	stub := newPublishStub(t)
	body := "signalk:\n  address: " + stub.server.URL + "\nanchor:\n  gps_from_bow_m: 8\n"
	if err := os.WriteFile(settings, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	seedHeadingTrue(t, 0)
	code, result := postAnchorWatch(t, map[string]any{"lat": -20.0, "lon": 149.0, "apply_bow_offset": true})
	if code != http.StatusOK {
		t.Fatalf("drop: %d %v", code, result)
	}
	frames := stub.captured()
	if len(frames) != 1 {
		t.Fatalf("wanted one delta, got %d", len(frames))
	}
	var delta signalKDelta
	if err := json.Unmarshal(frames[0], &delta); err != nil {
		t.Fatal(err)
	}
	pos := delta.Updates[0].Values[0].Value.(map[string]any)
	if pos["latitude"] != result["lat"] || pos["longitude"] != result["lon"] || pos["latitude"] == -20.0 {
		t.Fatalf("must publish corrected stored position: %v / %v", pos, result)
	}
}
