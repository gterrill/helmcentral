#!/bin/sh
# Fails, listing the files, when any Go file under backend/ is not gofmt-clean.
# third_party is vendored code kept as upstream wrote it. Used by `make check`
# and CI, so the two agree.
set -eu
cd "$(dirname "$0")/../backend"
# --others takes in new files not yet committed, so a builder's check sees
# them; the -f test drops tracked files deleted but not yet committed.
files=$(git ls-files --cached --others --exclude-standard '*.go' ':!:third_party/' |
	while read -r f; do if [ -f "$f" ]; then echo "$f"; fi; done)
# gofmt with no file arguments reads stdin, so an empty list must stop here.
[ -n "$files" ] || { echo "gofmt: no Go files found under backend/" >&2; exit 1; }
# shellcheck disable=SC2086 # file names in this repo have no spaces
out=$(gofmt -l $files)
if [ -n "$out" ]; then
	echo "gofmt: these files need formatting (run gofmt -w <file>):"
	echo "$out"
	exit 1
fi
