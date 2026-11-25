#!/bin/sh
# The Mac sleeps on idle under unattended agents; a 6-minute check once took 85
# minutes. Sourced by scripts/check.sh and scripts/security-review.sh.
#
#   . scripts/lib/stay-awake.sh; stay_awake
#
# Starts `caffeinate -i -w $$` in the background and returns; the script carries
# on in its own process, so a signal sent to it reaches it and its traps as
# usual. caffeinate exits by itself when this process does. There is no marker:
# a nested run starts a watcher of its own, which is harmless. Without
# caffeinate it exits 2.

stay_awake() {
	if ! command -v caffeinate >/dev/null 2>&1; then
		echo "stay-awake: caffeinate not found; the run must keep the Mac awake and needs macOS caffeinate(8)" >&2
		exit 2
	fi
	caffeinate -i -w $$ </dev/null >/dev/null 2>&1 &
}
