/**
 * Symbols for the temperature telltales under the dial (ADR 0054).
 *
 * Hand-drawn rather than picked from lucide: all three readings are
 * temperatures, so the icon set's one thermometer made the row three identical
 * symbols that only a word could tell them apart. These say what is hot, not
 * merely that something is, which is what lets the labels go.
 *
 * What actually separates them at 16px is silhouette, not detail: coolant is
 * upright, the gearbox is round, exhaust is horizontal. That reads before any
 * of the interior strokes do, which is the whole job of a telltale.
 *
 * Drawn to lucide's conventions - 24 viewBox, stroke currentColor, round caps,
 * no fill - so they drop into the same slots and take their colour from the
 * telltale's own class.
 */

interface TelltaleIconProps {
  className?: string
}

/** Coolant: a thermometer standing over water. Upright. */
export function CoolantIcon({ className }: TelltaleIconProps) {
  return (
    <svg data-telltale-icon="coolant" className={className} viewBox="0 0 24 24" fill="none"
      stroke="currentColor" strokeWidth="1.9" strokeLinecap="round" strokeLinejoin="round"
      aria-hidden="true">
      <path d="M13.5 11.2V4.3a1.5 1.5 0 1 0-3 0v6.9a3.4 3.4 0 1 0 3 0z" />
      <path d="M3.4 21.3c1.4-1.3 2.9-1.3 4.3 0s2.9 1.3 4.3 0 2.9-1.3 4.3 0 2.9 1.3 4.3 0" />
    </svg>
  )
}

/** Gearbox: a toothed wheel with a bore. Round. */
export function GearboxIcon({ className }: TelltaleIconProps) {
  return (
    <svg data-telltale-icon="gearbox" className={className} viewBox="0 0 24 24" fill="none"
      stroke="currentColor" strokeWidth="1.9" strokeLinecap="round" strokeLinejoin="round"
      aria-hidden="true">
      <circle cx="12" cy="12" r="6.4" />
      <circle cx="12" cy="12" r="2.3" />
      <path d="M12 5.6V2.4M12 21.6v-3.2M18.4 12h3.2M2.4 12h3.2M16.5 7.5l2.3-2.3M5.2 18.8l2.3-2.3M16.5 16.5l2.3 2.3M5.2 5.2l2.3 2.3" />
    </svg>
  )
}

/** Exhaust: a pipe with gas leaving it. Horizontal. */
export function ExhaustIcon({ className }: TelltaleIconProps) {
  return (
    <svg data-telltale-icon="exhaust" className={className} viewBox="0 0 24 24" fill="none"
      stroke="currentColor" strokeWidth="1.9" strokeLinecap="round" strokeLinejoin="round"
      aria-hidden="true">
      <path d="M2.4 14h8.3a1.5 1.5 0 0 1 1.5 1.5v3.9a1.5 1.5 0 0 1-1.5 1.5H2.4z" />
      <path d="M14.3 19.6c1.7 0 1.7-2.7 3.4-2.7s1.7 2.7 3.4 2.7" />
      <path d="M13.1 12.9c1.7 0 1.7-2.7 3.4-2.7s1.7 2.7 3.4 2.7" />
      <path d="M11.9 6.2c1.7 0 1.7-2.7 3.4-2.7s1.7 2.7 3.4 2.7" />
    </svg>
  )
}
