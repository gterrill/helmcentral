#!/bin/sh
# Watches a PR until it merges or closes, for the ship skill's Monitor.
# Prints `state | mergeable | failed checks` only when that line changes.
# Usage: sh scripts/watch-pr.sh <PR number>
set -u
pr=${1:?usage: sh scripts/watch-pr.sh <PR number>}
prev=
while :; do
	cur=$(gh pr view "$pr" --json state,mergeable,statusCheckRollup --jq '[.state, .mergeable,
		([.statusCheckRollup[] | select((.conclusion // .state) | IN("FAILURE","ERROR","CANCELLED","TIMED_OUT","STARTUP_FAILURE","ACTION_REQUIRED"))
			| (.name // .context)] | join(", "))] | join(" | ")' 2>&1) || cur="gh pr view failed: $cur"
	[ "$cur" != "$prev" ] && echo "$cur" && prev=$cur
	case $cur in MERGED* | CLOSED*) exit 0 ;; esac
	sleep 60
done
