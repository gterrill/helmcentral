# ADR 0271: The Rode Planner Shows Only What Anchoring Needs

## Status

Accepted (2026-11-08). Amends
[ADR 0059](0059-always-on-anchor-map-drop-raise-and-scope-method.md) §4: the LOA
provenance line and the "Applies the … swing" caption above the apply button are
gone, and the button changes its name.

## Context

The Rode Planner grew by adding a line whenever a figure needed explaining. Each
tab repeated the depth, bow height, wind, chain and hull it had been given. The
Swing figure had its sum spelled out under it. The drawing had a legend that
restated the drawing. The footer said where the boat length came from, then
said which method's swing the button would apply, then offered the button.

All of that is true, and none of it is what the operator is asking at the
moment the hook goes down. The operator needs three answers: how much to pay
out, whether what is out is enough, and what to set the alarm to. The rest is
the planner talking back the inputs the operator just chose, and it pushed the
deployed-rode input and the button toward the fold of a 20 rem sidebar.

## Decision

The planner shows each answer once, in the place the operator looks for it, and
drops anything that only echoes an input or explains where a figure came from.

### 1. What goes

- **The per-method note.** The line of depth, bow height, wind, chain and hull
  under each tab is removed, along with the `note` field on a method result and
  the code that built it.
- **The swing sum.** "54 m rode + 8 m GPS to bow + 16 m boat" under the Scope
  and Swing row is removed. The row itself stays.
- **The diagram legend.** The three-item list under the drawing is removed. The
  drawing's `aria-label` keeps every figure for screen readers, and the lift
  warning stays.
- **The LOA provenance line.** "LOA 17.9 m from SignalK" is removed. The warning
  that asks for the boat length when none is known stays, because it gates the
  button.
- **The "Applies the … swing" caption.** The button now carries the figure.
- **The bow height figure in the Depth caption.** The field still holds depth at
  high tide plus bow height, and the inversion that turns a typed figure back
  into a raw depth is unchanged. The caption drops the number but still says the
  figure is measured from the bow roller, because a bare sounder reading typed
  into the field would plan short.

### 2. What changes

- **The button names the radius.** "Apply as alarm radius" becomes "Set alarm to
  65 m", using the configured method's swing in the operator's unit. With no
  swing to set it reads "Set alarm" and is disabled, as before. The line saying
  the configured method is unavailable, and the line giving the reason the
  button is locked, stay above it.
- **The badge says what to do.** "Scope OK / Low / Insufficient / Unknown"
  becomes "Enough rode" when the deployed rode reaches the Pay Out figure as
  shown, and "Short by N" when it does not, where N is the Pay Out figure less
  the deployed rode in the operator's unit, rounded up so that paying out N
  clears the badge, never under 1. It judges against the configured method, the
  same one the button uses, and shows nothing when that method is unavailable. Amber and
  red stay. An unknown status shows no badge. The text is sentence case.
- **The Depth caption.** "From the bow roller, at high tide", "From the bow
  roller, no tide station", "No depth reading", "Type it from the bow roller, at
  high tide", "Type it from the bow roller", and the refusal "Below bow height
  plus tide rise, not saved".
- **The minimum is still named.** When the 3:1 floor sets the Catenary figure,
  the Scope row reads "Scope 3.0:1 minimum".
- **The tab names.** "Catenary Method" and "Ratio Method" become "Catenary" and
  "Ratio".

No calculation changes. The Pay Out figure, scope, swing, status ladder and
depth inversion are as they were.

## Consequences

- **Imperial units are right.** The note hardcoded metres for depth and bow
  height. It is gone, not fixed.
- **Fewer places to disagree.** The badge shortfall reads the same Pay Out
  figure the headline prints, through one helper beside `deployedScopeStatus`.
- **The inputs are still on screen.** The operator can read depth, wind, sea
  state and seabed in the fields above the tabs, so nothing an echo carried is
  missing.
