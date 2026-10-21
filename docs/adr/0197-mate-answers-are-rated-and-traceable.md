# ADR 0197: Mate's answers are rated and traceable

## Status

Accepted (2026-10-21). Step 1 of a Mate improvement loop. Nothing here changes
what Mate says; it records what is needed to judge and later replay an answer.

## Context

Mate has improved by hand: a bad answer is noticed, a prompt rule is written,
and a test checks the rule's wording is present. Nothing records which answers
were good or poor in the operator's own words, and nothing keeps how an answer
was made. The assistant row holds the final text and a count of tool rounds;
the calls themselves reach only the log buffer, and no answer says which prompt
it was written under. So a poor answer cannot be reproduced, and a fix cannot
be checked against answers that were already good.

## Decision

1. **Feedback on answers.** `message_feedback` holds one row per assistant
   message: a verdict (`up` or `down`), an optional note, created and updated
   times, `ON DELETE CASCADE` from the message. Rating again replaces the row;
   tapping the active thumb deletes it. `PUT` and `DELETE`
   `/api/assistant/messages/:id/feedback` write it (write tier) and refuse an
   unknown id (404), a user or watch message (400), a bad verdict or a note
   over 1000 characters (400). `GET /api/assistant/feedback` lists rated
   answers with the preceding question, the answer, model, prompt version and
   tool calls.
2. **Thumbs-up counts as well as thumbs-down.** A down case says what to fix;
   an up case says what must keep working. A later replay check needs both, or
   a fix for one answer can only be tested against other failures.
3. **Live chat only.** The thumbs are offered on answers delivered to the open
   chat (the streamed answer, or one rejoined after a reload), not on messages
   read back from a past conversation. The session tracks which answers were
   delivered live and forgets them when another conversation is opened.
4. **Tool calls are stored.** `message_tool_calls(message_id, round, seq, name,
   args, result, elapsed_ms, error)` is written in the transaction that saves
   the assistant row, from the same place that logs each call. Calls that were
   refused (round limit, failure budget) are recorded with the reason in
   `error`. The log lines stay. Tool calls are not sent with the message to the
   browser, only returned by the feedback list. Results are kept in full, since
   forecast and telemetry will not come back and a replay needs them as
   recorded.
5. **Prompt version.** `messages.prompt_version` is the first 12 hex digits of
   the SHA-256 of the stable (cached) system-prompt part. The live part
   changes every turn and is left out. The column is added to an existing
   database by the same inspect-and-`ALTER` the other message columns use.
6. **Direction.** The judge for any later replay will be the configured chat
   model, not a separate setting, and any learned prompt rule will take effect
   only when the operator approves it, as Mate's record changes already do
   ([ADR 0146](0146-mate-proposes-the-operator-applies.md)). Those are not
   built here.

## Alternatives

- **Rating past conversations too.** Rejected: a rating given weeks later is
  about a different state of the boat and the prompt; live ratings are cleaner.
- **Thumbs-down only.** Rejected for the reason in decision 2.
- **Keeping the full rendered prompt per answer.** Rejected as large and
  repetitive; the hash names the stable part, which is what changes by rule.
- **Deriving tool calls from the log.** Rejected: the log is in memory and
  truncated.

## Consequences

- Each answer that used tools costs some database space for its results.
  Conversation deletion removes them with the messages.
- The assistant store keeps its own mutex and transactions for these tables,
  like its other message tables, rather than the document store's `RunTx`.
