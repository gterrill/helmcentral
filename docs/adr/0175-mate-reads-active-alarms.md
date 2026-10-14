# ADR 0175: Mate reads the active alarms

## Status

Accepted (2026-10-14). Adds a tool under the registry rule of
[ADR 0172](0172-mate-tools-are-one-record-each.md). Wording of the alarms themselves is
[ADR 0157](0157-alarm-cards-say-what-happened-not-where-it-came-from.md).

## Context

Asked to "investigate the alarm", Mate had only the card text, "Impossible sensor reading:
PortALT voltage 72.3 V". It took "PortALT" as a device, matched it to
`electrical.alternator.2`, and investigated the wrong sensor. The alarm itself knew better:
its `sensors` and `live_sensors` entries carry the real identifier,
`electrical.batteries.1.voltage`, with the rule id, the watched path and the evidence. Mate
had no way to read any of it. Since ADR 0157 the card deliberately hides identifiers, so the
text Mate could see was the text least able to point at the sensor.

## Decision

1. **A read-only tool, `get_active_alarms`.** It returns the list the card and banner are
   built from (`activeAlarms`, including the live sensor evidence), reshaped for the model:
   label, message, state, phase, `raised_at`, `acked_at`, silenced, rule id, watched path,
   value, unit, operator and threshold where the alarm has them, and for sensor-health
   alarms each sensor's identifier, name, quantity and text, from `sensors` (at raise) and
   `live_sensors` (now). Nothing is derived or invented.
2. **An empty list is an answer.** No alarms returns `{"count":0,"alarms":[]}`; a result
   with times also carries `timezone`. If the alarm source is not wired the tool errors;
   there is no fallback.
   - **Times.** `raised_at` and `acked_at` are boat local time with a `timezone` label. The
     vessel position is read only when some alarm has a time to format. If it cannot be
     read, the times are UTC, `timezone` says so, and `timezone_note` gives the reason, so
     Mate does not present UTC as local time. The failure is also logged.
   - **What to investigate.** Each alarm carries `investigate`. A rule alarm gives its
     watched path. A bus alarm gives its path without the `notifications.` prefix, but only
     when the live tree has a value there; many bus alarms (collision, radar guard zone,
     `vebus.lowBattery`) name a notification with no data behind it, and their list is
     empty, meaning the alarm has no sensor of its own to read. A sensor-health alarm gives
     its sensor identifiers (a path, or a `$source` id for a silent source). The alarm's own
     `path` is a bus or derived path the history tools cannot use as is.
   - **Readings.** `value` is quoted only for a rule alarm with a threshold. A sensor-health
     alarm's value is a count of failing sensors and is never quoted.
   - **Live sensors.** Only sensor-health alarms carry `sensors` and `live_sensors`. When
     the sensors failing now match those at raise, `live_sensors` is replaced by
     `live_same_as_raised: true`, always and whatever the result size. For a sensor-health
     alarm a missing `live_sensors` without that marker means nothing is failing now; for
     rule, bus and collision alarms its absence says nothing.
   - **Size.** The result is capped. When it is too large it shrinks in bulk: the longest
     sensor or `investigate` lists are cut to half the longest length, repeatedly, so short
     lists keep their identifiers; `sensor_count`, `live_sensor_count` and
     `investigate_count` give the full length of any list that was cut. Only when lists are
     down to one entry are trailing alarms dropped, worst kept, and only then are `truncated`
     and `shown` set: `truncated` means alarms were left out, and `count` stays the true
     total. If nothing more can be cut the tool returns the standard "too large" note.
3. **A prompt rule next to the diagnostics rules.** For any alarm question, call
   `get_active_alarms` first, pass the `investigate` entries it returns to `check_signalk_paths`,
   `get_last_recorded` and `get_path_history`, never the alarm's own path or one guessed
   from the device name. An empty list means the alarm has no sensor of its own, and Mate
   says so. Keep the value at raise apart from the live value.

## Consequences

- The investigation starts from the sensor that tripped, whatever the operator named it.
- The identifiers stay off the card and out of notifications; only Mate sees them.
- The tool costs a round trip, but only on turns that ask about an alarm.

## Rejected alternatives

- **Inject the active alarms into the system prompt every turn.** Most turns have no alarm
  question, so it would spend tokens on all of them, and the prompt prefix is cached: a
  list that changes with every alarm would break the cache and, held across a long chat,
  would go stale.
- **Show the identifier on the card.** It undoes ADR 0157 for the watchkeeper to fix a
  problem only Mate has.
