# ADR 0194: Passage limits are no-go only

## Status

Accepted (2026-10-20). Amends the limits section of
[ADR 0191](0191-mate-cards-come-from-tool-results.md).

## Context

ADR 0191 gave each of three wind sectors (ahead, abeam, astern) a marginal and
a no-go threshold for wind, gusts and waves, plus a wind-against-tide band:
roughly twenty numbers. The operator found that too many to set and keep
consistent. Their own entries on the boat showed the pattern:

- Marginal against no-go for wind and waves averaged about 0.7.
- Gust no-go was 25, 30 and 40 kn against wind no-go of 15, 22 and 35 kn, which
  is wind no-go plus 10 kn to within a few knots.

So the extra numbers were being typed in by rule of thumb.

## Decision

Only no-go is entered. Per sector: a required no-go wind (kn) and an optional
no-go wave height (m); a blank wave height is still shown as not checked.

Everything else is computed, with the figures held as named constants in
`passage_limits.go`, commented as derived from the operator's entries:

- **Marginal** is 70% of the no-go, for wind, gusts and waves.
- **Gust no-go** is the sector's no-go wind plus 10 kn; gust marginal is 70% of
  that.
- **Wind against tide** is one optional number, a reduction in knots. While the
  stream runs against the wind (the same detection as before: the strong part
  of the flood or ebb, wind within 60 degrees of the stream's set, legs with a
  flood direction only) the effective wind no-go is the sector's no-go minus
  the reduction, and marginal is 70% of that. Blank is not checked. On save it
  must be more than 0 and less than every sector's no-go wind; the assessor
  also refuses an effective limit of zero or less.
- The daylight-arrival switch is unchanged.

The card text for a marginal cell names the threshold that decided it
("Wind SE 12 kn ahead (marginal from 10.5)"). A no-go cell keeps its short
text.

### Stored shape

```yaml
assistant:
  passage_limits:
    ahead:  { wind_kn_no_go: 15, wave_m_no_go: 1.3 }
    abeam:  { wind_kn_no_go: 22, wave_m_no_go: 2 }
    astern: { wind_kn_no_go: 35 }
    wind_against_tide_offset_kn: 5
    daylight_arrival: true
```

### Carrying v0.49.0 settings over

The v0.49.0 block (`wind_kn`/`gust_kn`/`wave_m` bands per sector, and
`wind_against_tide`) is recognised by those keys. On read, the `no_go` wind and
wave values become the new fields; marginal and gust figures are dropped, as is
any wind-against-tide band (a band has no sensible offset), with a log line.
The next save writes the new shape. This is the one exception to "an unreadable
stored block is an error": a legacy block with a bad value is still an error,
and so is any other unreadable block.

## Consequences

- Fewer numbers, and the same three sectors. The cost is that marginal and gust
  limits can no longer be tuned independently of no-go.
- Gusts are now always judged, because they follow from the wind no-go. The
  weather window card no longer lists "marginal wind limit" or "gusts" as not
  checked.
- A real forecast can now fail on gusts where it was previously silent: gusts
  of 32 kn against a 14 kn ahead no-go wind (gust no-go 24 kn) read as no-go.
- Mate's prompt now tells it to call `assess_passage_window` alongside the
  pilotage briefing when the operator asks whether or when to go. Before, the
  mandatory briefing routine for passages in the forecast range never named the
  tool, and the card did not appear.
