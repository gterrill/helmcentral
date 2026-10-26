# ADR 0213: Wave Figures Are Optional, and a Missing Direction Is Not Checked

## Status

Accepted (2026-10-26)

## Context

The weather window check judges each hour of a leg against the operator's wave
limit for the sector the sea comes from. Two lines of the wave path told it more
than the data said.

The reference plugin, `open-meteo-marine`, turned a null upstream value into
`0` for the combined wave height, period and direction. The host then read that
`0` as a figure. An hour with no wave data became a flat calm that passed. An
hour with a height and no direction became a sea from due north, and the check
applied the wave limit of whichever sector north fell in, which is the wrong
sector for most headings. `assistant_passage_window.go` also set `HasWaveDir`
to true for every wave hour, so the host never asked whether a direction
existed. `waveSeriesHasNoValues` caught only a series that was zero in every
hour, so a single missing hour in a good series went through unnoticed.

Both are the masking the Fallback Policy rules out: an absent figure became a
plausible one. The same `0` reached the forecast tile, the sea state chart and
Mate's `get_wind_forecast`, which each had to cope with it in their own way.

## Decision

1. **The combined figures are optional in the plugin contract.**
   `wave_height_m`, `wave_period_s` and `wave_direction_deg` may be left out of
   an hour. The host reads a missing key as absent, using a `*float64` as
   `sea_surface_temperature_c` already does. A reported `0` is a figure and is
   kept. The per-component fields keep their documented rule that a zero period
   marks a component as absent, because that is how Open-Meteo itself encodes a
   flat component.
2. **The reference plugin omits the key** for a null or non-numeric entry.
   Plugins built here are rebuilt with the change, so this is not a breaking
   change for the operator.
3. **`waveHourPoint` carries the absence.** Its height, period and direction
   are pointers. Each reader handles nil:
   - The forecast day series marks an absent figure with the negative marker
     its summary, direction range and indicator code already skip. The HTTP
     response leaves the key out, and the frontend decoder already reads a
     missing key as unknown.
   - `waveSeriesHasNoValues` counts an absent combined figure as no value.
   - Mate's `get_wind_forecast` gives an hour a wave row only when it has a
     height and a period. It adds the direction and the relative angle only
     when the provider gave a direction.
   - The passage window assessor sets `HasWave`, `HasWaveDir` and a new
     `HasWavePeriod` from the three figures separately.
4. **The wave check runs on what is present.**
   - No height: the cell lists `wave` as not checked.
   - Height and direction: the check runs against that sector's limit, as before.
   - Height without a direction: the host judges the height against the wave
     limit of each sector. With no wave limit in any sector, the cell lists
     `wave` as not checked, because the operator's limit is what is missing.
     With a limit in all three sectors and the same severity from each, the
     worst of the three stands, whichever way the sea comes from. Otherwise, the
     severities differ or some sectors have no limit, and the cell lists the new
     measure `wave_direction`, shown as "wave direction".
5. **Why that is not a guess.** The rule is that direction cannot change the
   answer. If every sector gives the same severity, every direction does, so
   the check needs no direction to reach it. Equal limits are the plain case,
   and different limits that still agree (a 0.5 m sea against 2, 2.5 and 3 m)
   are another. Nothing is assumed about where the sea comes from, and the
   limiting line names no sector: "Waves 2.4 m · 7 s". If the severities
   differ, the direction decides which limit applies, and picking one would be a
   guess, so the cell says what it could not check.
6. **The period is shown.** The wave limiting line carries the period when
   there is one above zero: "Waves 1.8 m · 6 s abeam". It is rounded to whole seconds, and
   left out when absent. There is no period limit; the figure is there for the
   reader to weigh a short steep sea against a long swell of the same height.
7. **An impossible figure is an error.** A negative height or period, or a
   direction outside 0 to 360, makes the fetch fail and names the hour. A broken
   plugin is reported, not read as absent.
8. **The on-disk wave cache starts afresh.** Files written before this change
   hold 0 for a missing figure and would read as a real flat sea, including as
   a stale fallback. The cache file name gained `v2`, so the old files are
   never read.
9. **The card accepts `wave_direction`.** The measure list in the frontend
   decoder and the card's wording both gain it. Cards stored before this
   change never contain it and decode as they did.

## Consequences

- An hour with no wave data stops reading as calm. A leg over land-adjacent
  points with patchy coverage will now show "Not checked: wave height" where it
  used to pass.
- A plugin that still writes `0` for a missing value keeps the old behaviour,
  because the host cannot tell a zero from a figure. The contract says to omit.
- A cell with a wave limit that differs by sector and a provider that gives no
  direction shows "Not checked: wave direction" until the operator makes the
  limits equal or the provider supplies a direction.

## Rejected

- **Assuming the wind sector for the waves.** The old code did this when a
  direction was missing. Wind and sea often differ, and that is the case where
  the wave limit matters most.
- **Taking the strictest sector limit.** It would answer "could it be no-go",
  not "is it", and turn missing data into a warning that looks like a forecast.
  The rule above uses the same comparison only to ask whether the sectors agree.
- **A sentinel such as `-1` in the contract.** Every plugin author would have to
  know it, and a plain omitted key says the same thing in JSON.
- **A period limit.** Nobody asked for one, and a figure beside the height does
  the job without another setting to get wrong.
