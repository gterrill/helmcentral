#!/bin/sh
# Cases for scripts/lib/machine-lock.sh and its use in check.sh:
# `sh scripts/check-lock-test.sh`. Uses lock files in a private directory, so it
# never touches a real check run's lock. Nothing here sleeps for a fixed time:
# each wait polls for a condition (a file, a line of output, a dead process)
# up to a 20 s deadline.
set -u
root=$(cd "$(dirname "$0")/.." && pwd)
lib=$root/scripts/lib/machine-lock.sh
work=$(mktemp -d)
pids=
fail=0

# Kills every process the test started (the lockf wrappers, the holders inside
# them, the waiters), waits for them to die, then removes the work directory.
cleanup() {
	[ -z "$pids" ] || kill -9 $pids 2>/dev/null
	for p in $pids; do
		poll "! kill -0 $p 2>/dev/null"
	done
	rm -rf "$work"
}
trap cleanup EXIT

# forget <pid>: drops a pid from $pids once the test has reaped it, so cleanup
# never signals a pid the system may since have reused.
forget() {
	_keep=
	for _p in $pids; do
		[ "$_p" = "$1" ] || _keep="$_keep $_p"
	done
	pids=$_keep
}

# reap <pid>: kills a child of this shell, waits for it, and forgets it.
reap() {
	kill -9 "$1" 2>/dev/null
	wait "$1" 2>/dev/null
	forget "$1"
}

# poll <shell condition>: true once the condition holds; false after 20 s.
poll() {
	_i=0
	while ! eval "$1"; do
		_i=$((_i + 1))
		[ "$_i" -lt 200 ] || return 1
		sleep 0.1
	done
}

bad() {
	echo "FAIL: $1"
	fail=1
}

# hold <name>: holds $work/<name>.lock with a single process, the lockf child
# `sleep`, whose real pid lands in $work/<name>.pid. Sets lf and holder.
hold() {
	lf=$work/$1.lock
	lockf -k "$lf" sh -c 'echo $$ >"$1"; exec sleep 600' sh "$work/$1.pid" &
	hpid=$!
	pids="$pids $!"
	poll "[ -s '$work/$1.pid' ]" || {
		bad "holder $1 never took the lock"
		return 1
	}
	holder=$(cat "$work/$1.pid")
	pids="$pids $holder"
}

# waiter <name> <lockfile>: starts a second caller through the helper.
waiter() {
	env -u WAIT_HELD sh -c ". '$lib'; machine_lock_reexec WAIT_HELD '$2' check 'make check' echo ran" >"$work/$1.out" 2>&1 &
	pids="$pids $!"
	waiter_pid=$!
}

# 1. A second caller prints the waiting line, then runs once the first releases.
hold a || exit 1
waiter a "$lf"
poll "grep -q '^check: waiting on another make check\$' '$work/a.out'" || bad "second caller printed no waiting line"
grep -q '^ran$' "$work/a.out" && bad "second caller ran while the first held the lock"
kill "$holder"
poll "grep -q '^ran\$' '$work/a.out'" || bad "second caller did not run after the first released"
[ "$(grep -c waiting "$work/a.out")" -eq 1 ] || bad "waiting line not printed exactly once"
grep -q 'already locked' "$work/a.out" && bad "lockf's own busy message leaked into the output"
reap "$waiter_pid"
reap "$hpid"
forget "$holder"

# 1b. With the lock free the command just runs, with no waiting line.
env -u WAIT_HELD sh -c ". '$lib'; machine_lock_reexec WAIT_HELD '$work/free.lock' check 'make check' echo ran" >"$work/free.out" 2>&1
[ "$(cat "$work/free.out")" = ran ] || bad "free lock: output was '$(cat "$work/free.out")', want 'ran'"

# 2. A holder killed with KILL frees the lock and a waiter proceeds.
hold b || exit 1
waiter b "$lf"
poll "grep -q '^check: waiting on another' '$work/b.out'" || bad "waiter b printed no waiting line"
kill -9 "$holder"
poll "grep -q '^ran\$' '$work/b.out'" || bad "waiter did not proceed after its holder was killed"
reap "$waiter_pid"
reap "$hpid"
forget "$holder"

# 3. The first attempt is a non-blocking try that leaves the lock file in place.
hold c || exit 1
lockf -k -t 0 "$lf" true 2>/dev/null
code=$?
[ "$code" -eq 75 ] || bad "try while held exited $code, want 75"
[ -f "$lf" ] || bad "lock file was removed while held"
kill -9 "$holder"
poll "lockf -k -t 0 '$lf' true" || bad "lock still held after its holder was killed"
[ -f "$lf" ] || bad "lock file was removed after release"
reap "$hpid"
forget "$holder"

# 3b. A caller running under `set -eu` still waits, rather than dying on lockf's
# busy exit status.
hold f || exit 1
env -u WAIT_HELD sh -c "set -eu; . '$lib'; machine_lock_reexec WAIT_HELD '$lf' check 'make check' echo ran" >"$work/f.out" 2>&1 &
waiter_pid=$!
pids="$pids $waiter_pid"
poll "grep -q '^check: waiting on another make check\$' '$work/f.out'" || bad "set -e caller printed no waiting line"
grep -q '^ran$' "$work/f.out" && bad "set -e caller ran while the lock was held"
kill -9 "$holder"
poll "grep -q '^ran\$' '$work/f.out'" || bad "set -e caller did not run after release"
reap "$waiter_pid"
reap "$hpid"
forget "$holder"

# Cases 4 and 5 run a copy of check.sh in a bare tree, so no real check runs.
fake=$work/fake
mkdir -p "$fake/scripts"
cp "$root/scripts/check.sh" "$fake/scripts/check.sh"
cp -R "$root/scripts/lib" "$fake/scripts/lib"
HELMCENTRAL_CHECK_LOCKFILE=$work/d.lock
export HELMCENTRAL_CHECK_LOCKFILE
hold d || exit 1

# 4. An invalid target exits 2 at once, even while the lock is held.
env -u HELMCENTRAL_CHECK_LOCK_HELD sh "$fake/scripts/check.sh" bogus >"$work/bogus.out" 2>&1 &
bogus=$!
pids="$pids $bogus"
poll "grep -q '^usage:' '$work/bogus.out'" || bad "invalid target printed no usage line"
wait "$bogus"
code=$?
forget "$bogus"
[ "$code" -eq 2 ] || bad "invalid target exited $code, want 2"
grep -q waiting "$work/bogus.out" && bad "invalid target waited for the lock"

# 5. With the held variable set, check.sh does not take the lock: it runs to
# its summary while the lock is held elsewhere.
HELMCENTRAL_CHECK_LOCK_HELD=1 sh "$fake/scripts/check.sh" backend >"$work/inner.out" 2>&1 &
inner=$!
pids="$pids $inner"
poll "grep -q '^load: ' '$work/inner.out'" || bad "nested run did not reach its summary (or has no load line)"
grep -q waiting "$work/inner.out" && bad "nested run waited for the lock"
wait "$inner"
forget "$inner"

# 6. Without lockf the helper exits 2 with a clear message and does not run the command.
bin=$work/bin
mkdir "$bin"
ln -s /bin/sh "$bin/sh"
env -u WAIT_HELD PATH="$bin" /bin/sh -c ". '$lib'; machine_lock_reexec WAIT_HELD '$work/e.lock' check 'make check' echo ran" >"$work/nolockf.out" 2>"$work/nolockf.err"
code=$?
[ "$code" -eq 2 ] || bad "missing lockf exited $code, want 2"
[ "$(cat "$work/nolockf.err")" = "check: lockf not found; the machine-wide lock needs macOS lockf(1)" ] || bad "missing lockf message was '$(cat "$work/nolockf.err")'"
[ -s "$work/nolockf.out" ] && bad "command ran without lockf"

[ "$fail" -eq 0 ] && echo "check-lock: all cases passed"
exit "$fail"
