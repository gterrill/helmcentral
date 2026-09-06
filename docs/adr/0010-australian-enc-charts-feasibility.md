# ADR 0010: Sourcing Real Australian ENC Chart Data (Feasibility Research)

## Status
Proposed

## Context
ADR 0009 (GSHHG Coastline Fallback) shipped a public-domain reference coastline layer for the route planner, explicitly deferring "sourcing ENC data for international waters beyond any single national hydrographic office" as future work. The operator's cruising area includes Australian waters, and asked what the best approach would be to get real navigational chart data there specifically, rather than just the GSHHG/OpenSeaMap reference layer.

ADR 0006 (Manual Route Planning) avoided chart licensing dependencies, rejecting NOAA ENC (US-only) and commercial chart vendors (Navionics/C-MAP/Garmin, who don't license their data for embedding in third-party apps). Australian chart integration would revisit that decision. This ADR records the licensing requirements and estimated engineering effort without selecting an approach.

## Findings

### AusENC: the official product
The Australian Hydrographic Office (AHO) is the sole producer of official charts for Australia, Papua New Guinea, and Solomon Islands waters (and Australian Antarctic Territory; AusENC also covers NZ waters). Official Electronic Navigational Charts (ENC) are, by IHO definition, S-57-format vector data encrypted with the IHO S-63 standard — only national hydrographic offices may produce data that can be called "ENC."

AusENC distributes ENC at recreational rates, typically under $1 per cell per year, through national agents (33 South Marine Electronics, Boat Books Australia, Cairns Charts and Maps, The Chart and Map Shop) or, outside Australia, through IC-ENC/PRIMAR-affiliated resellers (Admiralty Vector Chart Service, ChartCo, ChartWorld, Navtor, Primar, and others). ~850 cells cover the full AusENC service area. At these rates, licensing compliance and implementation are larger constraints than subscription cost.

### The real blocker: S-63 compliance
To use AusENC at all, the viewing software must be "IHO S-63 compatible," and a permit must be requested against that specific software/hardware combination. This is materially different from a simple paid API:

- The IHO Data Protection Scheme gatekeeps who can issue/validate S-63 permits correctly — becoming a recognized OEM (assigned a manufacturer ID and the cryptographic material needed to implement the scheme correctly) is an administrative registration step, separate from and prior to any per-cell subscription.
- Implementing S-63 decryption itself requires Blowfish-based decryption (current edition; the upcoming S-63 2.0 moves to AES-128-CBC) plus permit/hardware-ID-bound key handling. Go has the Blowfish primitive (`golang.org/x/crypto/blowfish`), but no existing library implements the permit/HW-ID scheme — that logic would need to be built from the IHO S-63 specification directly.
- Decrypted cells are standard S-57 — parsing them requires an S-57 reader (ISO 8211 file structure plus the S-57 object/attribute catalogue). No Go library exists for this; GDAL's OGR S-57 driver is the standard tool, but GDAL is not installed anywhere in this stack today (confirmed during ADR 0009's work — `ogr2ogr`/`gdalinfo` were unavailable in the dev environment).
- Unlike the static, one-time-converted GSHHG dataset, ENC cells receive weekly/fortnightly updates. Keeping AusENC data current requires an ongoing update-ingestion pipeline rather than a one-time conversion.
- Precedent for effort level: OpenCPN, a mature, actively-developed open-source ECS, does not bundle S-63 support in its core — it's a separate, paid third-party plugin ("o-charts"). If a well-resourced open-source navigation project treats this as separate, dedicated work rather than a core feature, it's realistically a multi-week effort for this project too, not a side-task alongside other features.

### Alternative considered and rejected: "unofficial" chart products
The AHO fact sheet on official vs. unofficial charts (FS_AusENC-Official_and_unofficial_electronic_charts, Oct 2020) warns that the simplified, proprietary vector formats used by consumer chart-plotter products omit critical hazards, can lag official updates by years, and are not warranted as suitable for navigation by either the Commonwealth or the manufacturer. It cites the 2014 loss of the racing yacht *Vestas Wind*, substantially destroyed after striking a reef missing from its unofficial chart data. These products also use proprietary vendor formats rather than S-57, introducing the commercial licensing dependency rejected in ADR 0006. They were rejected as an alternative to S-63 integration for both licensing and safety reasons.

### Lighter-weight alternative: enrich the free layer instead
Without touching S-63 or any per-vendor licensing at all, the GSHHG/OpenSeaMap reference layer shipped in ADR 0009 could be enriched with other free Australian open-government geospatial data — for example Geoscience Australia / AODN bathymetry grids, or AMSA hazard and wreck datasets. This would improve hazard context (e.g. depth contours, known wrecks) without reopening ADR 0006 at all. This ADR does not design that work, but names it as the standing alternative to weigh against the AusENC/S-63 path.

## Decision
**Deferred.** The options remain a multi-week S-63/OEM implementation that revisits ADR 0006, enrichment with free bathymetry/hazard data, or no further work for now. The findings above provide a basis for that decision.

## Consequences
Positive:
- Future decision-making on Australian chart data is informed by the actual licensing mechanism, cost, and engineering effort involved, rather than assumption.
- No engineering effort is spent before the licensing/effort tradeoff is understood and deliberately chosen.

Negative / explicitly deferred:
- The chart-data gap identified in ADR 0006 and carried forward in ADR 0009 remains open. This ADR resolves none of it — real navigational-grade chart data for Australian waters is still not available in helmcentral after this ADR.
- No technical design (S-57 parsing approach, S-63 decryption implementation, update-ingestion pipeline, or bathymetry-enrichment design) is included here; any of those would need their own ADR once a direction is chosen.

## Related
- ADR 0006: Manual Route Planning with Smart Helpers — the original "no chart licensing dependency of any kind" decision this ADR's findings bear directly on.
- ADR 0009: GSHHG Coastline Fallback Layer for the Route Planner Map — the reference-only layer this ADR's findings would extend or supersede, and whose "Deferred" section pointed to this research.
