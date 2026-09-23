# Set an alarm threshold

See [Alarms](../features/alarms.md) for what a rule is, how severities work,
and what dwell and deadband do. These are the steps for creating one, either
directly or from a gauge.

## From the Alarms drawer

1. Open the Alarms drawer.
2. Choose **Add Rule**.
3. Set **Label** to whatever you want the alarm to say, for example
   `House bank low`.
4. Set **SignalK path** to the reading you want to watch, for example
   `electrical.batteries.house.voltage`.
5. Set **Condition** and **Threshold**. When the reading publishes a unit,
   the drawer shows the converted value under the threshold field as you
   type, so you can check it reads what you expect before saving.
6. Set **Severity**. Every severity except Healthy also raises an alarm
   through your configured transports once the rule fires.
7. Open **Advanced** if the defaults don't suit this reading, to set
   **Deadband** (how far back past the threshold it has to fall before the
   alarm clears), **Must hold for** (the dwell, in seconds) and **Escalate
   after** (0 to never escalate).
8. Leave **Enabled** checked, then choose **Create Rule**.

To change a rule later, choose it from the rules list to reopen the same
form. To remove one, choose **Delete**; it asks you to confirm, naming the
rule and whether it is currently firing.

## From a gauge

Any gauge with a scale can raise its own alarm instead of writing a rule by
hand: colouring part of the dial is the same action.

1. Turn on **Layout** mode.
2. On the gauge's tile, choose the **Configure** control, the sliders icon
   in the tile header.
3. Under **Zones**, choose **Add zone**.
4. Set the zone's direction (**Below** or **Above**), its threshold, and its
   severity. **Healthy (no alarm)** colours the band without raising
   anything; every other severity also raises an alarm at that threshold.
5. Save the tile.

A rule made this way appears in the Alarms drawer's rules list marked as
coming from a gauge, and stays edited on the gauge rather than in the list.
Removing the zone removes the rule.
