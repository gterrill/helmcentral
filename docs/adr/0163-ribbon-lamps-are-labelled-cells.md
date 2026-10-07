# ADR 0163: Ribbon Lamps Are Labelled Cells

## Status
Accepted

Supersedes the dot presentation of ADR 0052 and the colour rules of ADR 0080
for lamps. The fixed order of ADR 0082, the staleness rule of ADR 0083 and the
suggestions of ADR 0085 stand.

## Context
The ribbon drew each lamp as a 20px dot under a label cut to six characters.
Across the saloon the dots read as colour with no meaning, and "GEN", "WING"
and a truncated "Aft Bi" asked the watchkeeper to remember a legend. MV
Dirona's Maretron N2KView ribbon, which ADR 0082 was modelled on, uses
labelled cells whose whole face takes the state colour: dark when off, green
when on, amber or red when on with a problem. A Claude Design mockup of that
treatment was approved.

## Decision
- **A cell, not a dot.** Every lamp is a fixed-width cell: a 16px icon at the
  left, the lamp's name above its reading at the right. The name is never
  truncated or wrapped. The state colour fills the whole cell. Cells never
  stretch and the ribbon wraps to more rows instead of scrolling, so one shape
  holds at every width. Fixed width keeps a row of mixed states scannable and
  stops a change of state from shifting its neighbours.
- **State ladder.** Off is a dark cell with muted text. On is the healthy
  green. Warn is amber and alarm or emergency is red. No data and stale are a
  dashed outline on a transparent cell with a `--` reading, visibly unlike
  off; a stale source still never shows its last "on" (ADR 0083).
- **Warn and alarm come from active alarms on the lamp's path.** A lamp takes
  the worst active alarm whose path equals its own, lit or not. There are no
  per-lamp thresholds: a second threshold would let a lamp disagree with the
  alarm card for the same signal, and the alarm engine already owns
  thresholds, hysteresis and acknowledgement. Alarms raised elsewhere on the
  network arrive with a `notifications.` prefix on the path, so a lamp matches
  either its own path or that prefixed form.
- **Reading line** says On, Off or `--`. Showing the bound number needs a
  quantity and unit per path, which a lamp does not carry, so it is left out
  rather than guessed.
- **Icons** come from a small named set (`lamp-icons.ts`, mirrored by an
  allowlist in the backend), inferred from the path when unset, with a picker
  in the dialog.
- **Groups.** An optional short group label per lamp. Consecutive lamps with
  the same group sit under one micro-heading with a wider gap between groups.
  Order is still exactly the saved order.
- **CHK** is the same cell, rendered first as its own "Alerts" group. Clear,
  it is a dark "CHK" cell. Lit, it widens and reads `CHK · <count> · <worst
  alarm title>`, using the alarm's own title (ADR 0157), never a path.
- **Interaction.** A cell with an active alarm opens the alarms drawer; other
  cells do nothing. CHK always opens it.

## Consequences
Saved ribbons need no change: icon and group are optional and icons are
inferred. The backend caps the group length and rejects an icon outside the
allowlist. A ribbon with many lamps takes more vertical space than the dots
did.
