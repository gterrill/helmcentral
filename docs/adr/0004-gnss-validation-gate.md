# ADR 0004: GNSS Validation Gate for Anchor Watch

## Status

Accepted

## Context

Anchor watch depends on vessel position updates. During GNSS degradation events (for example jamming, spoofing, or loss of fix), raw position jumps can trigger false anchor dragging alarms.

The backend and frontend need a clear, shared contract for handling invalid or degraded fixes before anchor radius logic runs.

## Decision

Introduce a GNSS validation gate with explicit fix-quality and HDOP thresholds.

Validation policy:

- Critical: quality indicator is 0 (fix unavailable or invalid), or HDOP is greater than 4.0.
- Trusted: quality indicator is valid and HDOP is less than or equal to 2.5.
- Degraded: non-critical but not trusted (for example HDOP between 2.5 and 4.0).

Fail-safe behavior:

- On critical validation state, freeze vessel position at the last trusted coordinates.
- Do not evaluate anchor dragging radius using the current corrupt position.
- Surface a distinct high-priority alert path for GPS corruption or jamming.

## API Contract

The vessel-state payload exposes GNSS validation metadata:

- gnss_quality_indicator: normalized NMEA quality or fix indicator (for example 1 GPS, 2 DGPS, 8 simulation).
- gnss_hdop: horizontal dilution of precision.
- gnss_validation_state: trusted, degraded, or critical.
- gnss_validation_reason: human-readable reason for degraded or critical state.
- gnss_critical_alert: true when anchor-watch calculations must be gated.

## Source Mapping Notes

NMEA 0183:

- Parse quality/fix indicator from GGA or GNS position-fix fields.
- Parse HDOP from the sentence HDOP field.

NMEA 2000:

- Parse quality or method from PGN 129029 GNSS Position Data.
- Parse HDOP from GNSS DOP values when available.

## Consequences

- Reduces false anchor dragging alarms during GNSS attacks or outages.
- Preserves operator trust by separating position-integrity failures from true anchor movement.
- Adds a stable contract for UI and alarm behavior around GNSS integrity transitions.