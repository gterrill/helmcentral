# ADR 0232: GFS-Wave Land Cells Are No Data, and the Blend Is Opt-In

## Status

Accepted (2026-10-30)

## Context

The Forecast drawer's Wave card showed "Significant wave height around 0.0 m
from the N" at the boat's position in the Whitsundays, with a flat line and
0.0 s period arrows. The reference plugin, `open-meteo-marine`, pins its wave
request to NOAA's GFS-Wave model (`models=ncep_gfswave025`, a 0.25 degree
grid). At -20.2666, 148.7099 the nearest cell, -20.25, 148.75, is land-masked.
For a masked cell Open-Meteo does not return null. It returns height, period,
direction and the wind-wave and swell components as a literal `0.0`.

ADR 0213 made the combined figures optional so that a null became absent, but a
zero is not a null, so these hours were read as a real flat calm from due
north. The same query without `models=` (Open-Meteo's default blend, on a finer
grid) returns 0.16 m, 3 s for that spot, and GFS-Wave 30 nm offshore returns
0.66 m, so only land cells are affected.

## Decision

1. **A wave with no period is not a wave.** The host reads an hour whose
   combined `wave_period_s` is exactly 0 as absent: height, period and direction
   all become nil. A wind-wave or swell component with period 0 is no train, so
   its height and direction are cleared. This is done once, in the host's
   `mapWasmFetchWavesOutput`, because any provider could do the same. A period
   that is simply missing is left as ADR 0213 describes. The rule matches the
   one `crossSeaSeparationDeg` already used for components.
2. **No data is a 200, not a 502.** When no hour carries a value, the wave
   forecast answers 200 with `days: []` and a new `no_data: true`, for the
   boat's own position and for a named place alike. The sea temperature stays
   in the answer, because it can come from a different model than the waves
   (the blend carries it at that cell) and the Today and Now tile reads it. The
   Forecast drawer's Wave card and the Forecast Conditions tile then show a
   quiet muted line, "The wave model has no data at this position" ("No wave
   forecast for this spot" in a planned stay), with no alarm box and no Retry
   button, because retrying changes nothing. A flat chart would claim a calm
   nobody forecast. A real failure stays a 502, and the card shows only its
   generic "Wave data unavailable"; technical error text never reaches the
   operator. Partly-empty series keep their gaps.
3. **The blend is an opt-in fallback.** The plugin declares a boolean config
   field, `use_blend_fallback`, labelled "Use Open-Meteo's blend where GFS-Wave
   has no data". Off is the default, because the blend runs lower than
   GFS-Wave elsewhere and the operator should choose that. When it is on and
   every hour of the GFS-Wave series has a missing or zero period, the plugin
   repeats the request without `models=`. The series is replaced as a whole,
   never hour by hour. If the second request fails, the fetch fails.
4. **The fallback is visible, and logged once.** The plugin output gains
   `model` and `fallback_used`, which the host folds into one field, the
   fallback model's name, empty when the usual model answered. The host logs
   each fetch that used the fallback (the plugin does not also log it), and
   `/api/wave-forecast` returns it as `fallback_model`, on the no-data answer
   too. The Wave card
   footer names the model without assuming which provider it is: "Data:
   open-meteo-marine · fallback model: best_match · ...". Mate's
   `get_wind_forecast` carries `wave_fallback_model` and `waves_no_data`,
   and `assess_passage_window` adds a note to the text Mate reads, so Mate can say the figures
   come from another model, or that there is no wave forecast, rather than
   reporting an error.
5. **Config fields gain a `boolean` type.** It is stored as "true" or "false"
   (blank is off), validated on save, and shown as a switch in the provider
   modal.

## Consequences

- A land cell is never again a calm sea. With the setting off the operator sees
  a quiet no-data line, and with it on they see the blend's figures labelled as
  such.
- A plugin's saved settings are part of its forecast cache key, so saving a
  setting makes the next call fetch afresh and a switch applies at once.
- The plugin must be rebuilt and installed with its new
  `open-meteo-marine.config_fields.json` for the switch to appear. A plugin
  without it behaves as before, except for the host-side zero-period rule.
- A real flat sea reported by a provider with height 0 and period 0 is now
  also read as no data. Open-Meteo reports a flat sea with a period, so this
  has not been seen.

## Alternatives rejected

- **Drop zeros in the plugin only.** Another provider with the same habit would
  bring the bug back.
- **Fall back by default.** It hides which model supplied the figures and
  changes numbers operators may already rely on.
- **Fall back hour by hour.** It would splice two models into one series.
