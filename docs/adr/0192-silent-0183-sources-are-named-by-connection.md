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
- Phrase: a talker id does not say what was sent (the boat's Vesper Cortex
  relays wind, depth, heading and speed under its `AI` talker), so the words
  come from the NMEA 0183 sentence ids each source published, recorded per
  `$source` from the update's source object and bounded. Fixed order, deduped:
  AIS (VDM, VDO), position (RMC, GGA, GLL, GNS, VTG, GSV, GSA, ZDA), heading
  (HDG, HDT, HDM, THS), wind (MWV, MWD, VWR, VWT), depth (DPT, DBT, DBS), boat
  speed (VHW), log (VLW), water temperature (MTW), weather (MDA, XDR). Unknown
  sentences are dropped. If every source on the connection is silent, all the
  words it carried are named. If some still send, only words carried
  exclusively by the silent ones are named; if none, "some of its data". With
  no known sentences the text is just "has stopped sending".
- Ignore posts the connection id; the silent-source alarm skips any 0183
  source whose connection is ignored. This is applied only to the alarm, not to
  the quiet-source set that decides which readings the detectors trust, so
  ignoring a connection never makes a stale reading from it trusted. The
  settings list names the identifier when it is a known 0183 connection.
- The connection of a 0183 source is everything before its last dot (the talker
  is the segment after it); other buses keep the first-dot split. One helper
  serves the card grouping, engine key-off and the engine-state check, so they
  agree about the same id.
- A sibling talker counts as still sending only when heard within its own quiet
  threshold. Ignored, unwatched or key-off-excluded talkers that are quiet are
  not sending. If unsure, not sending.
- A single silent source keeps the earlier naming order (bank, installation,
  model, manufacturer, then the connection name, then "An instrument"). The
  silent count the rule reads is the number of card entries, devices not talkers.
- The seeded title is "Instrument Silent". It is replaced once, at raise, from
  the entries the alarm freezes, and stays for the alarm's life, including the
  escalation and cleared messages: "Vesper Cortex Silent" when every source on
  the connection is quiet, "Vesper Cortex Partly Silent" when some still send,
  "Instruments Silent" with more than one device. A title the operator edited
  is left alone.
- The device name in the title is the SignalK connection id, which the operator
  typed in the SignalK admin, so it is their own name for it.

## Consequences

A talker that has published too few updates to be watched yet counts as still
sending, so it can turn a full list into a shorter one. Ignored entries stored earlier as a full source id keep working.
