#!/bin/sh
# The frontend test step of check.sh, run from frontend/: `npm test`, and when
# it fails on test files, each failed file once more on its own, to tell a file
# that fails under machine load from one that is really broken. Under load a
# different few unrelated files fail on each run and pass alone.
#
# The step's exit code is always the first run's. A rerun only labels what
# failed; it never turns a failure into a pass. Every npm run is in the
# foreground with its output on stdout (check.sh's step log), so the log fills
# live and a signal stops npm with the process group.
#
# Failed files come from vitest's JSON report, never from the printed output:
# a test can print any line it likes. The last line of the output is the marker
# "== check-npm-test labels", and the labels follow it, each only when it
# applies:
#
#    failed under load, passed alone: a.test.tsx, b.test.tsx
#    failed alone too: c.test.tsx
#    not confirmed alone: d.test.tsx (ran 3 files), e.test.tsx (no report)
#    no rerun: <reason>
#
# "not confirmed" is a rerun whose report did not show exactly that file, and
# only that file. No rerun happens when the report is missing or unreadable,
# lists no failed test file, or lists more than $CHECK_NPM_TEST_RERUN_MAX
# (default 10) of them: that is not flakiness. The labels explain the listed
# files only; they say nothing about other errors in the log.
#
# `check-npm-test.sh --labels <log>` prints the label lines after the log's
# last marker, so a test's own printed lines never pass as labels.
#
# Exit 2: bad $CHECK_NPM_TEST_RERUN_MAX, or no temporary directory.
marker="== check-npm-test labels"
if [ "${1-}" = --labels ]; then
	awk -v m="$marker" '$0 == m { seen = 1; n = 0; next } seen { l[++n] = $0 } END { for (i = 1; i <= n; i++) print l[i] }' "$2"
	exit
fi
set -u
tmp=
trap '[ -z "$tmp" ] || rm -rf "$tmp"' EXIT
max=${CHECK_NPM_TEST_RERUN_MAX-10}
case $max in
'' | *[!0-9]* | ?????*) bad=1 ;;
*) bad=0 ;;
esac
if [ "$bad" -eq 1 ] || [ "$max" -lt 1 ]; then
	echo "check-npm-test: CHECK_NPM_TEST_RERUN_MAX must be a positive integer of at most 4 digits, got '$max'" >&2
	exit 2
fi
tmp=$(mktemp -d "${TMPDIR:-/tmp}/check-npm-test.XXXXXX") || {
	echo "check-npm-test: cannot create a temporary directory" >&2
	exit 2
}
# Colour codes would hide vitest's summary lines from check.sh's greps.
NO_COLOR=1
export NO_COLOR
unset FORCE_COLOR

# run_npm <report> [file]: `npm test` (on one file when given), output on
# stdout, JSON report written to <report>. Sets rc.
run_npm() {
	report=$1
	shift
	rm -f "$report"
	npm test -- "$@" --reporter=default --reporter=json "--outputFile.json=$report" </dev/null 2>&1
	rc=$?
}

# read_report <report> failed|alone [file]: prints one verdict line, then for
# "failed" the failed test files, relative to the current directory.
# "failed": OK, or ERR <reason>. "alone": ONE passed, ONE failed, or ERR <reason>.
read_report() {
	node -e '
const fs = require("fs");
const [file, cwd, mode, want] = process.argv.slice(1);
let r;
try {
	r = JSON.parse(fs.readFileSync(file, "utf8"));
} catch (e) {
	console.log("ERR no JSON report to read (" + (e.code || e.message) + ")");
	process.exit(0);
}
if (!r || !Array.isArray(r.testResults)) {
	console.log("ERR no JSON report to read (no testResults)");
	process.exit(0);
}
const rel = (n) => (String(n).startsWith(cwd + "/") ? String(n).slice(cwd.length + 1) : String(n));
if (mode === "alone") {
	const t = r.testResults;
	if (t.length !== 1) console.log("ERR ran " + t.length + " files");
	else if (rel(t[0].name) !== want) console.log("ERR ran " + rel(t[0].name).replace(/[\r\n]/g, " "));
	else console.log("ONE " + (t[0].status === "passed" ? "passed" : "failed"));
	process.exit(0);
}
const files = [];
for (const t of r.testResults) {
	if (t.status !== "failed") continue;
	const n = rel(t.name);
	if (!/\.(test|spec)\.[jt]sx?$/.test(n) || /[\r\n]/.test(n)) {
		console.log("ERR a failed file in the report is not a test file: " + n.replace(/[\r\n]/g, " "));
		process.exit(0);
	}
	files.push(n);
}
console.log("OK");
for (const f of [...new Set(files)].sort()) console.log(f);
' "$1" "$PWD" "$2" "${3-}"
}

lab=
label() {
	lab="$lab   $1
"
}
# finish: the marker, then the labels, then the first run's exit code.
finish() {
	echo "$marker"
	printf '%s' "$lab"
	exit "$first"
}

run_npm "$tmp/report.json"
first=$rc
[ "$first" -ne 0 ] || finish

echo
verdict=$(read_report "$tmp/report.json" failed)
head=$(printf '%s\n' "$verdict" | head -n 1)
case $head in
OK) ;;
*)
	label "no rerun: ${head#ERR }"
	finish
	;;
esac
printf '%s\n' "$verdict" | sed 1d >"$tmp/files"
count=$(wc -l <"$tmp/files" | tr -d ' ')
if [ "$count" -eq 0 ]; then
	label "no rerun: no failed test file in the report"
	finish
fi
if [ "$count" -gt "$max" ]; then
	label "no rerun: $count test files failed, more than $max"
	finish
fi

echo "== rerun: $count failed test file(s), one at a time"
passed=
failed=
unsure=
while IFS= read -r f; do
	printf '%s\n' "-- npm test -- $f"
	run_npm "$tmp/rerun.json" "$f"
	# vitest's file argument is a substring filter: only a report showing
	# exactly this file, and no other, says what happened to it.
	alone=$(read_report "$tmp/rerun.json" alone "$f")
	case $alone in
	"ONE passed")
		if [ "$rc" -eq 0 ]; then
			passed="$passed${passed:+, }$f"
		else
			failed="$failed${failed:+, }$f"
		fi
		;;
	"ONE failed") failed="$failed${failed:+, }$f" ;;
	*)
		reason=${alone#ERR }
		case $reason in
		"no JSON report"*) reason="no report" ;;
		esac
		unsure="$unsure${unsure:+, }$f ($reason)"
		;;
	esac
done <"$tmp/files"

echo
[ -z "$passed" ] || label "failed under load, passed alone: $passed"
[ -z "$failed" ] || label "failed alone too: $failed"
[ -z "$unsure" ] || label "not confirmed alone: $unsure"
finish
