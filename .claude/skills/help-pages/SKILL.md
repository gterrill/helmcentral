---
name: help-pages
description: Voice and structure for Helmcentral's operator help pages. Use when writing or editing anything under docs/features, docs/how-to, docs/reference or docs/index.md, or CHANGELOG entries, or when asked to make help text read better.
---

# Help pages

The help pages are read by a skipper, navigator or owner, often at the helm,
on a phone, in glare, sometimes at 02:00 with an alarm going. Write every
sentence so it sounds **spoken**: what you would say to a crew member standing
beside you, in full, relaxed sentences with everyday words. Spoken is the test
for every choice below. Read the sentence aloud in your head. If you would
never say it that way out loud, rewrite it.

## Steps

1. **Read the page as the reader.** Note each sentence that makes you slow
   down: a stacked clause, a writerly verb, a thought left half said.
2. **Rewrite those sentences** using the moves below. Keep every fact, number,
   default, limit, UI label (exactly as the screen shows it), setting name,
   link target and code block.
3. **Run `make docs-lint`.** Done when it reports 0 errors and the page has
   fewer long-sentence warnings than before you started. The lint carries the
   hard rules (dashes, semicolons, "widget", ADR citations, filler words,
   named code technology), so fix what it flags rather than arguing with it.
   It covers the help pages only. Hold a CHANGELOG entry to the same rules
   by eye, since older entries predate them.
4. **Diff the page against the original** and confirm each fact from step 2
   is still there and still true. A rewrite that drops or bends a fact is not
   finished.

## The moves

- **Everyday verbs.** "Look at", "set up", "turn on", "shows", "has". Prefer
  them over "watch", "lay out", "holds", "carries", "surfaces".
- **One idea per sentence, said in full.** A longer sentence that says one
  thing plainly beats a short one that packs in three. Spell out the link
  between ideas with "so that", "because", "when", "if".
- **Say who does what.** "You tap **Save**", "Helmcentral raises an alarm",
  "Mate asks you". The reader or the boat is the subject.
- **A list for several cases.** When a paragraph walks through several
  conditions, cases or outcomes, or a page opens with several separate points
  about how a feature behaves, open with one sentence saying what follows,
  then give each case or point its own bullet. Start each bullet with a short label
  and a colon ("Instant warnings:", "Clearing alerts:"), then say what happens
  in a full sentence.
- **The reason up front.** When a behaviour exists for a reason, give the
  reason first: "Because a dragging anchor and a failed GPS antenna need
  different responses, a lost connection raises its own alert."
- **Name the familiar thing.** "A lost satellite fix", "a tablet mounted in
  the saloon", "every screen on board switched off". Explain a technical term
  the first time with the everyday thing it means.
- **Contractions.** "It's", "you're", "doesn't".
- **British/NZ spelling.** Colour, behaviour, metres, centre. "Whilst" is fine.
- **The operator's words.** "Tile" for anything placed on a dashboard page,
  every time. "Gauge", "dial" and "lamp" are right for the tiles that are
  those things.
  "Live instrument data" or "the NMEA network" rather than SignalK paths.
  "Alarm thresholds" rather than rules engines. Product names the operator
  installs (SignalK, InfluxDB, Overpass, Docker, Tailscale, YachtWave) are
  fine.
- **What the operator sees.** Describe the screen, the alarm, the reading.
  How the code does it belongs in an ADR.
- **Limits stated flatly.** "Mate can't read a scanned form." Admitted limits
  build trust.

## Examples

The operator ran these passages through a humaniser and preferred the
results. Learn the direction from them: longer, plainer, fully said, and
broken into a list when there are several cases.

### A paragraph said in full

Before:

> The dashboard is the screen you watch while the boat is working. It holds the
> readings that matter for whatever you are doing right now: depth and wind
> while you are conning, battery state and solar yield overnight at anchor,
> engine temperature and fuel burn on passage. You lay each screen out
> yourself, so the numbers in front of you are the ones you actually need.

After:

> The dashboard is the screen you look at whilst the boat is in operation. It's
> the screen that contains the readings that are relevant to what you're doing
> at that particular moment: depth and wind when you're conning, battery state
> and solar yield over the course of an overnight anchor, engine temperature
> and fuel burn on passage. You set up the screens yourself so that the
> readings you have in front of you are the ones that actually matter.

What changed: "watch" became "look at", "holds" became "contains", "lay out"
became "set up", "so" became "so that", and contractions came in. No fact
changed.

### Several cases become a list

Before:

> The dashboard reconnects on its own. It raises the warning immediately when
> the link fails outright or the device reports itself offline. A link that
> goes quiet without failing takes 45 seconds to catch, and coming back to the
> screen checks the clock again, so a phone that slept through an outage does
> not wake showing a stale screen as though it were live. Only a fresh reading
> clears the warning.

After:

> The Helmcentral dashboard automatically handles reconnections in the
> background. Here is how connection warnings and status checks work:
>
> - Instant warnings: If the connection drops completely or your device
>   reports itself offline, an alert appears immediately.
> - Silent drops (45-second check): If the connection stalls without an
>   explicit error, the system will detect the issue and flag it within 45
>   seconds.
> - Waking up your device: Whenever you unlock your phone or switch back to
>   the screen, the dashboard verifies the internal clock right away. This
>   prevents your phone from displaying outdated data as if it were still
>   live.
> - Clearing alerts: The warning will remain visible until the dashboard
>   successfully receives an active, up-to-date reading.

What changed: four cases packed into one paragraph became a lead-in and four
labelled bullets, one case each. "Catch" became "detect the issue and flag
it", "a phone that slept through an outage" became "whenever you unlock your
phone", and the reader is addressed as "you" and "your device". The 45
seconds and every condition carried over.

### A feature's opening becomes a benefit and labelled points

Before:

> Anchor watch catches a dragging anchor before it grounds the boat or fouls a
> neighbour, including at 0300 with nobody on deck: trail sampling and drag
> detection both run on the server, for your own vessel and for nearby AIS
> targets, and keep running with every browser on the boat closed.
>
> A drag is an ordinary alarm: it is logged, can be acknowledged, and uses the
> transports you have configured. Acknowledgement happens on the server, so
> silencing an alarm on your phone also silences it in a browser left open in
> the saloon.
>
> A lost GNSS fix never raises a drag. Position freezes at its last trusted
> value during an outage rather than wandering, and the watchdog for a lost
> instrument connection reports the outage separately, as its own alarm. A
> drag alert and a lost GPS fix require different responses.

After:

> The Anchor Watch monitors your position continuously to alert you the moment
> your boat, or a nearby vessel, starts dragging, even when everyone is asleep
> and every screen on board is switched off.
>
> - Always-on server monitoring: Trail tracking and drag detection run
>   directly on the server, not in your browser. They watch your own boat
>   alongside nearby AIS targets around the clock, so you don't need to keep a
>   device awake or a browser tab open.
> - Unified alarms and silencing: A drag event triggers like any standard
>   system alarm: it logs the event and alerts you across all your configured
>   channels. Silencing the alert on your phone immediately syncs back to the
>   server, quieting open screens anywhere else on the boat (like a tablet
>   mounted in the saloon).
> - Failsafe GPS handling: A lost satellite fix will never trigger a false
>   drag alarm. If your GNSS signal drops, your position simply freezes at the
>   last one the GPS reported reliably, rather than drifting across the
>   screen.
> - Dedicated instrument watchdog: Because a drifting anchor and a failed GPS
>   antenna require completely different responses, a lost instrument
>   connection triggers its own distinct alert instead of a drag warning.

What changed: three dense paragraphs became one benefit sentence and four
labelled points. "0300 with nobody on deck" became "even when everyone is
asleep", "transports" became "configured channels", "a lost GNSS fix" became
"a lost satellite fix", and the reason for the separate watchdog moved to the
front of its sentence. The humaniser's version had an em dash after "system
alarm". It is a colon here, because the lint fails on dashes. Two slips were
also corrected: "It monitors" became "They watch", since the subject is
plural, and "last known safe location" became "the last one the GPS reported
reliably", since the position is trusted, not known to be safe.

## Page shapes

- **features/**: open with one sentence on what the feature does for the
  boat or crew (catching a dragging anchor, keeping the house bank healthy).
  Follow it with labelled points for the key behaviours, as in the anchor
  watch example. Then what it shows, how it behaves, and where it stops.
- **how-to/**: numbered steps, each naming the exact control to click, tap,
  select or type, in the order the reader meets them. Background goes in a
  sentence before the steps, with a link to the feature page.
- **reference/**: fields, formats, environment variables and paths the reader
  looks up. Keep tables and code exact. The prose around them follows the
  moves above.
- **index.md**: one line per page, the link then a colon then a plain phrase
  saying what the page covers.
- **CHANGELOG entries**: a bold one-line summary of what the operator will
  notice, then a sentence or two on what it means for them. Breaking changes
  say what to do.

User-facing pages state the behaviour and the reason in their own words, so a
reader never needs an ADR to follow them.
