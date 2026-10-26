# ADR 0214: One set of tank levels drives the tiles and the alarms

## Status

Accepted (2026-10-27). Amends [ADR 0061](0061-cluster-fuel-rail.md) and
[ADR 0187](0187-equipment-items-own-their-signalk-source.md): a fuel bar no
longer carries zones or raises an alarm. Uses the derived-rule approach of
[ADR 0050](0050-gauge-zones-as-the-alarm-source.md) and the card wording of
[ADR 0157](0157-alarm-cards-say-what-happened-not-where-it-came-from.md).

## Context

The Furuno MFD alarmed on a low forward fuel tank. Helmcentral showed nothing.
The alarm never reached SignalK: the Furuno gateway publishes no `tanks` branch
under `notifications`, and the TZT2BB stayed silent on the bus. Mirroring a
chartplotter's private alarms is not something Helmcentral can do, so it raises
its own.

Three places already disagreed about what "low" means for a tank:

- The Tanks tile coloured by numbers baked into `tanks-tile.tsx`: fuel warned
  below 30 percent, water at 20, waste at 80.
- The Engine Cluster's fuel bars kept their own zones, because a tank has no
  profile to hold them, and those zones raised `zone:tank:<item>:<n>` alarms.
- Nothing else. A tank with no fuel bar and no hand-written rule never alarmed.

## Decision

One pair of levels per tank, a warn level and an alarm level, in percent of
full. The pair colours the Tanks tile, colours the fuel bars, and raises the
alarms.

### Where the levels come from

- **Kind defaults.** Fuel 20 and 10, fresh water 20 and 10 (low is bad), waste
  80 and 90 (high is bad). The kind is the one `tankKindFromCategory` already
  gives the tile. They are generic; nothing in them is one boat's. The operator
  edits them in Settings, Alarms, Tank levels. They are stored in
  `tank-alarm-defaults.json` beside `alarm-rules.json`, for the same reason the
  rules are: `POST /api/settings` rebuilds its sections wholesale and would
  drop a new key. A missing file reads as the built-in numbers. A file that
  exists but cannot be parsed or fails validation stops startup rather than
  falling back.
- **Per-tank override.** Three columns on the tank's equipment item:
  `tank_warn_percent`, `tank_alarm_percent` (0 means blank, use the default)
  and `tank_alarms_off`. Only an item whose source is a tank may carry them. The
  resolved pair must run the right way round (alarm below warn for a low-bad
  kind, above for waste), checked on save with blanks filled from the current
  defaults. A default changed later can leave an override backwards. That tank
  then raises nothing and the log says so once, rather than alarming on a pair
  nobody chose.
- **Alarms off** silences the alarm only. The tile still shows the level and
  its colours, because the colours are the levels.

### What raises an alarm

- Only a tank with an equipment item. A tank nobody has mapped (the unmapped
  `fuel.0` and `fuel.1`) is not listed on the tile and never alarms.
- Two derived rules per tank, `tank:<item id>:warn` and `tank:<item id>:alarm`,
  built each tick from the items and the defaults in `tank_alarms.go`. They are
  not seeded into `alarm-rules.json`, so editing a level changes the alarm and
  the tile together and there is nothing to migrate.
- The level must stay past the threshold for 60 seconds to raise. It clears
  when it is 5 percentage points back on the good side: a warn at 20 clears
  above 25, a waste warn at 80 clears below 75. Both are constants. A tank
  sloshes, and a setting for either would only be a way to switch the alarm off.
- A reading older than 120 seconds, the age the Tanks tile calls stale, raises
  nothing, and a live alarm is held rather than cleared by it. The silent
  sender is the Instruments Silent alarm's business, not a level alarm's.
- Severities are SignalK's: crossing the warn level is `warn`, crossing the
  alarm level is `alarm`. Both rules can be live at once, so the alarm list
  collapses them to one card per tank, the worse one. An operator who
  acknowledged the warn sees a fresh unacknowledged card when the level crosses
  the alarm level. Notifications go through the transports like any rule alarm.
- A tank with Helmcentral alarms owns its `tanks.<kind>.<n>.currentLevel` path
  in `helmcentralOwnsPath`, the same as an enabled rule would, so a foreign
  notification on that path is superseded.

### What the card says

- **Title:** the tank's name and "Low" or "High": "Fuel Port Fwd Low", "Black
  Water Stbd High".
- **Body:** the level, then the volume when SignalK publishes a capacity:
  `19% · 228 L`. Without a capacity it is `19%`. The server builds the same
  text for the notification transports. The volume is in litres, the unit the
  Engine Cluster shows.
- **Footer:** "Clears above 25%" (or "below" for waste), taken from the rule's
  clear value. It sits beside the raised time, never in the body.

### Fuel bars

A fuel bar takes its colour bands from its tank's resolved levels and stops
raising alarms. `Zones` is gone from the bar, `tankLevelReading` and the
`zone:tank:` ids are gone, and the Engine Cluster dialog no longer has a low
fuel alarm editor. The Tanks feed carries each tank's resolved `warn_percent`,
`alarm_percent` and `alarms_off`, and the dashboard hands those to the cluster
host, so the tile, the bars and the alarms read one set of numbers. Zones
already stored on a bar are dropped the next time the page is saved. There is
one operator and no installed base, so there is no migration.

## Rejected

- **Seeded, editable alarm rules.** Seeding two rules per tank into
  `alarm-rules.json` would let them drift from the tile's colours and leave
  rules behind when a tank item is deleted or remapped. Derived rules follow the
  items.
- **Leaving it to hand-written rules.** That is the state that let the forward
  tank run low unannounced. The default has to be an alarm.
- **Keeping the fuel bar zones as a second source.** Two ways to say what low
  means is how the tile, the bars and the alarms came to disagree.
- **A tank's alarm disabled by blanking its levels.** Blank means "use the
  default", so the off switch is its own control.
- **Mirroring the Furuno alarm.** It does not reach SignalK to be mirrored.

## Consequences

- The Tanks tile's fuel warning moves from 30 percent to 20 and now reads
  strictly past the level, as the alarm does.
- Engine Cluster fuel bar zones set before this release stop colouring and
  alarming. The operator sets levels on the tank's equipment page instead.
- A tank whose sender goes quiet shows stale on the tile and stays out of the
  level alarms.
- The Alarms rules list shows each tank's warn and alarm rules, read-only,
  marked as coming from the tank levels.
