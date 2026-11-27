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
#
# caffeinate -i stops idle sleep only. Closing the lid still sleeps a Mac with
# no external display, on AC as on battery, and `pmset -c sleep 0` does not
# change that. So check.sh also uses on_battery, sleep_mark and sleep_report
# (below): a warning at the start when on battery, and a summary line when the
# Mac slept, whatever the cause.

stay_awake() {
	if ! command -v caffeinate >/dev/null 2>&1; then
		echo "stay-awake: caffeinate not found; the run must keep the Mac awake and needs macOS caffeinate(8)" >&2
		exit 2
	fi
	caffeinate -i -w $$ </dev/null >/dev/null 2>&1 &
}

# on_battery: true when `pmset -g ps` says the Mac runs on battery power.
on_battery() {
	pmset -g ps 2>/dev/null | grep -q 'Battery Power'
}

# sleep_mark: prints "<monotonic> <awake>", the Mac's two clocks in whole
# seconds: CLOCK_MONOTONIC keeps counting through sleep, CLOCK_UPTIME_RAW does
# not. Returns 1 when perl cannot read them.
sleep_mark() {
	sleep_clocks=$(perl -MTime::HiRes=clock_gettime,CLOCK_MONOTONIC,CLOCK_UPTIME_RAW -e 'printf "%d %d\n", clock_gettime(CLOCK_MONOTONIC), clock_gettime(CLOCK_UPTIME_RAW)' 2>/dev/null) || return 1
	case $sleep_clocks in
	[0-9]*" "[0-9]*) ;;
	*) return 1 ;;
	esac
	echo "$sleep_clocks"
}

# sleep_report <mark>: after a run, compares the clocks with the mark from
# sleep_mark. The time asleep is the monotonic step minus the awake step; the
# run's length is the monotonic step. Prints one line when the Mac slept 60 s or
# more in all, nothing otherwise. When the mark is empty or malformed, or perl
# fails, it says so. It always returns 0: sleeping is not a failure of the run.
sleep_report() {
	sleep_in=${1:-}
	sleep_mono0=${sleep_in%% *}
	sleep_awake0=${sleep_in#* }
	# A mark with no space would parse as the same number twice.
	case $sleep_in in
	*" "*) ;;
	*) sleep_mono0= ;;
	esac
	sleep_now=$(sleep_mark) || sleep_now=
	sleep_mono1=${sleep_now%% *}
	sleep_awake1=${sleep_now#* }
	for sleep_n in "$sleep_mono0" "$sleep_awake0" "$sleep_mono1" "$sleep_awake1"; do
		case $sleep_n in
		'' | *[!0-9]*)
			echo "slept: could not read the Mac's clocks"
			return 0
			;;
		esac
	done
	sleep_run=$((sleep_mono1 - sleep_mono0))
	sleep_asleep=$((sleep_run - (sleep_awake1 - sleep_awake0)))
	if [ "$sleep_asleep" -ge 60 ]; then
		echo "slept: the Mac slept $((sleep_asleep / 60)) min of this run's $((sleep_run / 60)) min"
	fi
	return 0
}
