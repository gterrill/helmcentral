#!/bin/sh
# Cases for refuse-git-stash.sh: `sh scripts/hooks/refuse-git-stash-test.sh`.
set -u
hook=$(dirname "$0")/refuse-git-stash.sh
fail=0

# expect <exit code> <command>
expect() {
	want=$1
	cmd=$2
	jq -n --arg c "$cmd" '{tool_input: {command: $c}}' | sh "$hook" 2>/dev/null
	got=$?
	if [ "$got" -ne "$want" ]; then
		echo "FAIL: want exit $want, got $got: $cmd"
		fail=1
	fi
}

expect 2 'git stash'
expect 2 'git stash pop'
expect 2 'git -C /x stash list'
expect 2 'git stash push -m wip'
expect 2 'git status && git stash'
expect 2 'cd /w; git stash drop'
expect 2 'git --no-pager stash'
expect 2 'git -P stash list'
expect 2 'git --paginate stash'
expect 2 'git -c core.x=1 stash'
expect 2 'git --git-dir=/x/.git stash'
expect 2 '/usr/bin/git stash'
expect 2 'sudo git stash'
expect 2 'env git stash pop'
expect 2 'env FOO=1 BAR=2 git stash'
expect 2 'command git stash'
expect 2 'FOO=1 git stash'
expect 2 'ls && sudo env X=1 /opt/homebrew/bin/git -C /w stash drop'
expect 0 'git status'
expect 0 'git commit -m "stash the thing"'
expect 0 'git -C /x commit -m "git stash"'
expect 0 'echo git stash'
expect 0 'git log --oneline stash'
expect 0 'env FOO=1 make check'
expect 0 'git commit -m "drop the stash helper"'
expect 0 'grep stash file'
expect 0 'git log --oneline | grep stash'
expect 0 'ls'

# Multi-line commands: heredoc bodies are text, not commands.
expect 0 "git commit -F - <<'EOF'
Refuse git stash
git stash is shared by every worktree
EOF"
expect 0 "git commit -m \"\$(cat <<'EOF'
Refuse git stash
git stash is shared by every worktree
EOF
)\""
expect 0 "cat <<-\"END\"
	git stash
	END"
expect 2 'cd /x
git stash pop'
expect 2 "git commit -F - <<'EOF'
Refuse git stash
EOF
git stash"

[ "$fail" -eq 0 ] && echo "refuse-git-stash: all cases passed"
exit "$fail"
