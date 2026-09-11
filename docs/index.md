# Helmcentral documentation

Documentation here is split by what you are trying to do, following the
[Diátaxis](https://diataxis.fr/) distinction between task instructions and
reference material.

## Features

What each feature does and its limits. Start here to see whether Helmcentral
meets your needs.

- [The dashboard](features/dashboard.md), widgets, gauges, embeds and pages.
- [Forecast](features/forecast.md), weather, wind, wave and tide, and the
  heavy-weather warning signs the wave graph watches for.
- [Anchor watch](features/anchor-watch.md), including the rode planner.
- [Alarms](features/alarms.md), rules, transports and the decisions behind them.
- [Inventory tracking](features/inventory-tracking.md), designed but not yet built.

## How-to guides

Steps for specific tasks.

- [Installing Helmcentral](how-to/install.md), including Docker and manual binaries.
- [Upgrading across a breaking release](how-to/upgrading.md)
- [Reorder dashboard pages](how-to/reorder-dashboard-pages.md)
- [Pin an indicator ribbon](how-to/pin-an-indicator-ribbon.md)
- [Set up a wall display](how-to/set-up-a-wall-display.md)
- [Add a Nearby map](how-to/add-a-nearby-map.md)
- [Set battery state-of-charge bands](how-to/set-battery-state-of-charge-bands.md)
- [Development](how-to/development.md), running the stack, tests and release builds.

## Reference

Lookup material: fields, formats, environment variables and file paths.

- [Configuration](reference/configuration.md), covering every environment
  variable, state path and startup behaviour.
- [Engine profiles](reference/engine-profiles.md), the profile JSON format and
  its fields.
- [Provider plugins](reference/plugins.md), the WASM sandbox and the per-category
  contracts.
- [POI categories](reference/poi-categories.md), the eleven points-of-interest
  categories and each provider's coverage.

## Architecture decision records

[docs/adr/](adr/) is separate from the user-facing documentation. It records
engineering decisions, rejected alternatives and later reversals for code
maintainers.

Pages in `features/`, `how-to/` and `reference/` should be understandable
without consulting a decision record.

## What goes where

| You are writing | It goes in |
| --- | --- |
| What a feature does and its limits | `features/` |
| Steps to accomplish a task | `how-to/` |
| Fields, formats, variables, paths | `reference/` |
| Why a design choice was made, and which alternatives were rejected | `adr/` |

Tutorials, in the Diátaxis sense of a guided first run, do not exist yet. The
README's install section covers that ground for now, and a `tutorials/`
directory will be added when a longer guided introduction is needed.
