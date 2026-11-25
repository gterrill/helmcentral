#!/bin/sh
# PostToolUse hook for Edit|Write|MultiEdit: runs `gofmt -w` on the .go file
# just edited under backend/ of a Helmcentral checkout, so `make check`'s gofmt
# step never fails on formatting alone.
# When it changes the file it tells the agent through additionalContext.
# A file that does not parse yet (code mid-edit) is left as it is and exits 0:
# that is not the hook's business, and the check still catches it. Exit 1 shows
# a warning without blocking. It is wired in the operator's
# ~/.claude/settings.json, since this repo ignores .claude/settings.json.
set -u

input=$(cat)

command -v jq >/dev/null 2>&1 || {
	echo "gofmt-edited: jq not found, so edited Go files are not formatted" >&2
	exit 1
}
file=$(printf '%s' "$input" | jq -r '.tool_input.file_path // empty') || exit 0
case $file in
*.go) ;;
*) exit 0 ;;
esac
[ -f "$file" ] || exit 0

# Only Go files under backend/ of a Helmcentral checkout, the scope of
# scripts/gofmt-check.sh; any other Go file is none of this hook's business.
# Both paths are physical, so a symlinked temp or home directory still matches.
dir=$(cd "$(dirname "$file")" 2>/dev/null && pwd -P) || exit 0
top=$(git -C "$dir" rev-parse --show-toplevel 2>/dev/null) || exit 0
[ -f "$top/scripts/gofmt-check.sh" ] || exit 0
case $dir/ in
"$top"/backend/third_party/*) exit 0 ;; # vendored, kept as upstream wrote it
"$top"/backend/*) ;;
*) exit 0 ;;
esac

command -v gofmt >/dev/null 2>&1 || {
	echo "gofmt-edited: gofmt not found, so $file was not formatted" >&2
	exit 1
}
# gofmt -l prints the name only when the file needs formatting; a file it
# cannot parse prints nothing on stdout, so it is left alone.
[ -n "$(gofmt -l "$file" 2>/dev/null)" ] || exit 0
gofmt -w "$file" >/dev/null 2>&1 || exit 0
# Tell the agent, so its next Edit does not work from the old text.
jq -n --arg p "$file" '{hookSpecificOutput: {hookEventName: "PostToolUse", additionalContext: ("gofmt reformatted " + $p + "; read it again before the next Edit to it.")}}'
exit 0
