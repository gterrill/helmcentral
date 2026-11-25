#!/bin/sh
# Cases for scripts/lib/stay-awake.sh and its use in check.sh and
# security-review.sh: `sh scripts/stay-awake-test.sh`. Nothing here sleeps: a
# fake caffeinate on PATH records its arguments and exits.
set -u
root=$(cd "$(dirname "$0")/.." && pwd)
lib=$root/scripts/lib/stay-awake.sh
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
fail=0

bad() {
	echo "FAIL: $1"
	fail=1
}

bin=$work/bin
mkdir "$bin"
cat >"$bin/caffeinate" <<EOF
#!/bin/sh
echo "\$*" >>"$work/caffeinate.args"
EOF
chmod +x "$bin/caffeinate"
# A go that does nothing, so check.sh in a bare tree runs no real toolchain.
cat >"$bin/go" <<'EOF'
#!/bin/sh
exit 0
EOF
chmod +x "$bin/go"
# Every other tool the cases need, with no real caffeinate among them.
nobin=$work/nobin
mkdir "$nobin"
ln -s /bin/sh "$nobin/sh"
ln -s "$(command -v dirname)" "$nobin/dirname"
ln -s "$bin/go" "$nobin/go"

# wait_args: the fake caffeinate runs in the background, so wait for its line.
wait_args() {
	n=0
	while [ ! -s "$work/caffeinate.args" ] && [ "$n" -lt 100 ]; do
		n=$((n + 1))
		sleep 0.1
	done
}

# 1. stay_awake starts `caffeinate -i -w <the script's pid>` and returns, so the
# script goes on in its own process.
: >"$work/caffeinate.args"
PATH="$bin:$PATH" sh -c ". '$lib'; stay_awake; echo \$\$ >'$work/pid'; echo ran" >"$work/a.out" 2>&1
wait_args
[ "$(cat "$work/a.out")" = ran ] || bad "stay_awake: output was '$(cat "$work/a.out")', want 'ran'"
[ "$(cat "$work/caffeinate.args")" = "-i -w $(cat "$work/pid")" ] || bad "stay_awake: caffeinate got '$(cat "$work/caffeinate.args")', want '-i -w $(cat "$work/pid")'"

# 2. Without caffeinate the helper exits 2 with a clear message and runs nothing.
PATH="$nobin" /bin/sh -c ". '$lib'; stay_awake; echo ran" >"$work/d.out" 2>"$work/d.err"
code=$?
[ "$code" -eq 2 ] || bad "missing caffeinate exited $code, want 2"
grep -q '^stay-awake: caffeinate not found' "$work/d.err" || bad "missing caffeinate message was '$(cat "$work/d.err")'"
[ -s "$work/d.out" ] && bad "command ran without caffeinate"

# Cases 3 and 4 run copies of the scripts in a bare tree, so no real check or
# review runs.
fake=$work/fake
mkdir -p "$fake/scripts" "$fake/backend"
cp "$root/scripts/check.sh" "$fake/scripts/check.sh"
cp "$root/scripts/security-review.sh" "$fake/scripts/security-review.sh"
cp -R "$root/scripts/lib" "$fake/scripts/lib"
printf '#!/bin/sh\nexit 0\n' >"$fake/scripts/gofmt-check.sh"
printf '#!/bin/sh\nexit 0\n' >"$fake/scripts/tree-hash.sh"

# 3. check.sh starts the watcher once, before the lock, naming its own pid.
: >"$work/caffeinate.args"
HELMCENTRAL_CHECK_LOCK_HELD=1 HELMCENTRAL_CHECK_TREE_OWNER=1 PATH="$bin:$PATH" \
	sh "$fake/scripts/check.sh" backend >"$work/check.out" 2>&1
wait_args
[ "$(wc -l <"$work/caffeinate.args" | tr -d ' ')" -eq 1 ] || bad "check.sh called caffeinate $(wc -l <"$work/caffeinate.args" | tr -d ' ') times, want 1"
grep -Eq '^-i -w [0-9]+$' "$work/caffeinate.args" || bad "check.sh caffeinate args were '$(cat "$work/caffeinate.args")'"
HELMCENTRAL_CHECK_LOCK_HELD=1 HELMCENTRAL_CHECK_TREE_OWNER=1 PATH="$nobin" \
	/bin/sh "$fake/scripts/check.sh" backend >"$work/check2.out" 2>"$work/check2.err"
code=$?
[ "$code" -eq 2 ] || bad "check.sh without caffeinate exited $code, want 2"
grep -q 'caffeinate not found' "$work/check2.err" || bad "check.sh without caffeinate printed '$(cat "$work/check2.err")'"

# 4. security-review.sh starts it once (no arguments, so it stops at its usage
# line afterwards).
: >"$work/caffeinate.args"
PATH="$bin:$PATH" sh "$fake/scripts/security-review.sh" >"$work/sr.out" 2>&1
wait_args
grep -Eq '^-i -w [0-9]+$' "$work/caffeinate.args" || bad "security-review.sh caffeinate args were '$(cat "$work/caffeinate.args")'"
[ "$(wc -l <"$work/caffeinate.args" | tr -d ' ')" -eq 1 ] || bad "security-review.sh called caffeinate more than once"

[ "$fail" -eq 0 ] && echo "stay-awake: all cases passed"
exit "$fail"
