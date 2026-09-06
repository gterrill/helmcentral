# ADR 0077: Battery & Power Tile Reads the Bank, Not the Fields

## Status
Accepted

## Context

A perception-first design review run on 2026-09-06 against the live board,
Anchored page, evaluated the Battery & Power tile as it shipped: eight
sub-cards carrying fifteen readings. At anchor, four of them read `—` because
the shore charger was disconnected, which is the ordinary state at anchor, not
a fault. Amber sat on four separate numerals at once, so the state of charge,
the one figure that should own the tile's alert colour, competed for attention
with three others that were not alerts. "Time Remaining 13h" while the bank
was charging meant time to full, and the only thing saying so was a 12px
glyph next to the number. At the iPad width, one unit suffix was shrunk to
12px to stop it overflowing its card.

None of this answered the question the operator actually has at anchor: will
the bank hold until morning.

## Decision

### 1. Four cards, one conditional fifth

The tile becomes State, Net, Solar and Loads, plus a Shore line that renders
only when the charger reports anything at all (current, AC input, mode or an
error). At anchor with no shore power connected, the line is absent rather
than four fields reading `—`. An error from the charger always renders, in
red, even if every other charger field is null, because an operator scanning
the tile for a fault should not be able to miss it by virtue of the current
sensor also being quiet.

### 2. Amber means the bank's state, and nothing else

State of charge is the only numeral that carries the alert ladder. In band,
it reads the plain gauge colour. Out of band, it takes the same
`severityTextClass()` / `severityFill()` used everywhere else on the board,
so a low bank looks like every other alarm condition on the dashboard rather
than like a tile-specific colour scheme the operator has to learn separately.
Teal marks energy moving into or out of the bank (net, solar, the time-to-go
figure, the dawn estimate). The loads figure stays foreground-coloured: it is
consumption, not a state of the bank, and colouring it amber alongside the
SoC numeral was what diluted amber's meaning in the first place.

### 3. Direction lives in the label, not a glyph

`timeToGoHours` is already signed by `use-electrical-state.ts`: positive is
hours to full, negative is hours to empty. The label reads that sign directly
instead of asking a 12px lucide battery icon to carry it: "To full" while
charging, "To empty" while discharging with no lower band configured, or
"To {band}%" while discharging above a configured warn or alarm threshold.
The battery glyphs are removed; the words say what they showed.

### 4. Bands come from the alarm rules on one pinned path

The bar's two bands, the numeral's colour, and the "To {band}%" label all
read from `below` alarm rules on a single SignalK path rather than a
hardcoded 20/35 split. The path is pinned by `INFLUX_SOC_MEASUREMENT`
(default `electrical.batteries.0.capacity.stateOfCharge`) rather than looked
up dynamically, because the boat's main-battery lookup picks an instance by
which one is currently drawing the most current, and the same physical bank
appears under several SignalK instances (`batteries.0`, `.239`, `.512`,
`chargers.276`) that can all report the same value on a quiet day. A band
tied to whichever instance last had the highest current would drift with no
change to the bank itself. Pinning one path means the bar, the alarm rule and
the overnight history all agree on which battery "the bank" refers to. No
rule on that path means no band: the tile falls back to today's plain,
uncoloured reading rather than inventing a threshold nobody configured.

### 5. The dawn projection

The footer adds a projected state of charge at sunrise, so the operator's
actual question gets an actual answer.

**Sun times are computed, not fetched.** `backend/suntimes.go` implements
NOAA's General Solar Position calculation from the vessel's position and the
civil date, in Go, rather than reading the sunrise/sunset strings the weather
provider already returns for the current forecast day. Two things rule the
provider out: the history branch below needs several past nights' windows,
and the provider has never seen those days; and the computation works with no
network at all, which the weather API does not promise. It is checked against
the provider's own values for the vessel's position (Sep 7 2026, sunrise
6:08 AM / sunset 5:57 PM local) to within three minutes.

**The overnight model.** For each of the last seven nights, the window runs
from thirty minutes after sunset to fifteen minutes before the following
sunrise, trimmed on both ends so the evening's last loads settling and the
morning's first loads starting don't distort the slope. A night needs at
least 80% of its expected fifteen-minute samples to be trusted; anything
thinner is dropped rather than extrapolated across the gap. A night whose
slope comes out flat or rising is dropped too and does not count as "usable":
state of charge does not rise on its own overnight, so a rise means a
generator or shore charger ran, and that night says nothing about the boat's
baseline draw. The rate reported is the median across at least two usable
nights; one clean night is not enough to call a rate typical, so a single
usable night is treated the same as zero. The result is cached for fifteen
minutes rather than recomputed on every tick, because it costs up to seven
range queries against InfluxDB and the answer cannot move meaningfully faster
than the nights themselves do.

**Two bases, and what each assumes.** `history` applies the median overnight
rate to the coming night and the live rate to the remaining daylight before
sunset. `linear` extrapolates the live rate all the way to sunrise. Linear
overstates the bank badly in the afternoon, while the panels are still
charging, because it is applying a charging rate across hours that will
actually be spent discharging; it becomes accurate on its own once the sun
goes down, because at that point the live rate *is* the night's rate. The
tile always shows which basis produced the figure, so a linear estimate is
never mistaken for a historical one.

**The approved fallback.** `linear` is used whenever `history` cannot be
computed: InfluxDB is not configured, a query to it fails, or fewer than two
usable nights exist. This is the first use of the AGENTS.md exception rule
in this codebase. The default fallback policy is fail-fast: don't mask a
missing upstream source with a plausible-looking number. The operator
requested this specific exception on 2026-09-07, because a dash on a tile
that answers "will I make it to morning" is less useful than a labelled
estimate, provided the label is honest about what the estimate assumes. Per
the exception rule, it is gated behind `DAWN_LINEAR_FALLBACK` (default
`true`), named in a startup log line (`overnight model: InfluxDB not
configured; dawn projection will use live-rate extrapolation
(DAWN_LINEAR_FALLBACK=true)`, or the equivalent for the strict setting),
logged again at basis resolution on every 15-minute recompute that lands on
it, and labelled on the tile itself every time it is shown, never silently.
Setting the flag to `false` restores the strict behaviour: a dash and a
reason, nothing invented.

### 6. Micro-typography per the AGENTS.md scale, stepping together

Labels move to the one micro-label size (`text-[10px] uppercase
tracking-[0.16em]`); no `text-[11px]` label remains on the tile. Hero
numerals and their unit suffixes step down together at the 768px band
(`md:`) and back up above it (`lg:`), the same pattern already used for the
tile's amp and watt readouts, rather than shrinking one suffix in isolation
to avoid an overflow. Nothing on the tile wraps to a second line.

## Rejected

- **Hardcoded 20% / 35% bands.** A boat's own alarm thresholds are a
  configuration decision the operator already makes elsewhere; duplicating it
  as a constant in the tile would let the two disagree.
- **Showing the linear projection with no basis label.** An unlabelled linear
  figure reads "100% at dawn" through a sunny afternoon while the panels are
  still charging, which is exactly the kind of number that looks like a
  measurement and is actually an artifact of when it was computed. This is
  the same failure mode ADR 0068 already fixed for a frozen `0 W` reading, in
  a new place.
- **Removing Solar from the tile.** The Docked and Underway pages have no
  Solar tile of their own, so this card is the only solar reading they show.
  Anchored shows it twice because the operator placed the Solar tile there;
  that is a layout choice, not a reason to strip the reading from the other
  pages.
- **Choosing the SoC path per battery instance by current draw**, matching
  the existing main-battery lookup. Rejected for the same reason a fixed path
  was chosen in Decision 4: an alarm band, a history query and a bar all need
  to agree on one instance, and current draw is not stable enough to anchor
  that agreement.

## Consequences

The saved Anchored layout gives this tile `h=12`, sized for the old eight-card
version. The new layout is shorter, and the card fills its height, so space
sits empty below it until the operator resizes the tile in edit mode. Saved
layouts are not rewritten automatically.

The tile now depends on two things it did not before: the alarm rules fetch,
for its bands, and the overnight endpoint, for the dawn figure. Both degrade
to the tile's original plain behaviour when unavailable, never to an invented
number. No SoC alarm rule means no bands and a plain "To empty". No InfluxDB,
a failed query, or too little history means the dawn figure falls to the
linear basis (or to a dash, if the operator has turned the fallback off), and
says so.

The dawn figure is built to answer one question: whether to run the generator
or plug in before dark. It is not a forecast to plan a passage on, and it
does not attempt to be one.

## Follow-ups

The overnight model treats every usable night the same. Weighting it by
forecast cloud cover, or excluding nights where a generator run was planned
rather than just detected after the fact, is worth doing once the current
figure has been watched against reality for a week.
