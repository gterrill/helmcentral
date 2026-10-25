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
  conditions, cases or outcomes, open with one sentence saying what follows,
  then give each case its own bullet. Start each bullet with a short label
  and a colon ("Instant warnings:", "Clearing alerts:"), then say what happens
  in a full sentence.
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

## Page shapes

- **features/**: open with one or two sentences on what the feature does for
  the boat or crew (catching a dragging anchor, keeping the house bank
  healthy). Then what it shows, how it behaves, and where it stops.
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
