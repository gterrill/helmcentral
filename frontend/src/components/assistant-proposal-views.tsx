import type { ReactNode } from 'react'

import type { AssistantProposalOp } from '@/hooks/use-assistant-conversations'
import { formatPositionDM } from '@/lib/coordinates'
import { documentContentUrl } from '@/lib/document-download'
import { channelLabel, WATCH_SEND_LABELS } from '@/lib/watches'

// How a changeset operation's fields read on the card (ADR 0158). The default
// is a plain before and after; a type may supply its own view where a diff is
// not enough to judge the change. The first is a deck's plan, shown as a
// thumbnail: Mate cannot see pictures, so the operator has to.

/** The field's name as the operator reads it. */
const FIELD_LABELS: Record<string, string> = {
  zone_id: 'Location',
  bin_id: 'Bin',
  deck_id: 'Deck',
  plan_document_id: 'Plan',
  rule_id: 'Rule',
  equipment_id: 'Item',
  polygon: 'Outline',
}

/** Labels for one type's fields, ahead of the shared ones. */
const TYPE_FIELD_LABELS: Record<string, Record<string, string>> = {
  watch: {
    schedule: 'Runs',
    sections: 'Reports on',
    look_out_for: 'Looks out for',
    notify: 'Sends to',
    send: 'Sends',
    alarms_raised: 'Alarms that count',
    instrument_issues: 'Sensor problems that count',
    tank_change_points: 'Tank change that counts (points)',
    pressure_fall_hpa: 'Pressure fall that counts (hPa)',
  },
  alarm_rule: {
    value: 'Threshold',
    clear_value: 'Clears at',
    dwell_seconds: 'Holds for',
    stale_after_seconds: 'Silent for',
    op: 'Fires when',
    state: 'Severity',
  },
}

export function fieldLabel(name: string, type?: string): string {
  const label = (type ? TYPE_FIELD_LABELS[type]?.[name] : undefined) ?? FIELD_LABELS[name] ?? name.replace(/_/g, ' ')
  return label.charAt(0).toUpperCase() + label.slice(1)
}

const UUID = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i

/** A plain value as text. `ops` lets a local reference ("$1", the record the
 * first change creates) read as that change's name. */
export function formatFieldValue(value: unknown, ops: AssistantProposalOp[], type?: string, field?: string): string {
  if (type === 'watch' && field === 'send' && (value === 'findings' || value === 'every')) return WATCH_SEND_LABELS[value]
  if (type === 'watch' && field === 'notify' && Array.isArray(value) && value.length > 0) return value.map((c) => channelLabel(String(c))).join(', ')
  if (value === null || value === undefined || value === '') return '--'
  if (typeof value === 'boolean') return value ? 'Yes' : 'No'
  if (typeof value === 'number') return String(value)
  if (typeof value === 'string') {
    const ref = /^\$(\d+)$/.exec(value)
    if (ref) {
      const target = ops[Number(ref[1]) - 1]
      return target?.label ?? target?.description ?? 'a new record'
    }
    return UUID.test(value) ? `#${value.slice(0, 8)}` : value
  }
  if (Array.isArray(value)) {
    if (value.some(Array.isArray)) return `${value.length} points`
    return value.length === 0 ? '--' : value.join(', ')
  }
  return JSON.stringify(value)
}

type FieldView = (value: unknown) => ReactNode

interface PlanStopRow {
  arrive_label: string
  nights: number
  name: string
  lat: number
  lon: number
  source?: string
}

function isPlanStopRow(v: unknown): v is PlanStopRow {
  if (typeof v !== 'object' || v === null) return false
  const r = v as Record<string, unknown>
  return typeof r.arrive_label === 'string' && typeof r.nights === 'number' && typeof r.name === 'string' && typeof r.lat === 'number' && typeof r.lon === 'number'
}

/** A new plan's stops, one small block each: the place, then the day and nights,
 * then where its position came from and the position. The last stop has no
 * nights to say. No map. */
function PlanStopRows({ rows }: { rows: PlanStopRow[] }) {
  return (
    <ol className="flex min-w-0 flex-1 flex-col gap-2">
      {rows.map((row, index) => {
        const nights = index === rows.length - 1 && row.nights === 0 ? '' : row.nights === 1 ? '1 night' : `${row.nights} nights`
        const position = formatPositionDM(row.lat, row.lon)
        return (
          <li key={index} data-plan-stop="" className="flex min-w-0 flex-col gap-0.5">
            <span className="min-w-0 break-words font-medium text-foreground">{row.name}</span>
            <span className="min-w-0 break-words tabular-nums text-muted-foreground">{nights ? `${row.arrive_label} · ${nights}` : row.arrive_label}</span>
            <span className="min-w-0 break-words tabular-nums text-muted-foreground">{row.source ? `${row.source} · ${position}` : position}</span>
          </li>
        )
      })}
    </ol>
  )
}

/** Fields whose view fills the card's width and needs no label, keyed `type.field`. */
const FULL_WIDTH_VIEWS = new Set(['plan.stops'])

/** Views keyed `type.field`. */
const FIELD_VIEWS: Record<string, FieldView> = {
  'plan.stops': (value) => (Array.isArray(value) && value.length > 0 && value.every(isPlanStopRow) ? <PlanStopRows rows={value} /> : null),
  'deck.plan_document_id': (value) =>
    typeof value === 'string' && value !== '' ? (
      <img
        src={documentContentUrl(value)}
        alt="Deck plan"
        loading="lazy"
        className="max-h-40 max-w-full rounded-sm border border-border object-contain"
      />
    ) : null,
}

/** True when the field's view is shown across the card with no label column,
 * as long as the value has a view. */
export function fieldViewFillsWidth(type: string, field: string, value: unknown): boolean {
  return FULL_WIDTH_VIEWS.has(`${type}.${field}`) && fieldView(type, field, value) !== null
}

export function fieldView(type: string, field: string, value: unknown): ReactNode | null {
  const view = FIELD_VIEWS[`${type}.${field}`]
  return view ? view(value) : null
}
