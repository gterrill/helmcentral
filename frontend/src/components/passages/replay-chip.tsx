import type { ReactNode } from 'react'

import { cn } from '@/lib/utils'

interface ReplayChipProps {
  selected: boolean
  disabled: boolean
  /** The full name for assistive tech: what it is, and when. */
  label: string
  /** The leading icon or thumbnail. */
  icon: ReactNode
  onClick: () => void
  children: ReactNode
}

/** One pickable item (a sign or a camera shot) in an editor's picker row. */
export function ReplayChip({ selected, disabled, label, icon, onClick, children }: ReplayChipProps) {
  return (
    <button
      type="button"
      aria-pressed={selected}
      aria-label={label}
      disabled={disabled}
      className={cn(
        'flex h-8 shrink-0 items-center gap-1.5 rounded-md border px-1.5 focus-visible:outline-hidden focus-visible:ring-2 focus-visible:ring-ring',
        selected ? 'border-primary bg-primary text-primary-foreground' : 'border-border bg-card text-foreground',
      )}
      onClick={onClick}
    >
      {icon}
      {children}
    </button>
  )
}

/** The chip's time: muted on an idle chip, plain on the picked one. */
export function ReplayChipTime({ selected, children }: { selected: boolean; children: ReactNode }) {
  return <span className={cn('text-xs tabular-nums', selected ? 'text-primary-foreground' : 'text-muted-foreground')}>{children}</span>
}
