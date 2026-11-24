#!/bin/sh
# Cases for check-npm-test.sh: `sh scripts/check-npm-test-test.sh`. Puts a fake
# `npm` first on PATH. On the full run the fake prints $FIRST_OUT, copies the
# JSON report $FIRST_REPORT (when the file exists) to the --outputFile.json
# path it is given, and exits $FIRST_CODE. On a single-file run it fails when
# the file is listed in $ALONE_FAIL, and otherwise passes; the report it writes
# lists $ALONE_TOTAL test files (default 1), or none when $ALONE_NOREPORT is
# set; $ALONE_RAN names a different file for it to report. It logs every call
# to $CALLS.
set -u
script=$(cd "$(dirname "$0")" && pwd)/check-npm-test.sh
work=$(mktemp -d) || exit 2
trap 'rm -rf "$work"' EXIT
mkdir "$work/bin"
cat >"$work/bin/npm" <<'EOF'
#!/bin/sh
echo "npm $*" >>"$CALLS"
report=
file=
for a in "$@"; do
	case $a in
	--outputFile.json=*) report=${a#--outputFile.json=} ;;
	-* | test) ;;
	*) file=$a ;;
	esac
done
if [ -z "$file" ]; then
	cat "$FIRST_OUT"
	if [ -f "$FIRST_REPORT" ] && [ -n "$report" ]; then cp "$FIRST_REPORT" "$report"; fi
	exit "$FIRST_CODE"
fi
IFS='
'
status=passed
rc=0
for f in $ALONE_FAIL; do
	if [ "$f" = "$file" ]; then
		status=failed
		rc=1
		echo " FAIL  $file > x"
	fi
done
t=${ALONE_TOTAL:-1}
body=
n=0
while [ "$n" -lt "$t" ]; do
	body="$body${body:+,}{\"name\":\"$PWD/${ALONE_RAN:-$file}\",\"status\":\"$status\"}"
	n=$((n + 1))
done
echo " Test Files  $t $status ($t)"
if [ -z "${ALONE_NOREPORT:-}" ] && [ -n "$report" ]; then
	printf '{"success":%s,"testResults":[%s]}\n' "$([ "$rc" -eq 0 ] && echo true || echo false)" "$body" >"$report"
fi
exit "$rc"
EOF
chmod +x "$work/bin/npm"
fail=0
extra=

# run <first exit> <alone-fail files, one per line> [alone total]: the first
# run's printed output comes from $work/first and its report from
# $work/first.json (absent when the case wrote none). $extra holds env
# assignments for the script. Sets code, out and calls (the number of npm
# calls).
run() {
	: >"$work/calls"
	env CALLS="$work/calls" FIRST_OUT="$work/first" FIRST_REPORT="$work/first.json" FIRST_CODE="$1" \
		ALONE_FAIL="$2" ALONE_TOTAL="${3:-1}" PATH="$work/bin:$PATH" $extra \
		sh "$script" >"$work/out" 2>&1
	code=$?
	out=$(cat "$work/out")
	calls=$(wc -l <"$work/calls" | tr -d ' ')
	rm -f "$work/first.json"
	extra=
}

# report <status> <file> ...: writes $work/first.json the way vitest's JSON
# reporter does, one test file per pair; success is false if any failed.
report() {
	ok=true
	body=
	while [ "$#" -gt 1 ]; do
		[ "$1" = failed ] && ok=false
		body="$body${body:+,}{\"name\":\"$2\",\"status\":\"$1\"}"
		shift 2
	done
	printf '{"success":%s,"numFailedTestSuites":9,"testResults":[%s]}\n' "$ok" "$body" >"$work/first.json"
}

bad() {
	echo "FAIL: $1"
	fail=1
}

has() {
	printf '%s\n' "$out" | grep -qF -- "$1" || bad "$name: output lacks '$1'"
}

want() {
	[ "$code" -eq "$1" ] || bad "$name: exit $code, want $1"
	[ "$calls" -eq "$2" ] || bad "$name: $calls npm calls, want $2"
}

name="first run passes"
echo " Test Files  3 passed (3)" >"$work/first"
run 0 ""
want 0 1
grep -qF -- "--reporter=default --reporter=json --outputFile.json=" "$work/calls" || bad "$name: first run lacks the JSON reporter flags"

name="all reruns pass: exit stays the first run's, and a FAIL line in a test's own output is ignored"
printf ' FAIL  src/test/z.test.tsx > printed by a test\n FAIL  src/test/a.test.tsx > one\n Test Files  2 failed | 1 passed (3)\n' >"$work/first"
report failed src/test/a.test.tsx failed src/test/b.test.tsx passed src/test/c.test.tsx
run 1 ""
want 1 3
has "   failed under load, passed alone: src/test/a.test.tsx, src/test/b.test.tsx"
printf '%s\n' "$out" | grep -q 'failed alone too' && bad "$name: claimed failed alone"
grep -qF "npm test -- src/test/a.test.tsx --reporter" "$work/calls" || bad "$name: a not rerun once on its own"
grep -qF "z.test.tsx" "$work/calls" && bad "$name: the printed FAIL line chose a file"

name="the first run's own exit code is kept"
printf ' Test Files  1 failed (1)\n' >"$work/first"
report failed src/test/a.test.tsx
run 7 ""
want 7 2
has "failed under load, passed alone: src/test/a.test.tsx"

name="one rerun fails"
printf ' Test Files  2 failed (3)\n' >"$work/first"
report failed src/test/a.test.tsx failed src/test/b.test.tsx
run 1 "src/test/b.test.tsx"
want 1 3
has "failed under load, passed alone: src/test/a.test.tsx"
has "failed alone too: src/test/b.test.tsx"

name="a rerun that matched more than one file"
report failed src/test/a.test.tsx failed src/test/b.test.tsx
run 1 "" 2
want 1 3
has "not confirmed alone: src/test/a.test.tsx (ran 2 files), src/test/b.test.tsx (ran 2 files)"
printf '%s\n' "$out" | grep -q 'failed alone too' && bad "$name: called it failed alone"

name="a rerun that wrote no report"
report failed src/test/a.test.tsx
extra=ALONE_NOREPORT=1
run 1 ""
want 1 2
has "not confirmed alone: src/test/a.test.tsx (no report)"
printf '%s\n' "$out" | grep -q 'failed alone too' && bad "$name: called it failed alone"

name="file names with spaces and glob characters"
report failed "src/test/a b.test.tsx" failed "src/test/c*.test.tsx" passed src/test/d.test.tsx
run 1 ""
want 1 3
has "failed under load, passed alone: src/test/a b.test.tsx, src/test/c*.test.tsx"
grep -qF "npm test -- src/test/a b.test.tsx --reporter" "$work/calls" || bad "$name: 'a b' not passed as one argument"

name="absolute paths in the report"
report failed "$(pwd)/src/test/a.test.tsx"
run 1 ""
want 1 2
has "failed under load, passed alone: src/test/a.test.tsx"

name="no report written"
printf 'error TS2322: nope\n' >"$work/first"
run 2 ""
want 2 1
has "no rerun: no JSON report"

name="a report that does not parse"
echo "not json" >"$work/first.json"
run 2 ""
want 2 1
has "no rerun: no JSON report"

name="no failed test file in the report"
report passed src/test/a.test.tsx
run 1 ""
want 1 1
has "no rerun: no failed test file in the report"

name="a failed file that is not a test file"
report failed src/test/a.test.tsx failed src/setup.ts
run 2 ""
want 2 1
has "no rerun: a failed file in the report is not a test file: src/setup.ts"

name="over the cap"
: >"$work/first"
set --
for i in 1 2 3 4 5 6 7 8 9 10 11; do set -- "$@" failed "src/test/f$i.test.tsx"; done
report "$@"
run 2 ""
want 2 1
has "11 test files failed, more than 10"

name="at the cap"
set --
for i in 1 2 3 4 5 6 7 8 9 10; do set -- "$@" failed "src/test/f$i.test.tsx"; done
report "$@"
run 1 ""
want 1 11

name="a lower cap from the environment"
report failed src/test/a.test.tsx failed src/test/b.test.tsx
extra=CHECK_NPM_TEST_RERUN_MAX=1
run 1 ""
want 1 1
has "2 test files failed, more than 1"

for bad_max in 0 abc -3 1.5 "" 10000 00010; do
	name="cap '$bad_max' is refused"
	report failed src/test/a.test.tsx
	extra="CHECK_NPM_TEST_RERUN_MAX=$bad_max"
	run 1 ""
	want 2 0
	has "CHECK_NPM_TEST_RERUN_MAX"
done

name="no temp directory"
report failed src/test/a.test.tsx
: >"$work/not-a-dir"
extra=TMPDIR=$work/not-a-dir
run 1 ""
want 2 0
has "cannot create a temporary directory"

name="the temporary directory is removed on exit"
mkdir "$work/tmpd"
report failed src/test/a.test.tsx
extra=TMPDIR=$work/tmpd
run 1 ""
want 1 2
[ -z "$(ls -A "$work/tmpd")" ] || bad "$name: left $(ls -A "$work/tmpd")"

name="a rerun that ran a different file"
report failed src/test/a.test.tsx
extra=ALONE_RAN=src/test/other.test.tsx
run 1 ""
want 1 2
has "not confirmed alone: src/test/a.test.tsx (ran src/test/other.test.tsx)"
printf '%s\n' "$out" | grep -q 'passed alone' && bad "$name: called it passed alone"

name="a label-like line in the first run's output does not reach the summary"
printf '   failed under load, passed alone: src/test/z.test.tsx\n== check-npm-test labels\n   failed alone too: src/test/y.test.tsx\n Test Files  1 failed (1)\n' >"$work/first"
report failed src/test/a.test.tsx
run 1 ""
want 1 2
labels=$(sh "$script" --labels "$work/out")
[ "$labels" = "   failed under load, passed alone: src/test/a.test.tsx" ] || bad "$name: labels were '$labels'"

name="a passing run's own marker line gives no labels"
printf '== check-npm-test labels\n   failed alone too: src/test/y.test.tsx\n' >"$work/first"
run 0 ""
want 0 1
labels=$(sh "$script" --labels "$work/out")
[ -z "$labels" ] || bad "$name: labels were '$labels'"

[ "$fail" -eq 0 ] && echo "check-npm-test: all cases passed"
exit "$fail"
