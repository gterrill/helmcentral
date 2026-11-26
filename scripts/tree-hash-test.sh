#!/bin/sh
# Cases for scripts/tree-hash.sh: `sh scripts/tree-hash-test.sh`. Runs in a
# temporary git repository, never the real one.
set -u
root=$(cd "$(dirname "$0")/.." && pwd)
script=$root/scripts/tree-hash.sh
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
fail=0

bad() {
	echo "FAIL: $1"
	fail=1
}

repo=$work/repo
mkdir "$repo"
git -C "$repo" init -q
git -C "$repo" config user.email test@example.com
git -C "$repo" config user.name test
printf 'ignored.log\n' >"$repo/.gitignore"
printf 'one\n' >"$repo/tracked.txt"
git -C "$repo" add .gitignore tracked.txt
git -C "$repo" commit -q -m first

hash() {
	sh "$script" "$repo"
}

base=$(hash) || bad "clean repo: exit $?"
[ -n "$base" ] || bad "clean repo printed no hash"
[ "$(hash)" = "$base" ] || bad "the same tree printed two hashes"

# A change to a tracked file changes the hash.
printf 'two\n' >"$repo/tracked.txt"
edited=$(hash)
[ "$edited" != "$base" ] || bad "tracked edit did not change the hash"

# A new untracked file changes it again.
printf 'new\n' >"$repo/new.txt"
untracked=$(hash)
[ "$untracked" != "$edited" ] || bad "untracked file did not change the hash"

# A gitignored file does not.
printf 'noise\n' >"$repo/ignored.log"
[ "$(hash)" = "$untracked" ] || bad "ignored file changed the hash"

# Plugin binaries under the ignored /plugins/ are test inputs, so they count.
prepo=$work/prepo
mkdir -p "$prepo/plugins/tides"
git -C "$prepo" init -q
printf '/plugins/*\n' >"$prepo/.gitignore"
git -C "$prepo" add .gitignore
git -C "$prepo" -c user.email=t@example.com -c user.name=t commit -q -m i
printf 'wasm one\n' >"$prepo/plugins/tides/t.wasm"
plug1=$(sh "$script" "$prepo")
printf 'wasm two\n' >"$prepo/plugins/tides/t.wasm"
[ "$(sh "$script" "$prepo")" != "$plug1" ] || bad "a changed plugin binary did not change the hash"

# A same-size edit made in the same second as the index write is still seen
# when the hash runs a second later. git trusts a cached entry's stat unless
# the entry is as new as the index file, so a copy stamped "now" hid the edit
# about half the time. Three tries, so a miss shows reliably.
for try in 1 2 3; do
	racy=$work/racy$try
	mkdir "$racy"
	git -C "$racy" init -q
	printf 'one\n' >"$racy/t.txt"
	git -C "$racy" add t.txt
	git -C "$racy" -c user.email=t@example.com -c user.name=t commit -q -m i
	before_edit=$(sh "$script" "$racy")
	printf 'two\n' >"$racy/t.txt"
	sleep 1.1
	[ "$(sh "$script" "$racy")" != "$before_edit" ] || bad "same-second edit missed (try $try)"
done

# The real index is untouched: nothing staged, same status as before.
before=$(git -C "$repo" status --porcelain)
hash >/dev/null
[ -z "$(git -C "$repo" diff --cached)" ] || bad "the real index holds staged changes"
[ "$(git -C "$repo" status --porcelain)" = "$before" ] || bad "git status changed"
case $before in
*'?? new.txt'*) ;;
*) bad "new.txt is no longer untracked: $before" ;;
esac

# A staged change reads the same as the unstaged one.
git -C "$repo" add tracked.txt new.txt
[ "$(hash)" = "$untracked" ] || bad "staging changed the hash"

# Checks also read refs: check-changelog.sh the latest v* tag, docs-lint-changed
# the merge base with origin/main. A new tag or a moved merge base changes the
# hash although no file does.
withtag=$(hash)
git -C "$repo" tag v0.0.1
tagged=$(hash)
[ "$tagged" != "$withtag" ] || bad "a new tag did not change the hash"
git -C "$repo" tag v0.0.2
[ "$(hash)" != "$tagged" ] || bad "a newer tag did not change the hash"
git -C "$repo" tag -d v0.0.1 v0.0.2 >/dev/null
[ "$(hash)" = "$withtag" ] || bad "removing the tags did not restore the hash"
git -C "$repo" update-ref refs/remotes/origin/main HEAD
[ "$(hash)" != "$withtag" ] || bad "an origin/main ref did not change the hash"
git -C "$repo" update-ref -d refs/remotes/origin/main
[ "$(hash)" = "$withtag" ] || bad "removing origin/main did not restore the hash"

# The temp index starts as a copy of the real one, so git reuses its stat cache:
# with an index present, read-tree never runs.
realgit=$(command -v git)
mkdir "$work/fakebin"
cat >"$work/fakebin/git" <<EOF
#!/bin/sh
for a in "\$@"; do
	[ "\$a" = read-tree ] && echo read-tree >>"$work/git.log"
done
exec "$realgit" "\$@"
EOF
chmod +x "$work/fakebin/git"
want=$(hash)
: >"$work/git.log"
got=$(PATH=$work/fakebin:$PATH sh "$script" "$repo")
[ "$got" = "$want" ] || bad "copying the index changed the hash"
[ -s "$work/git.log" ] && bad "read-tree ran although the checkout has an index"

# A checkout with no index file yet reads HEAD instead, and gets the same hash.
rm -f "$repo/.git/index"
: >"$work/git.log"
got=$(PATH=$work/fakebin:$PATH sh "$script" "$repo")
[ -s "$work/git.log" ] || bad "no index: read-tree HEAD did not run"
git -C "$repo" reset -q
[ "$got" = "$(hash)" ] || bad "no index: hash differs from the one with an index"

# A nested git repository cannot be vouched for: both modes exit 2 naming it,
# and no "adding embedded git repository" warning prints.
# expect_nested <label> <repo> <path>
expect_nested() {
	for flag in "" --stat; do
		# $flag is deliberately unquoted: empty, or --stat.
		sh "$script" $flag "$2" >"$work/n.out" 2>"$work/n.err"
		code=$?
		[ "$code" -eq 2 ] || bad "$1 ${flag:-content}: exited $code, want 2"
		[ "$(cat "$work/n.err")" = "tree-hash: $3 is a nested git repository; this script cannot vouch for its files" ] || bad "$1 ${flag:-content}: message was '$(cat "$work/n.err")'"
		[ -s "$work/n.out" ] && bad "$1 ${flag:-content}: printed a hash"
	done
}

nrepo=$work/nrepo
mkdir "$nrepo"
git -C "$nrepo" init -q
printf 'outer\n' >"$nrepo/o.txt"
git -C "$nrepo" add o.txt
git -C "$nrepo" -c user.email=t@example.com -c user.name=t commit -q -m i
mkdir "$nrepo/inner"
git -C "$nrepo/inner" init -q
printf 'in\n' >"$nrepo/inner/i.txt"
git -C "$nrepo/inner" add i.txt
git -C "$nrepo/inner" -c user.email=t@example.com -c user.name=t commit -q -m i
expect_nested "untracked nested repo" "$nrepo" inner

# A freshly initialised nested repo with no commit counts too.
frepo=$work/frepo
mkdir "$frepo"
git -C "$frepo" init -q
printf 'o\n' >"$frepo/o.txt"
git -C "$frepo" add o.txt
git -C "$frepo" -c user.email=t@example.com -c user.name=t commit -q -m i
mkdir "$frepo/fresh"
git -C "$frepo/fresh" init -q
expect_nested "fresh nested repo" "$frepo" fresh

# A committed gitlink counts, even with its directory missing.
grepo=$work/grepo
mkdir "$grepo"
git -C "$grepo" init -q
printf 'x\n' >"$grepo/x.txt"
git -C "$grepo" add x.txt
git -C "$grepo" -c user.email=t@example.com -c user.name=t commit -q -m i
gsha=$(git -C "$grepo" rev-parse HEAD)
git -C "$grepo" update-index --add --cacheinfo "160000,$gsha,g"
git -C "$grepo" -c user.email=t@example.com -c user.name=t commit -q -m gitlink
expect_nested "committed gitlink" "$grepo" g

# A nested repo that is gitignored does not count.
irepo=$work/irepo
mkdir "$irepo"
git -C "$irepo" init -q
printf 'skip/\n' >"$irepo/.gitignore"
git -C "$irepo" add .gitignore
git -C "$irepo" -c user.email=t@example.com -c user.name=t commit -q -m i
mkdir "$irepo/skip"
git -C "$irepo/skip" init -q
sh "$script" "$irepo" >/dev/null || bad "ignored nested repo: exited $?"
sh "$script" --stat "$irepo" >/dev/null || bad "ignored nested repo: --stat exited $?"

# Submodules are not handled: fail fast.
printf '[submodule "x"]\n' >"$nrepo/.gitmodules"
sh "$script" "$nrepo" >"$work/sub.out" 2>"$work/sub.err"
code=$?
[ "$code" -eq 2 ] || bad ".gitmodules: exited $code, want 2"
grep -q 'submodule' "$work/sub.err" || bad ".gitmodules: message was '$(cat "$work/sub.err")'"
[ -s "$work/sub.out" ] && bad ".gitmodules: printed a hash"
rm -f "$nrepo/.gitmodules"

# --stat fingerprints mtime, ctime and size: a same-content rewrite moves it.
srepo=$work/srepo
mkdir "$srepo"
git -C "$srepo" init -q
printf 'one\n' >"$srepo/t.txt"
printf 'gone\n' >"$srepo/d.txt"
git -C "$srepo" add t.txt d.txt
git -C "$srepo" -c user.email=t@example.com -c user.name=t commit -q -m i
rm "$srepo/d.txt"
printf 'u\n' >"$srepo/u.txt"
s1=$(sh "$script" --stat "$srepo") || bad "--stat exited $?"
[ -n "$s1" ] || bad "--stat printed nothing"
[ "$(sh "$script" --stat "$srepo")" = "$s1" ] || bad "--stat changed with no writes"
sleep 1.1
printf 'one\n' >"$srepo/t.txt"
[ "$(sh "$script" --stat "$srepo")" != "$s1" ] || bad "--stat did not change after a same-content rewrite"

# A tracked file named like an option still gets a fingerprint, and a
# same-bytes rewrite of it moves the fingerprint.
drepo=$work/drepo
mkdir "$drepo"
git -C "$drepo" init -q
printf 'v\n' >"$drepo/-v.txt"
git -C "$drepo" add -- -v.txt
git -C "$drepo" -c user.email=t@example.com -c user.name=t commit -q -m i
d1=$(sh "$script" --stat "$drepo") || bad "leading-dash name: --stat exited $?"
[ -n "$d1" ] || bad "leading-dash name: --stat printed nothing"
sleep 1.1
printf 'v\n' >"$drepo/-v.txt"
[ "$(sh "$script" --stat "$drepo")" != "$d1" ] || bad "leading-dash name: --stat did not change after a same-bytes rewrite"

# A tracked file whose parent directory was replaced by a file is gone, not an error.
trepo=$work/trepo
mkdir "$trepo"
git -C "$trepo" init -q
mkdir "$trepo/a"
printf 'b\n' >"$trepo/a/b.txt"
git -C "$trepo" add a/b.txt
git -C "$trepo" -c user.email=t@example.com -c user.name=t commit -q -m i
rm -r "$trepo/a"
printf 'now a file\n' >"$trepo/a"
t1=$(sh "$script" --stat "$trepo") || bad "Not a directory: --stat exited $?"
[ -n "$t1" ] || bad "Not a directory: --stat printed nothing"

# An inherited GIT_INDEX_FILE changes neither mode.
want=$(sh "$script" "$repo")
wants=$(sh "$script" --stat "$repo")
got=$(GIT_INDEX_FILE=$work/other-index sh "$script" "$repo")
[ "$got" = "$want" ] || bad "inherited GIT_INDEX_FILE changed the hash"
got=$(GIT_INDEX_FILE=$work/other-index sh "$script" --stat "$repo")
[ "$got" = "$wants" ] || bad "inherited GIT_INDEX_FILE changed --stat"

# --stat covers the ignored /plugins/ files too.
printf 'wasm\n' >"$prepo/plugins/tides/t.wasm"
p1=$(sh "$script" --stat "$prepo")
sleep 1.1
printf 'wasm\n' >"$prepo/plugins/tides/t.wasm"
[ "$(sh "$script" --stat "$prepo")" != "$p1" ] || bad "--stat did not change after a same-bytes plugin rewrite"

# A stat or xargs failure with nothing on stderr is still a failure.
mkdir "$work/xbin"
printf '#!/bin/sh\nexit 1\n' >"$work/xbin/xargs"
chmod +x "$work/xbin/xargs"
PATH=$work/xbin:$PATH sh "$script" --stat "$srepo" >"$work/x.out" 2>"$work/x.err"
code=$?
[ "$code" -eq 1 ] || bad "silent xargs failure: --stat exited $code, want 1"
[ -s "$work/x.out" ] && bad "silent xargs failure: --stat printed a hash"

# A repository cloned under the ignored /plugins/ cannot be vouched for either.
nprepo=$work/nprepo
mkdir -p "$nprepo/plugins/x"
git -C "$nprepo" init -q
printf '/plugins/*\n' >"$nprepo/.gitignore"
git -C "$nprepo" add .gitignore
git -C "$nprepo" -c user.email=t@example.com -c user.name=t commit -q -m i
git -C "$nprepo/plugins/x" init -q
printf 'p\n' >"$nprepo/plugins/x/p.txt"
git -C "$nprepo/plugins/x" add p.txt
git -C "$nprepo/plugins/x" -c user.email=t@example.com -c user.name=t commit -q -m i
expect_nested "nested repo under plugins" "$nprepo" plugins/x

# Inherited git location variables change neither mode.
orepo=$work/orepo
mkdir "$orepo"
git -C "$orepo" init -q
want=$(sh "$script" "$repo")
wants=$(sh "$script" --stat "$repo")
got=$(GIT_DIR=$orepo/.git sh "$script" "$repo")
[ "$got" = "$want" ] || bad "inherited GIT_DIR changed the hash"
got=$(GIT_DIR=$orepo/.git sh "$script" --stat "$repo")
[ "$got" = "$wants" ] || bad "inherited GIT_DIR changed --stat"

# A path that is not a git checkout fails with a message.
mkdir "$work/plain"
sh "$script" "$work/plain" >"$work/plain.out" 2>"$work/plain.err"
code=$?
[ "$code" -ne 0 ] || bad "a plain directory exited 0"
grep -q 'not a git checkout' "$work/plain.err" || bad "plain directory message was '$(cat "$work/plain.err")'"
[ -s "$work/plain.out" ] && bad "a plain directory printed a hash"

# check.sh backend in its own temporary repo, with a fake go that exits 0.
# A fake caffeinate and a private lock file keep it off the real ones.
chk=$work/chk
mkdir -p "$chk/scripts" "$chk/backend" "$work/gobin"
cp "$root/scripts/check.sh" "$root/scripts/tree-hash.sh" "$root/scripts/gofmt-check.sh" "$chk/scripts/"
cp -R "$root/scripts/lib" "$chk/scripts/lib"
printf 'backend/tmp/\n' >"$chk/.gitignore"
printf 'package x\n' >"$chk/backend/x.go"
git -C "$chk" init -q
git -C "$chk" config user.email test@example.com
git -C "$chk" config user.name test
git -C "$chk" add .
git -C "$chk" commit -q -m first
cat >"$work/gobin/go" <<'EOF'
#!/bin/sh
# Fake go: exits 0. `go test` first does what the test case asks through env.
if [ "$1" = test ]; then
	[ -z "${FAKE_GO_WRITE:-}" ] || echo changed >"$FAKE_GO_WRITE"
	if [ -n "${FAKE_GO_REWRITE:-}" ]; then
		# Change a file and put its contents back.
		orig=$(cat "$FAKE_GO_REWRITE")
		echo changed >"$FAKE_GO_REWRITE"
		printf '%s\n' "$orig" >"$FAKE_GO_REWRITE"
	fi
	if [ -n "${FAKE_GO_RMGIT:-}" ]; then
		# Delete only the named .git, and only inside the test's temp dir.
		case $FAKE_GO_RMGIT in
		"$FAKE_GO_TMPDIR"/*/.git) rm -rf "$FAKE_GO_RMGIT" ;;
		*)
			echo "fake go: refusing to remove $FAKE_GO_RMGIT outside $FAKE_GO_TMPDIR" >&2
			exit 1
			;;
		esac
	fi
fi
exit 0
EOF
chmod +x "$work/gobin/go"
printf '#!/bin/sh\nexit 0\n' >"$work/gobin/caffeinate"
chmod +x "$work/gobin/caffeinate"

# run_check: runs check.sh backend in $chk, output in $work/chk.out, status in $code.
run_check() {
	env -u HELMCENTRAL_CHECK_LOCK_HELD -u HELMCENTRAL_CHECK_TREE_OWNER -u HELMCENTRAL_CHECK_LOAD_SAMPLED \
		PATH="$work/gobin:$PATH" HELMCENTRAL_CHECK_LOCKFILE="$work/chk.lock" \
		sh "$chk/scripts/check.sh" backend >"$work/chk.out" 2>&1
	code=$?
}

# An unchanged tree: the summary names its hash and the run passes.
expect=$(sh "$script" "$chk")
run_check
[ "$code" -eq 0 ] || bad "check.sh backend exited $code on an unchanged tree"
grep -qx "tree: $expect" "$work/chk.out" || bad "summary has no 'tree: $expect' line: $(cat "$work/chk.out")"
grep -qx 'check-backend: all steps passed' "$work/chk.out" || bad "unchanged tree: run did not pass"

# A tree changed by a step fails the run.
FAKE_GO_WRITE=$chk/backend/new.txt
export FAKE_GO_WRITE
run_check
unset FAKE_GO_WRITE
[ "$code" -eq 1 ] || bad "changed tree: exited $code, want 1"
grep -q '^tree: changed during the run' "$work/chk.out" || bad "changed tree: no 'changed during the run' line: $(cat "$work/chk.out")"
grep -qx 'check-backend: FAILED' "$work/chk.out" || bad "changed tree: run did not end FAILED"
rm -f "$chk/backend/new.txt"

# A file changed and restored during the run leaves the tree hash alone but
# moves its ctime: the run fails.
FAKE_GO_REWRITE=$chk/backend/x.go
export FAKE_GO_REWRITE
run_check
unset FAKE_GO_REWRITE
[ "$code" -eq 1 ] || bad "rewritten file: exited $code, want 1"
grep -qx 'tree: files were written during the run' "$work/chk.out" || bad "rewritten file: no 'files were written' line: $(cat "$work/chk.out")"
grep -qx 'check-backend: FAILED' "$work/chk.out" || bad "rewritten file: run did not end FAILED"

# An untouched run still passes.
run_check
[ "$code" -eq 0 ] || bad "untouched rerun exited $code: $(cat "$work/chk.out")"
grep -qx 'check-backend: all steps passed' "$work/chk.out" || bad "untouched rerun did not pass"

# A tree that hashes but cannot be fingerprinted at the end fails the run and
# says that, not that files were written. The fake stat fails once the fake go
# has made the marker file.
mkdir "$work/statbin"
cat >"$work/statbin/stat" <<'EOF'
#!/bin/sh
if [ -n "${FAKE_STAT_MARK:-}" ] && [ -e "$FAKE_STAT_MARK" ]; then
	echo "stat: injected failure" >&2
	exit 1
fi
exec /usr/bin/stat "$@"
EOF
chmod +x "$work/statbin/stat"
rm -f "$work/statmark"
FAKE_STAT_MARK=$work/statmark
FAKE_GO_WRITE=$work/statmark
PATH_SAVE=$PATH
PATH=$work/statbin:$PATH
export FAKE_STAT_MARK FAKE_GO_WRITE
run_check
PATH=$PATH_SAVE
unset FAKE_STAT_MARK FAKE_GO_WRITE
[ "$code" -eq 1 ] || bad "unfingerprintable end: exited $code, want 1"
grep -qx 'tree: could not fingerprint the tree at the end of the run' "$work/chk.out" || bad "unfingerprintable end: no 'could not fingerprint' line: $(cat "$work/chk.out")"
grep -q 'files were written' "$work/chk.out" && bad "unfingerprintable end: said files were written"
grep -qx 'check-backend: FAILED' "$work/chk.out" || bad "unfingerprintable end: run did not end FAILED"

# A tree that cannot be hashed at the end fails the run and says so.
FAKE_GO_RMGIT=$chk/.git
FAKE_GO_TMPDIR=$work
export FAKE_GO_RMGIT FAKE_GO_TMPDIR
run_check
unset FAKE_GO_RMGIT
[ "$code" -eq 1 ] || bad "unhashable end: exited $code, want 1"
grep -qx 'tree: could not hash the tree at the end of the run' "$work/chk.out" || bad "unhashable end: no 'could not hash' line: $(cat "$work/chk.out")"
grep -q 'changed during the run' "$work/chk.out" && bad "unhashable end: said the tree changed"
grep -qx 'check-backend: FAILED' "$work/chk.out" || bad "unhashable end: run did not end FAILED"

[ "$fail" -eq 0 ] && echo "tree-hash: all cases passed"
exit "$fail"
