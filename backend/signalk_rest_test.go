package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

const coursePutPath = "/signalk/v2/api/vessels/self/navigation/course/activeRoute"

func setupSignalKRestTest(t *testing.T) {
	t.Helper()
	withTestSecretsStore(t) // a store with nothing in it: no credential configured
	invalidateSignalKToken()
	t.Cleanup(invalidateSignalKToken)
}

type capturedRequest struct {
	Method string
	Path   string
	Body   []byte
}

// recordingServer captures every request it receives and dispatches by exact
// "METHOD path" key to a configurable response. Unmatched requests 404.
type recordingServer struct {
	mu        sync.Mutex
	requests  []capturedRequest
	responses map[string]struct {
		status int
		body   string
	}
}

func newRecordingServer(t *testing.T) (*httptest.Server, *recordingServer) {
	t.Helper()
	rs := &recordingServer{responses: map[string]struct {
		status int
		body   string
	}{}}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)

		rs.mu.Lock()
		rs.requests = append(rs.requests, capturedRequest{Method: r.Method, Path: r.URL.Path, Body: body})
		resp, ok := rs.responses[r.Method+" "+r.URL.Path]
		rs.mu.Unlock()

		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(resp.status)
		_, _ = w.Write([]byte(resp.body))
	}))
	return srv, rs
}

func (rs *recordingServer) on(method, path string, status int, body string) {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	rs.responses[method+" "+path] = struct {
		status int
		body   string
	}{status: status, body: body}
}

func (rs *recordingServer) calls() []capturedRequest {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	return append([]capturedRequest(nil), rs.requests...)
}

// ── auth retry ────────────────────────────────────────────────────────────────

func TestSignalkRequestJSONWithAuth_RetriesOnAuthFailure(t *testing.T) {
	setupSignalKRestTest(t)
	withSeededSecretsStore(t, map[string]string{
		"SIGNALK_USERNAME": "u",
		"SIGNALK_PASSWORD": "p",
	})

	srv, rs := newRecordingServer(t)
	defer srv.Close()

	rs.on(http.MethodPost, "/signalk/v1/auth/login", http.StatusOK, `{"token":"test-token","timeToLive":3600}`)

	callCount := 0
	var mu sync.Mutex
	srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/signalk/v1/auth/login" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"token":"test-token","timeToLive":3600}`))
			return
		}
		if r.Method == http.MethodPut && r.URL.Path == coursePutPath {
			mu.Lock()
			callCount++
			n := callCount
			mu.Unlock()
			if n == 1 {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			w.WriteHeader(http.StatusOK)
			return
		}
		http.NotFound(w, r)
	})

	settingsPath := settingsFileForServer(t, srv.URL)
	err := signalkRequestJSONWithAuth(srv.URL, settingsPath, coursePutPath, http.MethodPut, map[string]any{"href": "/resources/routes/x"})
	if err != nil {
		t.Fatalf("expected retry to succeed, got error: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if callCount != 2 {
		t.Fatalf("expected 2 attempts (401 then success), got %d", callCount)
	}
}
