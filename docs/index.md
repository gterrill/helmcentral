# Helmcentral documentation

Documentation here is split by what you are trying to do, following the
[Diátaxis](https://diataxis.fr/) idea that a page answering "how do I" and a
page answering "what are the fields" are different documents and should stop
pretending to be one.

## Features

What a part of Helmcentral does, what it gives you, and where it stops. Start
here if you are working out whether to install it or what it will bring aboard.

- [The dashboard](features/dashboard.md), widgets, gauges, embeds and pages.
- [Forecast](features/forecast.md), weather, wind, wave and tide, and the
  heavy-weather warning signs the wave graph watches for.
- [Anchor watch](features/anchor-watch.md), including the rode planner.
- [Alarms](features/alarms.md), rules, transports and the decisions behind them.
- [Inventory tracking](features/inventory-tracking.md), designed but not yet built.

## How-to guides

Task recipes. You know what you want, this tells you the steps.

- [Installing Helmcentral](how-to/install.md), including Docker and manual binaries.
- [Upgrading across a breaking release](how-to/upgrading.md)
- [Development](how-to/development.md), running the stack, tests and release builds.

## Reference

Lookup material: fields, formats, environment variables, file paths. Written to
be scanned, not read.

- [Configuration](reference/configuration.md), covering every environment
  variable, state path and startup behaviour.
- [Engine profiles](reference/engine-profiles.md), the profile JSON format and
  its fields.
- [Provider plugins](reference/plugins.md), the WASM sandbox and the per-category
  contracts.

## Architecture decision records

[docs/adr/](adr/) is **not part of this tree.** It is the engineering record:
why a thing was built the way it was, what was rejected, and which decisions
have since been reversed. It is written for whoever maintains the code,
including the wrong turns, because those are the useful part.

Nothing above needs an ADR to make sense of it. If a page in `features/`,
`how-to/` or `reference/` only reads correctly once you have followed a
decision record, that page is not finished.

## What goes where

| You are writing | It goes in |
| --- | --- |
| What a feature does and its limits | `features/` |
| Steps to accomplish a task | `how-to/` |
| Fields, formats, variables, paths | `reference/` |
| Why a design choice was made, and what lost | `adr/` |

Tutorials, in the Diátaxis sense of a guided first run, do not exist yet. The
README's install section covers that ground for now, and a `tutorials/`
directory gets created when something outgrows it rather than in advance.
