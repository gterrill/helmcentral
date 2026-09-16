# Equipment profiles

An equipment profile is a JSON file describing one piece of machinery: the
gauges it needs, their display scales, its normal operating ranges, and its
service intervals. Applying one builds a configured tile in two clicks instead
of typing a dozen paths and scales by hand.

Three kinds of equipment are covered, set by the file's `kind` field:

| `kind` | Covers | Path suffixes |
| --- | --- | --- |
| `engine` | Main propulsion engines | Anything except `phase.` or `total.` |
| `alternator` | Engine-driven alternators | Anything except `phase.` or `total.` |
| `generator` | AC gensets | Must start with `phase.` or `total.` |

Profiles live in `plugins/engine-profiles/`. Drop a file in and restart, or
manage them from **Settings → Equipment**, which can upload, edit, download and
delete without touching the filesystem. Either way they appear under **Add
Widget → From equipment profile…**.

Unlike the rest of `plugins/`, profiles are plain JSON rather than compiled
modules. Because a profile can carry alarm thresholds, you have to be able to
open the file and read the exact value at which an alarm will fire. A compiled
module would add a build step to declarative data that runs no code.

## What ships, and what does not

A bundled profile may carry a working alarm threshold, but only where the
manufacturer publishes the figure and the profile says so. Every threshold in a
bundled file cites its `source`, and a test refuses one that does not.

That split is deliberate, and the three bundled profiles land on both sides of
it:

| Profile | Thresholds |
| --- | --- |
| `cummins-5285862` (alternator) | Shipped, from the Prestolite Electric / Leece-Neville specification |
| `cummins-qsb67-550` (engine) | Empty slots |
| `cummins-onan-13-5kw-60hz` (generator) | Empty slots |

The two Cummins engines ship nothing because Cummins does not publish QSB 6.7
setpoints. They are in the operator's manual and in QuickServe. Publicly
available material gives general operating guidance instead, and a low oil
pressure alarm set at 25 psi because a forum says cruise pressure runs 40 to 80
would raise alarms your ECU does not, which teaches you to ignore the alarm
list. A number nobody can source is worse than no number.

Where a figure is published, withholding it helps nobody, so the alternator
ships its charging window, its sustained output limit and its thermal limit.

Every bundled profile also carries **green advisory bands** where a published
source exists, each citing it. Those colour the gauge and raise nothing.

Check any shipped threshold against your own installation before relying on it.
A specification describes the machine, not the way yours is mounted, loaded or
cooled.

Applying a profile to a tile that already exists configures the gauges it finds
and appends the ones it does not, which completes a partly built tile. The
instance prefix comes from the tile's existing gauges, so applying a profile to
a port tile cannot append starboard gauges. Gauges the profile does not mention
are left alone.

## Filling in your thresholds

Two ways, same result. Edit the JSON and restart, or apply the profile to a
tile and edit the zones on each gauge in the ordinary gauge configuration
dialog. The dialog is easier to check against the gauge layout as you go.

A zone at any severity other than **Healthy** raises an alarm at its threshold
through the whole pipeline: the banner, the CHK lamp, acknowledgement, and
every configured notification transport. **Healthy** bands colour the gauge and
raise nothing, which is how advisory ranges work.

From an engine manual you usually want the low oil pressure warning and alarm,
the high coolant temperature warning and derate point, and the overspeed limit.
From an alternator manual, the temperature limit and the output voltage window.

## Format

```json
{
  "schema_version": 1,
  "kind": "engine",
  "id": "cummins-qsb67-550",
  "name": "Cummins QSB 6.7 550",
  "manufacturer": "Cummins",
  "model": "QSB 6.7",
  "rating_hp": 550,
  "source": "where these numbers came from",
  "notes": "shown in the apply dialog",

  "gauges": [
    {
      "path_suffix": "oilPressure",
      "label": "Oil Press",
      "hero": true,
      "display": "radial",
      "quantity": "pressure",
      "unit": "psi",
      "min": 0,
      "max": 100,
      "zones": [
        { "direction": "above", "threshold": 40, "state": "normal",
          "source": "sbmar.com: 40-80 psi at medium to high RPM" },
        { "direction": "below", "threshold": null, "state": "alarm",
          "note": "Low oil pressure: from your manual" }
      ]
    }
  ],

  "service": [
    { "id": "engine-oil", "description": "Engine oil and filter",
      "interval_hours": 250, "interval_months": 12, "first_at_hours": 250,
      "source": "Operator's manual §5-2" }
  ]
}
```

### Fields worth explaining

**`schema_version` and `kind`.** Both are required. `schema_version` is `1`;
anything else is refused rather than guessed at. `kind` is `engine`,
`alternator` or `generator`, and it decides which path suffixes are legal. The
apply dialog lists every kind together and shows each profile's kind beside its
name. A file written before these fields existed still loads: it is read as a
version 1 engine profile.

**`path_suffix`, not a full path.** You pick the instance prefix, such as
`propulsion.port` or `electrical.alternator.1`, when you apply the profile, so
one file serves both engines. The dialog suggests prefixes the server is
currently publishing and lets you type your own. With the engines shut down
nothing under `propulsion.*` is published, which is usually exactly when you
are sitting at the nav station configuring gauges.

**`hero` marks one gauge as the headline.** At most one per profile. That
member renders larger and spans the tile's full width. Leave it out and every
member is the same size.

**Zones are a direction and a threshold**, not a from/to pair. `"direction":
"below"` with `"threshold": 15` covers everything below 15. A band has to
anchor to one end of the gauge scale, because that is the only shape that maps
to a single alarm threshold. A band floating in the middle of the range has no
equivalent and is rejected.

Thresholds are in the gauge's **display unit**, psi or °C, not SignalK's SI
unit. The conversion happens before the alarm engine sees the value, which is
why the next field matters more than it looks.

**`quantity` and `unit` must both be recognised** (see
`frontend/src/lib/quantities.ts` and `backend/quantities.go`). An unknown unit
is refused at load time rather than quietly comparing psi against pascals. This
is the field that decides how your threshold is converted, so a gauge declared
as a raw number will compare a Celsius threshold against a Kelvin reading and
raise an alarm that can never clear.

**`"threshold": null` is a slot.** The manufacturer has a number here and the
profile does not. It shows in the apply preview as "not set" so you know to
look it up, and it is left out of the saved configuration rather than being
filled with a default nobody chose.

**A threshold that is set must cite its `source`.** This is enforced for the
bundled profiles by a test, not for your own files. It is worth following in
yours anyway: in a year you will want to know whether 100 came from the
specification or from an afternoon's guess.

**Service items with no interval are slots too.** They name a job the machine
needs without inventing an interval for it.

## When a profile does not load

A bad file is skipped, logged, and reported. The others still load, and the
apply dialog names the file that failed and why. Common causes: an unknown
`unit`, `quantity` or `display`; a zone whose `direction` is neither `below`
nor `above`; `min` not below `max`; a zone with a threshold on a gauge that has
no scale; two profiles sharing an `id`; a generator gauge whose suffix does not
start with `phase.` or `total.`; or an engine or alternator gauge whose suffix
does.

## Managing profiles over the API

| Method and path | Does |
| --- | --- |
| `GET /api/equipment-profiles` | Every profile, plus any that failed to load. Takes `?kind=` to filter. |
| `GET /api/equipment-profiles/:id` | One profile. |
| `GET /api/equipment-profiles/:id/download` | The same, as a file attachment. |
| `POST /api/equipment-profiles` | Create. Refuses an id that already exists. |
| `PUT /api/equipment-profiles/:id` | Replace. |
| `DELETE /api/equipment-profiles/:id` | Remove the file from disk. |

Every write is validated against the JSON schema and then against the same
rules the loader applies, so the API cannot store a profile the dashboard would
refuse. A rejected write comes back with the failing field paths, which is what
**Settings → Equipment** shows you in its editor.

`GET /api/engine-profiles` and `PUT /api/engine-profiles/:id` still work,
filtered to `kind: engine`.

An `id` used as a filename must be lowercase letters, numbers, dots,
underscores and hyphens, starting with a letter or number.

## Service intervals

The `service` block is validated and served, but nothing consumes it yet.
Maintenance work (the datastore, completion logging, service-due alarms) comes
later. When it lands, a service-due rule will evaluate `runTime above (last
completion hours + interval)`, and logging a completed job will advance the
threshold so the alarm clears and re-arms on its own.
