# Engine profiles

An engine profile is a JSON file describing one engine model: the gauges it wants, the
scales they run on, its normal operating bands, and its service intervals. Dropping one
into `plugins/engine-profiles/` and restarting makes it available under
**Add Widget → From engine profile…**, which builds a configured tile in two clicks.

Profiles are plain JSON rather than compiled plugins, unlike everything else under
`plugins/`. The reason is that a profile supplies alarm thresholds, and you should be
able to open the file and read exactly what your oil-pressure alarm will fire at. See
[ADR 0053](../adr/0053-engine-profiles.md).

## What ships, and what does not

The bundled `cummins-qsb67-550.json` contains **no alarm thresholds**. That is
deliberate, not an oversight.

Cummins does not publish QSB 6.7 setpoints — they are in your operator's manual and in
QuickServe. What is publicly available is general operating guidance, and guidance is
not a setpoint. A profile that alarmed at 25 psi because a forum says cruise oil
pressure runs 40–80 would raise alarms your engine's own ECU does not, and would teach
you to ignore the alarm list. So the bundled profile ships:

- **Green advisory bands** where a published source exists, each citing it. These colour
  the gauge and raise nothing.
- **Empty alarm slots** everywhere else, each naming what to look up.

Filling those slots from your manual, once, turns the bundled file into a real profile
worth sharing.

Applying a profile to a tile that already exists both fills in the gauges that are
there and adds the ones that are missing, so it finishes a half-built tile. The
instance prefix comes from that tile's own gauges, so applying to the Port tile cannot
append starboard gauges. It never removes a gauge — one the tile has and the profile
does not know about is left alone.

## Filling in your thresholds

You can do it two ways. Either edit the JSON and restart, or — easier — apply the
profile to a tile and then edit the zones on each gauge through the normal gauge config
dialog. Both end up in the same place; the second gives you the gauge in front of you
while you do it.

A zone with any severity except **Healthy** raises an alarm at its threshold, through
the full alarm pipeline: the banner, the CHK lamp, acknowledgement, and whatever
notification transports you have configured. **Healthy** bands colour the gauge and
raise nothing, which is what the advisory ranges use.

The numbers you want from the manual are usually the low oil pressure warning and alarm,
the high coolant temperature warning and derate point, and the overspeed limit.

## Format

```json
{
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
      "display": "radial",
      "quantity": "pressure",
      "unit": "psi",
      "min": 0,
      "max": 100,
      "zones": [
        { "direction": "above", "threshold": 40, "state": "normal",
          "source": "sbmar.com: 40-80 psi at medium to high RPM" },
        { "direction": "below", "threshold": null, "state": "alarm",
          "note": "Low oil pressure — from your manual" }
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

**`path_suffix`, not a full path.** You choose the instance prefix (`propulsion.port`)
when you apply the profile, so one file serves both engines. The dialog suggests
prefixes the server is currently publishing, and lets you type one that is not — with
the engines off, nothing under `propulsion.*` is published, and that is exactly when
you set engine gauges up.

**Zones are a direction and a threshold**, not a from/to pair. `"direction": "below"`
with `"threshold": 15` means "everything below 15". A band anchors to one end of the
gauge's scale, which is the only shape that maps to an alarm threshold — a band floating
in the middle of the range has no single-threshold equivalent and is rejected.

Thresholds are in the gauge's **display unit** (psi, °C), never SignalK's SI unit. The
conversion happens on the way to the alarm engine.

**`"threshold": null` is a slot.** The manufacturer defines a number here and the profile
does not know it. It shows in the apply preview as "not set" so you know to look it up,
and is dropped from the saved config so it can never become a zone at some default
number nobody chose.

**Service items with no interval are slots too.** They name a service the engine has
without inventing how often it wants it.

**`quantity` and `unit`** must be ones Helmcentral knows — see `frontend/src/lib/quantities.ts`
and `backend/quantities.go`. An unknown unit is refused at load time rather than
silently comparing psi against pascals.

## When a profile does not load

A bad file is skipped, logged, and reported — the rest still load, and the apply dialog
shows what failed and why. Common causes: an unknown `unit` or `quantity`, an unknown
`display`, a zone whose `direction` is not `below` or `above`, `min` not below `max`, a
zone with a threshold on a gauge that has no scale, or two profiles claiming the same `id`.

## Service intervals

The `service` block is validated and served, and nothing consumes it yet. Maintenance —
the store, completion logging, and service-due alarms — is the next build. When it lands,
a due rule will be `runTime above (last completion hours + interval)`, so logging work
moves the threshold and the alarm clears and re-arms on its own.
