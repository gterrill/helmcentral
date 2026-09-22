# Provider plugins

Tides, weather, waves, points of interest (Nearby), forecast warnings and the
upper-air outlook each come from a small provider plugin rather than being
built into Helmcentral itself. Adding a provider for another region's
government data source, or swapping one out, needs no Helmcentral upgrade,
just a plugin dropped into the right folder and a restart.

| Category | Directory | Override | Bundled plugins |
| --- | --- | --- | --- |
| Tides | `plugins/tides/` | `PLUGINS_TIDES_DIR` | `bom` (Australia), `noaa` (US) |
| Weather | `plugins/weather/` | `PLUGINS_WEATHER_DIR` | `open-meteo` (worldwide, keyless, **default**), `weatherkit` (Apple, needs keys) |
| Waves | `plugins/waves/` | `PLUGINS_WAVES_DIR` | `open-meteo-marine` (**default**) |
| Points of interest | `plugins/poi/` | `PLUGINS_POI_DIR` | `osm-overpass` (worldwide, keyless, **default**; mirror set from the plugin's own settings, see below), `google-places` (needs a key, partial category coverage) |
| Forecast warnings | `plugins/forecast-warnings/` | `PLUGINS_FORECAST_WARNINGS_DIR` | `bom` (Australia, **default**), `nws` (US) |
| Upper air | `plugins/upper-air/` | `PLUGINS_UPPER_AIR_DIR` | `open-meteo-upper` (worldwide, keyless) |

Pick the active provider for each category in Settings.

Upper air is optional: leave that plugin out and the forecast page simply
shows no 500mb section. Points of interest is optional the same way: with no
`poi` plugin installed, Nearby reports an actionable error rather than any
part of the dashboard failing to load. The rest have a tile that goes empty
without a provider.

## Installing a plugin

Copy the plugin's file and every companion file that ships with it into the
matching category directory above, then restart. Depending on the plugin
that can be `<name>.allowed_hosts.json`, `<name>.allowed_secrets.json`,
`<name>.config.json` and `<name>.config_fields.json`. Leave out the secrets
file and the plugin is refused your credentials even after you enter them. To install or
update the bundled set manually:

```sh
curl -fsSL https://github.com/gterrill/helmcentral/releases/latest/download/helmcentral-plugins.tar.gz \
  | sudo tar -xz -C /var/lib/helmcentral/plugins
sudo systemctl restart helmcentral
```

After restarting, the plugin appears in the Settings provider dropdown for
its category.

A plugin can only reach the internet hosts named in its own allowlist file;
one with no such file gets no network access at all. Review a third-party
plugin's allowlist before installing it: if it is approved for both a secret
and an external host, it can send that secret to that host. If a plugin needs
your own credentials, such as WeatherKit's signing key, open that provider's
settings from its card in Settings and enter them there, rather than editing
a file by hand. The same dialog shows the plugin's allowed hosts and allowed
secrets; changes to those take effect after a restart.

## Plugin-declared settings

Some plugins expose one or two of their own settings for you to edit from
Settings, without an environment variable, a file to edit, or a restart. The
`osm-overpass` points-of-interest plugin's Overpass server address is the
first example: see [Configuration → Overpass](configuration.md#overpass).
Open the gear icon on a provider's card in Settings to reach settings like
this. Leaving one blank reverts to the plugin's own default, and a change
takes effect on the plugin's very next call.

## Why tides have no default

Weather and waves default to Open-Meteo and Open-Meteo Marine because both
services are free, keyless and worldwide, so a new installation gets a
working forecast dashboard without any configuration.

Tides have no equivalent global service. Tide predictions depend on physical
water-level stations rather than a global weather model, so there is no free
API with worldwide coverage to default to. `bom` covers Australia and `noaa`
covers the United States, with no coverage beyond those jurisdictions, and
Helmcentral ships no coordinate-based fallback: a global model standing in
for real station observations would show tide times that look authoritative
but are wrong for local waters, which is worse than an honest gap.

Pick `ui.tide_provider` in Settings for your region. Until you do, the
forecast page's tide section shows an explicit error naming what's missing
rather than a guessed prediction.

## Building your own

Helmcentral's plugin interface is open: see the [developer documentation](../developers/plugins.md)
for the contracts each category expects and how to build and package one.
