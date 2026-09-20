# ADR 0116: The Banner Paints the Severity

## Status

Accepted. Extends ADR 0082 (the alarm banner's worst-first triage), ADR 0087
(forecast warnings are alarms), ADR 0110 (the wall kiosk's status pill) and
ADR 0114 (the banner's Acknowledge button and bulletin link). Every other
surface on the board already carries the four-rung severity ladder in
`frontend/src/lib/severity.ts`; this ADR is the banner and the kiosk pill
catching up to it.

## Context

The alarm banner has always answered one question with its colour: is
anything here unacknowledged. `loud ? 'border-destructive bg-destructive/10
text-destructive' : 'border-border bg-muted text-muted-foreground'` was the
whole rule. A `warn`-state reading drifting slowly out of band painted the
identical red as an `emergency` the server will not even let the operator
acknowledge away, and identical again to a dragging anchor. The wall kiosk's
compact status pill (`display-status-badge.tsx`) carried the same ternary,
same two colours.

That was fine while the banner only ever showed one kind of thing worth
mentioning at all. It stopped being fine once ADR 0087 put forecast warnings
on the same board as everything else: a gale warning can sit in force for
three days before it either lapses or resolves into weather, and for all
three days the banner painted it exactly as red as a boat actually dragging
its anchor right now. Red is supposed to mean look now. Once it also means
"this has been true since Tuesday and nobody's done anything about it
because there's nothing to do," it stops meaning either.

The rest of the board never had this problem. Dial rings, gauge tiles, lamp
strips, the alarms drawer, cluster readings, tile edges — five different
components, each of which used to keep its own near-miss copy of a four-rung
switch, were consolidated into `severity.ts`'s ladder (`severityClass`,
`severityFill`, `severityTextClass`, `severityBorderClass`, `worstZoneState`)
specifically so that `alert`, `warn`, `alarm` and `emergency` would always
read as four different things wherever they showed up. The banner and the
kiosk pill are the two surfaces a live alarm is guaranteed to reach — every
page, and the wall — and they were the two that never joined the ladder.

## Decision

### `severityFieldClass`, the ladder's fifth member

`severity.ts` gains one new export, alongside the others:

```
export function severityFieldClass(state: AlarmState | string | null): string
```

The existing four functions each serve one particular kind of surface — text
colour, a gauge-zone fill, a gauge-zone text colour, a tile's edge border —
and none of them was quite the right shape for a banner or a pill, which
paint their *entire box*: border, a tinted background, and the text inside
it, together, as one thing. `severityFieldClass` is that triple, in the same
hues the rest of the ladder already committed to (sky for `alert`, amber for
`warn`, red for `alarm`), with `emergency` filled solid rather than merely
tinted — the same call `severityClass` already makes for the one state
SignalK will not let the operator silence or acknowledge away. Anything that
isn't a recognised state falls through to a neutral border, a muted fill and
muted text, because both callers below reuse exactly that fallback for their
own "acknowledged, still live" variant.

### The banner keys off its own worst-first order

`AlarmBanner` already sorts everything it shows worst-first (`worstFirst`,
ADR 0082) purely for triage — which label leads the headline, which
condition sentence renders. That same order now drives the container's
colour too: `shown[0].state` is the worst rung currently on the board, and
while anything shown is unacknowledged (`loud`), the whole banner takes
`severityFieldClass` of that rung. A `warn` banner and an `emergency` banner
are now visibly different things, not the same red at two different
insistence levels of guilt.

A mixed set — one `alarm` and one `warn` shown together — paints for the
worse of the two. The banner already rolls a mixed set's headline up into
one count per state present ("1 ALARM · 1 WARN"); that behaviour is
untouched. Only the container's colour changed, to agree with the labels
sitting inside it rather than flattening all of them to the same red the
worst one alone would have earned.

Once every shown alarm has been acknowledged, the banner drops into its
existing muted variant — grey fill, muted foreground text, "all
acknowledged, still live" — but the border no longer resets to the plain
neutral one that variant used to fall back on. It keeps the worst rung's own
border colour instead, reusing `severityBorderClass` (the tile-edge ladder)
rather than a second copy of the same hues. An acknowledged `warn` and an
acknowledged `alarm` still read differently at a glance, even though both
are calmer than they were unacknowledged. Acknowledging silences the sound;
it was never supposed to erase how bad the underlying condition still is.

### A second channel besides colour

The banner's icon now swaps with the rung too — `Info` for `alert`,
`TriangleAlert` for `warn`, `OctagonAlert` for `alarm` and `emergency` — and
carries the state redundantly in a `data-severity` attribute alongside a
`data-testid` for it. This is the same reasoning `severity.ts`'s own header
comment already gives for splitting `warn` and `alert` onto distinct hues
rather than two shades of the same colour: colour is not a channel every
operator or every screen renders identically, and a shape change costs
nothing extra to add once the rung is already known at render time.

### The kiosk pill joins the same ladder

`display-status-badge.tsx`'s `display-alarm-pill` gets the identical
treatment: the worst rung among whatever it's currently showing (the
unacknowledged set while anything is unacknowledged, the full set once
everything has been acked, found with `worstZoneState` the same way the
gauge tiles already rank their own zones) drives `severityFieldClass` while
loud, and the muted-fill-plus-rung-border pairing once acked, same as the
banner. The pill's size, uppercase tracking and icon are unchanged — only
the colour question it was already answering gets a better answer.

## What was rejected

**A single shared component for the banner's and the pill's colour logic.**
The two surfaces compute "which rung, and is it loud" slightly differently
already — the banner works from an array it has already sorted worst-first
for its own triage, the pill has no such array and asks `worstZoneState`
directly — and forcing them through one shared function would mean that
function taking on both shapes of caller for no reader-facing benefit.
`severityFieldClass` is the one piece actually worth sharing: what a given
rung looks like. How each caller decides which rung is loud stays local to
it, same as it always was.

**Keeping `border-destructive` as a fallback for an unrecognised state.**
Both callers now fall through to `severityFieldClass`'s own neutral default
(`border-border bg-muted text-muted-foreground`) for anything that isn't one
of the four known rungs, rather than keeping the old destructive styling
around "just in case" a future state slips through unhandled. An
unrecognised state is not automatically the worst one; treating it as calm
until it earns a rung is the same rationed-colour principle the rest of the
ladder already runs on.

## Consequences

- Red on the banner and the kiosk pill is now reachable only at `alarm` and
  above. A `warn` never paints it, regardless of how long it has been
  sitting there unacknowledged. That moves a real question — which rules
  deserve to fire at `alarm` grade rather than `warn` — onto whoever writes
  or tunes a rule, where it belongs, rather than leaving the banner to paper
  over it by making every rung look equally urgent.
- The forecast-warnings banner (a `warn`-grade condition, ADR 0087) no
  longer competes visually with an anchor-drag alarm (`alarm` grade) for the
  operator's attention. They were never the same kind of urgent; now they
  don't look like it either.
- `severity.ts` has five ladder functions instead of four, all still keyed
  off the same `AlarmState`/`ZoneState` vocabulary, and the file's own
  header comment's "two ladders, one source" framing grows a third strand:
  text, gauge zones, tile edges, and now full tinted fields.
- Any future surface that needs to paint its whole box by severity — a new
  banner-shaped component, a different wall layout — has `severityFieldClass`
  ready rather than another ternary to invent.

## Verification

Test-first: `alarm-banner.test.tsx` and `display-status-badge.test.tsx` were
extended with the cases below before `severityFieldClass` existed or either
component read it, confirmed failing against the old destructive-red
ternary, then made to pass. Two pre-existing assertions in each file that
checked for the literal `destructive` class name on a default `state:
'alarm'` fixture were updated in the same pass to check for that rung's new
class instead (`border-red-500`) — that substring is exactly the
implementation detail this ADR replaces, so the old assertion could not
survive it and still mean anything.

- a `warn` alarm paints amber and carries neither `border-destructive`,
  `bg-destructive/10` nor `text-destructive`;
- an `alert` alarm paints sky blue, distinct from both amber and destructive;
- an `alarm`-state alarm paints red;
- an `emergency` alarm fills solid deep red, with a different class string
  than a plain `alarm`;
- a mixed `warn` + `alarm` set paints for the worse of the two and still
  rolls the headline up into "1 ALARM · 1 WARN";
- the icon carries the rung in a `data-severity` attribute for `alert`,
  `warn` and `emergency`, a second channel independent of colour;
- the acknowledged "all acknowledged, still live" variant keeps its muted
  fill and text but keeps the worst shown rung's border colour rather than
  falling back to the plain neutral one;
- the kiosk pill paints amber, not destructive, for a `warn`-only
  unacknowledged alarm, and paints red for the same `alarm`-state fixture
  the banner's own pre-existing loud-variant test uses.

Full suite: 248 test files, 2973 tests pass (one existing test,
`app-display.test.tsx`, mocked `@/hooks/use-alarms` without its
`ALARM_STATES` export; the kiosk pill's new `worstZoneState` call reads that
constant from the real module through `severity.ts`, so the mock gained it).
`npm run lint` reports zero errors — 19 pre-existing warnings, none in a
file this ADR touched. `tsc --noEmit` is clean.
