#!/bin/sh
# Machine-wide lock, sourced by scripts that must not run twice at once across
# every checkout on this machine (`make check`, `make worktree`). It uses
# lockf(1), whose kernel lock is released the moment the holder dies, so there
# is no stale lock to clean up. Every lockf call passes -k, so the lock file is
# never unlinked and two callers can never lock different files.
#
#   . scripts/lib/machine-lock.sh
#   machine_lock_reexec <held-var> <lockfile> <label> <what> <cmd...>
#
# If the environment variable named <held-var> is set, this run already holds
# the lock and the function returns, so the caller carries on. Otherwise it
# exports <held-var>=1 and runs <cmd...> (normally the caller itself again)
# under <lockfile>: first a single non-blocking try, and if another run holds
# the lock, "<label>: waiting on another <what>" once, then a blocking wait.
# Without lockf it exits 2; there is no unlocked fallback.
#
# lockf exits 75 (EX_TEMPFAIL) when the lock is busy, so a command that itself
# exits 75 would be read as busy and run once more, waiting. That is acceptable
# for these callers.

machine_lock_reexec() {
	_held=$1
	_lockfile=$2
	_label=$3
	_what=$4
	shift 4
	eval "_value=\${$_held:-}"
	[ -z "$_value" ] || return 0
	eval "$_held=1"
	export "$_held"
	if ! command -v lockf >/dev/null 2>&1; then
		echo "$_label: lockf not found; the machine-wide lock needs macOS lockf(1)" >&2
		exit 2
	fi
	# `|| _code=$?` keeps a caller running under `set -e` alive on the busy exit.
	_code=0
	# -s drops lockf's own "already locked" line so the output reads as ours
	# alone; open and usage failures still print.
	lockf -s -k -t 0 "$_lockfile" "$@" || _code=$?
	[ "$_code" -eq 75 ] || exit "$_code"
	echo "$_label: waiting on another $_what"
	exec lockf -k "$_lockfile" "$@"
}
