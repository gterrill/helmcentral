#!/bin/sh
# Builds the bundled reference WASM plugins with TinyGo.
#
# Intended to run inside tinygo/tinygo:0.41.1 (pinned to match this repo's
# other TinyGo fixtures) from the repo root:
#
#   docker run --rm -v "$PWD:/src" -w /src tinygo/tinygo:0.41.1 \
#     sh packaging/build-plugins.sh docs/examples ./plugins
#
# That plain form works locally, where Docker Desktop's bind mounts ignore host
# ownership. It does NOT work on a Linux host whose checkout is owned by some
# other uid — the image runs as its own uid 1000 and cannot create $OUT in the
# mount. .github/workflows/release.yml therefore adds -u and a writable HOME;
# keep the two in step when changing either.
#
# WASM output is architecture-independent, so one build serves every release
# platform. Plugins are deliberately never baked into the binary or the image
# (ADR 0017: drop a plugin in without a rebuild) — they ship as a separate
# release bundle that the installer unpacks into <state dir>/plugins.

set -eu

SRC="${1:-docs/examples}"
OUT="${2:-./plugins}"

SRC=$(cd "$SRC" && pwd)
mkdir -p "$OUT"
OUT=$(cd "$OUT" && pwd)

# category:plugin-dir pairs, mirroring the layout the backend expects under
# PLUGINS_{TIDES,WEATHER,WAVES,FORECAST_WARNINGS}_DIR.
build() {
  category="$1"
  src_dir="$2"
  name="$3"

  echo "==> building $category/$name"
  mkdir -p "$OUT/$category"
  cd "$SRC/$src_dir"
  go mod download
  tinygo build -o "$OUT/$category/$name.wasm" -target wasip1 -buildmode c-shared .

  # Sidecars declare each plugin's network and secret allowances; the backend
  # refuses hosts and secrets that aren't listed, so they must travel with the
  # .wasm rather than being optional extras.
  for sidecar in allowed_hosts config allowed_secrets; do
    if [ -f "$name.$sidecar.json" ]; then
      cp "$name.$sidecar.json" "$OUT/$category/$name.$sidecar.json"
    fi
  done
}

build tides             tide-plugins/bom                     bom
build tides             tide-plugins/noaa                    noaa
build weather           weather-plugins/open-meteo           open-meteo
build weather           weather-plugins/weatherkit           weatherkit
build waves             wave-plugins/open-meteo-marine       open-meteo-marine
build forecast-warnings forecast-warnings-plugins/bom        bom
build forecast-warnings forecast-warnings-plugins/nws        nws

echo
echo "Plugins built into $OUT:"
ls -R "$OUT"
