# Watch report webhook

When a watch sends to the **Webhook** channel, Helmcentral posts one JSON object
to the URL you set under **Settings** in **Alarms**. It's a different shape from
the one used for alarms, so a receiver can tell them apart by the `kind` field.
An alarm has a `kind` of `raised` or `cleared`. A watch report always has
`"kind": "watch_report"`. See [Watches](../features/watches.md#notifications)
for when a report is sent.

A report is posted once for each run that qualifies. If your receiver doesn't
answer with a success status, Helmcentral tries again for up to 24 hours. A
newer report from the same watch replaces an older one that's still waiting.

## Fields

| Field | Type | What it is |
| --- | --- | --- |
| `kind` | string | Always `watch_report`. |
| `vessel` | string | The boat's name, empty if it isn't known. |
| `title` | string | The same title the other channels use, such as `Overnight check: 2 findings`. |
| `summary` | string | The short message: up to three findings, then the opening sentence of Mate's review. Lines are separated by a newline. |
| `watch.id` | string | The watch's id. |
| `watch.name` | string | The watch's name. |
| `watch.schedule_sentence` | string | The schedule in words, such as `Every hour`. |
| `run.id` | string | The run's id. |
| `run.boat_time` | string | When the run started on the boat's clock, with its zone. |
| `run.outcome` | string | Always `ran`, because a skipped run is never sent. |
| `findings` | array | Every finding of the run, possibly empty. Each has `section`, `rule`, `text` and `by`, which is `rules` or `mate`. |
| `charts` | array | The run's charts, possibly empty. See [Charts](#charts). |
| `report` | object or null | The report: `boat_time`, `since`, and `sections`. Each section has `name`, `title`, and either `lines` or `unavailable`, and sometimes `figures`. |
| `review` | string or null | Mate's full review, or null when Mate didn't review the run. |
| `path` | string | Where the run is in the app, such as `/watches/<watch id>?run=<run id>`. |
| `link` | string | The full link, `path` after the address you set as **Address for links**. Left out when no address is set. |

Each finding can also have a `chart_problem` string, which says why a chart that
was asked for could not be drawn, for example `No history to chart: the barometer
history is empty`.

## Charts

A run has at most two charts. Each is an object with these fields:

| Field | Type | What it is |
| --- | --- | --- |
| `finding_index` | number | Which finding the chart belongs to, counting from 0 in `findings`. |
| `title` | string | The chart's title, such as `Pressure, last 12 hours`. |
| `unit` | string | The unit of the first series, in your units, such as `hPa`. The threshold is in this unit. |
| `timezone` | string | The boat clock's offset, such as `UTC+12`. |
| `utc_offset_seconds` | number | That offset in seconds. |
| `window` | object | `from` and `to`, as UTC times. |
| `series` | array | One or two lines. Each has `label`, `unit` and `points`, a list of `[unix milliseconds, value]` pairs, at most 200 of them. |
| `threshold` | object | Left out when there isn't one. `label` and `value`, a dashed line in the first series' unit. |
| `png_base64` | string | An 800 by 400 PNG of the chart, base64 encoded. Left out when the chart could not be drawn. |
| `problem` | string | Only when the chart could not be drawn: why it was left out. The report is still sent, with no `png_base64` on this entry. |

`path` has no host in front of it, because Helmcentral can't tell what address
you reach it on from outside the boat. Set **Address for links** in **Settings**
under **Alarms** and `link` is filled in. Without it, put your own address in
front of `path`.

## Example

```json
{
  "kind": "watch_report",
  "vessel": "Pikorua",
  "title": "Overnight check: 1 finding",
  "summary": "House bank drained 40 Ah overnight, more than usual.\nThe fridge ran longer than the night before.",
  "watch": { "id": "3f1c", "name": "Overnight check", "schedule_sentence": "Every hour" },
  "run": { "id": "9a7e", "boat_time": "Thu 29 Oct 03:00 UTC+13", "outcome": "ran" },
  "findings": [
    { "section": "power", "rule": "overnight_draw", "text": "House bank drained 40 Ah overnight, more than usual.", "by": "rules" }
  ],
  "charts": [],
  "report": {
    "kind": "sections",
    "generated_at": "2026-10-28T14:00:00Z",
    "boat_time": "Thu 29 Oct 03:00 UTC+13",
    "since": "Thu 29 Oct 02:00 UTC+13",
    "sections": [
      { "name": "power", "title": "Power", "lines": ["House bank 71% at 12.6 V"] }
    ]
  },
  "review": "The fridge ran longer than the night before. Nothing else looks different.",
  "path": "/watches/3f1c?run=9a7e",
  "link": "http://boat.local:9091/watches/3f1c?run=9a7e"
}
```
