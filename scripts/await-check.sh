#!/bin/sh
# Wait for a backgrounded check run by its task ID.
#
#   sh scripts/await-check.sh <task-id>
#
# The Bash tool moves any command to the background after 180 s, whatever its
# timeout, and prints "Command was moved to the background (ID: <id>)". `make
# check` often runs longer. This waits for that task by ID, so nobody has to
# poll or type the output path.
#
# A run is finished when the output has an unindented line
# `check-(all|backend|frontend): all steps passed` or `... FAILED`. Nested runs
# print theirs indented by three spaces, so the unindented line is the
# outermost run's. That line alone decides the exit code. The output is then
# printed from the last unindented `== summary (` line to the end, or the last
# 20 lines when there is none.
#
# check.sh can also exit before any summary (a load sample failure, a lock
# error). The outermost make then prints `make: *** ... Error N`; this prints
# the last 20 lines and exits 1. Nested makes say `make[N]:`, and check.sh
# sends their output to the step logs, so those lines are not read as a stop.
#
# Exit 0: the run passed. 1: a step failed, or the run stopped early. 2: bad
# arguments or unknown task. 3: still running after the wait (150 s, well under
# the tool's 180 s); run this again.
#
# AWAIT_CHECK_ROOT (default /private/tmp) and AWAIT_CHECK_LIMIT (default 150)
# are for scripts/await-check-test.sh.
set -eu

usage() {
  echo "usage: sh scripts/await-check.sh <task-id>" >&2
  exit 2
}

[ $# -eq 1 ] || usage
id=$1
case $id in
'' | *[!a-z0-9]*) usage ;;
esac

root=${AWAIT_CHECK_ROOT:-/private/tmp}
limit=${AWAIT_CHECK_LIMIT:-150}

# Newest match wins when more than one session holds the same ID.
file=
for f in "$root"/claude-*/*/*/tasks/"$id".output; do
  [ -f "$f" ] || continue
  if [ -z "$file" ] || [ "$f" -nt "$file" ]; then
    file=$f
  fi
done
if [ -z "$file" ]; then
  echo "await-check: no output file for task $id under $root/claude-*/*/*/tasks/" >&2
  exit 2
fi

waited=0
while :; do
  final=$(grep -E '^check-(all|backend|frontend): (all steps passed|FAILED)$' "$file" | tail -n 1 || true)
  if [ -n "$final" ]; then
    start=$(grep -n '^== summary (' "$file" | tail -n 1 | cut -d: -f1 || true)
    if [ -n "$start" ]; then
      tail -n +"$start" "$file"
    else
      tail -n 20 "$file"
    fi
    case $final in
    *'all steps passed') exit 0 ;;
    *) exit 1 ;;
    esac
  fi
  if grep -q '^make: \*\*\* ' "$file"; then
    tail -n 20 "$file"
    echo "await-check: $id stopped before its summary"
    exit 1
  fi
  if [ "$waited" -ge "$limit" ]; then
    echo "await-check: $id still running; run this again"
    exit 3
  fi
  sleep 5
  waited=$((waited + 5))
done
