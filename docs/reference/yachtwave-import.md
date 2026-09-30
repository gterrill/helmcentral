# YachtWave import fields

Where each part of a YachtWave Vessel Export ends up in Helmcentral. The
import reads the HTML file YachtWave saves as its Vessel Export. Any other
file is rejected.

Placeholders in the export (`--`, `—`, `Not set`, `Unknown`) are read as
empty.

## Vessel particulars

| YachtWave | Helmcentral |
| --- | --- |
| Brand | Builder |
| Model | Model |
| Year | Year |
| Hull ID (HIN) | HIN |
| Flag | Flag |
| Hailing port | Hailing port |
| Hull type | Hull type |
| Hull material | Hull material |
| Displacement | Displacement, in kilograms. Tonnes are converted; any other unit stops the import |
| Shore power | Shore power |
| System voltage | System voltage |
| Registration | Registration |
| IMO | IMO |
| EPIRB beacon ID | EPIRB id |
| Date acquired | Date acquired |
| Name, MMSI, Call sign, Length overall, Beam, Draft, Clearance | Not stored. Shown beside the live instrument value |
| USCG documentation no. | Not stored. Shown only |

## Equipment and spares

| YachtWave | Helmcentral equipment |
| --- | --- |
| Item, Engine | Name |
| Manufacturer | Manufacturer |
| Serial | Serial |
| Installed | Install date |
| Location | Zone |
| Text under the item name | Location detail |
| Type, e.g. "Electronics (Radar)" | System, picked from the type's keywords, and the type itself kept in the notes. Engines with no other match go under propulsion |
| Part no. | Part number |
| On board / required | On hand / required. A lone figure is on hand with nothing required |

Engines and equipment come in as deployed, spares as stored.

A spares row whose name is a locker and whose contents are listed one per
line becomes a bin coded with the locker's name, holding one item per line.

## Service log

| YachtWave | Helmcentral log entry |
| --- | --- |
| Date | Date |
| Work done (title and text) | Description, title first |
| Engine / equipment | The equipment the entry is on. Chosen by you when two items share the name |
| Hours | Hours |
| Type | Not kept; every entry is recorded as maintenance |

## Notes and tasks

A note keeps its title, date and text. A task becomes a note titled
`Task: <name>`, listing priority, assigned to, due, status and completed date.

A note is left behind if it looks like it holds a password or wifi key. Its
text is not kept.

## Documents and photos

Each keeps its YachtWave label as its title. You supply the file. Photos must
be JPEG or PNG; YachtWave's `.jpe` photos are JPEG. A document or photo whose
YachtWave record names one piece of equipment is linked to it.

## Not imported

Checklists (the export has no steps), crew, service schedules, cruise log,
general log, readings and expenses.
