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
2. **An empty list is an answer.** No alarms returns `{"count":0,"alarms":[]}`. If the alarm
   source is not wired the tool errors; there is no fallback.
3. **A prompt rule next to the diagnostics rules.** For any alarm question, call
   `get_active_alarms` first, pass the identifiers it returns to `check_signalk_paths`,
   `get_last_recorded` and `get_path_history`, never a path guessed from the device name,
   and keep the value at raise apart from the live value.

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
