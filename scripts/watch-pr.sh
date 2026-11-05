#!/bin/sh
# Watches a PR until it merges or closes, for the ship skill.
# Prints `state | mergeable | merge state | failed checks | checks`. The last
# field is `no checks yet` while CI is queued, `no checks after 15 min` if the
# PR has gone that long since its last update with none, `checks running`, or
# `checks done`. `mergeable` means only that there are no conflicts; a merge
# state of CLEAN is what means the PR can merge. While the PR is OPEN and GitHub
# is still computing either value (UNKNOWN), the line is not a change: polling
# goes on.
# A PR stuck BLOCKED on a required check that never reports, or on a CI run that
# never starts, still produces a change that wakes a --once run.
# A single failed `gh` call is not reported (the network drops, the Mac
# sleeps); the third in a row is, as `gh pr view failed: ...`. gh's stderr is
# kept apart from the line and used only for that message.
#
# Usage: sh scripts/watch-pr.sh <PR number> [--once]
#   Without --once: prints each time the line changes, exits on merge or close.
#   With --once: compares the line with the last one reported for this PR,
#   kept in the git directory (git rev-parse --git-path watch-pr-<N>). When it
#   differs it saves and prints the line and exits 0, so the first run reports
#   at once and each later run stays silent until the line changes. A MERGED or
#   CLOSED line always prints and exits 0. Run it as a background command to be
#   woken only on a change.
# WATCH_PR_INTERVAL sets the seconds between polls (default 60).
set -u
once=0
pr=
for arg in "$@"; do
	case $arg in
	--once) once=1 ;;
	*) pr=$arg ;;
	esac
done
[ -n "$pr" ] || { echo "usage: sh scripts/watch-pr.sh <PR number> [--once]" >&2; exit 2; }
interval=${WATCH_PR_INTERVAL:-60}

statefile=
if [ "$once" = 1 ]; then
	statefile=$(git rev-parse --git-path "watch-pr-$pr") || {
		echo "watch-pr: cannot find the git directory for the state file" >&2
		exit 1
	}
fi

err=$(mktemp) || { echo "watch-pr: mktemp failed" >&2; exit 1; }
trap 'rm -f "$err"' EXIT
trap 'exit 130' INT TERM

prev=
fails=0
while :; do
	if cur=$(gh pr view "$pr" --json state,mergeable,mergeStateStatus,statusCheckRollup,updatedAt --jq '(.statusCheckRollup // []) as $checks | [.state, .mergeable, .mergeStateStatus,
		([$checks[] | select((.conclusion // .state) | IN("FAILURE","ERROR","CANCELLED","TIMED_OUT","STARTUP_FAILURE","ACTION_REQUIRED","STALE"))
			| (.name // .context)] | join(", ")),
		(if ($checks | length) == 0 then (if (now - (.updatedAt | fromdate)) > 900 then "no checks after 15 min" else "no checks yet" end)
		elif any($checks[]; (.status // .state) | IN("COMPLETED","SUCCESS","FAILURE","ERROR") | not) then "checks running"
		else "checks done" end)] | join(" | ")' 2>"$err"); then
		fails=0
	else
		fails=$((fails + 1))
		if [ "$fails" -lt 3 ]; then
			sleep "$interval"
			continue
		fi
		msg=$(tr '\n' ' ' <"$err")
		cur="gh pr view failed: ${msg% }"
	fi

	# GitHub is still computing: not a change. MERGED and CLOSED report anyway.
	case $cur in
	"OPEN | UNKNOWN | "* | "OPEN | "*" | UNKNOWN | "*)
		sleep "$interval"
		continue
		;;
	esac

	if [ "$once" = 1 ]; then
		last=
		[ -f "$statefile" ] && last=$(cat "$statefile")
		if [ "$cur" != "$last" ]; then
			printf '%s\n' "$cur" >"$statefile"
			printf '%s\n' "$cur"
			exit 0
		fi
		case $cur in
		MERGED* | CLOSED*)
			printf '%s\n' "$cur"
			exit 0
			;;
		esac
	else
		if [ "$cur" != "$prev" ]; then
			printf '%s\n' "$cur"
			prev=$cur
		fi
		case $cur in MERGED* | CLOSED*) exit 0 ;; esac
	fi
	sleep "$interval"
done
