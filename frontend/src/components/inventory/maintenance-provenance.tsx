import type { MaintenanceOverrideField, MaintenanceRule } from '@/hooks/use-maintenance'

// ADR 0148: a profile job shows the profile's own values unless this item
// overrides them. The marker says "this item differs from its profile" and
// the tooltip says which values, in the operator's words.

export const OVERRIDE_FIELD_LABELS: Record<MaintenanceOverrideField, string> = {
  description: 'description',
  interval_hours: 'hours interval',
  interval_months: 'months interval',
  not_applicable: 'not applicable',
}

export function OverrideMarker({ rule }: { rule: Pick<MaintenanceRule, 'overridden_fields'> }) {
  if (rule.overridden_fields.length === 0) return null
  const what = rule.overridden_fields.map((f) => OVERRIDE_FIELD_LABELS[f] ?? f).join(', ')
  return (
    <span
      title={`Differs from the profile: ${what}`}
      className="inline-flex h-4 shrink-0 items-center rounded-full border border-border px-1.5 text-[10px] font-semibold tracking-wider text-muted-foreground uppercase"
    >
      Edited
    </span>
  )
}

/** "250 h", "12 months", both joined, or null when neither is set. */
export function intervalSummary(rule: Pick<MaintenanceRule, 'interval_hours' | 'interval_months'>): string | null {
  const parts: string[] = []
  if (rule.interval_hours != null) parts.push(`${rule.interval_hours} h`)
  if (rule.interval_months != null) parts.push(`${rule.interval_months} months`)
  return parts.length > 0 ? parts.join(' / ') : null
}
