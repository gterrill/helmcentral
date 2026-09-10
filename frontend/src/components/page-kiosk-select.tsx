import { useState } from 'react'
import { MonitorPlay } from 'lucide-react'
import { cn } from '@/lib/utils'

export interface KioskablePage {
  id: string
  name: string
  kiosk?: boolean
  kiosk_seconds?: number
  kiosk_when?: 'always' | 'anchored'
}

export interface KioskPatch {
  kiosk?: boolean
  kiosk_seconds?: number
  kiosk_when?: 'always' | 'anchored'
}

interface PageKioskSelectProps {
  page: KioskablePage | null
  onPatch: (id: string, patch: KioskPatch) => void
}

// Mirrors backend/dashboard_pages.go's kioskSecondsMin/kioskSecondsMax
// exactly - a value this control would let through must be one the server
// actually accepts.
const DEFAULT_KIOSK_SECONDS = 30
const MIN_KIOSK_SECONDS = 5
const MAX_KIOSK_SECONDS = 3600

/**
 * The kiosk flag, duration and condition (ADR 0089), templated directly on
 * PageSkinSelect and living beside it in the chip row for the same reason:
 * this is a property of the page an operator is already looking at, not a
 * setting three clicks away.
 *
 * Seconds only ever commit on blur or Enter, never per keystroke - a PATCH
 * on every digit typed would spam the server and briefly leave the stored
 * value at whatever partial number was on screen mid-edit. An out-of-range
 * value shows the destructive border and is never sent at all.
 */
export function PageKioskSelect({ page, onPatch }: PageKioskSelectProps) {
  const [invalid, setInvalid] = useState(false)

  if (!page) return null

  const commitSeconds = (raw: string) => {
    const value = Number(raw)
    if (!Number.isInteger(value) || value < MIN_KIOSK_SECONDS || value > MAX_KIOSK_SECONDS) {
      setInvalid(true)
      return
    }
    setInvalid(false)
    if (value !== page.kiosk_seconds) {
      onPatch(page.id, { kiosk_seconds: value })
    }
  }

  return (
    <div className="inline-flex w-fit items-center gap-2 rounded-md border border-border bg-background/70 px-3 py-1.5 text-xs font-semibold uppercase tracking-[0.1em] text-muted-foreground transition-colors focus-within:border-primary/40 hover:border-primary/40 hover:text-primary">
      <label className="inline-flex cursor-pointer items-center gap-1.5">
        <MonitorPlay className="h-3.5 w-3.5 shrink-0" aria-hidden="true" />
        <input
          type="checkbox"
          checked={page.kiosk ?? false}
          aria-label={`Kiosk for ${page.name}`}
          onChange={(e) => {
            setInvalid(false)
            onPatch(
              page.id,
              e.target.checked
                ? { kiosk: true, kiosk_seconds: page.kiosk_seconds || DEFAULT_KIOSK_SECONDS }
                : { kiosk: false },
            )
          }}
        />
        Kiosk
      </label>
      {page.kiosk && (
        <>
          <input
            key={page.id}
            type="number"
            min={MIN_KIOSK_SECONDS}
            max={MAX_KIOSK_SECONDS}
            step={5}
            defaultValue={page.kiosk_seconds ?? DEFAULT_KIOSK_SECONDS}
            aria-label={`Seconds for ${page.name}`}
            className={cn(
              'w-14 rounded border bg-transparent px-1 py-0.5 text-xs normal-case tabular-nums outline-none',
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
            aria-label={`Kiosk condition for ${page.name}`}
            className="cursor-pointer bg-transparent text-xs font-semibold uppercase tracking-[0.1em] outline-none"
            value={page.kiosk_when ?? 'always'}
            onChange={(e) => onPatch(page.id, { kiosk_when: e.target.value as 'always' | 'anchored' })}
          >
            <option value="always">Always</option>
            <option value="anchored">While anchored</option>
          </select>
        </>
      )}
    </div>
  )
}
