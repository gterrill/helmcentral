package main

// This binary is built with CGO_ENABLED=0 (a static binary for the ZimaOS
// deployment target), so it carries no system zoneinfo database and
// time.LoadLocation would otherwise fail for every IANA zone name on a host
// that doesn't happen to ship /usr/share/zoneinfo. The assistant's get_tides
// tool (ADR 0093, assistant_tools.go) is the first caller in this codebase
// that resolves a tide station's own timezone (e.g. "Australia/Brisbane")
// rather than relying on vesselLocalLocation's longitude-derived fixed
// offset, so this import - which embeds the zoneinfo database in the binary
// - is required from here on.
import _ "time/tzdata"
