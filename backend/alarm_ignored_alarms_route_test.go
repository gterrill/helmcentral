package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
)

func TestRouteAlarmKindIsOneKeyWhicheverPathRaisesIt(t *testing.T) {
	withTempIgnoreStore(t)
	e := echo.New()
	e.POST("/i", ignoreAlarmHandler)
	e.DELETE("/i/:kind", unignoreAlarmHandler)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/i", strings.NewReader(`{"kind":"notifications.navigation.perpendicularPassed"}`))
	req.Header.Set("Content-Type", "application/json")
	e.ServeHTTP(rec, req)
	got := globalIgnoreStore.entries(ignoredAlarmsList)
	if len(got) != 1 || got[0].Key != "perpendicularPassed" {
		t.Fatalf("stored %+v", got)
	}

	for _, path := range []string{"notifications.perpendicularPassed", "notifications.navigation.perpendicularPassed"} {
		snapshot := snapshotWithNotification(path, map[string]any{"state": "alarm", "message": "x"})
		if got := signalKNotifications(snapshot, ownsNothing, alarmNow); len(got) != 0 {
			t.Fatalf("%s must be hidden by the one ignored kind, got %+v", path, got)
		}
	}

	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/i/navigation.perpendicularPassed", nil))
	if len(globalIgnoreStore.entries(ignoredAlarmsList)) != 0 {
		t.Fatal("DELETE under the other spelling must remove the same kind")
	}
}
