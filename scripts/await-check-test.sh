#!/bin/sh
# Cases for await-check.sh: `sh scripts/await-check-test.sh`. Builds fake task
# output files under a private root, shaped like check.sh's real output, and
# never waits (AWAIT_CHECK_LIMIT=0).
set -u
script=$(cd "$(dirname "$0")" && pwd)/await-check.sh
root=$(mktemp -d)
trap 'rm -rf "$root"' EXIT
tasks=$root/claude-501/proj/sess/tasks
mkdir -p "$tasks"
fail=0

# expect <exit code> <task id> <line the output must contain, or empty>
expect() {
	want=$1
	id=$2
	line=$3
	out=$(AWAIT_CHECK_ROOT=$root AWAIT_CHECK_LIMIT=0 sh "$script" "$id" 2>&1)
	got=$?
	if [ "$got" -ne "$want" ]; then
		echo "FAIL: $id: want exit $want, got $got"
		fail=1
	fi
	if [ -n "$line" ] && ! printf '%s\n' "$out" | grep -qF -- "$line"; then
		echo "FAIL: $id: output lacks '$line'"
		fail=1
	fi
}

# A passing nested run, as the outer run's summary prints it (indented).
nested_pass='PASS  backend  (/x/backend/tmp/check/all-backend.log)
   == summary (backend)
   load: 1.00
   PASS  go-vet  (/x/backend/tmp/check/backend-go-vet.log)
   check-backend: all steps passed'

cat >"$tasks/allpass.output" <<EOF
== backend: sh scripts/check.sh backend
   ok

== summary (all)
load: 1.00
$nested_pass
PASS  lock-tests  (/x/backend/tmp/check/all-lock-tests.log)
check-all: all steps passed
EOF
expect 0 allpass "check-all: all steps passed"
expect 0 allpass "== summary (all)"

cat >"$tasks/allfail.output" <<'EOF'
== summary (all)
load: 1.00
FAIL  backend  exit 1  (/x/backend/tmp/check/all-backend.log)
   == summary (backend)
   FAIL  go-test  exit 1  (/x/backend/tmp/check/backend-go-test.log)
   check-backend: FAILED
check-all: FAILED
EOF
expect 1 allfail "check-all: FAILED"

cat >"$tasks/backpass.output" <<'EOF'
== go-vet: go vet ./...
   ok

== summary (backend)
load: 1.00
PASS  go-vet  (/x/backend/tmp/check/backend-go-vet.log)
check-backend: all steps passed
EOF
expect 0 backpass "check-backend: all steps passed"

# A nested indented FAILED line with the outer run also FAILED: exit 1.
cat >"$tasks/nestedonly.output" <<'EOF'
== summary (all)
FAIL  backend  exit 1  (/x/backend/tmp/check/all-backend.log)
   check-backend: FAILED
check-all: FAILED
EOF
expect 1 nestedonly "check-all: FAILED"

# An indented FAILED with no outer final line is not a finished run.
cat >"$tasks/nestedrunning.output" <<'EOF'
== backend: sh scripts/check.sh backend
   check-backend: FAILED
EOF
expect 3 nestedrunning "still running"

cat >"$tasks/early.output" <<'EOF'
check: waiting on another make check
make: *** [check] Error 2
EOF
expect 1 early "stopped before its summary"
expect 1 early "make: *** [check] Error 2"

cat >"$tasks/early1.output" <<'EOF'
make[1]: *** [check-backend] Error 2
EOF
expect 1 early1 "stopped before its summary"

cat >"$tasks/running.output" <<'EOF'
== backend: sh scripts/check.sh backend
EOF
expect 3 running "still running"

# The same ID in two sessions: the newer file decides, here a pass over an
# older failure.
other=$root/claude-501/proj/sess2/tasks
mkdir -p "$other"
cat >"$other/twosess.output" <<'EOF'
== summary (all)
check-all: FAILED
EOF
cat >"$tasks/twosess.output" <<'EOF'
== summary (all)
check-all: all steps passed
EOF
touch -t 202601010000 "$other/twosess.output"
touch -t 202601020000 "$tasks/twosess.output"
expect 0 twosess "check-all: all steps passed"

expect 2 nosuchtask ""
expect 2 'bad/id' ""
expect 2 '' ""

[ "$fail" -eq 0 ] && echo "await-check: all cases passed"
exit "$fail"
