import { Check, LayoutDashboard } from 'lucide-react'

import { cn } from '@/lib/utils'

interface LayoutModeToggleProps {
  editing: boolean
  onToggle: () => void
}

export function LayoutModeToggle({ editing, onToggle }: LayoutModeToggleProps) {
  return (
    <button
      type="button"
      onClick={onToggle}
      className={cn(
        'inline-flex items-center gap-1 rounded-md border px-2 py-1 text-[10px] font-semibold uppercase tracking-[0.08em] transition-colors md:text-[11px]',
        editing
          ? 'border-primary/40 bg-primary/10 text-primary'
          : 'border-border bg-background/70 text-muted-foreground hover:border-primary/40 hover:text-primary',
      )}
      aria-label={editing ? 'Exit edit mode' : 'Enter edit mode'}
      aria-pressed={editing}
    >
      {editing ? (
        <Check className="h-3.5 w-3.5" />
      ) : (
        <LayoutDashboard className="h-3.5 w-3.5" />
      )}
      <span className="hidden sm:inline">{editing ? 'Done' : 'Edit'}</span>
    </button>
  )
}
