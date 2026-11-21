#!/bin/sh
# Cases for refuse-piped-checks.sh: `sh scripts/hooks/refuse-piped-checks-test.sh`.
set -u
hook=$(dirname "$0")/refuse-piped-checks.sh
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

expect 2 'make check 2>&1 | tail -5'
expect 2 'make -C /w/tree check | grep FAIL'
expect 2 'make check-backend | head'
expect 2 'make docs-lint-changed | tail'
expect 2 'sh scripts/security-review.sh /w | tail -3'
expect 2 './scripts/security-review.sh /w | tail -3'
expect 2 'bash scripts/security-review.sh /w | tail'
expect 2 'sh -x scripts/security-review.sh /w | tail'
expect 2 'git status; sh scripts/security-review.sh /w | tail'
expect 2 'sh scripts/await-check.sh abc123 | tail -5'
expect 0 'sh scripts/await-check.sh abc123'
expect 0 'make check'
expect 0 'make -C /w/tree check'
expect 0 'make check || echo failed'
expect 0 'make check 2>&1'
expect 0 'git log | head'
expect 0 'ls | grep check'
expect 0 'make dev | tee out.log'
expect 0 'make check && git log | head'
expect 0 'make check; ls | wc -l'
expect 0 'make check-frontend > /tmp/x; cat /tmp/x | tail'
expect 2 'git status; make check | tail'

[ "$fail" -eq 0 ] && echo "refuse-piped-checks: all cases passed"
exit "$fail"
