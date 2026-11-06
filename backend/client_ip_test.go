package main

import (
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
)

func TestClientIPTrustsForwardedForOnlyFromALocalProxy(t *testing.T) {
	e := echo.New()
	e.IPExtractor = clientIPExtractor()

	cases := []struct {
		name, peer, forwardedFor, want string
	}{
		{"Tailscale Serve on the same host", "127.0.0.1:41000", "203.0.113.5", "203.0.113.5"},
		{"a proxy on the boat's network", "192.168.50.240:41000", "203.0.113.5", "203.0.113.5"},
		{"a public client claiming another address", "198.51.100.7:41000", "203.0.113.5", "198.51.100.7"},
		{"a direct request with no header", "192.168.50.20:41000", "", "192.168.50.20"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/api/health", nil)
			req.RemoteAddr = tc.peer
			if tc.forwardedFor != "" {
				req.Header.Set(echo.HeaderXForwardedFor, tc.forwardedFor)
			}
			c := e.NewContext(req, httptest.NewRecorder())
			if got := c.RealIP(); got != tc.want {
				t.Fatalf("RealIP() = %q, want %q", got, tc.want)
			}
		})
	}
}
