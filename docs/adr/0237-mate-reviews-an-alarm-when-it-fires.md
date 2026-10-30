# ADR 0237: Mate reviews an alarm when it fires

## Status

Accepted (2026-10-31). Builds on [ADR 0226](0226-ask-mate-about-an-alarm.md)
(`get_alarm_context`, the alarm context Ask Mate builds, and the automatic
review it left for later), [ADR 0224](0224-alarm-rules-live-in-the-database.md)
and [ADR 0225](0225-mate-proposes-alarm-rules.md) (rules in SQLite, and Mate
proposing them), [ADR 0227](0227-watches.md) (the unattended turn, the submit
tool, the cost meter) and [ADR 0120](0120-turning-mate-on-is-the-consent.md)
(turning Mate on is the consent). It finishes cycle 5 of the agent-first plan.

## Context

Ask Mate answers when the operator asks. At 02:00 the operator is asleep when
the alarm raises, wakes to the sound, and has to open Mate and type a question
before learning whether the reading is real. Watches showed that an unattended
Mate turn can read the boat and write something short and useful. The alarm is
the other place where a read in context, done before anyone asks, is worth the
cost.

Two rules from the research notes bound the change. The alarm stays
deterministic: Mate never raises, clears, holds or rewords one (AGENTS.md "Alarm
Wording", [ADR 0157](0157-alarm-cards-say-what-happened-not-where-it-came-from.md)).
And a model call that no one asked for needs the operator's say-so, once.

## Decision

**The switch is the consent, per rule.** An alarm rule gains `mate_reviews`,
off by default, stored with the rule (column `alarm_rules.mate_reviews`, added
to a database made by an earlier build when it opens). With Mate off, a
rule's raise starts no review and writes no row: turning Mate off withdraws the
consent the switch rested on. The rule editor still shows the switch, disabled
and with the line "Mate is off, so it won't review this", so the operator sees
the setting; one that is on stays enabled so it can be turned off. The rule
keeps its setting and saving it does not clear it. There is no spending cap,
matching Watches: ADR 0120 already made turning Mate on the consent to send
the boat's readings to the model, and the switch narrows it to the alarms the
operator chose. Mate can set it through an `alarm_rule` proposal
([ADR 0225](0225-mate-proposes-alarm-rules.md)) as a writable `mate_reviews` field,
so the operator's Apply tap stays the consent to that too. Built-in checks,
tank and gauge-zone alarms (which have no stored rule to carry the switch) and
network alarms are not reviewed in this cycle.

**When it runs.** `recordAlarmEvent` calls `alarmReviewer.onRaise` after the
alarm has been queued for notification, so a review can only follow the alarm.
It acts on a raised event of a rule-sourced alarm whose rule has the switch on.
A repeat raise of the same rule within an hour of its last review's start is
not reviewed. `onRaise` only checks the event and starts a goroutine, because
it runs on the alarm evaluator's path and nothing there may wait on the
database. The goroutine reads the settings, takes the claim (the window check
and the record of the new review, one step under a lock, so raises that arrive
together are reviewed once) and runs the turn. The card path reads a published
copy of the latest reviews and never the database. A failed review counts as a
review for this window: retrying a failing model on every raise is the kind of
silent repeat the fallback policy rules out, and the failure is on the card.
A review that ended with Helmcentral does not count.

**Restart and shutdown.** At startup a review still "running" is marked failed
("Helmcentral restarted during the review"). When the server stops, the
reviewer's context is cancelled, each review under way is recorded as failed
("Helmcentral stopped during the review"), and main waits up to ten seconds for
that. Both kinds are skipped by the window. A raise that carries no raise time
is a bug upstream: the review is recorded as failed and says so.

**Boat time.** The turn's date and the prompt's times are the boat's, read as
the watch review reads them. With no position fix the prompt says the times are
UTC and not boat time.

**The turn.** It is the watch review's shape, not a stored conversation turn:
`assistantRunner` is driven directly with a short system prompt, the alarm
context as its only message, and a tool list narrowed to the watch review's read
tools without `search_web`, plus `submit_alarm_review`. The turn ends on that
call, so the review is parsed from arguments, never read from prose. A reply
that ends without it is a failure. `propose_changes`, `start_watch`, the form
tools, `run_watch_now`, chart tools and web search are neither offered nor
executed. The context is `get_alarm_context` called before the model is, so a
raise whose details cannot be read fails at once, and the model can still call
the tool again to look closer. The turn has a three minute deadline; whatever
the turn spent is still priced.

`submit_alarm_review` takes `review` (up to 1,200 characters) and an optional
`suggested_rule_change` (up to 300). The tool refuses text with `!` or `?`, the
rule's id or path, or anything shaped like a SignalK path, and the model gets
the reason and may submit again. This is the Alarm Wording rule enforced where
it can be: the card, the phone and the conversation all show the same words.

**Review rules in the prompt.** Say what the reading is doing and whether it
needs the watchkeeper now, in the first sentence, because the first sentence is
what goes to the phone. Use the named source and never guess a cause. Say
"looks real" or "looks like a sensor problem" only from evidence. No paths, rule
ids or device keys, and no question or exclamation marks. At most one suggested
change to the rule, in words only, because the turn cannot propose.

**Where the answer goes.** A review is stored in the `alarm_reviews` table (rule,
raise time, state, conversation, first sentence, problem, cost, model, follow-up
state). A finished review also becomes a Mate conversation of the kind Ask Mate
opens, titled with the alarm's title, holding the question "Why did House Bank
Low go off?" and the review as Mate's answer, with its model and cost like any
other answer. A suggested change is appended to that answer as "A change to the
rule to consider". The operator can carry on from there like any conversation;
the alarm id is not carried in, so a follow-up that needs the alarm's figures
asks Mate to look it up by name.

On the card, `activeAlarms` attaches the rule's latest review as `mate_review`
when it was made for the card's own raise (matched on the raise time), so a
later raise starts with a bare card. The card shows "Mate is reviewing this", a
link "Mate reviewed this" with the first sentence and the cost, or "Mate's
review failed: why". The latest review per rule is held in memory, loaded
once from the table, so the 1 s alarm payload does not read the database.

**The follow-up notification** is a message of its own kind, `alarm_review`,
like a watch report: not an alarm, not in the alarm log, never on the banner and
never to the SignalK transport (which would put Mate's words on the bus as an
alarm; the transport and the retry drain both refuse it). Title "Mate on House
Bank Low", body the review's first sentence and, when Settings has an Address
for links, a new line with the link to the conversation (also the ntfy Click
target and the web push URL). It goes to the rule's own channels with an
alarm's selection rule: a rule that names none uses every switched-on channel,
SignalK excepted. It is queued on the dispatcher after the alarm's notification
and delivered by the same single worker, so it can neither precede nor change
the alarm. A failed delivery is retried by the notification queue under its own
key, which no alarm transition for the rule shares, so a later raise or clear
does not drop it.

If the alarm has cleared, or been raised again, before the review finishes, the
follow-up is not sent. The review stands, with `followup` set to
`skipped_cleared`.

## Consequences

- An operator pays for a review on every raise of a rule they switched on, at
  most hourly per rule. A flapping rule is bounded by the window, not by a cap.
- A failed review costs one model call at most and is shown, not retried.
- A review that finishes after the alarm cleared is kept, in the conversation
  list, without a notification. Nothing on the History rows shows it yet.
- Follow-up conversations do not carry the alarm id, so Mate asks to be pointed
  at the alarm rather than fetching it.
- The unattended turn is a second copy of the watch review's loop (config,
  meter, panic guard, cost lookup). A shared runner is the next refactor if a
  third unattended turn arrives.
- A watch report already waiting in the retry queue when this shipped has no
  top-level link, so it loses its ntfy tap target. Queued items expire within
  24 hours and there is one operator, so nothing migrates them.
- Built-in checks and network alarms stay unreviewed until they have a stored
  record to carry the switch.
