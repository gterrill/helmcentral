#!/bin/sh
# The pre-PR checks in one command: `check.sh backend` or `check.sh frontend`
# (`make check-backend`, `make check-frontend`), or `check.sh all` (`make
# check`), which runs both, then the help-pages lint, the changelog check and
# the commit-message check.
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
	backend | frontend)
		# The nested run's own summary block, from its "== summary" line on.
		sed -n '/^== summary/,$p' "$log" | sed 's/^/   /'
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

case $target in
backend | frontend | all) ;;
*)
	echo "usage: check.sh backend|frontend|all" >&2
	exit 2
	;;
esac

# One check run at a time across every checkout on this machine: two at once
# overload it and fail tests at random. The outermost run re-runs itself under
# the lock with HELMCENTRAL_CHECK_LOCK_HELD set, so the runs it starts skip it.
. "$PWD/scripts/lib/machine-lock.sh"
machine_lock_reexec HELMCENTRAL_CHECK_LOCK_HELD "${HELMCENTRAL_CHECK_LOCKFILE:-/tmp/helmcentral-check.lock}" check "make check" sh "$PWD/scripts/check.sh" "$target"

mkdir -p "$logdir"

case $target in
backend)
	step go-vet backend go vet ./...
	step gofmt backend sh ../scripts/gofmt-check.sh
	# Shuffled, like main's "Backend tests (shuffled)" job, so a test that
	# leaks state into the next fails here rather than after the merge. The
	# log holds the seed as "-test.shuffle <seed>".
	step go-test backend go test -short -shuffle=on ./...
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
all)
	# Each area is its own run of this script, so its summary sits in the
	# step's log and the summary below names the log.
	step backend . sh scripts/check.sh backend
	step frontend . sh scripts/check.sh frontend
	step docs-lint-changed . make --no-print-directory docs-lint-changed
	step changelog . sh scripts/check-changelog.sh
	step commit-messages . sh scripts/check-commit-messages.sh
	step hook-tests . sh scripts/hooks/refuse-piped-checks-test.sh
	step hook-tests-stash . sh scripts/hooks/refuse-git-stash-test.sh
	step lock-tests . sh scripts/check-lock-test.sh
	;;
esac

echo
echo "== summary ($target)"
echo "load: $(uptime | sed 's/.*load averages*: *//')"
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
		# The nested summary printed above already lists their FAIL lines.
		case $name in
		backend | frontend)
			grep -q '^== summary' "$logdir/$target-$name.log" || failures "$name"
			;;
		*) failures "$name" ;;
		esac
	fi
done

if [ "$failed" -ne 0 ]; then
	echo "check-$target: FAILED"
	exit 1
fi
echo "check-$target: all steps passed"
