# Set up your vessel

Anomaly detection (frozen and impossible sensor readings, charging into a
full house bank, twin engines pulling apart) needs to know which engines
and which battery bank to watch before any of it runs. Nothing is
pre-filled for your boat: every detector starts as "not set up" until you
pick these from Settings.

## Engines

1. Open **Settings → Vessel**.
2. Under **Engines**, tick every propulsion instance you want watched. Each
   row shows its live rpm and coolant temperature so you can tell which is
   which.
3. Give each engine a name (Port, Starboard, and so on).
4. Under **Linked equipment item**, pick the matching item from your
   equipment registry, or choose **+ Create new item** and name one on the
   spot.
5. Once an item is linked, pick its **Equipment profile** from the same
   row. This is the same profile your engine gauges already use, so
   assigning it here and in Inventory is the same edit.
6. With a profile picked, an **Apply gauge zones** button appears. Use it to
   set that engine's gauge tile up from the profile, the same step you would
   otherwise reach from the dashboard, with the engine already filled in.
7. Choose **Save Vessel Settings**.

The frozen-sensor check is ready once at least one engine is ticked. The
engine-differential check (comparing engines against each other) needs at
least two.

## House bank

1. Still under **Settings → Vessel**, find **Power**.
2. Under **House bank**, pick the battery bank the charging check should
   watch. Every bank the boat publishes is listed with its live voltage,
   current and state of charge, so you can tell them apart even when the
   instance numbers alone would not (for example, a bank that reports under
   two different addresses).
3. Under **Linked equipment item**, pick or create the registry item for
   this bank, then pick its **Equipment profile**: the battery's chemistry
   and charge thresholds.
4. Enter the bank's **cell count** and **capacity**.
5. Leave **Warn override** and **High override** blank to use the profile's
   own thresholds, multiplied out by your cell count. Fill either in only if
   your bank needs a different number than the profile gives.
6. Choose **Save Vessel Settings**.

Each fieldset shows whether its check is ready, or what is still missing,
so you always know what saving will turn on.

## Excluding a sensor you know is dead

A sender that has failed outright (a stuck or open-circuit reading) can
trip the frozen or impossible-reading check forever. Rather than a
settings list, this is handled from the alarm itself: open the alarm card
for that check and choose **Ignore this sensor** next to the reading you
want excluded. To see everything currently ignored, or to bring one back,
open **Settings → Alarms** and look under **Ignored sensors**.
