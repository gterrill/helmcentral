# Helmcentral documentation

These pages ship inside Helmcentral itself, so they're on the boat whether
or not the boat has a connection. The `?` button in the app's header opens
the page for whatever screen you're looking at; the sidebar's **Manual**
item opens this contents page instead. Links within this manual stay in
the app; a link to something outside it, an ADR, an example plugin's
source, the release workflow, opens on GitHub, which does need a
connection.

Documentation here is split by what you are trying to do, following the
[Diátaxis](https://diataxis.fr/) distinction between task instructions and
reference material.

## Features

What each feature does and its limits. Start here to see whether Helmcentral
meets your needs.

- [The dashboard](features/dashboard.md), instrument tiles, gauges, embeds and pages.
- [Forecast](features/forecast.md), weather, wind, wave and tide, and the
  heavy-weather warning signs the wave graph watches for.
- [Anchor watch](features/anchor-watch.md), including the rode planner.
- [Alarms](features/alarms.md), rules, transports and the decisions behind them.
- [Mate](features/assistant.md), a chat assistant that answers
  passage-planning questions using the boat's own live data, from any page
  and by voice.
- [Documents](features/documents.md), a searchable library for manuals,
  receipts and logs, with OCR, summaries and tags once Mate reads a scan or
  a photo. Built and working; no Documents panel in the dashboard yet.
- [Inventory tracking](features/inventory-tracking.md), designed but not yet built.

## How-to guides

Steps for specific tasks.

- [Installing Helmcentral](how-to/install.md), including Docker and manual binaries.
- [Upgrading across a breaking release](how-to/upgrading.md)
- [Create a dashboard page](how-to/create-a-dashboard-page.md)
- [Reorder dashboard pages](how-to/reorder-dashboard-pages.md)
- [Pin an indicator ribbon](how-to/pin-an-indicator-ribbon.md)
- [Set up a wall display](how-to/set-up-a-wall-display.md)
- [Add a Nearby map](how-to/add-a-nearby-map.md)
- [Set up Mate](how-to/set-up-the-assistant.md)
- [Talk to Mate](how-to/talk-to-mate.md)
- [Set battery state-of-charge bands](how-to/set-battery-state-of-charge-bands.md)
- [Duplicate a gauge group onto another instance](how-to/duplicate-a-gauge-group.md)
- [Development](how-to/development.md), running the stack, tests and release builds.

## Reference

Lookup material: fields, formats, environment variables and file paths.

- [Configuration](reference/configuration.md), covering every environment
  variable, state path and startup behaviour.
- [Equipment profiles](reference/equipment-profiles.md), the profile JSON
  format for engines, alternators and generators.
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
