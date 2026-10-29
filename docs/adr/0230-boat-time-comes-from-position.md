# ADR 0230: Boat Time Comes From Position

## Status

Accepted. Supersedes in part ADR 0035, whose longitude rule (the boat's zone is
a fixed offset of round(longitude / 15) hours) is replaced here. The rest of
ADR 0035 stands: the host names the zone and plugins must use it.

## Context

Helmcentral took the boat's clock from its longitude: `round(lon / 15)` hours,
held as a fixed offset. That is the nautical zone, and it is wrong wherever
people live.

- **Daylight saving.** A boat in Sydney in January is on UTC+11. The rule said
  UTC+10, so every sunrise, tide time and "today" boundary was an hour out for
  half the year in New South Wales, Victoria, Tasmania, the ACT and South
  Australia.
- **Half-hour zones.** Adelaide is UTC+9:30 and Lord Howe Island is UTC+10:30
  with a thirty-minute summer shift. A whole-hour offset can't say either.
- **Three clocks disagreed.** The Mate tile card work found the browser, the
  vessel stream and Mate's prompt each telling the time differently. The
  stream and Mate used the longitude rule. The browser used the device zone.

The fix has to give the boat the zone a person ashore would give it, and it
has to keep working a long way offshore, where no country's zone applies.

## Decision

### One lookup from a position to a zone

`zoneAt(lat, lon)` in `backend/boat_time.go` returns a `*time.Location` and
the IANA name. It reads github.com/ringsaturn/tzf's default finder, which
combines two things:

- a fuzzy pre-index that answers quickly for most of the globe, and
- lite boundaries from timezone-boundary-builder, with ocean zones, for the
  coasts the pre-index leaves open.

Near land the answer is a civil zone with its daylight saving
(`Australia/Sydney`, `Australia/Adelaide`, `Australia/Lord_Howe`). Within
about 12 nautical miles of a coast the civil zone still applies, so a boat
anchored off Sydney keeps Sydney's time. Further out the answer is the nautical
`Etc/GMT` zone for the longitude (`Etc/GMT-11` in the Coral Sea,
`Etc/GMT+10` in mid Pacific).

The finder builds on the first lookup, behind a `sync.Once`, and never at
boot. An empty answer from the finder is an error. It is never read as UTC.

Real answers the finder gave in the benchmark that settled this: Sydney,
`Australia/Sydney`. Shute Harbour, `Australia/Brisbane`. Adelaide,
`Australia/Adelaide`. Lord Howe, `Australia/Lord_Howe`. Coral Sea at -18, 158,
`Etc/GMT-11`. Mid Pacific at 0, -150, `Etc/GMT+10`. Five miles off Sydney,
`Australia/Sydney`. These are the table tests in `boat_time_test.go`.

### Places keep their own zone

A planned stay, a tide station, the first point of a passage-window route, a
forecast asked for `lat` and `lon`, and the start of a recorded passage each
look up the zone at their own position. The boat's zone is for the boat. A
forecast for a marina is in the marina's time. Upstream weather plugins receive
the real IANA name. Open-Meteo and WeatherKit both accept one, and the guest
contract from ADR 0035 is otherwise unchanged. A tide station's own published
zone still comes first, and the lookup at its position is the fallback.

### The last-fix rule

With no usable own-ship fix (the -1,-1 sentinel, a critical GNSS alert, a
failed read), the boat's time is the zone of the last trusted fix. Helmcentral
remembers that zone in `data/boat_zone.json`, so it survives a restart.

This is a fallback, so it is named and held to one place. `boatTimeAt` is the
only function that applies it. It logs once each time the rule starts to apply.
Mate's clock line says "last fix", and the vessel stream sends
`timezone_source: "last_fix"` beside the zone.

Before any fix has ever been seen, the boat's time is UTC, said plainly: Mate's
prompt says it is UTC because there is no position yet, and the stream sends
`"UTC"` with `timezone_source: "none"`. Watches refuse to run in that state
because a schedule can't be read without a clock. That is the one place a
missing clock still stops work. A stored zone that can't be read is an error
with a log line, and the boat's time is UTC with `none` until the next fix
repairs it.

### Mate's clock line

The prompt used to say "derived from the vessel's longitude". It now names the
zone: `Local time: Thursday 29 October 2026 10:34 (Australia/Brisbane, UTC+10).`
The offset is worked out at that instant, because daylight saving makes it
depend on the date. Tool results use a label computed the same way at the time
being shown: `AEDT (UTC+11)`, or `UTC+11` offshore where the zone has no name.

## What was rejected

- **An operator-set zone setting.** One more thing to forget to change. A boat
  that crosses a date line or a border would be wrong until someone noticed.
- **The browser's zone.** The wall display is stuck on UTC and a phone ashore
  is not on the boat's clock. This was the disagreement that started the work.
- **tzf at full precision.** Exact everywhere, and 360 MB at peak while the
  index builds. That doesn't fit a Raspberry Pi 5 beside everything else.
- **tzf fuzzy-only.** Cheap, but it answers with nothing near coasts, which is
  exactly where a boat spends its time.
- **bradfitz/latlong.** Its data dates from 2017 and it has no ocean zones, so
  it can't give the nautical zone offshore.

## Consequences

- **Size and memory.** The binary grows by about 9 MB for linux/arm64. The
  index holds about 30 MB of heap once built. On an M-series laptop it builds in
  under 0.2 s. A Raspberry Pi 5 is the target, so `TestZoneFinderInitStaysSmall`
  holds it to 3 s and 64 MB and CI runs it on a native arm64 runner.
- **Data licence.** The boundaries are ODbL data from OpenStreetMap through
  timezone-boundary-builder. `THIRD_PARTY_NOTICES.md` carries the notice, and
  `docs/reference/boat-time.md` says so in the operator's words.
- **Dependencies.** tzf pulls in protobuf at v1.36, up from v1.34.
- **Times shift for operators in daylight-saving zones.** Nothing to do on
  upgrade. Existing recorded instants are unchanged. Only the clock they are
  shown on moves.
- **Screens follow the server (step two, done).** Every time an operator reads
  is formatted in the boat's zone from the vessel stream, through
  `frontend/src/lib/vessel-time.ts`. The stream's `timezone_source` is read as
  well, so the clock tile says "last fix" or "UTC · no position yet". The
  browser no longer works out a zone from longitude: `place-time.ts` and
  `vesselZone` are gone. A place's zone comes from the server instead. A
  passage (summary and detail) and the planned stay each carry a `timezone`
  field, the IANA name from `zoneAt` at the first recorded point or at the
  stay's position. A passage with no recorded point has `timezone: null`, and
  the page shows `--` for its dates. A passage that cannot be placed in a
  zone says so on its own row: `timezone` is null and `timezone_error` gives the
  reason in plain words, and the reason is logged. Fail-fast is per record, so
  one such passage never takes down the passage list, the Mate logbook or the
  other passages. A position with no zone is never read as UTC. The Mate
  logbook lists such a passage without local times and with a
  `time_zone_note`. A bare-date range includes it only when its instant is near
  enough to the dates for some zone to put it inside them. The stay's `timezone` is worked out on every
  read, is not stored, and is ignored if a client sends one.
- **A lint rule keeps it that way.** `helm/no-zoneless-time` rejects
  `toLocaleTimeString`, `toLocaleDateString`, `toLocaleString` on a Date,
  `Intl.DateTimeFormat` and the local-zone getters (`getHours`, `getDate` and
  so on) unless a zone is named. A deliberate device-clock reading opts out with
  `// helm-browser-zone: <reason>` on the line before. Document timestamps (when
  the operator uploaded or ticked something) use it, as do the maintenance
  "today" date (ADR 0138) and the vessel-time formatter's fallback before the
  first vessel frame.
- **Exports keep one offset.** The standalone passage player counts days with a
  single fixed offset. It takes the offset in force at the first passage's
  departure, in the passage's zone, so a passage that crosses a daylight-saving
  change keeps that offset throughout.
