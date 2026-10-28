# Set an alarm threshold

[Alarms](../features/alarms.md) covers what a rule is, how severities work, and
what dwell and deadband do. These are the steps for creating one, either
directly or from a gauge.

## From the Alarms drawer

1. Open the Alarms drawer.
2. Choose **Add Rule**.
3. Set **Label** to what you want the alarm to say, for example
   `House bank low`.
4. Set **SignalK path** to the reading you want to watch, for example
   `electrical.batteries.house.voltage`.
5. Set **Condition** and **Threshold**. When the reading publishes a unit, the
   drawer shows the converted value under the threshold field as you type. You
   can check that it reads what you expect before saving. Helmcentral
   remembers the last unit each reading declared, so this works even when the
   instrument is switched off. A reading that has never declared a unit is shown
   as it arrives.
6. Set **Severity**. Every severity except Healthy also raises an alarm through
   your configured transports once the rule fires.
7. If the defaults don't suit this reading, open **Advanced**. There you can set
   **Deadband**, which is how far back past the threshold the reading has to fall
   before the alarm clears. You can also set **Must hold for** (the dwell, in
   seconds) and **Escalate after** (0 to never escalate).
8. Leave **Enabled** checked, then choose **Create Rule**.

To change a rule later, choose it from the rules list to reopen the same form.
To remove one, choose **Delete**. It asks you to confirm, naming the rule and
saying whether it's currently firing.

## By asking Mate

You can also describe the alarm in a sentence and let Mate fill in the form.

1. Open Mate and say what you want to hear about, for example "alarm when the
   house bank drops below 50%".
2. Read the card under its reply. It names the alarm, says how serious it is,
   the level it fires at, the level it clears at and how long the reading has
   to hold, all in your units.
3. Ask for different numbers if they're not right, then tap **Apply**.

Nothing is set until you tap **Apply**. Afterwards the alarm is in the rules
list like any other, and you can change it there. Mate can't change an alarm
that comes from a tank's levels or a gauge zone. It points you to the place that
does, which is the next section for a gauge and the tank's equipment item for a
tank.

## From a gauge

Any gauge with a scale can raise its own alarm, so you don't have to write a
rule by hand. Colouring part of the dial is the same action.

1. Turn on **Layout** mode.
2. On the gauge's tile, choose the **Configure** control, the sliders icon in the
   tile header.
3. Under **Zones**, choose **Add zone**.
4. Set the zone's direction (**Below** or **Above**), its threshold, and its
   severity. **Healthy (no alarm)** colours the band without raising anything.
   Every other severity also raises an alarm at that threshold.
5. Save the tile.

A rule made this way appears in the Alarms drawer's rules list, marked as coming
from a gauge. You edit it on the gauge, not in the list. Removing the zone
removes the rule.

On a tile that shows a piece of equipment, such as an engine cluster, the zones
belong to the equipment's profile. The form shows the profile's zones and says
what else the change applies to. Saving the tile saves the profile, and every
other tile and alarm using that profile follows.
