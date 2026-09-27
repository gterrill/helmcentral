import { MAINTENANCE_STATUS_LABELS, type MaintenanceStatus } from '@/hooks/use-maintenance'

// ADR 0138: the one place that decides what a maintenance status looks
// like. Raw palette colors are reserved for alert semantics
// (AGENTS.md's Telemetry & Color Mapping) - overdue/due-soon/ok are exactly
// that (a warning/critical/healthy ladder), matching the identical
// red/amber/emerald ladder scopeBadgeClass already uses for anchor rode
// scope (anchor-rode-planner.tsx). never_recorded/hours_unknown/
// interval_not_set are data-quality states, not alerts, so they stay
// neutral.
export function maintenanceStatusBadgeClass(status: MaintenanceStatus): string {
  switch (status) {
    case 'overdue':
      return 'border-red-500/40 bg-red-500/10 text-red-400'
    case 'due_soon':
      return 'border-amber-500/40 bg-amber-500/10 text-amber-400'
    case 'ok':
      return 'border-emerald-500/40 bg-emerald-500/10 text-emerald-400'
    default:
      return 'border-border bg-muted/40 text-muted-foreground'
  }
}

export function MaintenanceStatusBadge({ status }: { status: MaintenanceStatus }) {
  return (
    <span
      className={`inline-flex h-5 shrink-0 items-center rounded-full border px-2 text-[10px] font-semibold tracking-wider uppercase ${maintenanceStatusBadgeClass(status)}`}
    >
      {MAINTENANCE_STATUS_LABELS[status]}
    </span>
  )
}
