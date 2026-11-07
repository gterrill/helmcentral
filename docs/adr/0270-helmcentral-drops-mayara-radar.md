# ADR 0270: Helmcentral Drops Mayara Radar

## Status

Accepted (2026-11-07). Supersedes [ADR 0062](0062-marine-radar-targets-from-mayara.md)
(radar targets) and [ADR 0069](0069-radar-picture-relayed-from-mayara.md) (the
radar picture on the anchor map). Amends
[ADR 0207](0207-ignored-network-alarms.md) (radar guard zones leave the list of
alarms that can never be ignored),
[ADR 0157](0157-alarm-cards-say-what-happened-not-where-it-came-from.md) (a
guard zone alarm keeps its title and loses its radar figures),
[ADR 0098](0098-collision-cards-describe-the-encounter.md) (the radar-only
encounter branch is gone) and
[ADR 0088](0088-collision-alarms-colour-the-ais-marker.md) (the radar
triangles are gone).

## Context

ADR 0062 took ARPA targets from mayara-server through its SignalK plugin, and
ADR 0069 added the live radar picture to the anchor map. Both have been in use
on the boat, and neither earned its place:

- **The guard-zone alarm never clears at anchor.** Every other boat in an
  anchorage sits inside the zone for as long as the hook is down. The alarm
  stays on the board all night, and the operator learns to look past it.
- **The link kept dying.** The mayara-to-SignalK link froze for hours at a time
  while mayara itself stayed healthy. Radar targets and the own-ship fix went
  stale together, and the tile showed old data as though it were live.
- **The targets were a second opinion on a plotter that already shows them.**
  ADR 0062 said as much: Helmcentral wanted collision awareness, not a second
  plotter. In practice AIS gave the collision watch everything it acted on.

The operator is the only user, and has decided the integration adds nothing to
Helmcentral for now.

Mayara itself stays on the boat. Helmcam, a separate project, reads the radar's
targets from SignalK. The mayara plugin therefore keeps publishing
`radars.*` data and `notifications.radar.<id>.guardZone.<n>` notifications on
the bus, and Helmcentral hears them whether it wants to or not.

## Decision

Helmcentral stops knowing about radar. Git history keeps the code, in case the
operator wants to try it again.

### 1. What goes

- **Targets:** the Radar Targets tile, its stream, the capabilities lookup and
  the target store.
- **The picture:** the echo overlay on the anchor map, its toggle, its spoke
  relay endpoint and its map markers.
- **Settings:** the Mayara section and the `mayara:` block of `settings.yaml`.
- **Collision watch:** the radar branch of the COLREGS classifier. Collision
  watch runs on AIS alone, as it did before ADR 0062.
- **Mate:** the radar figures on an alarm and the prompt text that mentioned
  them.

### 2. A guard-zone alarm is an ordinary network alarm

The radar figures and the safety exception both go. The wording stays.

- **Wording:** the alarm presenter keeps one rule for the guard-zone path. The
  title is "Radar Guard Zone 1" and the body is "Target in guard zone 1.",
  with the zone number from the path. The card no longer carries bearing,
  range or closest approach. The radar's device key and the target id stay off
  it, as the alarm wording rules require, so the generic path-to-words title
  and the plugin's raw message are not used.
- **Ignorable:** `alarmKindIgnorable` no longer refuses guard zones, so the
  operator can press **Stop showing** on one. The other safety kinds stay
  refused: the emergency kinds, `navigation.anchor`,
  `navigation.closestApproach`, received distress, and any alarm in the
  `emergency` state.

The alarm that never clears at anchor is the reason for the second point. An
alarm the operator can never silence is worse than one that is easy to
silence, and a guard zone was only ever on the safety list because Helmcentral
owned the radar picture behind it. With no picture behind it, the notification
is just a message from another device.

### 3. The bus still carries radar

Mayara keeps publishing, so two pieces of defensive code stay.

- **Target nodes are dropped on arrival.** The snapshot still discards
  `radars.<id>.targets.*` before it reaches the tree. The plugin's target ids
  only climb, and about 6,000 of them are replayed as nulls on every
  subscribe. Storing them would slow every whole-tree copy a little more each
  week. The stream's larger read limit stays for the same frame.
- **The rest of the radar tree is stored like any other path.** Nothing reads
  it, and it is small.

### 4. Stored state is not migrated

No migration code is written. One operator and one boat make it more code than
the fix it saves.

- **A saved page with a Radar Targets tile** loses the tile on load. The id
  joins the retired tile ids (ADR 0047), which are stripped from saved pages
  and written back. Without that, every layout save on such a page would be
  refused for an unknown tile id.
- **A `settings.yaml` with a `mayara:` block** still loads, because settings
  are read into a plain map and unknown keys are ignored. The next save from
  Settings leaves the block out.
- **The browser's saved echo toggle** stays in local storage, unread.

## Consequences

- **Radar is invisible in Helmcentral.** No tile, no map picture, no alarm
  figures, no Mate answers about radar contacts. Operators of other boats lose
  the optional radar support the README used to list.
- **The guard-zone presenter rule stays.** It is the one radar-shaped line
  left in the alarm presenter. Dropping it would put the radar's device key and
  the plugin's message, which names a target id, on the card.
- **A guard-zone alarm can be hidden by mistake.** **Stop showing** asks for
  confirmation, and the Settings list restores it. The collision watch, anchor
  alarm and distress alarms are unaffected.
- **Reversal costs a revert.** Restoring the integration means reverting the
  change that removed it, then re-reading ADR 0062's amendment about the
  plugin's REST endpoint, which may have moved on by then.
