#!/bin/sh
# Cases for scripts/lib/stay-awake.sh and its use in check.sh and
# security-review.sh: `sh scripts/stay-awake-test.sh`. Nothing here sleeps: a
# fake caffeinate on PATH records its arguments and exits.
set -u
# Under `make check` these are exported; the cases below set them where they
# want a nested or re-exec'd run and need them unset where they want the
# outermost one.
unset HELMCENTRAL_CHECK_TREE_OWNER \
	HELMCENTRAL_CHECK_SLEEP_MARK HELMCENTRAL_CHECK_ON_BATTERY \
	HELMCENTRAL_CHECK_LOCK_HELD outermost
root=$(cd "$(dirname "$0")/.." && pwd)
lib=$root/scripts/lib/stay-awake.sh
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
fail=0

bad() {
	echo "FAIL: $1"
	fail=1
}

bin=$work/bin
mkdir "$bin"
cat >"$bin/caffeinate" <<EOF
#!/bin/sh
echo "\$*" >>"$work/caffeinate.args"
EOF
chmod +x "$bin/caffeinate"
# A go that does nothing, so check.sh in a bare tree runs no real toolchain.
cat >"$bin/go" <<'EOF'
#!/bin/sh
exit 0
EOF
chmod +x "$bin/go"
# Every other tool the cases need, with no real caffeinate among them.
nobin=$work/nobin
mkdir "$nobin"
ln -s /bin/sh "$nobin/sh"
ln -s "$(command -v dirname)" "$nobin/dirname"
ln -s "$bin/go" "$nobin/go"

# wait_args: the fake caffeinate runs in the background, so wait for its line.
wait_args() {
	n=0
	while [ ! -s "$work/caffeinate.args" ] && [ "$n" -lt 100 ]; do
		n=$((n + 1))
		sleep 0.1
	done
}

# 1. stay_awake starts `caffeinate -i -w <the script's pid>` and returns, so the
# script goes on in its own process.
: >"$work/caffeinate.args"
PATH="$bin:$PATH" sh -c ". '$lib'; stay_awake; echo \$\$ >'$work/pid'; echo ran" >"$work/a.out" 2>&1
wait_args
[ "$(cat "$work/a.out")" = ran ] || bad "stay_awake: output was '$(cat "$work/a.out")', want 'ran'"
[ "$(cat "$work/caffeinate.args")" = "-i -w $(cat "$work/pid")" ] || bad "stay_awake: caffeinate got '$(cat "$work/caffeinate.args")', want '-i -w $(cat "$work/pid")'"

# 2. Without caffeinate the helper exits 2 with a clear message and runs nothing.
PATH="$nobin" /bin/sh -c ". '$lib'; stay_awake; echo ran" >"$work/d.out" 2>"$work/d.err"
code=$?
[ "$code" -eq 2 ] || bad "missing caffeinate exited $code, want 2"
grep -q '^stay-awake: caffeinate not found' "$work/d.err" || bad "missing caffeinate message was '$(cat "$work/d.err")'"
[ -s "$work/d.out" ] && bad "command ran without caffeinate"

# Cases 3 and 4 run copies of the scripts in a bare tree, so no real check or
# review runs.
fake=$work/fake
mkdir -p "$fake/scripts" "$fake/backend"
cp "$root/scripts/check.sh" "$fake/scripts/check.sh"
cp "$root/scripts/security-review.sh" "$fake/scripts/security-review.sh"
cp -R "$root/scripts/lib" "$fake/scripts/lib"
printf '#!/bin/sh\nexit 0\n' >"$fake/scripts/gofmt-check.sh"
printf '#!/bin/sh\nexit 0\n' >"$fake/scripts/tree-hash.sh"

# 3. check.sh starts the watcher once, before the lock, naming its own pid.
: >"$work/caffeinate.args"
HELMCENTRAL_CHECK_LOCK_HELD=1 HELMCENTRAL_CHECK_TREE_OWNER=1 PATH="$bin:$PATH" \
	sh "$fake/scripts/check.sh" backend >"$work/check.out" 2>&1
wait_args
[ "$(wc -l <"$work/caffeinate.args" | tr -d ' ')" -eq 1 ] || bad "check.sh called caffeinate $(wc -l <"$work/caffeinate.args" | tr -d ' ') times, want 1"
grep -Eq '^-i -w [0-9]+$' "$work/caffeinate.args" || bad "check.sh caffeinate args were '$(cat "$work/caffeinate.args")'"
HELMCENTRAL_CHECK_LOCK_HELD=1 HELMCENTRAL_CHECK_TREE_OWNER=1 PATH="$nobin" \
	/bin/sh "$fake/scripts/check.sh" backend >"$work/check2.out" 2>"$work/check2.err"
code=$?
[ "$code" -eq 2 ] || bad "check.sh without caffeinate exited $code, want 2"
grep -q 'caffeinate not found' "$work/check2.err" || bad "check.sh without caffeinate printed '$(cat "$work/check2.err")'"

# 4. security-review.sh starts it once (no arguments, so it stops at its usage
# line afterwards).
: >"$work/caffeinate.args"
PATH="$bin:$PATH" sh "$fake/scripts/security-review.sh" >"$work/sr.out" 2>&1
wait_args
grep -Eq '^-i -w [0-9]+$' "$work/caffeinate.args" || bad "security-review.sh caffeinate args were '$(cat "$work/caffeinate.args")'"
[ "$(wc -l <"$work/caffeinate.args" | tr -d ' ')" -eq 1 ] || bad "security-review.sh called caffeinate more than once"

# Cases 5 on use a fake pmset and perl on PATH. The fake perl prints line N of
# $work/perl.seq on its Nth call (the last line once the file runs out), each
# line "<monotonic> <awake>" in seconds, so a case scripts the clocks: asleep =
# the monotonic step minus the awake step. $work/perl.fail makes it exit 1.
cat >"$bin/pmset" <<EOF
#!/bin/sh
cat "$work/pmset.out"
EOF
cat >"$bin/perl" <<EOF
#!/bin/sh
[ -f "$work/perl.fail" ] && exit 1
n=\$(cat "$work/perl.n" 2>/dev/null || echo 0)
n=\$((n + 1))
echo \$n >"$work/perl.n"
total=\$(wc -l <"$work/perl.seq" | tr -d ' ')
[ "\$n" -gt "\$total" ] && n=\$total
sed -n "\${n}p" "$work/perl.seq"
EOF
chmod +x "$bin/pmset" "$bin/perl"
# clocks <line>...: the fake perl's answers, from the first call on.
clocks() {
	: >"$work/perl.seq"
	for l in "$@"; do echo "$l" >>"$work/perl.seq"; done
	rm -f "$work/perl.n"
}
battery='Now drawing from '"'"'Battery Power'"'"
ac='Now drawing from '"'"'AC Power'"'"
battery_line='check: on battery: closing the lid will sleep the Mac and stall this run'
rm -f "$work/perl.fail"

# runcheck <out> <err> [VAR=value...]: check.sh backend in the bare tree, on PATH
# with the fakes and the given environment, under a private lock file. Pass
# HELMCENTRAL_CHECK_LOCK_HELD=1 for a re-exec'd run and HELMCENTRAL_CHECK_TREE_OWNER=1
# for a nested one; with neither it is the outermost run.
runcheck() {
	out=$1
	err=$2
	shift 2
	env HELMCENTRAL_CHECK_LOCKFILE="$work/check.lock" "$@" PATH="$bin:$PATH" \
		sh "$fake/scripts/check.sh" backend >"$out" 2>"$err"
}

# 5. on_battery is true on battery power and false on AC.
echo "$battery" >"$work/pmset.out"
PATH="$bin:$PATH" sh -c ". '$lib'; on_battery" || bad "on_battery false on battery power"
echo "$ac" >"$work/pmset.out"
PATH="$bin:$PATH" sh -c ". '$lib'; on_battery" && bad "on_battery true on AC power"

# 6. The outermost run warns on stderr once and adds "power: on battery" to its
# summary, only on battery, and still passes. A nested run does neither, even
# with the battery variable inherited.
clocks "1000 800"
echo "$battery" >"$work/pmset.out"
runcheck "$work/b.out" "$work/b.err"
[ "$?" -eq 0 ] || bad "check.sh on battery did not pass"
[ "$(grep -cxF "$battery_line" "$work/b.err")" -eq 1 ] || bad "battery warning not printed once, stderr was '$(cat "$work/b.err")'"
grep -qxF 'power: on battery' "$work/b.out" || bad "summary lacks 'power: on battery': '$(cat "$work/b.out")'"
echo "$ac" >"$work/pmset.out"
runcheck "$work/b.out" "$work/b.err"
grep -q 'on battery' "$work/b.err" && bad "battery warning printed on AC"
grep -q '^power:' "$work/b.out" && bad "summary printed a power line on AC"
echo "$battery" >"$work/pmset.out"
runcheck "$work/b.out" "$work/b.err" HELMCENTRAL_CHECK_TREE_OWNER=1 HELMCENTRAL_CHECK_ON_BATTERY=1
grep -q 'on battery' "$work/b.err" && bad "nested run printed the battery warning"
grep -q '^power:' "$work/b.out" && bad "nested run printed a power line"

# 7. sleep_report prints nothing when the Mac slept under 60 s, and the line at
# exactly 60 s.
echo "$ac" >"$work/pmset.out"
clocks "1059 800"
PATH="$bin:$PATH" sh -c ". '$lib'; sleep_report '1000 800'" >"$work/r.out" 2>&1
[ -s "$work/r.out" ] && bad "59 s asleep printed '$(cat "$work/r.out")'"
clocks "1060 800"
PATH="$bin:$PATH" sh -c ". '$lib'; sleep_report '1000 800'" >"$work/r.out" 2>&1
[ "$(cat "$work/r.out")" = "slept: the Mac slept 1 min of this run's 1 min" ] || bad "60 s asleep printed '$(cat "$work/r.out")'"
# An awake run prints nothing in the summary either.
clocks "1000 800" "2800 2600"
runcheck "$work/b.out" "$work/b.err"
grep -q '^slept:' "$work/b.out" && bad "summary printed a slept line with no sleep: '$(cat "$work/b.out")'"

# 8. The line gives the whole sleep and the whole run: 1800 s of run, 600 s of
# it awake, so 20 min asleep of 30. sleep_mark prints the clock pair. In the
# summary the line follows load, and the run still passes.
clocks "1000 800"
PATH="$bin:$PATH" sh -c ". '$lib'; sleep_mark" >"$work/r.out" 2>&1
[ "$(cat "$work/r.out")" = "1000 800" ] || bad "sleep_mark printed '$(cat "$work/r.out")', want '1000 800'"
clocks "2800 1400"
PATH="$bin:$PATH" sh -c ". '$lib'; sleep_report '1000 800'" >"$work/r.out" 2>&1
[ "$(cat "$work/r.out")" = "slept: the Mac slept 20 min of this run's 30 min" ] || bad "slept line was '$(cat "$work/r.out")'"
clocks "1000 800" "2800 1400"
runcheck "$work/b.out" "$work/b.err"
[ "$?" -eq 0 ] || bad "check.sh failed because the Mac slept"
grep -A1 '^load:' "$work/b.out" | grep -qxF "slept: the Mac slept 20 min of this run's 30 min" || bad "slept line not right after load: '$(cat "$work/b.out")'"

# 9. A sleep while the run queued for the lock counts: the mark is taken before
# the lock, so a fake lockf (which only records what it was run with) sees the
# mark and the battery marker already exported, and the warning is already out.
cat >"$bin/lockf" <<EOF
#!/bin/sh
echo "\$HELMCENTRAL_CHECK_SLEEP_MARK|\$HELMCENTRAL_CHECK_ON_BATTERY" >"$work/lockf.env"
exit 0
EOF
chmod +x "$bin/lockf"
echo "$battery" >"$work/pmset.out"
clocks "1000 800"
env PATH="$bin:$PATH" sh "$fake/scripts/check.sh" backend >"$work/q.out" 2>"$work/q.err"
[ "$(cat "$work/lockf.env" 2>/dev/null)" = "1000 800|1" ] || bad "mark not exported before the lock: '$(cat "$work/lockf.env" 2>/dev/null)'"
grep -qxF "$battery_line" "$work/q.err" || bad "battery warning not out before the lock: '$(cat "$work/q.err")'"
rm -f "$bin/lockf"
# The re-exec'd run (lock now held, marker set) reports from that mark, not a new
# one, and does not warn again; the pmset it sees says AC.
echo "$ac" >"$work/pmset.out"
clocks "2800 1400"
runcheck "$work/b.out" "$work/b.err" HELMCENTRAL_CHECK_LOCK_HELD=1 'HELMCENTRAL_CHECK_SLEEP_MARK=1000 800' HELMCENTRAL_CHECK_ON_BATTERY=1
[ "$?" -eq 0 ] || bad "re-exec'd run failed"
grep -qxF "slept: the Mac slept 20 min of this run's 30 min" "$work/b.out" || bad "queued sleep not counted: '$(cat "$work/b.out")'"
grep -qxF 'power: on battery' "$work/b.out" || bad "re-exec'd run lost the battery line: '$(cat "$work/b.out")'"
grep -q 'on battery' "$work/b.err" && bad "re-exec'd run warned a second time"

# 10. A nested run never prints the line, and neither does an inherited
# "outermost" variable make it.
runcheck "$work/b.out" "$work/b.err" HELMCENTRAL_CHECK_TREE_OWNER=1 'HELMCENTRAL_CHECK_SLEEP_MARK=1000 800' outermost=1
grep -q '^slept:' "$work/b.out" && bad "nested run with inherited outermost=1 printed a slept line"
runcheck "$work/b.out" "$work/b.err" HELMCENTRAL_CHECK_TREE_OWNER=1 'HELMCENTRAL_CHECK_SLEEP_MARK=1000 800'
grep -q '^slept:' "$work/b.out" && bad "nested run printed a slept line"

# 10b. Stale values in the shell never disable or poison the outermost run's
# mark: leftover mark and battery values are overwritten with fresh ones.
echo "$ac" >"$work/pmset.out"
clocks "1000 800" "2800 1400"
runcheck "$work/b.out" "$work/b.err" 'HELMCENTRAL_CHECK_SLEEP_MARK=5 5' HELMCENTRAL_CHECK_ON_BATTERY=1
[ "$?" -eq 0 ] || bad "outermost run with stale values failed"
grep -qxF "slept: the Mac slept 20 min of this run's 30 min" "$work/b.out" || bad "stale mark was not overwritten: '$(cat "$work/b.out")'"
grep -q '^power:' "$work/b.out" && bad "stale battery value was not overwritten"

# 11. When perl fails, or the mark is empty or malformed, the report says so and
# does not fail.
cannot="slept: could not read the Mac's clocks"
clocks "2800 1400"
PATH="$bin:$PATH" sh -c ". '$lib'; sleep_report ''" >"$work/r.out" 2>&1
code=$?
[ "$code" -eq 0 ] || bad "sleep_report with an empty mark exited $code, want 0"
[ "$(cat "$work/r.out")" = "$cannot" ] || bad "empty mark line was '$(cat "$work/r.out")'"
PATH="$bin:$PATH" sh -c ". '$lib'; sleep_report 'abc 12'" >"$work/r.out" 2>&1
[ "$(cat "$work/r.out")" = "$cannot" ] || bad "malformed mark line was '$(cat "$work/r.out")'"
touch "$work/perl.fail"
PATH="$bin:$PATH" sh -c ". '$lib'; sleep_report '1000 800'" >"$work/r.out" 2>&1
code=$?
[ "$code" -eq 0 ] || bad "sleep_report exited $code when perl failed, want 0"
[ "$(cat "$work/r.out")" = "$cannot" ] || bad "perl failure line was '$(cat "$work/r.out")'"
PATH="$bin:$PATH" sh -c ". '$lib'; sleep_mark" >"$work/r.out" 2>&1 && bad "sleep_mark succeeded with perl failing"
# In a whole outermost run: the mark could not be taken, the summary says so and
# the run passes.
runcheck "$work/b.out" "$work/b.err"
[ "$?" -eq 0 ] || bad "check.sh failed because perl failed"
grep -qxF "$cannot" "$work/b.out" || bad "summary lacks the could-not-read line: '$(cat "$work/b.out")'"
rm -f "$work/perl.fail"

[ "$fail" -eq 0 ] && echo "stay-awake: all cases passed"
exit "$fail"
