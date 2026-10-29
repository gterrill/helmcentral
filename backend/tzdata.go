package main

// This binary is built with CGO_ENABLED=0 (a static binary for the ZimaOS
// deployment target), so it carries no system zoneinfo database and
// time.LoadLocation would otherwise fail for every IANA zone name on a host
// that doesn't happen to ship /usr/share/zoneinfo. Every zone the boat and
// its places keep (boat_time.go, ADR 0228) is looked up by IANA name, as is a
// tide station's own timezone (e.g. "Australia/Brisbane", ADR 0093), so this
// import - which embeds the zoneinfo database in the binary - is required.
import _ "time/tzdata"
