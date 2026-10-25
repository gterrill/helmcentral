# Import GPX tracks

This adds trips that Helmcentral didn't record itself, such as last season's
passages from a chartplotter or a phone app. They then appear on the
[Passages](../features/passages.md) page alongside the ones it logged. You'd
normally do it once after installing, or whenever you find old tracks worth
keeping.

## Before you start

- Export each trip from the chartplotter or app as a GPX file. Several files
  can be imported at once.
- Every position in the file needs a time. If the positions have none,
  Helmcentral refuses the file, because there would be no way to place it in
  the log.

## Steps

1. Open **Passages** in the sidebar.
2. Press **Import GPX**.
3. Choose one or more `.gpx` files and confirm.
4. Read the summary that appears above the list, then dismiss it.

The summary says how many new passages were found in the files. It also
mentions anything that was left out:

- positions that Helmcentral had already imported
- positions that fall in a stretch Helmcentral recorded itself, because it
  keeps its own track for that stretch
- positions that were out of range or jumped further than a boat could travel
- passages skipped because a passage already covers that time

If any file isn't valid GPX, nothing from that import is stored. The message
names the file, so you can fix it and try again.

## What you get

Imported passages are found by the same rules as recorded ones, and they appear
in the list newest first. Their battery, wind, fuel and water figures show `--`,
because a GPX file carries only positions and times. You can name the ends, add
notes and file them in a cruise, as described in
[Edit a passage](edit-passages.md).

Importing the same file twice is harmless. The second time, Helmcentral reports
the positions as already imported and adds nothing.
