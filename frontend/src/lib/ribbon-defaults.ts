import type { SignalKPath } from '@/hooks/use-signalk-paths'

import { LAMP_LABEL_MAX_LENGTH, LAMP_STRIP_MAX_LAMPS, type LampConfig } from './dashboard-widgets'

/**
 * Default lamps for a fresh indicator ribbon (ADR 0085), resolved against the
 * paths the vessel is actually publishing rather than guessed. An entry
 * contributes nothing when its pattern has no match, so a boat missing a
 * generator or a second alternator simply gets a shorter ribbon.
 */
interface RibbonCatalogueEntry {
  /** A dotted path with exactly one `*` standing in for one instance segment. */
  pattern: string
  /** Orders the matched instances; the label function sees each one's position in this order. */
  sortInstances: (instances: string[]) => string[]
  /** The lamp's label for one matched instance, at its position after sorting. */
  label: (instance: string, index: number) => string
}

/** Numeric-aware ascending sort, so instance "10" does not sort before "2". */
function naturalCompare(a: string, b: string): number {
  const numA = Number(a)
  const numB = Number(b)
  if (!Number.isNaN(numA) && !Number.isNaN(numB)) return numA - numB
  return a.localeCompare(b)
}

function sortNatural(instances: string[]): string[] {
  return [...instances].sort(naturalCompare)
}

/** port, then starboard/stbd, then everything else alphabetically. */
function engineRank(instance: string): number {
  if (instance === 'port') return 0
  if (instance === 'starboard' || instance === 'stbd') return 1
  return 2
}

function sortEngineInstances(instances: string[]): string[] {
  return [...instances].sort((a, b) => {
    const rankDiff = engineRank(a) - engineRank(b)
    return rankDiff !== 0 ? rankDiff : a.localeCompare(b)
  })
}

/** "Port" / "Stbd" for the known sides, "Eng <instance>" otherwise. */
function engineLabel(instance: string): string {
  if (instance === 'port') return 'Port'
  if (instance === 'starboard' || instance === 'stbd') return 'Stbd'
  return `Eng ${instance}`
}

/** The first matched instance takes the bare base label; further ones get "<base> <instance>". */
function firstThenNumbered(base: string): (instance: string, index: number) => string {
  return (instance, index) => (index === 0 ? base : `${base} ${instance}`)
}

/** Every matched instance gets "<base> <instance>". */
function numbered(base: string): (instance: string) => string {
  return (instance) => `${base} ${instance}`
}

/** Keeps only the naturally-first matched instance, for a lamp that should appear at most once. */
function onlyFirst(instances: string[]): string[] {
  return sortNatural(instances).slice(0, 1)
}

const RIBBON_CATALOGUE: RibbonCatalogueEntry[] = [
  // Engines: one lamp per propulsion instance, non-zero while running.
  { pattern: 'propulsion.*.revolutions', sortInstances: sortEngineInstances, label: engineLabel },
  // Generator: 0 stopped, non-zero running.
  { pattern: 'electrical.generator.*.stateNumber', sortInstances: sortNatural, label: firstThenNumbered('Gen') },
  // Shore power: 1 when AC-in is present. Only one lamp, even with multiple inverters.
  { pattern: 'electrical.inverters.*.acState.acIn1Available', sortInstances: onlyFirst, label: () => 'Shore' },
  // Alternators: 0 off, non-zero charging (bulk/absorption/float).
  { pattern: 'electrical.alternator.*.chargingModeNumber', sortInstances: sortNatural, label: numbered('Alt') },
]

/** Builds a `^segment\.segment\.…$` matcher, with `*` capturing one dotted instance segment. */
function patternToRegex(pattern: string): RegExp {
  const segments = pattern
    .split('.')
    .map((segment) => (segment === '*' ? '([A-Za-z0-9_-]+)' : segment.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')))
  return new RegExp(`^${segments.join('\\.')}$`)
}

function matchInstances(paths: SignalKPath[], pattern: string): Map<string, string> {
  const regex = patternToRegex(pattern)
  const pathByInstance = new Map<string, string>()
  for (const { path } of paths) {
    const match = regex.exec(path)
    if (match) pathByInstance.set(match[1], path)
  }
  return pathByInstance
}

/**
 * Resolves the ribbon catalogue against the paths this vessel actually
 * publishes. Never invents a path: a pattern with no match contributes
 * nothing. Capped at the strip's own lamp limit.
 */
export function suggestRibbonLamps(paths: SignalKPath[]): LampConfig[] {
  const lamps: LampConfig[] = []

  for (const entry of RIBBON_CATALOGUE) {
    const pathByInstance = matchInstances(paths, entry.pattern)
    if (pathByInstance.size === 0) continue

    const instances = entry.sortInstances([...pathByInstance.keys()])
    instances.forEach((instance, index) => {
      const path = pathByInstance.get(instance)
      if (path === undefined) return
      lamps.push({ path, label: entry.label(instance, index).slice(0, LAMP_LABEL_MAX_LENGTH) })
    })
  }

  return lamps.slice(0, LAMP_STRIP_MAX_LAMPS)
}
