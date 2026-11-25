#!/bin/sh
# Cases for gofmt-edited.sh: `sh scripts/hooks/gofmt-edited-test.sh`.
set -u
hook=$(dirname "$0")/gofmt-edited.sh
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
fail=0

bad() {
	echo "FAIL: $1"
	fail=1
}

# A stand-in Helmcentral checkout: a git repo with scripts/gofmt-check.sh.
hc=$work/hc
mkdir -p "$hc/scripts" "$hc/backend"
: >"$hc/scripts/gofmt-check.sh"
git -C "$hc" init -q
b=$hc/backend

# run_path <file>: runs the hook on an edit of <file> and sets $code.
run_path() {
	jq -n --arg f "$1" '{tool_input: {file_path: $f}}' | sh "$hook" >/dev/null 2>&1
	code=$?
}

# A badly formatted Go file is rewritten.
printf 'package x\nfunc  F( ) {\nreturn\n}\n' >"$b/ugly.go"
run_path "$b/ugly.go"
[ "$code" -eq 0 ] || bad "ugly.go: exit $code, want 0"
[ "$(gofmt -l "$b/ugly.go")" = "" ] || bad "ugly.go was not formatted"

# A reformatted file is reported to the agent as PostToolUse additionalContext.
printf 'package x\nfunc  G( ) {\nreturn\n}\n' >"$b/ugly2.go"
jq -n --arg f "$b/ugly2.go" '{tool_input: {file_path: $f}}' | sh "$hook" >"$work/ugly2.out" 2>/dev/null
code=$?
[ "$code" -eq 0 ] || bad "ugly2.go: exit $code, want 0"
want=$(jq -n --arg p "$b/ugly2.go" '{hookSpecificOutput:{hookEventName:"PostToolUse",additionalContext:("gofmt reformatted " + $p + "; read it again before the next Edit to it.")}}')
[ "$(jq -c . "$work/ugly2.out" 2>/dev/null)" = "$(printf '%s' "$want" | jq -c .)" ] || bad "ugly2.go: stdout was '$(cat "$work/ugly2.out")'"

# A file that is already formatted prints nothing.
printf 'package x\n\nfunc H() {}\n' >"$b/clean.go"
jq -n --arg f "$b/clean.go" '{tool_input: {file_path: $f}}' | sh "$hook" >"$work/clean.out" 2>/dev/null
[ -s "$work/clean.out" ] && bad "clean.go: printed '$(cat "$work/clean.out")'"

# Vendored code under third_party/ is left as upstream wrote it.
mkdir -p "$b/third_party/lib"
printf 'package x\nfunc  F( ) {\nreturn\n}\n' >"$b/third_party/lib/v.go"
cp "$b/third_party/lib/v.go" "$work/v.before"
jq -n --arg f "$b/third_party/lib/v.go" '{tool_input: {file_path: $f}}' | sh "$hook" >"$work/v.out" 2>/dev/null
code=$?
[ "$code" -eq 0 ] || bad "third_party: exit $code, want 0"
cmp -s "$b/third_party/lib/v.go" "$work/v.before" || bad "third_party file was formatted"
[ -s "$work/v.out" ] && bad "third_party: printed '$(cat "$work/v.out")'"

# A checkout that itself sits under a directory named third_party is still
# formatted: only backend/third_party/ is vendored.
vt=$work/third_party/hc
mkdir -p "$vt/scripts" "$vt/backend"
: >"$vt/scripts/gofmt-check.sh"
git -C "$vt" init -q
printf 'package x\nfunc  F( ) {\nreturn\n}\n' >"$vt/backend/u.go"
run_path "$vt/backend/u.go"
[ "$(gofmt -l "$vt/backend/u.go")" = "" ] || bad "checkout under a third_party directory was not formatted"

# A Go file in a git repo that is not Helmcentral (no scripts/gofmt-check.sh)
# is left alone.
other=$work/other
mkdir "$other"
git -C "$other" init -q
printf 'package x\nfunc  F( ) {\nreturn\n}\n' >"$other/o.go"
cp "$other/o.go" "$work/o.before"
jq -n --arg f "$other/o.go" '{tool_input: {file_path: $f}}' | sh "$hook" >"$work/o.out" 2>/dev/null
code=$?
[ "$code" -eq 0 ] || bad "other repo: exit $code, want 0"
cmp -s "$other/o.go" "$work/o.before" || bad "a Go file in another repo was formatted"
[ -s "$work/o.out" ] && bad "other repo: printed '$(cat "$work/o.out")'"

# A Go file outside backend/ in a Helmcentral checkout is left alone.
mkdir "$hc/tools"
printf 'package x\nfunc  F( ) {\nreturn\n}\n' >"$hc/tools/t.go"
cp "$hc/tools/t.go" "$work/t.before"
run_path "$hc/tools/t.go"
[ "$code" -eq 0 ] || bad "outside backend: exit $code, want 0"
cmp -s "$hc/tools/t.go" "$work/t.before" || bad "a Go file outside backend/ was formatted"

# A Go file outside any git repo is left alone.
mkdir "$work/plain"
printf 'package x\nfunc  F( ) {\nreturn\n}\n' >"$work/plain/p.go"
cp "$work/plain/p.go" "$work/p.before"
run_path "$work/plain/p.go"
[ "$code" -eq 0 ] || bad "no repo: exit $code, want 0"
cmp -s "$work/plain/p.go" "$work/p.before" || bad "a Go file outside any repo was formatted"

# A file that is not Go is left alone.
printf 'func  F( ) {\n' >"$b/notes.txt"
cp "$b/notes.txt" "$work/notes.before"
run_path "$b/notes.txt"
[ "$code" -eq 0 ] || bad "notes.txt: exit $code, want 0"
cmp -s "$b/notes.txt" "$work/notes.before" || bad "notes.txt was changed"

# A Go file that does not parse exits 0 and is left as written.
printf 'package x\nfunc F( {\n' >"$b/broken.go"
cp "$b/broken.go" "$work/broken.before"
run_path "$b/broken.go"
[ "$code" -eq 0 ] || bad "broken.go: exit $code, want 0"
jq -n --arg f "$b/broken.go" '{tool_input: {file_path: $f}}' | sh "$hook" >"$work/broken.out" 2>/dev/null
[ -s "$work/broken.out" ] && bad "broken.go: printed '$(cat "$work/broken.out")'"
cmp -s "$b/broken.go" "$work/broken.before" || bad "broken.go was changed"

# A Go file that does not exist exits 0.
run_path "$b/missing.go"
[ "$code" -eq 0 ] || bad "missing.go: exit $code, want 0"

# Input without a file_path exits 0.
printf '%s' '{"tool_input":{}}' | sh "$hook" >/dev/null 2>&1
code=$?
[ "$code" -eq 0 ] || bad "empty tool_input: exit $code, want 0"

[ "$fail" -eq 0 ] && echo "gofmt-edited: all cases passed"
exit "$fail"
