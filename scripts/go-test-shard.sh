#!/bin/sh
# Runs one shard of the backend's tests: `go-test-shard.sh <shard> <total>`
# from backend/, with shard counted from 1.
#
# The backend is one package of ~5,000 tests that mostly run one after
# another, so `go test` on one runner is bound by count, not by any slow test.
# This lists every top-level test, example and fuzz target (fuzz seeds run
# under plain `go test` too), takes every <total>th one starting at <shard>,
# and runs exactly those. Every name lands in exactly one shard, so the shards
# together run what `go test -short ./...` runs.
set -eu

shard=$1
total=$2

# Only the root package is sharded. If another package grows tests, stop
# rather than let CI pass without running them.
others=$(go list -f '{{if or .TestGoFiles .XTestGoFiles}}{{.ImportPath}}{{end}}' ./... | grep -vx helmcentral || true)
if [ -n "$others" ]; then
	echo "go-test-shard: these packages have tests this script does not run: $others" >&2
	exit 1
fi

names=$(go test -short -list . . | grep -E '^(Test|Example|Fuzz)' |
	awk -v s="$shard" -v t="$total" '(NR - 1) % t == s - 1')

if [ -z "$names" ]; then
	echo "go-test-shard: shard $shard/$total selected no tests" >&2
	exit 1
fi

echo "go-test-shard: shard $shard/$total runs $(echo "$names" | wc -l | tr -d ' ') tests"
go test -short -run "^($(echo "$names" | paste -sd '|' -))\$" .
