# ADR 0226: Ask Mate about an alarm

## Status

Accepted (2026-10-28). Builds on [ADR 0175](0175-mate-reads-active-alarms.md)
(`get_active_alarms` and its `investigate` list), [ADR 0167](0167-mate-knows-which-document-is-on-screen.md)
(the screen context), [ADR 0183](0183-one-mate-session-on-the-client.md)
(`useMateSession`), [ADR 0225](0225-mate-proposes-alarm-rules.md) (`list_alarm_rules`
and alarm_rule proposals) and [ADR 0172](0172-mate-tools-are-one-record-each.md)
(the tool registry). It is cycle 5 of the agent-first plan: ranked opportunity 1
and the first half of item 8 of `docs/research/agent-first-helmcentral.md`. The
automatic "Mate reviews" flag is not in this cycle.

## Context

Mate was never told an alarm fired. The operator could type "why did the house
bank alarm go off?" and Mate would call `get_active_alarms`, which says what is
alarming now but not what the path did around the raise, how often the alarm has
come before, what else changed at the same time, or what the rule behind it is.
A cleared alarm could not be asked about at all. Two rules from the research
notes bound the change: the alarm stays deterministic and Mate only explains it,
and Mate's text never replaces the card's title or body, which have one presenter
(AGENTS.md "Alarm Wording", [ADR 0157](0157-alarm-cards-say-what-happened-not-where-it-came-from.md)).

## Decision

**The alarm rides on the screen context.** No new attachment mechanism. The
`screen` object sent with every question (ADR 0093, 0167) gains `alarm_id` and
`alarm_title`. `openMate` takes an `alarm` option beside `newConversation`; App
keeps it in state, layers it over the screen with `withAlarmContext`, and clears
it once the seeded question has been answered (the tool result is then in the
conversation's history for follow-ups), when the operator starts a new
conversation or picks another from search, and when the sheet closes. The server caps `alarm_title` like the other fields. It drops an
`alarm_id` over 200 runes instead of cutting it, because a cut id names a
different alarm (a collision alarm's id holds a vessel urn).
`assistantScreenSentence` then says the operator tapped Ask Mate on that alarm
and to call `get_alarm_context` with the id before answering.

**The id is the card's own.** An active card sends its `rule_id` (a stored rule's
id, a derived `tank:` or `zone:` id, or `notifications:<path>` for a network
alarm). A History row sends the log row's `id`, so a cleared alarm is asked about
as that occurrence and not as the newest one with the same rule.

**Where the button is.** On each active alarm card in the Alarms drawer, beside
Acknowledge, and on each History row. Not on the banner. The banner is one strip
for the whole board, with its own View and Acknowledge buttons, and it already
has no room for a per-alarm action. View opens the drawer where each card has
one. Both open a new conversation with a visible question, "Why did House Bank
Low go off?". A History row sends its log id, which pins the occurrence, so the
question carries no time. The drawer draws no button when the
page cannot open Mate.

**`get_alarm_context(alarm_id)`**, a read tool in the registry
(`assistant_alarm_context.go`). It finds the alarm among the live ones by
`rule_id`, then in the log by log id, then as the latest occurrence of that rule,
and fails when none matches. It returns, in boat local time and the operator's
units:

- the alarm as the card shows it: title (the label), body (the message), state,
  phase, raised, acknowledged and cleared times, whether it is still live, and
  whether a rule or the boat network raised it. Nothing is reworded. A log row is
treated as the live alarm only when it is the rule's open occurrence; an older
row that was never closed is reported as logged, with a note that no clear was
recorded.
- the rule: the same entry `list_alarm_rules` gives (op, value, clear point,
  dwell, severity, built-in or own, version, or read-only with where to change
  it), found among stored and derived rules. A network alarm, or a rule since
  deleted, gets `rule_absent` with the reason.
- the trigger path: the rule's path, or the alarm's `investigate` target.
- the live reading, its source and age, and whether it is stale, from the same
  snapshot `check_signalk_paths` reads.
- history: `get_path_history` is called for the window from 30 minutes before
  the raise to 30 minutes after (or the clearing, up to two hours, never past
  now), and its overall min, mean, max, first and last seen and gap count are
  given in display units, converted before they are rounded (a ratio of 0.4951
  reads 49.51 %, not 50 %). It is the same query, so angles keep their vector
  mean. A `shape` is computed from the buckets: the largest step between
  neighbouring readings, the longest run of one identical value, and the bucket
  count and width, which is what lets Mate speak of a jump or a frozen sensor
  with figures. The window's start and end and the first and last sample are
  also given in boat local time with the result's timezone, so every time is in
  one frame.
- the alarm's own log: how many times the rule was raised in the last 7 days.
- what else changed: other alarms raised or cleared within 15 minutes of the
  raise, from the alarm log, at most 12. The alarm's own rule is left out in
  SQL, so its repeats cannot use up the row limit; `cleared_at` has an index.

Each part that cannot be given is absent with a reason: no rule, no data path
(a collision or radar alarm), SignalK not connected, InfluxDB not wired, an
empty window, an unreadable log. A history query that fails is reported with its
error, not skipped. The result carries `sibling_paths: []` and a note instead of
a guess: Helmcentral keeps no grouping of related paths. The anomaly detector
works per path and an item's other paths are not tied back to an alarm without
the equipment record, so a cheap, correct notion does not exist yet.

**Log lookups.** `alarmLogStore` gains `Get`, `LatestForRule`, `RaisedCount` and
`ChangedBetween`. A new `alarmLog` dependency reaches the store; a nil store is
reported as absent, never as an empty history.

**The prompt.** One fixed paragraph in the stable prefix: call `get_alarm_context`
first when asked about one alarm; say what tripped it with figures against the
threshold, clear point and dwell; say whether the reading looks real or like a
sensor problem as evidence (freshness, a window that moved smoothly, sat flat or
jumped, other alarms, how often it came before) and never a cause the figures do
not show; say what the tool left out; draw the window with `show_telemetry_chart`;
never restate the card's title or body as Mate's own; and if the threshold looks
wrong for how the boat behaves, offer an `alarm_rule` update proposal with the
rule's version, which the operator applies and Mate never does.

## Consequences

- Asking about a cleared alarm works, within the log's retention.
- The same turn can end in an Apply card, so the research notes' 02:00 flow
  (alarm, explanation, a fix to tap) is one conversation.
- The alarm context is dropped after the seeded question is answered, so an
  unrelated question later in the same sheet is not read as about the alarm.
  A follow-up still works because the tool result is in the history.
- Sibling paths, and the automatic review when an alarm raises, are follow-ups.

## Amendment, 2026-10-29: first answers from the dev stack

Two alarms asked about in the dev stack showed four faults, fixed together.

- The collision profile refusal raised a new alarm event on every
  navigation.state change while the SignalK read kept failing, and the log held
  327 open rows in a week. The syncer now raises once. While that refusal
  stands, a refusal with different words (another state or cause) updates the
  card in place, and the same words do nothing. The alarm log is not changed:
  a store-level "reuse the open row" rule was rejected, because a restart leaves
  open rows behind for the watchdog and anchor checks and a fresh raise must not
  be folded into one.
- The refusal body is one plain sentence chosen by the HTTP status (401 and 403
  read as a refused login, on reading and on switching), with the technical error kept in the server log only.
- `get_alarm_context` reports `built_in_check` instead of "deleted" for ids in
  `builtInAlarmCheckIDs`, and adds `occurrences.pattern` at 20 or more raises in
  seven days, a daily rate over the span the raises cover or, under two days,
  a count since the first raise. The built-in list was kept over a new log
  source value because rows logged before the change carry the source "rule".
  A test checks the event builders against the list. The `helmcentral:` prefix is not used to decide, because the
  seeded stored rules carry it too.
- The prompt rule now asks for the operator's words, no internals, no list of
  missing fields, a plain "keeps going off", and a next step only when the cause
  points to one.
