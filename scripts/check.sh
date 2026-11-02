#!/bin/sh
# The pre-PR checks in one command: `check.sh backend` or `check.sh frontend`
# (`make check-backend`, `make check-frontend`).
#
# Every step runs even when an earlier one fails, so one run reports
# everything; only a failed node_modules check stops the frontend run, since
# every later step would fail because of it. Each step's full output goes to
# its own log under backend/tmp/check/ (already gitignored, and per checkout),
# and the summary names the log. The summary is built from the steps' exit codes alone: a step
# that exited non-zero reads FAIL whatever its output looked like. Pass counts
# and failure lines are read from the logs for the reader's benefit and never
# decide pass or fail, so a filter that matches nothing cannot turn a failure
# into a pass. Exits 1 if any step failed.
set -u
cd "$(dirname "$0")/.." || exit 2

target=${1:-}
logdir=$PWD/backend/tmp/check
results=

# step <name> <directory> <command...>: runs the command in the directory,
# logs it to $logdir/<target>-<name>.log and records "<name>:<exit code>".
step() {
	name=$1
	dir=$2
	shift 2
	echo "== $name: $*"
	(cd "$dir" && "$@") >"$logdir/$target-$name.log" 2>&1
	code=$?
	results="$results $name:$code"
	if [ "$code" -eq 0 ]; then
		echo "   ok"
	else
		echo "   FAILED (exit $code)"
	fi
}

# counts <name>: one line of pass counts from the step's log, best effort.
counts() {
	log="$logdir/$target-$1.log"
	case $1 in
	go-test)
		echo "   $(grep -c '^ok' "$log") package(s) ok"
		;;
	npm-test)
		found=$(grep -E '^ +(Test Files|Tests) ' "$log" | sed 's/^ */   /')
		echo "${found:-   (no pass counts found in the log)}"
		;;
	esac
}

# failures <name>: the lines that say what failed, with the file heading ESLint
# prints above its errors, or the log's tail when none match, so a failing step
# always shows something.
failures() {
	log="$logdir/$target-$1.log"
	found=$(awk '
		/^\// { heading = $0 }
		/^$/ { heading = "" }
		/(^|[ \t])(FAIL|--- FAIL|panic:)|error TS|✖|[0-9]+:[0-9]+ +error|\.go:[0-9]+:/ {
			if (heading != "") print heading
			heading = ""
			print
		}
	' "$log" | head -40)
	if [ -n "$found" ]; then
		echo "$found" | sed 's/^/   /'
	else
		echo "   (no FAIL line matched; last lines of the log)"
		tail -15 "$log" | sed 's/^/   /'
	fi
}

mkdir -p "$logdir"

case $target in
backend)
	step go-vet backend go vet ./...
	step go-test backend go test -short ./...
	;;
frontend)
	step deps . make --no-print-directory check-frontend-deps
	if [ "$code" -eq 0 ]; then
		step npm-test frontend npm test
		step npm-lint frontend npm run lint
		step knip-deps frontend npm run knip:deps
		step npm-build frontend npm run build
	fi
	;;
*)
	echo "usage: check.sh backend|frontend" >&2
	exit 2
	;;
esac

echo
echo "== summary ($target)"
failed=0
for result in $results; do
	name=${result%:*}
	code=${result##*:}
	if [ "$code" -eq 0 ]; then
		echo "PASS  $name  ($logdir/$target-$name.log)"
		counts "$name"
	else
		failed=1
		echo "FAIL  $name  exit $code  ($logdir/$target-$name.log)"
		counts "$name"
		failures "$name"
	fi
done

if [ "$failed" -ne 0 ]; then
	echo "check-$target: FAILED"
	exit 1
fi
echo "check-$target: all steps passed"
