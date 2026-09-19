import { useState } from 'react'
import { MonitorPlay } from 'lucide-react'
import { cn } from '@/lib/utils'
import { DEFAULT_DWELL_SECONDS } from '@/lib/displays'

export interface DisplayablePage {
  id: string
  name: string
  display_id?: string
  dwell_seconds?: number
  show_when?: 'always' | 'anchored' | 'motoring' | 'sailing' | 'moored'
}

export interface DisplayPatch {
  display_id?: string
  dwell_seconds?: number
  show_when?: 'always' | 'anchored' | 'motoring' | 'sailing' | 'moored'
}

/** The subset of a wall display (lib/displays.ts's `Display`) this control
 * needs — kept minimal and locally typed, the way page-hero-select.tsx and
 * page-skin-select.tsx already type their own page shapes, so this component
 * doesn't have to import the full record just to read an id and a name. A
 * full `Display` satisfies this structurally, so callers can pass one
 * straight through. */
export interface PageDisplayOption {
  id: string
  name: string
}

interface PageDisplaySelectProps {
  page: DisplayablePage | null
  displays: PageDisplayOption[]
  onPatch: (id: string, patch: DisplayPatch) => void
  /**
   * Opens the displays-management dialog. With zero displays configured this
   * control has nothing else useful to offer, so it renders a single button
   * that calls this instead of a `<select>` that could only ever say "Not on
   * a wall" back — a control that can only say no is a dead end.
   */
  onManageDisplays: () => void
}

// Mirrors backend/dashboard_pages.go's dwellSecondsMin/dwellSecondsMax
// exactly - a value this control would let through must be one the server
// actually accepts.
const MIN_DWELL_SECONDS = 5
const MAX_DWELL_SECONDS = 3600

const NOT_ON_WALL = ''

// Stated up front, in the one place an operator sees before they act: the
// server clears a page's hero the moment a display is assigned (ADR 0110),
// and a silent clear after the fact would look like a bug rather than a rule.
const HELP_TEXT = 'Putting this page on a wall display clears its hero tile.'

/**
 * The display assignment, dwell and condition (ADR 0110, superseding ADR
 * 0089's kiosk checkbox), templated directly on the control it replaces
 * (page-kiosk-select.tsx) and living beside it in the chip row for the same
 * reason: this is a property of the page an operator is already looking at,
 * not a setting three clicks away.
 *
 * Seconds only ever commit on blur or Enter, never per keystroke - a PATCH
 * on every digit typed would spam the server and briefly leave the stored
 * value at whatever partial number was on screen mid-edit. An out-of-range
 * value shows the destructive border and is never sent at all.
 */
export function PageDisplaySelect({ page, displays, onPatch, onManageDisplays }: PageDisplaySelectProps) {
  const [invalid, setInvalid] = useState(false)

  if (!page) return null

  const commitSeconds = (raw: string) => {
    const value = Number(raw)
    if (!Number.isInteger(value) || value < MIN_DWELL_SECONDS || value > MAX_DWELL_SECONDS) {
      setInvalid(true)
      return
    }
    setInvalid(false)
    if (value !== page.dwell_seconds) {
      onPatch(page.id, { dwell_seconds: value })
    }
  }

  if (displays.length === 0) {
    return (
      <div
        title={HELP_TEXT}
        className="inline-flex w-fit items-center gap-1.5 rounded-md border border-border bg-background/70 px-3 py-1.5 text-xs font-semibold uppercase tracking-[0.1em] text-muted-foreground"
      >
        <MonitorPlay className="h-3.5 w-3.5 shrink-0" aria-hidden="true" />
        <button
          type="button"
          onClick={onManageDisplays}
          className="cursor-pointer bg-transparent text-xs font-semibold uppercase tracking-[0.1em] outline-hidden hover:text-primary"
        >
          No displays yet — add one
        </button>
      </div>
    )
  }

  return (
    <div
      title={HELP_TEXT}
      className="inline-flex w-fit items-center gap-2 rounded-md border border-border bg-background/70 px-3 py-1.5 text-xs font-semibold uppercase tracking-[0.1em] text-muted-foreground transition-colors focus-within:border-primary/40 hover:border-primary/40 hover:text-primary"
    >
      <label className="inline-flex cursor-pointer items-center gap-1.5">
        <MonitorPlay className="h-3.5 w-3.5 shrink-0" aria-hidden="true" />
        <select
          aria-label={`Wall display for ${page.name}`}
          className="cursor-pointer bg-transparent text-xs font-semibold uppercase tracking-[0.1em] outline-hidden"
          value={page.display_id ?? NOT_ON_WALL}
          onChange={(e) => {
            setInvalid(false)
            const value = e.target.value
            if (value === NOT_ON_WALL) {
              onPatch(page.id, { display_id: '' })
              return
            }
            onPatch(page.id, { display_id: value, dwell_seconds: page.dwell_seconds || DEFAULT_DWELL_SECONDS })
          }}
        >
          <option value={NOT_ON_WALL}>Not on a wall</option>
          {displays.map((d) => (
            <option key={d.id} value={d.id}>{d.name}</option>
          ))}
        </select>
      </label>
      {page.display_id && (
        <>
          <input
            key={page.id}
            type="number"
            min={MIN_DWELL_SECONDS}
            max={MAX_DWELL_SECONDS}
            step={5}
            defaultValue={page.dwell_seconds ?? DEFAULT_DWELL_SECONDS}
            aria-label={`Seconds for ${page.name}`}
            className={cn(
              'w-14 rounded-sm border bg-transparent px-1 py-0.5 text-xs normal-case tabular-nums outline-hidden',
              invalid ? 'border-destructive text-destructive' : 'border-border',
            )}
            onBlur={(e) => commitSeconds(e.target.value)}
            onKeyDown={(e) => {
              if (e.key !== 'Enter') return
              e.preventDefault()
              commitSeconds(e.currentTarget.value)
            }}
          />
          <span className="lowercase">s</span>
          <select
            aria-label={`Wall condition for ${page.name}`}
            className="cursor-pointer bg-transparent text-xs font-semibold uppercase tracking-[0.1em] outline-hidden"
            value={page.show_when ?? 'always'}
            onChange={(e) => onPatch(page.id, { show_when: e.target.value as DisplayPatch['show_when'] })}
          >
            <option value="always">Always</option>
            <option value="anchored">While anchored</option>
            <option value="motoring">While motoring</option>
            <option value="sailing">While sailing</option>
            <option value="moored">While moored</option>
          </select>
        </>
      )}
    </div>
  )
}
