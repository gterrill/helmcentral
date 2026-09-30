# Import

If the boat's records already live in another boat management app, you can
bring them across instead of retyping them. You don't have to re-enter years
of service history, the spares list, or the notes you wrote down the first
time the watermaker played up, and they end up next to the live instrument
data that shows when each was last needed.

YachtWave is the first app Helmcentral imports from. The import is in
**Settings → Import**.

## What comes across

| From YachtWave | Becomes |
| --- | --- |
| Vessel particulars | The vessel's particulars in **Settings → Vessel** |
| Engines and equipment | Equipment records, grouped by location |
| Inventory and spares | Stored equipment records, with part number, number on board and number required |
| A locker listing several things | A bin holding one item per line |
| Maintenance log | Service log entries on the right piece of equipment |
| Notes | Notes |
| Tasks | Notes, one per task, with its priority, who it was assigned to, when it was due and whether it was done |
| Documents and photos | Documents, linked to the equipment they belonged to where the export says which |

## What doesn't

- **Checklists.** YachtWave's export lists each checklist's name and how many
  steps it has, but not the steps themselves, so there is nothing to run.
- **Notes holding a password or wifi key.** The import flags them and leaves
  them behind, and doesn't keep a copy. Anything imported as a note is
  searchable and can be read by Mate, and a password doesn't belong there.
- **Crew, cruise log, general log, readings and expenses.** Helmcentral has
  nowhere to put these yet. If your export has any, the review page says so.
- **Particulars that come from the boat's instruments.** Name, MMSI, call
  sign, length, beam, draft and air height come from the live instrument
  data. Where YachtWave's figure differs, the import shows both and changes
  neither. Fix it at the source if the instruments are wrong, because the
  low-water warning uses the draft the instruments report.

## How it works

The import doesn't change anything until the last page. You go through it one
page per topic: what was found, the vessel, locations, equipment and spares,
the service log, notes, then each document and photo in turn, and finally a
summary to confirm. Your choices are saved as you go, so you can stop halfway
and pick it up again from the same device.

Along the way it asks you about the things the export can't settle:

- **Which engine.** If both main engines have the same name, as a pair of
  Cummins QSB 6.7s do, a service log entry that names only the model can't
  say which one was serviced. You pick port, starboard, or neither.
- **Whether it's already here.** An item with the same name as one you
  already have is matched to it and not duplicated. You can change that
  either way.
- **Whether the data is right.** Duplicate rows, placeholder names and
  figures that can't be true (a 60-foot catamaran displacing 32 kg) are
  flagged, not quietly corrected. You decide what to keep.

YachtWave keeps documents and photos on its own servers, and Helmcentral
doesn't fetch them from there. For each one, the import shows its YachtWave
link. Open it, save the file, then paste, drop or pick it on that page, or
skip it.

## Importing again

Helmcentral remembers what each import brought across. Import a newer
YachtWave export later and anything already imported is marked and left out,
so you only get what's new. A record you edited in YachtWave after the first
import counts as new, because the export carries nothing that identifies a
record except what it says. When that happens, match it to the one you
already have.

See [Import from YachtWave](../how-to/import-from-yachtwave.md) for the steps,
and [YachtWave import fields](../reference/yachtwave-import.md) for where each
field ends up.
