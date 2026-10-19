# ADR 0192: Silent NMEA 0183 sources are named by their connection

## Status

Accepted (2026-10-19). Extends [ADR 0164](0164-sensor-health-cards-name-the-sensor.md)
and the wording rules of [ADR 0157](0157-alarm-cards-say-what-happened-not-where-it-came-from.md).

## Context

A multi-talker NMEA 0183 unit publishes under one SignalK connection with one
`$source` per talker id (`Vesper_Cortex.AI`, `.GA`, `.GB`, `.GN`, `.GP`, `.GQ`).
The sensor namer only knew NMEA 2000 device information, so each of the six
fell through to "An instrument" and the Silent sensor source card showed six
rows, "An instrument (1 of 6)", with six Ignore buttons.

## Decision

- A source is NMEA 0183 when the update's source object declared `"type":
  "NMEA0183"`. The type is recorded on the source's history and carried into
  the silent-source check. The shape of the id (`<conn>.<two capitals>`) is not
  used: it would also claim a plugin or future bus and misname it, and a source
  whose type was never seen falls back to "An instrument".
- Its name is the connection id, underscores and hyphens read as spaces. That
  is the operator's own name from the SignalK config.
- Silent 0183 sources on one connection are one entry, identified by the
  connection id. NMEA 2000 sources are never grouped: a gateway carries many
  devices.
- Phrase: if every talker the connection has published is silent, the device
  has stopped and what it sends is named collapsed (any satellite talkers read
  "satellite positions"). If some still send, the silent talkers are listed by
  name. Unknown talker codes are never printed.
- Ignore posts the connection id; the silent-source check skips any 0183
  source whose connection is ignored. The settings list names it the same way.
- The seeded title is "Instrument Silent". The engine replaces it when the
  rule still carries that or the old "Silent sensor source" title: "Vesper
  Cortex Silent" with one quiet device, "Instruments Silent" with more. A
  title the operator edited is left alone.

## Consequences

A talker that has published too few updates to be watched yet counts as still
sending, so it can turn a "stopped sending satellite positions" into a list of
named talkers. Ignored entries stored earlier as a full source id keep working.
