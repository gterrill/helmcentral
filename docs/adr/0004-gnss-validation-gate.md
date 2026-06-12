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

Additional heuristic checks:

- Staleness:
	- Degraded when GNSS timestamp age exceeds 10 seconds.
	- Critical when GNSS timestamp age exceeds 30 seconds, or timestamp is unavailable.
- Anchor/moored jump plausibility:
	- Degraded when implied speed between samples exceeds 6 knots.
	- Critical when implied speed between samples exceeds 12 knots.
- Depth consistency while anchored/moored:
	- Degraded when depth changes by more than 1.5 meters within 10 seconds.
	- Critical when depth jump is combined with implausible position jump.
- SNR-based Spoofing/Jamming Detection (when satellite data is available):
	- Critical (Spoofing) when SNR standard deviation is < 1.5 dB and average SNR > 30 dB.
	- Critical (Jamming) when max SNR drops below 20 dB.
- Recovery hysteresis after critical:
	- Stay critical until at least 5 consecutive trusted samples and at least 15 seconds have elapsed since entering critical.

## API Contract

The vessel-state payload exposes GNSS validation metadata:

- gnss_quality_indicator: normalized NMEA quality or fix indicator (for example 1 GPS, 2 DGPS, 8 simulation).
- gnss_hdop: horizontal dilution of precision.
- gnss_validation_state: trusted, degraded, or critical.
- gnss_validation_reason: human-readable reason for degraded or critical state.
- gnss_critical_alert: true when anchor-watch calculations must be gated.

## Source Mapping Notes

SignalK-native extraction used by Helmcentral:

- Parse quality/fix from navigation.gnss.methodQuality.value.
- Parse HDOP from navigation.gnss.horizontalDilution.value.
- If horizontal dilution is missing, use navigation.gnss.positionDilution.value.

Current deployment evidence:

- Live SignalK data exposes GNSS fix quality under methodQuality (with source values including pgn: 129029 and GGA sentence sources).
- Live SignalK data exposes DOP values under horizontalDilution and positionDilution.

No legacy fallbacks:

- The gate intentionally does not fall back to alternate legacy path names such as navigation.gnss.method, navigation.gnss.quality, or horizontalDilutionOfPrecision.
- This fail-fast policy prevents silent drift between expected SignalK contracts and upstream plugin mapping changes.

## Consequences

- Reduces false anchor dragging alarms during GNSS attacks or outages.
- Preserves operator trust by separating position-integrity failures from true anchor movement.
- Adds a stable contract for UI and alarm behavior around GNSS integrity transitions.
- Improves resilience to spoofing-like teleports and transient data spikes without alarm flapping.