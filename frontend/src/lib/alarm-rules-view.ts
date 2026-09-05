/**
 * Grouping for the Rules tile (PFD fix 3). The domain a rule belongs to is
 * already encoded in the SignalK path it watches: electrical, environment,
 * radar. Grouping by that segment answers the "give me categories" need
 * without inventing a second field, since the path already names the
 * system and a separate category would be one more thing to keep in step
 * with it.
 */
import type { AlarmRule } from '@/hooks/use-alarm-rules'

/**
 * The domain of a rule's path: its first segment once a leading
 * `helmcentral.` namespace (ADR 0070's derived-value paths) is stripped.
 * `electrical.batteries.house.voltage` -> `electrical`,
 * `helmcentral.environment.pressureRate` -> `environment`,
 * `depth` -> `depth`.
 */
export function ruleDomain(path: string): string {
  const stripped = path.replace(/^helmcentral\./, '')
  return stripped.split('.')[0]
}

export interface RuleDomainGroup {
  domain: string
  rules: AlarmRule[]
}

/** Rules bucketed by domain, domains alphabetical, rules alphabetical by label within each. */
export function groupRulesByDomain(rules: AlarmRule[]): RuleDomainGroup[] {
  const byDomain = new Map<string, AlarmRule[]>()
  for (const rule of rules) {
    const domain = ruleDomain(rule.path)
    const bucket = byDomain.get(domain)
    if (bucket) {
      bucket.push(rule)
    } else {
      byDomain.set(domain, [rule])
    }
  }

  return [...byDomain.entries()]
    .sort(([a], [b]) => a.localeCompare(b))
    .map(([domain, domainRules]) => ({
      domain,
      rules: [...domainRules].sort((a, b) =>
        a.label.localeCompare(b.label, undefined, { sensitivity: 'base' })),
    }))
}
