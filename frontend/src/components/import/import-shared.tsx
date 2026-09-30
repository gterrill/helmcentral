import type { ReactNode, SelectHTMLAttributes } from 'react'

import { cn } from '@/lib/utils'
import type { ImportDecisions, ImportRun, RecordAction } from '@/lib/import-run'

// Small pieces every wizard page uses. The select is the browser's own: a
// page of forty rows each needing one choice is faster to work, and easier to
// hit with a thumb, than forty popups.

export interface StepProps {
  run: ImportRun
  decisions: ImportDecisions
  setDecision: <K extends keyof ImportDecisions>(category: K, key: string, value: ImportDecisions[K][string]) => void
}

export function ImportSelect({ className, ...props }: SelectHTMLAttributes<HTMLSelectElement>) {
  return (
    <select
      className={cn(
        'h-9 w-full min-w-0 rounded-md border border-input bg-background px-2 text-sm focus-visible:outline-hidden focus-visible:ring-2 focus-visible:ring-ring disabled:opacity-50',
        className,
      )}
      {...props}
    />
  )
}

export function StepHeading({ title, children }: { title: string; children?: ReactNode }) {
  return (
    <div className="space-y-1">
      <h2 className="text-base font-semibold text-foreground">{title}</h2>
      {children && <div className="text-sm text-muted-foreground">{children}</div>}
    </div>
  )
}

export function Pill({ tone = 'muted', children }: { tone?: 'muted' | 'warning' | 'alert'; children: ReactNode }) {
  return (
    <span
      className={cn(
        'inline-flex h-5 shrink-0 items-center rounded-4xl border px-2 text-xs font-medium whitespace-nowrap',
        tone === 'muted' && 'border-border text-muted-foreground',
        tone === 'warning' && 'border-amber-500/40 bg-amber-500/10 text-amber-700 dark:text-amber-400',
        tone === 'alert' && 'border-red-500/40 bg-red-500/10 text-red-700 dark:text-red-400',
      )}
    >
      {children}
    </span>
  )
}

export function AlreadyImported({ run, keyName }: { run: ImportRun; keyName: string }) {
  return run.already_imported.includes(keyName) ? <Pill>Already imported</Pill> : null
}

/** Card chrome shared by a wizard page's rows. */
export function RowCard({ children, className }: { children: ReactNode; className?: string }) {
  return <li className={cn('flex min-w-0 flex-col gap-2 rounded-md border border-border bg-card p-3', className)}>{children}</li>
}

/** create / match / skip as one select value, `match:<id>` carrying the target. */
export function encodeChoice(action: RecordAction, targetId: string): string {
  return action === 'match' ? `match:${targetId}` : action
}

export function decodeChoice(value: string): { action: RecordAction; target: string } {
  if (value.startsWith('match:')) return { action: 'match', target: value.slice('match:'.length) }
  return { action: value === 'create' ? 'create' : 'skip', target: '' }
}
