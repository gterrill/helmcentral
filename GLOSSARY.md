# Helmcentral

Helm and remote monitoring for one vessel: live instrument data, alarms,
anchoring, passages, the boat's records, and Mate, the onboard assistant.

## Offline coastline

**Offline coastline**:
The charted shoreline and reefs Helmcentral keeps on board for the area the
boat is cruising, so questions about land and shelter answer without internet.
_Avoid_: Chart data, shoreline extract, land polygons

**Coverage**:
The part of the world the offline coastline currently holds: every square
within 200 nm of the boat, plus squares kept from earlier in the cruise.
_Avoid_: Region, extract, area

**Coverage square**:
One 1° latitude by 1° longitude square of coverage, the unit that is fetched,
dated and updated on its own.
_Avoid_: Cell, tile, chunk

**Inland square**:
A coverage square with no part of it within 20 nm of the sea. It is not
downloaded, since no shelter estimate can reach it from the water.
_Avoid_: Land square, dry square

**Edits since**:
The number of coastline or reef changes made to the public map inside
coverage after it was downloaded; what decides whether an update is worth it.
_Avoid_: New version, staleness

## Shelter

**Shelter estimate**:
Mate's reading of which wind directions an anchorage is protected from,
worked out from the offline coastline alone.
_Avoid_: Wind protection, shelter rating

**Sheltered arc**:
A run of compass directions in a shelter estimate from which the anchorage is
sheltered, partly sheltered or exposed, e.g. sheltered 030°–190°.
_Avoid_: Sector, protection range

**Reef shelter**:
Shelter that comes only from reef lying upwind, which changes with the tide
and never counts as better than partial.

## Alarms

**Network alarm**:
An alarm raised by another device on the boat's network, such as the chart
plotter or a radar, rather than by Helmcentral's own alarm rules or watches.
_Avoid_: Bus notification, SignalK alarm, external alarm

**Alarm kind**:
What a network alarm reports, independent of which device sent it or when,
e.g. every Perpendicular Passed alarm is one kind.
_Avoid_: Alarm type, alarm path

**Ignored alarm**:
An alarm kind the operator has told Helmcentral never to show, until they
restore it; it never reaches the board, notifications, Mate or history.
_Avoid_: Muted, silenced, suppressed, hidden alarm

**Safety alarm**:
An alarm that can never be ignored: a kind such as man overboard, fire,
flooding, collision, distress or anchor dragging, or any alarm the sending
device marks as an emergency.
_Avoid_: Critical alarm, emergency alarm
