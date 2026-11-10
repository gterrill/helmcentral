#!/bin/sh
# Runs a worktree's Go backend against a throwaway copy of the dev state, so a
# preview can show real data and be clicked through without touching the
# running dev stack or the main checkout's backend/data.
#
#   sh scripts/preview-backend.sh start NAME   (make preview-backend NAME=...)
#   sh scripts/preview-backend.sh stop NAME    (make preview-backend-stop NAME=...)
#
# NAME is the worktree suffix from `make worktree NAME=...`, so the worktree is
# ../<this checkout's directory name>-NAME. The backend listens on the first
# free port from 8091 up. State lives in ${TMPDIR:-/tmp}/helmcentral-preview-NAME
# and is rebuilt from scratch on every start.
#
# What the copy holds (HELMCENTRAL_STATE_DIR roots every runtime write there,
# ADR 0026): settings.yaml, the secrets store and its key, dashboard pages, the
# boat time zone, and helmcentral.sqlite taken with sqlite3 .backup, never cp,
# because the live database is in WAL mode. Push subscriptions and the
# notification queue are deleted from the copy, so the preview cannot send a
# phone a notification, and so are Mate's watches, so it cannot start a Mate
# run on the operator's OpenRouter key. `stop` deletes the whole copy. Alarm
# transports, login sessions, documents and the
# tile cache are not copied.
#
# The copy's SignalK address points at a black hole (127.0.0.1:9, as the E2E
# seed does): tiles show placeholders and nothing can reach the boat. LIVE=1
# keeps the real address, so the preview READS the boat's live data, and
# anything that publishes to SignalK (setting an anchor, for one) WRITES to the
# real server. The backend has no switch to stop that.
#
# The copy's influxdb.url is checked at /health before the backend starts, and
# the start stops if it does not answer. INFLUX_URL=http://<host>:8086 replaces
# the copied address (the main checkout's may be one only the boat's network
# reaches). INFLUX_URL=none runs without InfluxDB.
set -eu

action="${1:-}"
name="${2:-}"
if [ -z "$action" ] || [ -z "$name" ]; then
  echo "usage: sh scripts/preview-backend.sh start|stop NAME" >&2
  exit 2
fi

root="$(cd "$(dirname "$0")/.." && pwd)"
main="$(git -C "$root" worktree list --porcelain | awk '/^worktree /{print substr($0, 10); exit}')"
wt="$(dirname "$main")/$(basename "$main")-$name"
tmp="${TMPDIR:-/tmp}"
state="${tmp%/}/helmcentral-preview-$name"

stop_running() {
  if [ -f "$state/backend.pid" ]; then
    pid="$(cat "$state/backend.pid")"
    # Only our binary: a crashed preview's pid may since belong to anything.
    if kill -0 "$pid" 2>/dev/null && ps -p "$pid" -o comm= | grep -q "helmcentral$"; then
      kill "$pid" && echo "preview-backend: stopped pid $pid"
    fi
    rm -f "$state/backend.pid"
  fi
}

if [ "$action" = stop ]; then
  stop_running
  # The copy holds the decryptable secrets store; leave nothing behind.
  rm -rf "$state"
  exit 0
fi
[ "$action" = start ] || { echo "preview-backend: unknown action $action" >&2; exit 2; }

[ -d "$wt/backend" ] || { echo "preview-backend: no worktree at $wt (make worktree NAME=$name)" >&2; exit 1; }
command -v sqlite3 >/dev/null 2>&1 || { echo "preview-backend: sqlite3 is required" >&2; exit 1; }
src="$main/backend/data"
[ -f "$src/helmcentral.sqlite" ] || { echo "preview-backend: $src/helmcentral.sqlite is missing, so there is no dev state to copy" >&2; exit 1; }
[ -f "$main/settings.yaml" ] || { echo "preview-backend: $main/settings.yaml is missing" >&2; exit 1; }

stop_running
rm -rf "$state"
mkdir -p "$state/data"
# The copy holds the decryptable secrets store: a start that fails before the
# backend launches (build, settings rewrite, InfluxDB check) deletes it again.
# Cleared at the launch below, after which `stop` owns the directory.
trap 'rm -rf "$state"' EXIT

make -C "$wt" help-stage >/dev/null
(cd "$wt/backend" && go build -o "$state/helmcentral" .)

cp "$main/settings.yaml" "$state/settings.yaml"
for f in secrets.key secrets.sqlite dashboard-pages.json boat_zone.json; do
  if [ -f "$src/$f" ]; then cp "$src/$f" "$state/data/$f"; fi
done
sqlite3 -readonly "$src/helmcentral.sqlite" ".backup '$state/data/helmcentral.sqlite'"
sqlite3 "$state/data/helmcentral.sqlite" "DELETE FROM push_subscriptions; DELETE FROM notification_queue; DELETE FROM watches; DELETE FROM watch_runs; DELETE FROM watch_samples;"
# The WASM providers the dev stack builds into ./plugins; HELMCENTRAL_STATE_DIR
# roots the plugin directories too.
ln -s "$main/plugins" "$state/plugins"

if [ "${LIVE:-0}" != 1 ]; then
  awk '
    /^signalk:/ { in_sk = 1; print; next }
    /^[^ \t#]/ { in_sk = 0 }
    in_sk && /^[ \t]+address:/ { sub(/address:.*/, "address: 127.0.0.1") }
    in_sk && /^[ \t]+port:/ { sub(/port:.*/, "port: 9") }
    { print }
  ' "$state/settings.yaml" > "$state/settings.yaml.tmp"
  mv "$state/settings.yaml.tmp" "$state/settings.yaml"
  # A settings layout the awk does not know would leave the boat's address in
  # place; stop rather than run a preview that can write to it.
  if ! awk '/^signalk:/{s=1;next} /^[^ \t#]/{s=0} s && /^[ \t]+address: 127\.0\.0\.1$/{ok=1} END{exit !ok}' "$state/settings.yaml"; then
    echo "preview-backend: could not point SignalK at 127.0.0.1 in $state/settings.yaml; stopping (LIVE=1 to run against the boat)" >&2
    exit 1
  fi
fi

# The copied influxdb.url is often an address this machine cannot reach (a
# Tailscale one, say), and training then times out with nothing said. INFLUX_URL
# replaces it; either way an enabled InfluxDB must answer before the preview
# starts. INFLUX_URL=none turns InfluxDB off in the copy and skips the check.
# Applies under LIVE=1 too, which only keeps the SignalK address.
#
# Both helpers touch only the keys at the first indentation level under
# influxdb:, so a nested url: elsewhere is left alone, and neither passes the
# value through awk's sub() (which reads & and \ in a replacement).
influx_get() {
  KEY="$1" awk '
    /^influxdb:/ { in_in = 1; ind = -1; next }
    /^[^ \t#]/ { in_in = 0 }
    in_in && /^[ \t]+[^ \t#]/ {
      match($0, /^[ \t]+/)
      if (ind < 0) ind = RLENGTH
      if (RLENGTH == ind && index($0, ENVIRON["KEY"] ":") == ind + 1) {
        v = substr($0, ind + length(ENVIRON["KEY"]) + 2)
        sub(/[ \t]+#.*$/, "", v)
        gsub(/^[ \t]+|[ \t]+$/, "", v)
        gsub(/["\047]/, "", v)
        print v
        exit
      }
    }
  ' "$state/settings.yaml"
}
influx_set() {
  KEY="$1" VAL="$2" awk '
    /^influxdb:/ { in_in = 1; ind = -1; print; next }
    /^[^ \t#]/ { in_in = 0 }
    in_in && /^[ \t]+[^ \t#]/ {
      match($0, /^[ \t]+/)
      if (ind < 0) ind = RLENGTH
      if (RLENGTH == ind && index($0, ENVIRON["KEY"] ":") == ind + 1) {
        print substr($0, 1, ind) ENVIRON["KEY"] ": " ENVIRON["VAL"]
        next
      }
    }
    { print }
  ' "$state/settings.yaml" > "$state/settings.yaml.tmp"
  mv "$state/settings.yaml.tmp" "$state/settings.yaml"
}

if [ "${INFLUX_URL:-}" = none ]; then
  influx_set enabled false
  if [ "$(influx_get enabled)" != false ]; then
    echo "preview-backend: could not turn influxdb off in $state/settings.yaml; stopping" >&2
    exit 1
  fi
elif [ -n "${INFLUX_URL:-}" ]; then
  influx_set url "$INFLUX_URL"
  if [ "$(influx_get url)" != "$INFLUX_URL" ]; then
    echo "preview-backend: could not set influxdb.url to $INFLUX_URL in $state/settings.yaml; stopping" >&2
    exit 1
  fi
fi
# Anything but false (any case) is enabled, so an odd spelling cannot skip the check.
enabled="$(influx_get enabled | tr '[:upper:]' '[:lower:]')"
if [ -n "$enabled" ] && [ "$enabled" != false ]; then
  influx_url="$(influx_get url)"
  if ! curl -fsS --max-time 3 "${influx_url%/}/health" >/dev/null 2>&1; then
    echo "preview-backend: InfluxDB at ${influx_url:-<no influxdb.url set>} does not answer, so training and history would time out. Set INFLUX_URL=http://<reachable-host>:8086 and start again, or INFLUX_URL=none to run without InfluxDB" >&2
    exit 1
  fi
fi

port=8091
while lsof -nP -iTCP:"$port" -sTCP:LISTEN >/dev/null 2>&1; do port=$((port + 1)); done

trap - EXIT

# From the worktree, so `make prune-merged` sees the process and keeps the
# worktree while it runs.
cd "$wt/backend"
PORT="$port" HELMCENTRAL_STATE_DIR="$state" SETTINGS_FILE="$state/settings.yaml" \
  nohup "$state/helmcentral" >"$state/backend.log" 2>&1 &
echo "$!" >"$state/backend.pid"

i=0
until curl -fsS "http://localhost:$port/api/health" >/dev/null 2>&1; do
  i=$((i + 1))
  if [ "$i" -gt 60 ] || ! kill -0 "$(cat "$state/backend.pid")" 2>/dev/null; then
    kill "$(cat "$state/backend.pid")" 2>/dev/null || true
    echo "preview-backend: backend did not come up, see $state/backend.log" >&2
    exit 1
  fi
  sleep 0.5
done

echo "preview-backend: pid $(cat "$state/backend.pid"), log $state/backend.log"
if [ "${LIVE:-0}" = 1 ]; then
  echo "preview-backend: LIVE SignalK. Reads the boat; publishing (anchor set) writes to it."
else
  echo "preview-backend: SignalK points at 127.0.0.1:9 (LIVE=1 for the boat's data)"
fi
echo "VITE_API_PROXY_TARGET=http://localhost:$port"
echo "stop with: make preview-backend-stop NAME=$name"
