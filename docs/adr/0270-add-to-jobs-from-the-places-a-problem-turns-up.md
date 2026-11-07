# ADR 0270: Add To Jobs From The Places A Problem Turns Up

## Status

Accepted (2026-11-07). Builds on [ADR 0269](0269-open-jobs-hold-known-problems-until-they-are-fixed.md),
which made the Open jobs record and already kept where a job came from.

## Context

A problem usually turns up somewhere other than the jobs list: a watch report
names a falling house bank, an alarm card shows a bilge pump cycling, a line of
a checklist fails its check. Before this change the operator had to remember it
and retype it into Add job later, and the record of where it was seen was lost.

## Decision

Three screens get an **Add to jobs** button. It opens the same Add job form,
already filled in, and nothing is saved until the operator taps Save.

| Screen | Title | Notes | Link back | Source |
| --- | --- | --- | --- | --- |
| A finding in a watch report | The finding's text | None | That watch's page, scrolled to that report | `watch_finding` |
| An active alarm card | The alarm's label | The alarm's own words | The Alarms page | `alarm` |
| A line of a checklist run | The line's text | None | The checklist's note | `checklist_line` |

- **One component.** `AddToJobsButton` takes a draft and opens `OpenJobDialog`
  with it. The dialog sends the draft's source, link and label with the first
  save only, so editing the job afterwards never moves where it came from.
- **A long text keeps whole.** A title is at most 200 characters. A finding
  longer than that gets a shortened title ending in an ellipsis, and the full
  text goes in the notes unless the draft supplied notes.
- **No item is guessed.** A finding, an alarm label or a checklist line may name
  gear, but matching a sentence to an equipment record is a guess, and a wrong
  item hides the job from the right item's page. The form opens with no item and
  the operator chooses.
- **The link is a page, not a record.** The alarms page has no address for one
  alarm, so the link goes to the page. A watch report and a checklist note have
  their own addresses. If the report or note is later deleted, the link goes to
  a page that no longer shows it, and the label still says what it was.
- **The backend is unchanged.** ADR 0269 already accepts the source, a link that
  stays inside the app, and a label.

## Limits

- **An open checklist run only.** A finished or abandoned run has no button,
  because the run is over and its lines are no longer the thing being checked.
- **No write check on the button.** Like Acknowledge and a tick on a checklist,
  the button shows for everyone and a read-only session sees the server's refusal
  in the form.
- **The checklist button sits beside the row,** not inside it. The row is itself
  a button, and a button cannot hold another.
