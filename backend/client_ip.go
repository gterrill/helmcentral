package main

import "github.com/labstack/echo/v4"

// clientIPExtractor decides whose address c.RealIP() returns, which is what
// the access log records as remote_ip. Since Echo v4.16.3 an unset extractor
// ignores X-Forwarded-For entirely, so a request through Tailscale Serve would
// log as 127.0.0.1. This trusts the header only from a loopback, link-local or
// private peer (Echo's defaults for ExtractIPFromXFFHeader), so a proxy on the
// box or the boat's network passes the client's address through, and a client
// anywhere else can't choose its own.
func clientIPExtractor() echo.IPExtractor {
	return echo.ExtractIPFromXFFHeader()
}
