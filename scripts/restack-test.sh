#!/bin/sh
# Cases for scripts/restack.sh and the stacked-branch record worktree.sh keeps:
# `sh scripts/restack-test.sh`. Everything runs in throwaway repositories under
# mktemp -d (a bare "origin", a pusher clone and a main checkout), never the
# real repository, its branches or its worktrees.
set -u
root=$(cd "$(dirname "$0")/.." && pwd)
restack=$root/scripts/restack.sh
worktree=$root/scripts/worktree.sh
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
fail=0

bad() {
	echo "FAIL: $1"
	fail=1
}

unset GIT_DIR GIT_WORK_TREE GIT_INDEX_FILE
export GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_NOSYSTEM=1
export GIT_AUTHOR_NAME=test GIT_AUTHOR_EMAIL=test@example.com
export GIT_COMMITTER_NAME=test GIT_COMMITTER_EMAIL=test@example.com
export GIT_EDITOR=true
export HELMCENTRAL_WORKTREE_LOCK_HELD=1

# commit_file <repo> <file> <content> <message>
commit_file() {
	printf '%s\n' "$3" >"$1/$2"
	git -C "$1" add "$2"
	git -C "$1" commit -q -m "$4"
}

# The bare origin, a pusher clone that moves branches on it, and the "main
# checkout" worktree.sh runs in, with the lock helper and a frontend/ directory
# in the tree as the real repository has.
origin=$work/origin.git
pusher=$work/pusher
main=$work/h
git init -q --bare -b main "$origin"
git init -q -b main "$pusher"
git -C "$pusher" remote add origin "$origin"
mkdir -p "$pusher/scripts/lib" "$pusher/frontend"
cp "$root/scripts/lib/machine-lock.sh" "$pusher/scripts/lib/machine-lock.sh"
printf 'x\n' >"$pusher/frontend/.keep"
printf 'frontend/node_modules\n' >"$pusher/.gitignore"
git -C "$pusher" add .gitignore scripts frontend
git -C "$pusher" commit -q -m "seed"
commit_file "$pusher" conflict.txt orig "conflict file"
git -C "$pusher" push -q origin main
git clone -q "$origin" "$main"

# newbase <branch> <file>: a branch off origin/main on origin with one commit.
newbase() {
	git -C "$pusher" fetch -q origin
	git -C "$pusher" checkout -q -B "$1" origin/main
	commit_file "$pusher" "$2" "$1 base" "$1: base commit"
	git -C "$pusher" push -q -f origin "$1"
}

# stack <name> <base>: a worktree stacked on origin/<base> with two own commits.
stack() {
	sh "$worktree" "$1" "$main" "$2" >"$work/stack.out" 2>&1 || bad "worktree.sh $1 $2 failed: $(cat "$work/stack.out")"
	commit_file "$work/h-$1" "own-$1-1.txt" one "$1: own one"
	commit_file "$work/h-$1" "own-$1-2.txt" two "$1: own two"
}

# run <args>: restack.sh with the output in $work/r.out and $work/r.err, the
# exit code in $code.
run() {
	sh "$restack" "$@" >"$work/r.out" 2>"$work/r.err"
	code=$?
}

# ahead <repo> <ref>: how many commits HEAD has beyond <ref>.
ahead() {
	git -C "$1" rev-list --count "$2..HEAD"
}

cfg() {
	git -C "$1" config --get "branch.$2.$3"
}

# 1. worktree.sh with a stack-on branch records the branch and the mark, and says
# so; without one it records nothing.
newbase b1 b1.txt
sh "$worktree" plain "$main" >"$work/w.out" 2>&1 || bad "plain worktree.sh failed: $(cat "$work/w.out")"
cfg "$main" plain stackedon >/dev/null && bad "a plain worktree recorded stackedon"
cfg "$main" plain basemark >/dev/null && bad "a plain worktree recorded basemark"
grep -q 'stacked on' "$work/w.out" && bad "a plain worktree said it was stacked"
stack s1 b1
b1sha=$(git -C "$pusher" rev-parse origin/b1)
[ "$(cfg "$main" s1 stackedon)" = b1 ] || bad "stackedon was '$(cfg "$main" s1 stackedon)', want b1"
[ "$(cfg "$main" s1 basemark)" = "$b1sha" ] || bad "basemark was '$(cfg "$main" s1 basemark)', want $b1sha"
short=$(git -C "$pusher" rev-parse --short "$b1sha")
sh "$worktree" probe "$main" b1 >"$work/w.out" 2>&1
grep -qxF "stacked on b1 at $short" "$work/w.out" || bad "worktree.sh output was '$(cat "$work/w.out")', want 'stacked on b1 at $short'"
[ "$(git -C "$work/h-probe" rev-parse HEAD)" = "$b1sha" ] || bad "the stacked worktree did not start at origin/b1"

# 2. --mark prints the mark.
run "$work/h-s1" --mark
[ "$code" -eq 0 ] || bad "--mark exited $code"
[ "$(cat "$work/r.out")" = "$b1sha" ] || bad "--mark printed '$(cat "$work/r.out")', want $b1sha"
run "$work/h-plain" --mark
[ "$code" -eq 2 ] || bad "--mark on a plain branch exited $code, want 2"

# 3. The base gets a new commit: restack replays only the two own commits.
git -C "$pusher" checkout -q b1
commit_file "$pusher" b1-more.txt more "b1: second base commit"
git -C "$pusher" push -q origin b1
newtip=$(git -C "$pusher" rev-parse b1)
run "$work/h-s1" b1
[ "$code" -eq 0 ] || bad "restack onto a moved base exited $code: $(cat "$work/r.err")"
[ "$(ahead "$work/h-s1" origin/b1)" -eq 2 ] || bad "after restack the branch is $(ahead "$work/h-s1" origin/b1) ahead of origin/b1, want 2"
[ -f "$work/h-s1/b1-more.txt" ] || bad "the new base commit is missing after restack"
[ "$(cfg "$main" s1 basemark)" = "$newtip" ] || bad "basemark was '$(cfg "$main" s1 basemark)' after restack, want $newtip"
grep -q "$newtip" "$work/r.out" || bad "restack did not print the new mark: '$(cat "$work/r.out")'"

# 4. The base is force-pushed (rewritten): none of its old commits come along.
newbase b2 b2.txt
git -C "$pusher" checkout -q b2
commit_file "$pusher" b2-second.txt second "b2: old second commit"
git -C "$pusher" push -q origin b2
stack s2 b2
git -C "$pusher" reset -q --hard HEAD~1
commit_file "$pusher" b2-rewritten.txt rewritten "b2: rewritten second commit"
git -C "$pusher" push -q -f origin b2
run "$work/h-s2" b2
[ "$code" -eq 0 ] || bad "restack onto a rewritten base exited $code: $(cat "$work/r.err")"
[ "$(ahead "$work/h-s2" origin/b2)" -eq 2 ] || bad "after a force-push the branch is $(ahead "$work/h-s2" origin/b2) ahead, want 2"
git -C "$work/h-s2" log --format=%s origin/b2..HEAD | grep -q 'b2: old second commit' && bad "the base's old commit was replayed"
git -C "$work/h-s2" log --format=%s origin/b2..HEAD | grep -q 'b2: base commit' && bad "the base's first commit was replayed"
[ -f "$work/h-s2/b2-rewritten.txt" ] || bad "the rewritten base commit is missing"
[ -f "$work/h-s2/b2-second.txt" ] && bad "the old base file is still present"

# 5. The base is merged into main with a merge commit: restack onto main leaves
# exactly the own commits ahead of origin/main.
newbase b3 b3.txt
stack s3 b3
git -C "$pusher" checkout -q main
git -C "$pusher" merge -q --no-ff -m "Merge pull request #1 from b3" origin/b3
git -C "$pusher" push -q origin main
run "$work/h-s3" main
[ "$code" -eq 0 ] || bad "restack onto main exited $code: $(cat "$work/r.err")"
[ "$(ahead "$work/h-s3" origin/main)" -eq 2 ] || bad "after the merge the branch is $(ahead "$work/h-s3" origin/main) ahead of origin/main, want 2"
[ "$(cfg "$main" s3 stackedon)" = main ] || bad "stackedon was '$(cfg "$main" s3 stackedon)' after restack onto main"
[ "$(cfg "$main" s3 basemark)" = "$(git -C "$pusher" rev-parse main)" ] || bad "basemark is not origin/main after restack onto main"

# 6. A dirty tree is refused, untouched.
printf 'dirty\n' >"$work/h-s3/own-s3-1.txt"
before=$(git -C "$work/h-s3" rev-parse HEAD)
run "$work/h-s3" main
[ "$code" -eq 2 ] || bad "dirty tree exited $code, want 2"
grep -q 'commit your work on the branch first; it is not pushed until ship step 6' "$work/r.err" || bad "dirty message was '$(cat "$work/r.err")'"
[ "$(git -C "$work/h-s3" rev-parse HEAD)" = "$before" ] || bad "the dirty tree was rebased anyway"
git -C "$work/h-s3" checkout -q -- own-s3-1.txt

# 7. A branch with no mark is refused.
run "$work/h-plain" main
[ "$code" -eq 2 ] || bad "no mark exited $code, want 2"
grep -q 'not a stacked branch' "$work/r.err" || bad "no-mark message was '$(cat "$work/r.err")'"

# 8. A conflict keeps the old mark, leaves the rebase in progress and exits 1;
# after resolving, --set-mark records the new one.
newbase b4 b4.txt
sh "$worktree" s4 "$main" b4 >"$work/stack.out" 2>&1
commit_file "$work/h-s4" conflict.txt own "s4: own change to conflict.txt"
oldmark=$(cfg "$main" s4 basemark)
git -C "$pusher" checkout -q b4
commit_file "$pusher" conflict.txt theirs "b4: other change to conflict.txt"
git -C "$pusher" push -q origin b4
onto4=$(git -C "$pusher" rev-parse b4)
run "$work/h-s4" b4
[ "$code" -eq 1 ] || bad "conflict exited $code, want 1"
[ "$(cfg "$main" s4 basemark)" = "$oldmark" ] || bad "the mark moved on a conflict"
[ -d "$(git -C "$work/h-s4" rev-parse --git-path rebase-merge)" ] || bad "the rebase is not left in progress"
grep -q 'git rebase --continue' "$work/r.err" "$work/r.out" || bad "conflict message lacks 'git rebase --continue'"
grep -q -- '--set-mark b4' "$work/r.err" "$work/r.out" || bad "conflict message lacks '--set-mark b4'"
printf 'resolved\n' >"$work/h-s4/conflict.txt"
git -C "$work/h-s4" add conflict.txt
[ "$(cfg "$main" s4 pendingmark)" = "$onto4" ] || bad "pendingmark was '$(cfg "$main" s4 pendingmark)' on a conflict, want $onto4"
git -C "$work/h-s4" rebase --continue >/dev/null 2>&1 || bad "could not continue the rebase"
# The base moves again while the operator resolves: --set-mark must still record
# the commit the rebase went onto.
commit_file "$pusher" b4-later.txt later "b4: later commit"
git -C "$pusher" push -q origin b4
git -C "$work/h-s4" fetch -q origin
[ "$(git -C "$work/h-s4" rev-parse origin/b4)" != "$onto4" ] || bad "origin/b4 did not move"
run "$work/h-s4" --set-mark b4
[ "$code" -eq 0 ] || bad "--set-mark exited $code: $(cat "$work/r.err")"
[ "$(cfg "$main" s4 basemark)" = "$onto4" ] || bad "--set-mark left basemark '$(cfg "$main" s4 basemark)', want $onto4"
[ "$(cfg "$main" s4 stackedon)" = b4 ] || bad "--set-mark left stackedon '$(cfg "$main" s4 stackedon)'"
cfg "$main" s4 pendingmark >/dev/null && bad "--set-mark left pendingmark set"
[ "$(ahead "$work/h-s4" "$onto4")" -eq 1 ] || bad "after the resolved rebase the branch is $(ahead "$work/h-s4" "$onto4") ahead, want 1"
run "$work/h-s4" --set-mark b4
[ "$code" -eq 2 ] || bad "--set-mark with no pendingmark exited $code, want 2"
grep -q 'no pending' "$work/r.err" || bad "no-pendingmark message was '$(cat "$work/r.err")'"

# 9. Mid-rebase, --set-mark and --mark say to finish the rebase; after an abort
# --set-mark is refused because the onto commit is not in the branch.
newbase b5 b5.txt
sh "$worktree" s5 "$main" b5 >"$work/stack.out" 2>&1
commit_file "$work/h-s5" conflict.txt own5 "s5: own change to conflict.txt"
git -C "$pusher" checkout -q b5
commit_file "$pusher" conflict.txt theirs5 "b5: other change to conflict.txt"
git -C "$pusher" push -q origin b5
run "$work/h-s5" b5
[ "$code" -eq 1 ] || bad "second conflict exited $code, want 1"
run "$work/h-s5" --set-mark b5
[ "$code" -eq 2 ] || bad "--set-mark mid-rebase exited $code, want 2"
grep -q 'finish the rebase first (git rebase --continue)' "$work/r.err" || bad "mid-rebase --set-mark message was '$(cat "$work/r.err")'"
run "$work/h-s5" --mark
[ "$code" -eq 2 ] || bad "--mark mid-rebase exited $code, want 2"
grep -q 'finish the rebase first (git rebase --continue)' "$work/r.err" || bad "mid-rebase --mark message was '$(cat "$work/r.err")'"
git -C "$work/h-s5" rebase --abort
run "$work/h-s5" --set-mark b5
[ "$code" -eq 2 ] || bad "--set-mark after an abort exited $code, want 2"
grep -q 'not in' "$work/r.err" || bad "after-abort message was '$(cat "$work/r.err")'"

[ "$fail" -eq 0 ] && echo "restack: all cases passed"
exit "$fail"
