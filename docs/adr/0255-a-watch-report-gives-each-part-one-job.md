# ADR 0255: A Watch Report Gives Each Part One Job

## Status

Accepted (2026-11-04). Amends the page layout in ADR 0227.

## Context

A morning report took two screens on a desktop and opened on a form. On the
4 Nov run, one tank's fall appeared four times: as a rule finding, as Mate's
finding, as a report line and in the review. The alarm sentence appeared twice,
word for word. Tiny report text gave a full tank the same weight as a check
that never ran, and the first readable line was five lines of alarms.

The report's volume was not the problem. The operator reads the same figures
each morning and wants them all, in the same places. What was missing was rank:
nothing said which part to read first.

## Decision

Each part of a run does one job, in this order:

- **Opening line:** Mate's first sentence. It answers how the morning looks.
- **Needs a look:** The findings. They say what to attend to.
- **Mate's review:** The rest of the review. It gives Mate's judgement.
- **Readings:** The report's sections, in fixed places, for looking a figure up.
- **Foot:** Who was told and what the review cost.

The page opens on reports for a watch that has any. Its name and look-out text
join the settings in the aside, and Run now moves to the page header. A new
watch has no reports, so its form stays in the main column. A one-off watch
keeps its details first, because it has no aside.

A folded run shows the opening line and loses the cost and the Ran badge. Ran
is nearly always true, so it told the operator nothing.

## What the server owns

The server splits the review, using the same rule that gives a push message its
opening sentence, and sends `review_opening` and `review_rest` with each run.
The stored review stays whole, and the page never parses prose.

Older runs stored `boat_time` in 24-hour form. The store rewrites it to the
12-hour form on read, so the page, Mate and the notifications all quote one
label. Those runs' reports and findings keep their wording.

## When there is no opening line

A run without a review shows `review_problem`, or "Mate did not review this
run", in the opening line's place. A skipped run shows why. The page never
builds a summary of its own, because a generated line could read as Mate's
judgement when Mate had none.

## Rejected

- **Folding the readings:** A figure that changes place or hides behind a tap
  costs the daily comparison the fixed report exists for.
- **Keeping the form above the reports:** Settings change rarely, and reports
  arrive daily.

## What the report carries

A report's sections gain `rows`, `summary` and `not_checked` beside `lines`.
`lines` stays for email, Mate's input and past runs, which keep their wording, so
the page draws `rows` when a section has them and `lines` when it doesn't. Mate's
copy of the report leaves out `rows` and `summary`, because they repeat `lines`
and each token is paid for.

- **Not checked:** A sentence joins `not_checked` when a check or rule could not
  run: the overnight draw, the pressure fall, a tank with no level, the anchor's
  distance, the active alarms, the forecast. A missing reading that no check uses
  stays in its row, so a boat with no solar isn't told so every morning.
- **Tanks:** Each row carries its change since the last report. The same `now` and
  `was` feed the finding, so the row and the finding can't disagree.
- **Alarms:** The finding shrinks to the count, the top title and the active
  state. The rows and the long line keep the detail.

## What Mate is held to

The prompt already asked for four short sentences and no restating, and the
4 Nov review ran to six. `submit_watch_review` now refuses what the prompt only
asked for:

- **Length:** More than four sentences in `review`, counted by the scanner that
  finds the opening line.
- **Questions:** A `?` in `review`. Questions go in `questions`, at most three,
  stored on the run and given to the next review with the do-not-repeat rule.
- **A second chart:** A chart of a path a rule finding's chart already draws. The
  pressure chart reads the live barometer and has no path, so this doesn't catch
  a Mate chart of pressure.
- **A bad link:** An `extends` index outside `findings_by_the_rules`.

A review that never passes ends the way any failed review does, with its problem
on the run. It is never accepted as it stands.

## How issues are counted

A Mate finding sets `extends` to name the rule finding it adds to. The issue count
leaves those out, so the 4 Nov run reads "2 to look at" and not "3 findings". The
page and the notification titles both use it. Rule findings come first in
`findings`, so the index holds and charts keep their `finding_index`. Email and
short messages place an extending finding under its parent without reordering
what is stored.

The agreed mock-up showed Mate's 48-hour tank chart in place of the rule's
24-hour one. The rule's chart stays, because replacing it would let Mate remove
something the rules reached, including its warning line.

## Answering a question

**Answer** opens a chat linked to the run with the question in the composer,
unsent. The operator's words then reach the next review the way any reply in that
chat does.

## Limits

- **The any-alarm rule:** One alarm still makes a finding by default, so most
  mornings still have something to look at. That is a separate decision.
- **Grouping is Mate's call:** A Mate finding that fails to set `extends` counts
  as its own issue, and nothing checks that a parent is really about the same
  thing.
