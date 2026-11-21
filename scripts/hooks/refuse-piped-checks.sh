#!/bin/sh
# PreToolUse hook for Bash: refuses a check target with a pipe after it, such
# as `make check 2>&1 | tail -5`. The targets print their own pass counts, FAIL
# lines and log paths, and a pipe hides them and the exit code. Exit 2 refuses
# and shows stderr to the model; exit 0 lets the command run. It is wired in
# the operator's ~/.claude/settings.json, since this repo ignores
# .claude/settings.json.
set -u

input=$(cat)
# Most commands name no check target; let them through without starting jq.
case $input in
*check* | *docs-lint-changed* | *security-review*) ;;
*) exit 0 ;;
esac

# Exit 1 lets the command run but shows the warning, so the guard never turns
# itself off unseen.
command -v jq >/dev/null 2>&1 || {
	echo "refuse-piped-checks: jq not found, so piped check targets are not refused" >&2
	exit 1
}
cmd=$(printf '%s' "$input" | jq -r '.tool_input.command // empty') || exit 0
[ -n "$cmd" ] || exit 0

# Drop `||` so it is not read as a pipe and redirections such as `2>&1` so
# their `&` is not read as a separator, then look for a `|` after a check
# target within the same command (before any `;` or `&`).
if printf '%s\n' "$cmd" | sed 's/||/ /g; s/[0-9]*>&[0-9-]*/ /g' |
	grep -Eq '(make( +[^ |;&]+)* +(check[a-z-]*|docs-lint-changed)|security-review\.sh|await-check\.sh)([^|;&]*)\|'; then
	echo "Run check targets unpiped: they print their own pass counts, FAIL lines and log paths." >&2
	exit 2
fi
exit 0
