/**
 * The readings SignalK is publishing under an item's source, as suffixes of
 * that source: `electrical.batteries.292.capacity.stateOfCharge` under
 * `electrical.batteries.292` is `capacity.stateOfCharge`. Sorted, without
 * repeats, and empty for an item with no source.
 */
export function liveReadings(source: string | undefined, paths: readonly string[]): string[] {
  if (!source) return []
  const prefix = `${source}.`
  const found = new Set<string>()
  for (const p of paths) {
    if (!p.startsWith(prefix)) continue
    const suffix = p.slice(prefix.length)
    if (suffix !== '') found.add(suffix)
  }
  return [...found].sort()
}
