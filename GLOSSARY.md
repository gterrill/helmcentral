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
