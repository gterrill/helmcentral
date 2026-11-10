#!/bin/sh
# Runs one read-only Flux script against the boat's real InfluxDB and prints
# the result as CSV, so a query is run before it is handed to anyone or shipped.
#
#   sh scripts/flux.sh query.flux
#   sh scripts/flux.sh - <<'EOF'
#   from(bucket: "SignalK_Data") |> range(start: -5m) |> limit(n: 3)
#   EOF
#   make flux FILE=query.flux
#
# The token lives in the encrypted secrets store, so the backend's `flux`
# subcommand reads it the way the server does. Settings and secrets come from
# the MAIN checkout (found as scripts/preview-backend.sh finds it), so this
# works from any worktree. SETTINGS_FILE and HELMCENTRAL_STATE_DIR (whose data/
# holds secrets.sqlite and secrets.key) override that. INFLUX_URL replaces the
# settings address, which is often a Tailscale one this machine cannot reach:
# INFLUX_URL=http://<lan-host>:8086.
#
# The secrets store is copied to a temporary directory first (sqlite3 .backup,
# never cp, as in preview-backend.sh), so the running server's files are never
# opened for writing. The copy is deleted on exit.
#
# Scripts containing to, delete, post, requests or api( are refused. That is a
# best-effort guard against accidental writes, NOT a guarantee: the query runs
# with the server's write-capable token, so do not feed it untrusted Flux. A
# failure (no settings, no secrets, no token, InfluxDB unreachable) is reported
# and exits non-zero;
# nothing is retried or substituted. The query gets 120 s.
set -eu

if [ "$#" -ne 1 ] || [ -z "$1" ]; then
  echo "usage: sh scripts/flux.sh <file|->" >&2
  exit 2
fi
script="$1"

root="$(cd "$(dirname "$0")/.." && pwd)"
main="$(git -C "$root" worktree list --porcelain | awk '/^worktree /{print substr($0, 10); exit}')"
tmp="${TMPDIR:-/tmp}"
tmp="${tmp%/}"

settings="${SETTINGS_FILE:-$main/settings.yaml}"
src="${HELMCENTRAL_STATE_DIR:-$main/backend}/data"
[ -f "$settings" ] || { echo "flux: settings file not found: $settings" >&2; exit 1; }
[ -f "$src/secrets.sqlite" ] || { echo "flux: secrets database not found: $src/secrets.sqlite" >&2; exit 1; }
[ -f "$src/secrets.key" ] || { echo "flux: secrets key not found: $src/secrets.key" >&2; exit 1; }
command -v sqlite3 >/dev/null 2>&1 || { echo "flux: sqlite3 is required" >&2; exit 1; }

work="$(mktemp -d "$tmp/helmcentral-flux-state.XXXXXX")"
trap 'rm -rf "$work"' EXIT INT TERM
mkdir "$work/data"

# Built into this run's own directory, so worktrees running at once cannot
# overwrite each other's binary. Go's build cache keeps that quick.
go -C "$root/backend" build -o "$work/helmcentral" .
sqlite3 -readonly "$src/secrets.sqlite" ".backup '$work/data/secrets.sqlite'"
cp "$src/secrets.key" "$work/data/secrets.key"
chmod 600 "$work/data/secrets.key"

# Not exec: the trap must run to delete the copy. The exit code passes through.
rc=0
# The explicit paths win over any SECRETS_*_PATH in the caller's environment,
# which could otherwise send the binary to the real store.
SETTINGS_FILE="$settings" HELMCENTRAL_STATE_DIR="$work" \
  SECRETS_DB_PATH="$work/data/secrets.sqlite" SECRETS_KEY_PATH="$work/data/secrets.key" \
  "$work/helmcentral" flux "$script" || rc=$?
exit "$rc"
