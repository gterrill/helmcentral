# Set battery state-of-charge bands

The Battery & Power tile takes its state-of-charge colour and its "To 20%"
line from your own alarm rules rather than a built-in threshold. Until you
create a rule, the tile's state-of-charge number stays plain amber at any
level, and the line beside it only ever reads "To full" or "To empty".

This sets up a warn band at 20% and an alarm band at 10% on the main bank.
Adjust the numbers to suit your own bank.

1. Open the Alarms drawer.
2. Choose **Add Rule**.
3. Set **SignalK path** to `electrical.batteries.0.capacity.stateOfCharge`
   (or whichever path you have pinned with `INFLUX_SOC_MEASUREMENT`, if you
   have changed it from the default).
4. Set **Condition** to `below`.
5. Set **Threshold** to `0.2`.
6. Set **Severity** to `warn`.
7. Leave **Enabled** checked, then choose **Create Rule**.
8. Repeat for a second rule on the same path: **Condition** `below`,
   **Threshold** `0.1`, **Severity** `alarm`.

State of charge is published on the bus as a ratio, not a percentage, so
`0.2` is 20%. If you type `20` instead, that works too: anything you enter
over 1 is read as already being a percentage. When the server publishes a
unit for the path, the drawer shows the converted value under the threshold
field as you type, so you can check it reads 20% before saving; if no unit
is published, the field shows only what you typed.

Once both rules are saved, the Battery & Power tile updates immediately,
without a reload:

- The bar under the state-of-charge number gains two shaded bands, one at
  10% and one at 20%.
- The big number, and the bar's fill, turn amber below 20% and red below 10%,
  the same colours used for every other alarm on the board.
- The line beside the number reads "To 20%" while discharging above that
  level, "To 10%" between the bands, and "To empty" once you are below both.
- The Dawn figure in the footer takes the same colours when its projected
  percentage falls into one of these bands, which is your cue to run the
  generator or plug in before dark.

To change a threshold later, edit either rule from the Alarms drawer's rules
list. To go back to the plain, uncoloured behaviour, delete both rules; the
tile does not need them to keep working, it just stops colouring the number.
