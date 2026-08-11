#!/bin/sh
# Helmcentral installer.
#
#   curl -fsSL https://raw.githubusercontent.com/gterrill/helmcentral/main/install.sh | sh
#
# Downloads the release archive for this machine, verifies it against the
# published checksums, installs the binary plus the reference WASM plugin
# bundle, and (on Linux) enables a systemd service.
#
# Environment overrides:
#   HELMCENTRAL_VERSION   tag to install, e.g. v0.9.1 (default: latest release)
#   HELMCENTRAL_REPO      owner/name to install from
#   HELMCENTRAL_PREFIX    binary install dir (default: /usr/local/bin)
#   HELMCENTRAL_STATE_DIR state dir (default: /var/lib/helmcentral)

set -eu

REPO="${HELMCENTRAL_REPO:-gterrill/helmcentral}"
PREFIX="${HELMCENTRAL_PREFIX:-/usr/local/bin}"
STATE_DIR="${HELMCENTRAL_STATE_DIR:-/var/lib/helmcentral}"
SERVICE_USER=helmcentral

info() { printf '\033[1;34m==>\033[0m %s\n' "$1"; }
warn() { printf '\033[1;33mwarning:\033[0m %s\n' "$1" >&2; }
die() { printf '\033[1;31merror:\033[0m %s\n' "$1" >&2; exit 1; }

need() { command -v "$1" >/dev/null 2>&1 || die "'$1' is required but not installed"; }

need uname
need tar
if command -v curl >/dev/null 2>&1; then
  fetch() { curl -fsSL "$1" -o "$2"; }
  fetch_stdout() { curl -fsSL "$1"; }
elif command -v wget >/dev/null 2>&1; then
  fetch() { wget -qO "$2" "$1"; }
  fetch_stdout() { wget -qO- "$1"; }
else
  die "either curl or wget is required"
fi

# ── privilege escalation ─────────────────────────────────────────────────────
SUDO=""
if [ "$(id -u)" -ne 0 ]; then
  if command -v sudo >/dev/null 2>&1; then
    SUDO="sudo"
    info "Installing to $PREFIX and $STATE_DIR requires sudo."
  else
    die "run as root, or install sudo"
  fi
fi

# ── platform detection ───────────────────────────────────────────────────────
os=$(uname -s)
case "$os" in
  Linux) GOOS=linux ;;
  Darwin) GOOS=darwin ;;
  *) die "unsupported OS '$os'. Windows users: download the .zip from https://github.com/$REPO/releases" ;;
esac

machine=$(uname -m)
case "$machine" in
  x86_64 | amd64) GOARCH=amd64 ;;
  aarch64 | arm64) GOARCH=arm64 ;;
  armv7l | armv6l | arm) GOARCH=armv7 ;;
  *) die "unsupported architecture '$machine'" ;;
esac

if [ "$GOOS" = darwin ] && [ "$GOARCH" = armv7 ]; then
  die "unsupported architecture '$machine' on macOS"
fi

# A 64-bit Pi running the 32-bit Pi OS reports armv7l even though the CPU is
# arm64; that is genuinely the right binary to install, so no warning here.

# ── resolve version ──────────────────────────────────────────────────────────
VERSION="${HELMCENTRAL_VERSION:-}"
if [ -z "$VERSION" ]; then
  info "Resolving latest release of $REPO..."
  VERSION=$(fetch_stdout "https://api.github.com/repos/$REPO/releases/latest" \
    | sed -n 's/.*"tag_name": *"\([^"]*\)".*/\1/p' \
    | head -n1)
  [ -n "$VERSION" ] || die "could not determine the latest release; set HELMCENTRAL_VERSION"
fi
# Archive names carry the bare semver, tags carry the leading v.
BARE_VERSION="${VERSION#v}"

BASE_URL="https://github.com/$REPO/releases/download/$VERSION"
ARCHIVE="helmcentral_${BARE_VERSION}_${GOOS}_${GOARCH}.tar.gz"
PLUGIN_BUNDLE="helmcentral-plugins-${BARE_VERSION}.tar.gz"

TMP=$(mktemp -d)
cleanup() { rm -rf "$TMP"; }
trap cleanup EXIT INT TERM

# ── download and verify ──────────────────────────────────────────────────────
info "Downloading $ARCHIVE ($VERSION)..."
fetch "$BASE_URL/$ARCHIVE" "$TMP/$ARCHIVE" \
  || die "no release asset $ARCHIVE — check https://github.com/$REPO/releases"

if fetch "$BASE_URL/checksums.txt" "$TMP/checksums.txt" 2>/dev/null; then
  info "Verifying checksum..."
  expected=$(grep " $ARCHIVE\$" "$TMP/checksums.txt" | awk '{print $1}')
  [ -n "$expected" ] || die "$ARCHIVE missing from checksums.txt"
  if command -v sha256sum >/dev/null 2>&1; then
    actual=$(sha256sum "$TMP/$ARCHIVE" | awk '{print $1}')
  elif command -v shasum >/dev/null 2>&1; then
    actual=$(shasum -a 256 "$TMP/$ARCHIVE" | awk '{print $1}')
  else
    die "neither sha256sum nor shasum available to verify the download"
  fi
  [ "$actual" = "$expected" ] || die "checksum mismatch for $ARCHIVE (expected $expected, got $actual)"
else
  die "could not download checksums.txt; refusing to install an unverified binary"
fi

tar -xzf "$TMP/$ARCHIVE" -C "$TMP"
[ -f "$TMP/helmcentral" ] || die "archive did not contain a helmcentral binary"

# ── install binary ───────────────────────────────────────────────────────────
info "Installing helmcentral to $PREFIX..."
$SUDO mkdir -p "$PREFIX"
$SUDO install -m 0755 "$TMP/helmcentral" "$PREFIX/helmcentral"

# ── service user and state dir ───────────────────────────────────────────────
if [ "$GOOS" = linux ]; then
  if ! id "$SERVICE_USER" >/dev/null 2>&1; then
    info "Creating system user '$SERVICE_USER'..."
    if command -v useradd >/dev/null 2>&1; then
      $SUDO useradd --system --home-dir "$STATE_DIR" --shell /usr/sbin/nologin "$SERVICE_USER"
    elif command -v adduser >/dev/null 2>&1; then
      $SUDO adduser -S -H -h "$STATE_DIR" -s /sbin/nologin "$SERVICE_USER"
    else
      warn "no useradd/adduser found; the service will run as root"
      SERVICE_USER=root
    fi
  fi
fi

info "Preparing state directory $STATE_DIR..."
$SUDO mkdir -p "$STATE_DIR/data" "$STATE_DIR/cache" "$STATE_DIR/plugins"

# Never clobber an existing config on upgrade.
if [ ! -f "$STATE_DIR/settings.yaml" ] && [ -f "$TMP/settings.example.yaml" ]; then
  $SUDO cp "$TMP/settings.example.yaml" "$STATE_DIR/settings.yaml"
  info "Seeded $STATE_DIR/settings.yaml from the example."
fi

# ── reference plugins ────────────────────────────────────────────────────────
# WASM is architecture-independent, so one bundle serves every platform. These
# are the tide/weather/wave/warning providers; without them those widgets have
# no data source.
info "Installing reference plugins..."
if fetch "$BASE_URL/$PLUGIN_BUNDLE" "$TMP/$PLUGIN_BUNDLE" 2>/dev/null; then
  $SUDO tar -xzf "$TMP/$PLUGIN_BUNDLE" -C "$STATE_DIR/plugins"
else
  warn "plugin bundle $PLUGIN_BUNDLE not published for $VERSION; tide/weather/wave widgets will have no providers until you add plugins to $STATE_DIR/plugins"
fi

if [ "$GOOS" = linux ] && [ "$SERVICE_USER" != root ]; then
  $SUDO chown -R "$SERVICE_USER:$SERVICE_USER" "$STATE_DIR"
fi

# ── service ──────────────────────────────────────────────────────────────────
if [ "$GOOS" = linux ] && command -v systemctl >/dev/null 2>&1 && [ -f "$TMP/packaging/helmcentral.service" ]; then
  info "Installing systemd unit..."
  $SUDO install -m 0644 "$TMP/packaging/helmcentral.service" /etc/systemd/system/helmcentral.service
  $SUDO systemctl daemon-reload
  $SUDO systemctl enable --now helmcentral

  # Give it a moment to fail fast on a broken state dir rather than reporting
  # success for a unit that is already restarting.
  sleep 2
  if $SUDO systemctl is-active --quiet helmcentral; then
    started=yes
  else
    started=no
  fi
else
  started=skipped
fi

# ── report ───────────────────────────────────────────────────────────────────
port="${PORT:-8080}"
host=$(hostname 2>/dev/null || echo localhost)

echo
info "Helmcentral $VERSION installed."
echo

case "$started" in
  yes)
    echo "  Dashboard:  http://${host}:${port}/  (or http://<this-machine-ip>:${port}/)"
    echo "  Service:    systemctl status helmcentral"
    echo "  Logs:       journalctl -u helmcentral -f"
    ;;
  no)
    warn "the service was installed but is not running."
    echo "  Check:      journalctl -u helmcentral -n 50"
    ;;
  skipped)
    echo "  Run it:     HELMCENTRAL_STATE_DIR=$STATE_DIR SETTINGS_FILE=$STATE_DIR/settings.yaml $PREFIX/helmcentral"
    echo "  Dashboard:  http://localhost:${port}/"
    ;;
esac

cat <<EOF

  State dir:  $STATE_DIR
              Back up $STATE_DIR/data/secrets.key — without it the stored
              credentials cannot be recovered.

  First run:  Helmcentral searches your network for a SignalK server and
              offers what it finds. Open the dashboard to finish setup.

  SECURITY:   Helmcentral has no authentication and its API can control
              connected equipment. Run it on a trusted boat LAN only; do not
              port-forward it to the internet.
EOF
