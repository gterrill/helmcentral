# Engine profiles

An engine profile is a JSON file that defines a specific engine model,
including the required gauges, display scales, normal operating ranges, and
service intervals. Placing a file in `plugins/engine-profiles/` and restarting
makes it available under **Add Widget → From engine profile…**, which builds a
configured tile in two clicks.

Unlike other components in `plugins/`, profiles are plain JSON rather than
compiled plugins. Because a profile specifies alarm thresholds, you must be
able to open the file and verify the exact value at which an alarm, such as oil
pressure, will trigger. A compiled module introduces an unnecessary build step
for declarative data that contains no executable logic.

## What ships, and what does not

The bundled `cummins-qsb67-550.json` file contains **no alarm thresholds**.

Cummins does not publish QSB 6.7 setpoints in public documentation: they are
located in the operator's manual and in QuickServe. Publicly available material
provides general operating guidance rather than verified setpoints. For
example, configuring an alarm at 25 psi based on forum guidance stating cruise
oil pressure runs 40 to 80 psi risks raising alarms that the engine ECU does
not trigger, causing operators to disregard the alarm list. Therefore, the
bundled profile provides:

- **Green advisory bands** where a published source exists, citing each
  source. These colour the gauge without raising alarms.
- **Empty alarm slots** for other parameters, with notes specifying what to
  look up.

Entering values from your manual completes the file for operational use and
sharing.

Applying a profile to an existing tile configures existing gauges and adds
missing ones, completing a partially built tile. The instance prefix is taken
from the tile's existing gauges, so applying a profile to a port tile cannot
append starboard gauges. Existing gauges not defined in the profile are left
untouched.

## Filling in your thresholds

You can configure thresholds in two ways: either edit the JSON file and restart
the server, or apply the profile to a tile and edit the
zones on each gauge through the standard gauge configuration dialog. Both
methods produce the same configuration; the UI dialog provides immediate visual
reference to the gauge layout during configuration.

A zone configured with any severity level other than **Healthy** raises an
alarm at its threshold through the full alarm pipeline: the banner, the CHK
lamp, operator acknowledgement, and all configured notification transports.
**Healthy** bands colour the gauge without raising alarms, which is the
mechanism used for advisory ranges.

The values required from the manual are typically the low oil pressure warning
and alarm, the high coolant temperature warning and derate point, and the
overspeed limit.

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

**`path_suffix`, not a full path.** You select the instance prefix (such as
`propulsion.port`) when applying the profile, allowing one file to serve
multiple engines. The dialog suggests prefixes currently published by the
server and permits manual entry. When engines are shut down, nothing under
`propulsion.*` is published, which is typically when gauge configuration
takes place.

**Zones are a direction and a threshold**, not a from/to pair. For example,
`"direction": "below"` with `"threshold": 15` covers all values below 15. A
band must anchor to one end of the gauge scale, which is the only structure
that maps directly to an alarm threshold; a band floating in the middle of the
range has no single-threshold equivalent and is rejected.

Thresholds are defined in the gauge's **display unit** (psi, °C), not
SignalK's SI unit. Conversion occurs before values are passed to the alarm
engine.

**`"threshold": null` is a slot.** The manufacturer specifies a value here
that the profile does not include. It appears in the apply preview as "not set"
to prompt manual lookup, and is excluded from the saved configuration to prevent
assigning an arbitrary default value.

**Service items with no interval are slots too.** They identify an engine
service task without defining an unverified interval.

**`quantity` and `unit`** must be recognized by Helmcentral (see
`frontend/src/lib/quantities.ts` and `backend/quantities.go`). An unknown unit
is rejected at load time rather than silently comparing psi against pascals.

## When a profile does not load

An invalid file is skipped, logged, and reported. Other profiles still load, and
the apply dialog indicates which file failed and why. Common causes include: an
unknown `unit` or `quantity`, an unknown `display`, a zone whose `direction` is
not `below` or `above`, `min` not lower than `max`, a zone with a threshold on
a gauge that has no scale, or duplicate `id` values across profiles.

## Service intervals

The `service` block is validated and served, but no subsystem consumes it yet.
Maintenance functionality (the datastore, completion logging, and service-due
alarms) will be implemented in the next build. When deployed, a service-due
rule will evaluate `runTime above (last completion hours + interval)`.
Logging completed work will advance the threshold, causing the alarm to clear
and re-arm automatically.
