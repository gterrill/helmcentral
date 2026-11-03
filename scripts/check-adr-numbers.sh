#!/bin/sh
# Fail when two files in docs/adr share a number.
#
#   sh scripts/check-adr-numbers.sh
#
# A file's number is its leading NNNN. Two branches that each take the next
# free number merge without a conflict and leave two files with one number;
# the next free number is printed so the later one can be renamed. Files that
# do not start with a number (a README, a template) are ignored.
set -eu

# Run from the repository root, wherever this was started.
root=$(cd "$(dirname "$0")/.." && pwd)
dir=$root/docs/adr

# Seven numbers were each taken twice before this check existed. The older file
# of each pair was renamed with "-a" after its number, so these seven files
# count as NNNN-a and sit beside their twin. A file name here is a one-off
# repair: the suffix is not for new ADRs, which take the next free number.
renamed="
0116-a-notes-are-documents-and-folders-are-the-manual.md
0131-a-mate-diagnoses-missing-and-stale-telemetry.md
0163-a-engine-cluster-reads-a-quiet-engine-as-off.md
0199-a-passage-map-satellite-imagery.md
0232-a-outside-assistants-reach-the-boat-through-tailscale-funnel.md
0252-a-one-time-format-and-tools-give-boat-time-labels.md
0254-a-nearby-lists-ais-vessels.md
"

if [ ! -d "$dir" ]; then
  echo "check-adr-numbers: $dir does not exist" >&2
  exit 1
fi

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

for f in "$dir"/*.md; do
  [ -e "$f" ] || continue
  name=${f##*/}
  num=$(printf '%s\n' "$name" | sed -n -E 's/^([0-9]{4})-.*/\1/p')
  [ -n "$num" ] || continue
  if printf '%s' "$renamed" | grep -qxF "$name"; then
    num=$num-a
  fi
  printf '%s %s\n' "$num" "$name"
done | sort >"$tmp/all"

if [ ! -s "$tmp/all" ]; then
  echo "check-adr-numbers: found no numbered ADRs in $dir" >&2
  exit 1
fi

awk '{ print $1 }' "$tmp/all" | uniq -d >"$tmp/dup"

if [ -s "$tmp/dup" ]; then
  while IFS= read -r num; do
    echo "check-adr-numbers: $num is used by more than one file:" >&2
    awk -v n="$num" -v d="docs/adr/" '$1 == n { print "  " d $2 }' "$tmp/all" >&2
  done <"$tmp/dup"
  next=$(awk '{ n = substr($1, 1, 4) + 0; if (n > max) max = n } END { printf "%04d", max + 1 }' "$tmp/all")
  echo "check-adr-numbers: the next free number is $next" >&2
  exit 1
fi

echo "check-adr-numbers: every ADR number in docs/adr is unique"
