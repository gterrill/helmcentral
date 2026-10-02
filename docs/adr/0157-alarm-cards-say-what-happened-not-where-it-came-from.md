# ADR 0157: Alarm cards say what happened, not where it came from

## Status

Accepted (2026-10-03). Amends the bus-notification wording in
[ADR 0038](0038-alarms.md). Uses the radar target store from
[ADR 0062](0062-marine-radar-targets-from-mayara.md) and the live-field
contract from [ADR 0098](0098-collision-cards-describe-the-encounter.md).

## Context

A radar guard zone alarm on the boat read like this on the Active Alarms tile:

    radar.fur6424A.guardZone.1
    Radar fur6424A guard zone 1: target 100000030 acquired. Clears when the source clears it.
    Raised 2 Oct 09:17 · acknowledged 2 Oct 09:59

The crew's verdict was that alert messages are awful, and it is fair. The card
was written from the point of view of a server reading its own data bus. A
watchkeeper at 02:00 wants three things from it: what happened, where it is,
and whether it needs doing something about. The card answered none of them
first.

- The title was the SignalK path. An alarm raised elsewhere on the network has
  no rule label of its own, so `notificationStatus` set the label to the path
  and the card showed it in the display face, where it reads like a stack trace.
- The body carried mayara's radar key (`fur6424A`) and its 9-digit track id.
  Neither means anything at the helm, and `fur6424A`/`fur6424B` are the two
  ranges of one physical scanner, so the key does not even tell two radars
  apart.
- "Clears when the source clears it" described Helmcentral's latching, not the
  water around the boat. The phase badge already says the alarm is still live.
- The meta line repeated the path whenever the label differed from it.

The figures a watchkeeper does want, bearing, range and CPA, were already in
the radar target store, keyed by the same radar and target ids the
notification message carries. Nothing joined the two.

## Decision

1. **The title names the event.** A bus notification gets a title built on the
   server: a known source gets a written one ("Radar Guard Zone 1"), anything
   else gets its path turned into words ("Perpendicular Passed"). No path,
   device key, plugin name or rule id appears in a title. A rule alarm keeps
   the label the operator gave it.

2. **The body is the situation.** For a guard zone it is "Target in guard zone
   1." and, when the target is in the radar target store, the bearing, range
   and CPA after it: `Target in guard zone 1 · 042°T · 1.4 NM · CPA 0.3 NM`.
   mayara documents its bearing as radians true, so it is shown as °T and not
   converted to magnetic. For any other source it is the source's own message
   with its punctuation normalised, so "Anchor dragging!" reads as a sentence
   rather than a shout.

3. **Nothing the card cannot vouch for.** If the target has dropped out of the
   store, or the radar feed is down, the body stays the one sentence. No
   figures are guessed. The card states no clearing condition for an alarm
   raised elsewhere, because Helmcentral does not know how that source clears
   it; "Clears when the source clears it" is removed rather than reworded. A
   rule alarm still says "Clears above 12.4 V", because that it does know.

4. **The radar figures are computed live**, on the same contract as the
   collision encounter line (ADR 0098): looked up on every read, carried as
   structured fields (`radar_target`) so the frontend formats them, and never
   part of the raise/clear key, so a target moving inside the zone does not
   re-raise the alarm.

5. **The title and body are set once, on the server**, after the ownership and
   stale-sentence checks that still key on the bare path. The card, the banner
   and every notification transport read the same strings.

6. **The meta line is times only.** The path is gone from the card.

7. **Written titles for the sources this boat actually hears.** The AIS
   prioritizer's collision alarm was titled `navigation.closestApproach`; it
   is now "Collision Risk", with "TASHTEGO inside the CPA limit." in place of
   "TASHTEGO - CPA WARNING". The same path under self carries the plugin's
   GPS fault, which is titled "Collision Watch Fault" so it is not mistaken
   for a vessel. SignalK's anchor alarm is "Anchor Alarm". Helmcentral's own
   anchor drag alarm reads "Anchor Dragging", "Anchor 45 m from the drop point."

The wording rules themselves live in AGENTS.md under "Alarm Wording", so the
next alarm source is written to them from the start.

## Consequences

- Push notifications, email and ntfy titles now carry the event name rather
  than the path. Anything filtering on the old title text has to change. On a
  single-operator boat that is the ntfy app, which filters on priority and tags
  rather than text.
- The path is no longer anywhere on the card. To find which source raised an
  alarm, ask Mate or look at the instrument network itself.
- The generic path-to-words title is plain rather than good:
  `propulsion.starboard.overTemperature` becomes "Propulsion Starboard Over
  Temperature". Each source that turns up often enough to matter gets its own
  written title, as the radar guard zone has here.
- If the same zone number is live on both ranges of a dual-range radar at once,
  the two cards carry the same title. They stay separate alarms with separate
  acknowledgements. Naming the range is left until it happens on the boat.

## Rejected

- **Shortening the track id to "Target #30".** mayara's ids are not documented
  as an offset plus a number, and the radar overlay does not label targets
  that way. A number the operator cannot match to anything on the map is noise.
- **Rewording the clearing clause** ("Auto-clears when target leaves zone").
  Whether mayara clears the notification when the target leaves, or holds it
  until acknowledged, has not been checked against the scanner. The live alarm
  on the boat had been up for a day with the radar feed disabled.
- **Converting the bearing to magnetic.** That needs a variation source the
  radar path does not read today, and a wrong °M is worse than an honest °T.
