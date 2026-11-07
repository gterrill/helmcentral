import type { ReactNode } from 'react'
import { cn } from '@/lib/utils'

export interface WarningNoteProps {
  /** Spacing from the neighbours (the note sets none of its own). */
  className?: string
  children: ReactNode
}

/** An amber note under a figure, for something the operator should act on. */
export function WarningNote({ className, children }: WarningNoteProps) {
  return (
    <p
      data-slot="warning-note"
      className={cn('rounded-md border border-amber-500/40 bg-amber-500/10 px-2 py-1 text-xs text-amber-500', className)}
    >
      {children}
    </p>
  )
}
