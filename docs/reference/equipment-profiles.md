# Equipment profiles

An equipment profile describes one piece of machinery: the
gauges it needs, their display scales, its normal operating ranges, and its
service intervals. Applying one builds a configured tile in two clicks instead
of typing a dozen paths and scales by hand.

Three kinds of equipment are covered, set by the profile's `kind` field:

| `kind` | Covers | Path suffixes |
| --- | --- | --- |
| `engine` | Main propulsion engines | Anything except `phase.` or `total.` |
| `alternator` | Engine-driven alternators | Anything except `phase.` or `total.` |
| `generator` | AC gensets | Must start with `phase.` or `total.` |

Your profiles are stored with the rest of your boat's records and are managed
from **Inventory → Profiles**, which can add, edit, download and delete them.
**Add from catalogue** there copies one of the profiles that ship with
Helmcentral into your own list, where you can edit it freely; a profile already
in your list is greyed out in the catalogue. Your profiles appear under **Add
Tile → From equipment profile…**.

Because a profile can carry alarm thresholds, you can open it and read the exact
value at which an alarm will fire.

If you ran an earlier version that kept profiles as files in a folder, run
`helmcentral convert-profile-rules` once to bring them across. It shows what it
would import and changes nothing until you add `--apply`. Helmcentral will not
start while profile files are waiting in the old folder and your profile list is
empty, and its message names this command.

## What ships, and what does not

A catalogue profile may carry a working alarm threshold, but only where the
manufacturer publishes the figure and the profile says so. Every threshold in a
catalogue profile cites its `source`, and a test refuses one that does not.

That split is deliberate, and the three catalogue profiles land on both sides of
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

Every catalogue profile also carries **green advisory bands** where a published
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

Two ways, same result. Edit the profile in **Inventory → Profiles**, or apply the profile to a
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
name. A profile written before these fields existed still loads: it is read as a
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
catalogue profiles by a test, not for your own profiles. It is worth following in
yours anyway: in a year you will want to know whether 100 came from the
specification or from an afternoon's guess.

**Service items with no interval are slots too.** They name a job the machine
needs without inventing an interval for it.

## When a profile does not load

A bad profile is skipped, logged, and reported. The others still load, and the
apply dialog names the profile that failed and why. Common causes: an unknown
`unit`, `quantity` or `display`; a zone whose `direction` is neither `below`
nor `above`; `min` not below `max`; a zone with a threshold on a gauge that has
no scale; a service item whose `id` is not lowercase letters, numbers, dots, underscores and hyphens; a generator gauge whose suffix does not
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
| `DELETE /api/equipment-profiles/:id` | Remove the profile. |
| `GET /api/equipment-profiles/catalogue` | The profiles that ship with Helmcentral, and whether each is already in your list. |
| `POST /api/equipment-profiles/catalogue/:id` | Copy a catalogue profile into your list. Refuses an id that already exists; a body of `{"id": "new-id"}` saves the copy under another id. |

Every write is validated against the JSON schema and then against the same
rules the loader applies, so the API cannot store a profile the dashboard would
refuse. A rejected write comes back with the failing field paths, which is what
**Inventory → Profiles** shows you in its editor.

`GET /api/engine-profiles` and `PUT /api/engine-profiles/:id` still work,
filtered to `kind: engine`.

A profile `id` must be lowercase letters, numbers, dots,
underscores and hyphens, starting with a letter or number.

## Service intervals

The `service` block is the maintenance schedule of every equipment record
that uses the profile. **Inventory → Maintenance** reads it live: change an
interval here and every item using the profile follows at once. An entry
with no interval shows as a job whose interval isn't set yet rather than
being skipped. An item can change a job's description or intervals, or mark
it not applicable, for itself only; those changes are marked on the item and
reset to the profile's values.

Each service `id` is how an item keeps its history for that job, so keep it
stable. Renaming an id reads as removing one job and adding another: saving
a profile that removes a job some item has history for asks you to confirm
and names the items, and the old job's history stays on those items under
**No longer in the profile**. `first_at_hours` and `supersedes` are shown as
written but don't yet change when a job falls due. See
[Maintenance](../features/maintenance.md) and
[Set up a maintenance schedule](../how-to/set-up-a-maintenance-schedule.md).

There is no service-due alarm - Maintenance is a list and a service log,
not a notification.
