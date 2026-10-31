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

# One -run regex of every name in the shard was about 89 KB at 5,000 tests, and
# Linux refuses any single argv string over 128 KiB (MAX_ARG_STRLEN), so the
# shard would have stopped working as tests were added, whatever the shard
# count. Instead the names are cut into runs of at most 60 KB each. That bound
# does not depend on how many tests there are or how many shards deal them out.
# The test binary is built once and started once per run, so the cost is a
# process start, not another compile and link.
bin=$(mktemp "${TMPDIR:-/tmp}/helmcentral-shard.XXXXXX")
chunks=$(mktemp "${TMPDIR:-/tmp}/helmcentral-shard.XXXXXX")
trap 'rm -f "$bin" "$chunks"' EXIT
go test -short -c -o "$bin" .

echo "$names" | awk -v max=60000 '
	{
		if (len > 0 && len + length($0) + 1 > max) { printf "\n"; len = 0 }
		if (len > 0) { printf "|"; len++ }
		printf "%s", $0
		len += length($0)
	}
	END { if (len > 0) printf "\n" }' >"$chunks"

# `go test` adds -test.paniconexit0 so a test that calls os.Exit(0) fails the
# run instead of ending it quietly with success, and it gives the whole package
# one time limit (10 minutes by default). A compiled binary gets neither unless
# it is told, so both are passed here. The limit is for the whole shard, not for
# each run: every run gets what is left of it.
limit=${GO_TEST_SHARD_TIMEOUT_SECONDS:-600}
deadline=$(($(date +%s) + limit))

status=0
while IFS= read -r pattern; do
	left=$((deadline - $(date +%s)))
	if [ "$left" -le 0 ]; then
		echo "go-test-shard: shard $shard/$total ran past its ${limit}s limit" >&2
		exit 1
	fi
	"$bin" -test.short -test.paniconexit0 -test.timeout "${left}s" -test.run "^($pattern)\$" || status=1
done <"$chunks"
exit "$status"
