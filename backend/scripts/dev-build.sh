#!/bin/sh
#
# Build step for air (see .air.toml). Wraps `go build` so a failure is legible
# rather than silent.
#
# Air keeps the previously built binary running when a build fails, so a
# compile error leaves a stale backend serving requests with nothing in the UI
# to say so. Air's own error log is no help: it appends the string "exit
# status 1" — no newline, no timestamp, and never the compiler output — so it
# grows into one unreadable line and cannot answer "did my last edit build?".
#
# This script fixes both halves of that:
#   - the compiler's actual output is written to tmp/build-errors.log,
#     truncated each run so the file always describes the latest attempt
#   - a failure prints a banner to stdout, which is where air's console picks
#     it up, and so `make logs`
#
# Air's own log is pointed at tmp/air-internal.log so it cannot clobber this.

set -u

BIN="./tmp/helmcentral"
LOG="./tmp/build-errors.log"
PKG="${DEV_BUILD_PKG:-.}"

mkdir -p ./tmp

output=$(go build -o "$BIN" "$PKG" 2>&1)
status=$?
stamp=$(date -u '+%Y-%m-%dT%H:%M:%SZ')

if [ "$status" -eq 0 ]; then
    # Clear it: a leftover failure log outliving the fix that resolved it is
    # the same misleading-staleness problem in the other direction.
    : > "$LOG"
    exit 0
fi

{
    echo "BUILD FAILED  $stamp  (go build exit $status)"
    echo
    echo "$output"
    echo
    echo "The previously built binary is still running. It does NOT include"
    echo "these changes until this builds cleanly."
} > "$LOG"

# stdout, not stderr: air relays the build command's output to its console, so
# this is what shows up in `make logs`.
echo ""
echo "┌───────────────────────────────────────────────────────────────┐"
echo "│  BUILD FAILED — the running backend is now STALE              │"
echo "└───────────────────────────────────────────────────────────────┘"
echo "  $stamp"
echo ""
echo "$output"
echo ""
echo "  Still serving the last binary that compiled. Fix the above and"
echo "  air will rebuild. Also: make build-status"
echo ""

exit "$status"
