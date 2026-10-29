import { useState } from 'react'

import { Field, FieldDescription, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Select, SelectItem, SelectPopup, SelectTrigger, SelectValue } from '@/components/ui/select'
import type { SchedulePreviewState } from '@/hooks/use-watches'
import {
  cronOf,
  EVERY_HOURS_CHOICES,
  parseCron,
  type SchedulePreset,
  type SchedulePresetKind,
} from '@/lib/watch-schedule'
import { cn } from '@/lib/utils'

// ADR 0227: how the operator says when a watch runs. Each choice writes a cron
// string the server understands; the sentence and the next three times under
// it come from the server's own preview, so what is shown is what will run.

const PRESET_LABELS: Record<SchedulePresetKind, string> = {
  hourly: 'Every hour',
  every: 'Every few hours',
  daily: 'Daily at a time',
  weekdays: 'Chosen weekdays at a time',
  window: 'Hours between two times',
  custom: 'Custom (cron)',
}

const PRESET_ORDER: SchedulePresetKind[] = ['hourly', 'every', 'daily', 'weekdays', 'window', 'custom']

const DAYS = [
  { id: 1, label: 'Mon' },
  { id: 2, label: 'Tue' },
  { id: 3, label: 'Wed' },
  { id: 4, label: 'Thu' },
  { id: 5, label: 'Fri' },
  { id: 6, label: 'Sat' },
  { id: 0, label: 'Sun' },
]

const HOURS = Array.from({ length: 24 }, (_, h) => h)
const squash = (cron: string) => cron.trim().split(/\s+/).join(' ')
const pad = (n: number) => String(n).padStart(2, '0')

function startingPreset(kind: SchedulePresetKind, current: string): SchedulePreset {
  switch (kind) {
    case 'hourly': return { kind }
    case 'every': return { kind, hours: 2 }
    case 'daily': return { kind, hour: 6, minute: 0 }
    case 'weekdays': return { kind, hour: 7, minute: 30, days: [1, 2, 3, 4, 5] }
    case 'window': return { kind, from: 22, to: 6 }
    case 'custom': return { kind, cron: current }
  }
}

export interface WatchSchedulePickerProps {
  /** The cron string being edited. */
  value: string
  onChange: (cron: string) => void
  preview: SchedulePreviewState
  disabled?: boolean
}

export function WatchSchedulePicker({ value, onChange, preview, disabled = false }: WatchSchedulePickerProps) {
  const [preset, setPreset] = useState<SchedulePreset>(() => parseCron(value))
  // The draft was changed from outside (Discard): follow it.
  if (squash(cronOf(preset)) !== squash(value)) setPreset(parseCron(value))

  const set = (next: SchedulePreset) => {
    setPreset(next)
    onChange(cronOf(next))
  }

  const answer = preview.preview
  const invalid = answer !== null && !answer.valid
  const hourLabel = (h: number) => `${pad(h)}:00`

  return (
    <div className="flex flex-col gap-3" data-testid="watch-schedule-picker">
      <Field>
        <FieldLabel htmlFor="watch-schedule-kind">Schedule</FieldLabel>
        <Select
          value={preset.kind}
          disabled={disabled}
          onValueChange={(v) => { if (v) set(startingPreset(v as SchedulePresetKind, value)) }}
        >
          <SelectTrigger id="watch-schedule-kind" aria-label="Schedule" className="max-w-sm">
            <SelectValue>{(v: SchedulePresetKind) => PRESET_LABELS[v] ?? v}</SelectValue>
          </SelectTrigger>
          <SelectPopup>
            {PRESET_ORDER.map((kind) => (
              <SelectItem key={kind} value={kind}>{PRESET_LABELS[kind]}</SelectItem>
            ))}
          </SelectPopup>
        </Select>
      </Field>

      {preset.kind === 'every' && (
        <Field>
          <FieldLabel htmlFor="watch-schedule-hours">Every</FieldLabel>
          <Select value={String(preset.hours)} disabled={disabled} onValueChange={(v) => { if (v) set({ kind: 'every', hours: Number(v) }) }}>
            <SelectTrigger id="watch-schedule-hours" aria-label="Hours between runs" className="max-w-40">
              <SelectValue>{(v: string) => `${v} hours`}</SelectValue>
            </SelectTrigger>
            <SelectPopup>
              {EVERY_HOURS_CHOICES.map((n) => <SelectItem key={n} value={String(n)}>{n} hours</SelectItem>)}
            </SelectPopup>
          </Select>
        </Field>
      )}

      {(preset.kind === 'daily' || preset.kind === 'weekdays') && (
        <Field>
          <FieldLabel htmlFor="watch-schedule-time">At</FieldLabel>
          <Input
            id="watch-schedule-time"
            type="time"
            aria-label="Time of day"
            className="max-w-40"
            disabled={disabled}
            value={`${pad(preset.hour)}:${pad(preset.minute)}`}
            onChange={(e) => {
              const m = /^(\d{2}):(\d{2})$/.exec(e.target.value)
              if (m) set({ ...preset, hour: Number(m[1]), minute: Number(m[2]) })
            }}
          />
        </Field>
      )}

      {preset.kind === 'weekdays' && (
        <div role="group" aria-label="Days" className="flex flex-wrap gap-2">
          {DAYS.map((d) => {
            const on = preset.days.includes(d.id)
            return (
              <button
                key={d.id}
                type="button"
                aria-pressed={on}
                disabled={disabled}
                onClick={() => set({ ...preset, days: on ? preset.days.filter((x) => x !== d.id) : [...preset.days, d.id] })}
                className={cn(
                  'h-9 min-w-12 rounded-md border px-2 text-sm font-medium focus-visible:outline-hidden focus-visible:ring-2 focus-visible:ring-ring disabled:opacity-50',
                  on ? 'border-primary bg-primary/10 text-foreground' : 'border-border bg-background text-muted-foreground',
                )}
              >
                {d.label}
              </button>
            )
          })}
        </div>
      )}

      {preset.kind === 'window' && (
        <div className="flex flex-wrap items-end gap-3">
          <Field className="w-auto">
            <FieldLabel htmlFor="watch-schedule-from">From</FieldLabel>
            <Select value={String(preset.from)} disabled={disabled} onValueChange={(v) => { if (v) set({ ...preset, from: Number(v) }) }}>
              <SelectTrigger id="watch-schedule-from" aria-label="From hour" className="w-28">
                <SelectValue>{(v: string) => hourLabel(Number(v))}</SelectValue>
              </SelectTrigger>
              <SelectPopup>{HOURS.map((h) => <SelectItem key={h} value={String(h)}>{hourLabel(h)}</SelectItem>)}</SelectPopup>
            </Select>
          </Field>
          <Field className="w-auto">
            <FieldLabel htmlFor="watch-schedule-to">To</FieldLabel>
            <Select value={String(preset.to)} disabled={disabled} onValueChange={(v) => { if (v) set({ ...preset, to: Number(v) }) }}>
              <SelectTrigger id="watch-schedule-to" aria-label="To hour" className="w-28">
                <SelectValue>{(v: string) => hourLabel(Number(v))}</SelectValue>
              </SelectTrigger>
              <SelectPopup>{HOURS.map((h) => <SelectItem key={h} value={String(h)}>{hourLabel(h)}</SelectItem>)}</SelectPopup>
            </Select>
          </Field>
        </div>
      )}

      {preset.kind === 'custom' && (
        <Field>
          <FieldLabel htmlFor="watch-schedule-cron">Cron</FieldLabel>
          <Input
            id="watch-schedule-cron"
            aria-label="Cron"
            className="max-w-sm font-mono"
            disabled={disabled}
            value={preset.cron}
            placeholder="0 6 * * *"
            onChange={(e) => set({ kind: 'custom', cron: e.target.value })}
          />
          <FieldDescription>Five fields: minute, hour, day of month, month, day of week. Never more often than every 15 minutes.</FieldDescription>
        </Field>
      )}

      <div className="flex flex-col gap-1 text-sm" aria-live="polite" data-testid="watch-schedule-preview">
        {preview.error ? (
          <p role="alert" className="text-destructive">{preview.error}</p>
        ) : answer === null ? (
          <p className="text-muted-foreground">Checking the schedule...</p>
        ) : invalid ? (
          <p role="alert" className="text-destructive">{answer.error}</p>
        ) : (
          <>
            <p className="font-medium text-foreground">{answer.sentence}</p>
            {answer.next_problem ? (
              <p role="alert" className="text-muted-foreground">{answer.next_problem}</p>
            ) : (
              <ul className="flex flex-col gap-0.5 text-muted-foreground" aria-label="Next runs">
                {answer.next.map((t) => <li key={t.at} className="tabular-nums">{t.local}</li>)}
              </ul>
            )}
          </>
        )}
      </div>
    </div>
  )
}

/** True when Save must wait: the schedule is unchecked, refused, or could not be asked about. */
export function schedulePreviewBlocksSave(preview: SchedulePreviewState): boolean {
  return preview.pending || preview.error !== null || preview.preview === null || !preview.preview.valid
}
