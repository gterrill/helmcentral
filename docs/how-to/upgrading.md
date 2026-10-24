# Upgrading

Re-running `install.sh` upgrades in place and preserves your settings and data.
The notes below cover the releases that required manual steps.

If you are installing fresh, none of this applies.

## Checking what you are running

The release version sits in the footer of the menu sidebar, below the
navigation items. Use it to confirm an upgrade. If the footer still reads the
old tag after a restart, the container is running the previous image.

The version comes from the running server, not the browser. Hover over it for
the full build stamp including the git revision, which is what to quote when
reporting a problem. A build made outside the release workflow reports `dev`
or `(devel)` instead of a tag.

You can also read the same values from the shell:

```sh
curl -s http://localhost:8080/api/health
```

## Open pages pick up the new version

Helmcentral checks for a newer version every five minutes while a page is on
screen, and again when you return to it. When it finds one, a phone, tablet or
laptop tells you and leaves the page alone. An "Update available" message with
a **Reload** button appears, and the version line at the foot of the menu
changes to "Update available · Reload". Nothing reloads until you press one of
them. If you have something unsaved (a Settings change, a note you're
writing, a service log entry, an inventory edit or a Mate message you haven't
sent), you are asked "You have unsaved changes. Reload anyway?" first.

A screen nobody is operating picks up the new version by itself. That means
the wall display and the dashboard of a Home Screen app on a phone or tablet.
They reload as soon as they see the update. A Home Screen app showing
Documents, Settings or any page other than a dashboard counts as in use and
asks like a browser tab does. So does a Home Screen dashboard with a sheet or
dialog open over it, such as Mate, Help or a tile's settings. A dashboard in
the browser's full screen mode also asks, because a reload would drop it out
of full screen. If a Home Screen dashboard has unsaved work open, it waits and
reloads as soon as you save or close it.

After the new version loads, a short "Helmcentral updated to" message names it,
with a **Release notes** button. Each browser remembers the last version it
showed, so the message appears however the page came back: Reload, a manual
refresh, a new tab or reopening the Home Screen app. A browser seeing
Helmcentral for the first time stays quiet. The wall display shows the same message for a
few seconds across the bottom of the screen.

If a screen won't open after an upgrade because it has changed since the page
loaded, you see "This screen needs the update. Reload to open it." If the
update isn't the cause, you see "Couldn't load this screen. Check the
connection." Both messages have a **Reload** button. If a check can't reach
Helmcentral, nothing happens and it tries again at the next one. Reloading the
page by hand always works.

## SignalK delegated authentication (opt-in)

This is not a breaking change, and no action is required. `auth.mode` defaults
to `none` (no authentication, as in every earlier release), so an upgrade
doesn't introduce a login requirement.

**Recommended:** once your SignalK server has security enabled, set
`auth.mode: signalk` in `settings.yaml`. Helmcentral then answers nothing but
the login screen until you log in. It checks at startup and won't start in
`auth.mode: signalk` against a server with security switched off, so turn on
SignalK's own security first. See the
[README's Configuration section](../../README.md#configuration).

## Weather and waves became plugins

This applies if you previously ran a version with the hardcoded WeatherKit /
Open-Meteo-marine integration.

- Delete the orphaned cache files. Weather and wave caching moved to
  per-plugin files:

  ```sh
  rm -f cache/weather_today_cache.json cache/weather_forecast_cache.json
  ```

  The replacements are `cache/weather_wasm_*_cache.json` and
  `cache/wave_wasm_*_cache.json`, one pair per installed provider.

- **If you relied on WeatherKit:** the default provider on upgrade is
  Open-Meteo (keyless). Paste the four WeatherKit credentials into the Apple
  WeatherKit card's settings and select "Apple WeatherKit" under Settings →
  Weather.

Both providers remain keyless by default, so an install that never touches
Settings continues working.

## Marine warnings became forecast warnings

- The separate Marine Warnings feature is gone. Warnings now show alongside
  the rest of the forecast, as one more provider category.
- `cache/bom_marine_warnings_cache.json` is orphaned and can be deleted.
  Warnings are now cached per-plugin under
  `cache/forecast_warnings_wasm_*_cache.json`.
- The default BOM plugin needs no environment variables. It is keyless and
  reads BOM's public anonymous FTP mirror.
- Anything that fetched marine warnings directly from Helmcentral, such as a
  script or a third-party display, has to be pointed at the new address. See
  [Upgrade notes](../developers/configuration.md#upgrade-notes).

## Tides became plugin-only

The last built-in tide provider (Storm Glass) was removed. There is no built-in
tide provider and no fallback. Install a tide plugin for your region and set
`ui.tide_provider` in Settings. Until you do, the forecast page's tide section
shows an error naming what is missing.

Storm Glass was a worldwide, position-based provider. A global model standing
in for a missing local station network gives tide times that look authoritative
but are wrong for local waters, so it was removed instead of left as a
fallback. See [docs/reference/plugins.md](../reference/plugins.md).

## Anchor watch state moved to data/

The anchor watch and its map pins now save under `data/`, alongside the rest
of your settings, instead of `cache/`. On a Docker install this is what lets
the watch survive an update. On every install, this upgrade drops a running
watch one last time. The release's changelog entry has the details.

`cache/anchor_watch.json` and `cache/anchor_placemarks.json` are orphaned and
can be deleted once you've confirmed the new watch and pins are saving
correctly under `data/`.
