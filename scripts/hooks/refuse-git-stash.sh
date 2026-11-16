#!/bin/sh
# PreToolUse hook for Bash: refuses `git stash` and `git -C <dir> stash`. The
# stash is shared by every worktree of the repo, so one session's `stash pop`
# can apply another worktree's stash. Exit 2 refuses and shows stderr to the
# model; exit 0 lets the command run. It is wired in the operator's
# ~/.claude/settings.json, since this repo ignores .claude/settings.json.
set -u

input=$(cat)
# Most commands never mention stash; let them through without starting jq.
case $input in
*stash*) ;;
*) exit 0 ;;
esac

# Exit 1 lets the command run but shows the warning, so the guard never turns
# itself off unseen.
command -v jq >/dev/null 2>&1 || {
	echo "refuse-git-stash: jq not found, so git stash is not refused" >&2
	exit 1
}
cmd=$(printf '%s' "$input" | jq -r '.tool_input.command // empty') || exit 0
[ -n "$cmd" ] || exit 0

# Heredoc bodies are text, not commands: drop the lines after one that holds
# `<<WORD` (optionally `<<-`, optionally quoted) up to and including the line
# that is exactly WORD (leading tabs allowed for `<<-`).
body_free=$(printf '%s\n' "$cmd" | awk '
	inhere {
		line = $0
		if (dash) sub(/^\t+/, "", line)
		if (line == word) inhere = 0
		next
	}
	{
		print
		if (match($0, /<<-?[ \t]*["\047]?[A-Za-z_][A-Za-z0-9_]*["\047]?/) &&
		    !(RSTART > 1 && substr($0, RSTART - 1, 1) == "<")) {
			s = substr($0, RSTART, RLENGTH)
			dash = (s ~ /^<<-/)
			sub(/^<<-?[ \t]*["\047]?/, "", s)
			sub(/["\047]$/, "", s)
			word = s
			inhere = 1
		}
	}')

# At the start of the command or after a separator: optional prefixes (VAR=val,
# sudo, env, command), `git` or a path ending in /git, any global flags (-C,
# -c, --git-dir, --work-tree and --namespace take a value; others, such as
# --no-pager or -P, do not), then `stash` as the subcommand.
if printf '%s\n' "$body_free" |
	grep -Eq '(^|[;&|(])[[:space:]]*(([A-Za-z_][A-Za-z0-9_]*=[^[:space:]]*|sudo|env|command)[[:space:]]+)*([^[:space:]]*/)?git([[:space:]]+(-C|-c|--git-dir|--work-tree|--namespace)[[:space:]]+[^[:space:]]+|[[:space:]]+-[^[:space:]]*)*[[:space:]]+stash([[:space:]]|$|;|&|\|)'; then
	echo "refuse-git-stash: git stash is shared by every worktree of this repo. Leave work in progress in the working tree, or commit it on your branch." >&2
	exit 2
fi
exit 0
