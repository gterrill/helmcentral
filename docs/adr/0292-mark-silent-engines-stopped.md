# ADR 0292: Mark silent engines stopped

## Status

Accepted (2026-11-19). An approved fallback under the Exception Rule in
`AGENTS.md`: it is behind a setting, off by default, and logs every time it acts.

## Context

On Pikorua the engine gateways stop sending NMEA 2000 data when the engine is
switched off. The last `propulsion.<instance>.revolutions` SignalK receives is
the idle figure, about 11.6 Hz, never zero.

Two SignalK plugins then draw the wrong conclusion from that silence:

- **signalk-derived-data** writes `propulsion.<instance>.state = "stopped"` only
  when a revolutions update arrives at zero or null. None arrives, so the state
  stays `"started"` for good.
- **signalk-autostate** believes an engine is running and reports `motoring`
  whenever it sees movement. A boat drifting on the tide, or sailing with the
  engines off, is called motoring.

Helmcentral already judges for itself that such an engine is off: the Engine
Cluster tile reads it from `engineStates`. The bus still carries the wrong
state, and everything else that reads `navigation.state` goes on believing it:
the collision profile syncer (ADR 0058) and the watch report's under-way test
among them.

## Decision

With the setting on, Helmcentral writes `"stopped"` to
`propulsion.<instance>.state` for a mapped engine that its own engine-state
judgement (`engineStates`, the Engine Cluster tile's off/lost call) reads as
`off` and witnessed, while SignalK still says `"started"`. Witnessed means a
device that is not an engine, on the same connection, has been sending. Without
that witness the write does not happen.

- **Setting.** `engines.mark_stopped_when_silent` in `settings.yaml`, shown as
  **Mark engines stopped when RPM goes quiet** under Settings → SignalK. It is off
  by default, and a missing key reads as off. Nothing in it is vessel specific.
  A settings save whose request omits `engines` leaves the stored block alone,
  the way it leaves `roles` alone.
- **One definition of a quiet engine.** The stopper does not keep its own RPM
  clock. It asks `engineStatesFor`, which already answers `running`, `off` or
  `lost` for each propulsion id. `off` means every source for the engine has been
  quiet for two minutes while its connection still carries other data, which is
  what a key-off looks like. `lost` means the connection went quiet too, which is
  a gateway failure. The judgement reads all of the engine's data, not only its
  revolutions, so the help text says the engine's data has been quiet. Because the tile and the bus read the same answer, they
  cannot disagree about whether an engine is off.
- **Which engines.** Only the engines the operator has mapped (the engine items).
  The states are keyed by propulsion id, which is the mapped engine's `Instance`
  (the assistant's vessel status reads them the same way). An id nobody mapped is
  never touched.
- **When.** On the alarm evaluator's tick, in the shape of the collision profile
  syncer: a small struct with a `check(now)` and injectable functions. It judges
  at most every 15 seconds, since the judgement has a two-minute grain. It writes
  only when the engine's state is `off` with a witness and
  `propulsion.<instance>.state` reads `"started"`. `running`, `lost`, an
  unwitnessed `off`, an engine with no entry and a nil map never write. Turning
  the setting off forgets every backoff, so turning it on starts clean.
- **Its own writes are not engine activity.** Deltas from Helmcentral's own
  source label (`helmcentral` and `helmcentral.XX`, exactly) come back on the
  read stream. Any other suffix, such as `helmcentral.115`, is a real device that
  shares the name and is recorded as usual. The snapshot does not record them in its per-source history,
  so they neither make a quiet engine read `running` nor vouch for a connection.
  The root is the snapshot, not `engineStates`, because the same would be true
  of any consumer of that history, Helmcentral's notifications included.
- **Cost with the setting off.** The tick reads the flag from the shared settings
  parse and starts nothing. With it on, the check asks for the engine states
  first and resolves the mapped engines only when some engine is `off`. The
  list is cached and re-read when equipment is edited (the source-items
  generation counter) or after five minutes, so an engine left off all winter
  does not cost a store read every 15 seconds.
- **Where it runs.** The tick starts the check on its own goroutine and skips a
  tick while the last check is still going. A publish waits on the SignalK
  server for seconds, and the evaluator must not.
- **How it writes.** One delta over the existing publish transport
  (`publishSignalKValue`, source label `helmcentral`), confirmed by reading the
  value back from the server's model, the way a notification is confirmed. The
  only value it ever writes is the string `"stopped"`.
- **Stalled stream.** `engineStates` returns nil when Helmcentral's own SignalK
  stream has delivered nothing for more than 10 seconds
  (`silentSourceStreamMaxAge`). A nil map writes nothing, so the check judges
  again as soon as messages arrive.
- **Level-triggered.** The rule is a state, not an event: on each check, an
  engine that is `off` and witnessed while SignalK reads `"started"` gets a
  write. Once SignalK reads `"stopped"` the condition is false and nothing is
  written, so a gateway that stays silent for a week gets one write, not 600 000.
  The write is confirmed by reading the value back from the server before it
  returns, so a "started" that is still there on the next check is a real
  difference, such as a SignalK restart that re-emitted the old state. The
  stopper corrects it rather than trusting a record that it already wrote. An
  earlier draft remembered each write per engine and wrote once per silence. The
  operator dropped that mark: it could hide a state SignalK had gone back on.
- **A missing state path.** A mapped engine judged `off` and witnessed whose
  `propulsion.<instance>.state` has no value at all has nothing to correct. The
  stopper logs that once per engine, until the path appears, and writes nothing.
  The line names the engine item and its own propulsion id:
  `engine state: Port Engine has no propulsion.port.state on SignalK, so it can't be
  marked stopped (setting "Mark engines stopped when RPM goes quiet")`.
- **Failure.** A failed publish is logged and retried after a minute, then after
  two, doubling to a cap of ten minutes. It reuses `saveBackoff` from the
  collision profile syncer, with a first delay and cap of its own. The failure is logged on each try, which the backoff
  keeps to a handful an hour.
- **Logging.** Every action is one line, once per write, counting from the
  engine's last update:

  ```
  engine state: port has been off for 150s (no RPM while its connection still sends other data) but SignalK says started; marked stopped (setting "Mark engines stopped when RPM goes quiet")
  ```

  An unreadable setting is logged once and the check does nothing until it
  reads.

## Why the state and not the revolutions

A written zero in `revolutions` would be a measurement nobody took. It would
reach Influx as a reading, and from there the engine hour meter, the fuel
estimate and the learned normal (ADR 0276), each of which would treat it as
real. The state is a conclusion, and a labelled one: Helmcentral's source label
is on the write.

It is also self-correcting. The derived-data plugin sets the state back to
`"started"` when the next real revolutions arrive, so the write never outlasts
the engine being off.

## Why it is off by default

The fix is a write to the boat's bus, and it rests on an assumption about the
hardware: that an engine going quiet while its connection stays busy means off.
That holds for the Pikorua gateways and for some others. It does not hold for a
gateway that is merely slow, or an engine whose feed is wired through something
that drops out.

## What it risks

The stopper cannot tell a key-off from the engine's own interface failing while
the engine runs. When Helmcentral loses the whole connection to the engine data,
every source on it goes quiet, `engineStates` reads `lost`, and the stopper never
writes. But a gateway such as a CANvu that fails or loses power while other
devices on the same NMEA 2000 network keep talking produces the same data as a
key-off: the engine is quiet and its connection is witnessed. After about two
minutes the stopper marks the running engine stopped. derived-data puts
`"started"` back as soon as RPM returns. This is the risk the operator accepted
by choosing the `engineStates` rule, and the help page says so plainly.

The case `engineStates` marks as unwitnessed is left alone: an engine that is
the only thing on its connection looks the same when the connection fails, so a
failure can pass for a switch-off. The stopper does not write for it. An engine
alone on its connection is never marked, and an operator with that wiring gets
nothing from the setting.

A failure of Helmcentral's own link to SignalK is not this risk: a stalled
stream gives a nil map and nothing is written. The write is logged, so the cause
can be read after the fact, and the state comes back by itself when the
revolutions do.

## What was left out

- **Writing a zero to `revolutions`.** Rejected for the reason above.
- **Doing it by default.** Rejected: see above.
- **A 30 second RPM-silence rule of its own.** The first draft judged silence
  from the revolutions timestamp and a stream-age guard. Rejected: it would have
  been a second definition of a quiet engine beside `engineStates`, and it could
  not tell a key-off from a failed gateway. It was also quicker (30 seconds, not
  two minutes). The slower call is accepted.
- **A per-engine switch.** One switch covers every mapped engine. A boat with one
  silent gateway and one honest one has not asked for it.
