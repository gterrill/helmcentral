package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
)

func TestGetLogsHandlerReturnsBufferedLogs(t *testing.T) {
	orig := globalLogBuffer
	defer func() { globalLogBuffer = orig }()

	buf := newLogBuffer(10)
	buf.write("sample log 1")
	buf.write("sample log 2")
	globalLogBuffer = buf

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/logs", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	if err := getLogsHandler(c); err != nil {
		t.Fatalf("getLogsHandler returned error: %v", err)
	}

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}

	var res []logEntry
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if len(res) != 2 {
		t.Fatalf("expected 2 log entries, got %d", len(res))
	}
	if res[0].Message != "sample log 1" || res[1].Message != "sample log 2" {
		t.Fatalf("unexpected entries: %+v", res)
	}
}

func TestGetLogsHandlerSinceParam(t *testing.T) {
	orig := globalLogBuffer
	defer func() { globalLogBuffer = orig }()

	buf := newLogBuffer(10)
	buf.write("msg 1")
	buf.write("msg 2")
	buf.write("msg 3")
	globalLogBuffer = buf

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/logs?since=2", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	if err := getLogsHandler(c); err != nil {
		t.Fatalf("getLogsHandler returned error: %v", err)
	}

	var res []logEntry
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if len(res) != 1 {
		t.Fatalf("expected 1 log entry, got %d", len(res))
	}
	if res[0].ID != 3 || res[0].Message != "msg 3" {
		t.Fatalf("unexpected entry: %+v", res[0])
	}
}

func TestLogsStreamHandlerStreamsEntries(t *testing.T) {
	orig := globalLogBuffer
	defer func() { globalLogBuffer = orig }()

	buf := newLogBuffer(10)
	buf.write("initial log")
	globalLogBuffer = buf

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/logs/stream", nil)
	ctx, cancel := context.WithTimeout(req.Context(), 100*time.Millisecond)
	defer cancel()
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	go func() {
		time.Sleep(20 * time.Millisecond)
		buf.write("streamed live log")
	}()

	_ = logsStreamHandler(c)

	body := rec.Body.String()
	if !strings.Contains(body, "initial log") {
		t.Errorf("expected stream to contain initial log, got %q", body)
	}
	if !strings.Contains(body, "streamed live log") {
		t.Errorf("expected stream to contain streamed live log, got %q", body)
	}
}
