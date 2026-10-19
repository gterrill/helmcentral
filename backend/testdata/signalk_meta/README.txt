Real frames captured 2026-10-20 from the boat's SignalK 2.33.0
(ws://192.168.50.240:3000/signalk/v1/stream?subscribe=none&sendMeta=all, after
sending the same subscription the client sends). One frame per file, untrimmed.
hello.json         server hello (names self)
meta_soc.json      meta update: electrical.batteries.512.capacity.stateOfCharge, units "ratio"
values_soc.json    values update for the same path
meta_no_units.json meta update for design.draft: units only nested under properties, none at top level
