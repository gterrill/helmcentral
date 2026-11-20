/** A number from an API body, or null when it is missing or not finite. */
export const finiteOrNull = (v: unknown): number | null => (typeof v === 'number' && Number.isFinite(v) ? v : null)
