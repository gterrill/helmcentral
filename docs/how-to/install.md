# Installing Helmcentral

The one-line installer covers most cases:

```sh
curl -fsSL https://raw.githubusercontent.com/gterrill/helmcentral/main/install.sh | sh
```

Then open `http://<this-machine>:8080/`. On first run Helmcentral searches the
network for a SignalK server and offers what it finds, without requiring manual
configuration.

The other installation options are described below.

> [!WARNING]
> **Authentication is off by default** (`auth.mode: none`). A fresh install's
> API can control connected equipment, including generator start/stop and CZone
> switching, with no login. Run it on a trusted boat LAN and do not port-forward
> it to the internet. For remote access, put it behind a VPN or an
> authenticating reverse proxy either way. See
> [Configuration](../reference/configuration.md) for turning login on.

## Install script

The script detects the platform, verifies the download against the
published checksums, installs to `/usr/local/bin`, creates `/var/lib/helmcentral`
for state, installs the reference plugin bundle, and enables a systemd service
on Linux.

**Re-run it any time to upgrade.** Settings and data are left alone.

Pin a version or change locations with `HELMCENTRAL_VERSION`,
`HELMCENTRAL_PREFIX` and `HELMCENTRAL_STATE_DIR`.

Useful afterwards:

```sh
systemctl status helmcentral
journalctl -u helmcentral -f
```

On macOS the script installs the binary and prints how to run it, with no
launchd service. On Windows, download the `.zip` from the
[releases page](https://github.com/gterrill/helmcentral/releases).

## Manual binary download

Take the archive for your platform from the
[releases page](https://github.com/gterrill/helmcentral/releases), then:

```sh
tar -xzf helmcentral_<version>_linux_arm64.tar.gz
sudo install -m0755 helmcentral /usr/local/bin/helmcentral

# State needs an explicit home, or it lands in the working directory.
sudo mkdir -p /var/lib/helmcentral
HELMCENTRAL_STATE_DIR=/var/lib/helmcentral \
  SETTINGS_FILE=/var/lib/helmcentral/settings.yaml \
  helmcentral
```

The archive also contains `packaging/helmcentral.service` if you want the
systemd unit, and `settings.example.yaml` as a starting config.

## Docker

The image is multi-arch (amd64, arm64, armv7), so it runs on a Raspberry Pi.
Take `docker-compose.yml` from the repository, then from the directory
containing it:

```sh
# 1. Add the reference plugins first. Compose bind-mounts ./plugins, and without
#    them there are no tide, weather, wave or warning providers. They are
#    deliberately not baked into the image, so you can add or update one without
#    repulling.
mkdir -p plugins && curl -fsSL \
  https://github.com/gterrill/helmcentral/releases/latest/download/helmcentral-plugins-<version>.tar.gz \
  | tar -xz -C plugins

# 2. Start it.
docker compose pull
docker compose up -d --force-recreate
```

The dashboard and API are served at <http://localhost:9091>. Change the left-hand side of
the port mapping to serve it elsewhere. State goes to `./backend-data`, created
on first run, so nothing needs to exist beforehand.

Stop with `docker compose down`.

## Upgrading across a breaking release

See [Upgrading](upgrading.md).
