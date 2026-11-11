/**
 * The size of a numeric reading that scales with the box around it. It is
 * written in container units, so the element carrying it must sit in a
 * `[container-type:size]` box. A Gauge tile makes its whole body that box. The
 * Current tile makes each of its three rows its own, so a row is smaller than
 * a gauge body and the two match only when both sit at the floor below, for
 * example a 2 by 5 Current tile under 2 by 2 gauges.
 *
 * Height drives it until the box is wide, then width caps it so the value and
 * unit still fit two columns across; 2.25rem is the floor the two-row minimum
 * height of a gauge is built on.
 */
export const FLUID_READOUT_FONT = 'clamp(2.25rem, min(60cqh, 22cqi), 9rem)'

/** A unit beside a fluid reading: 30% of it, never under 12px. */
export const FLUID_UNIT_FONT = 'max(0.75rem, 0.3em)'
