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

## Limits

This decision changes order and weight only. The page still shows long alarm
findings, and it can't yet separate a check that didn't run from a reading,
because the report holds both as lines of text. The next change gives the
report that structure.
