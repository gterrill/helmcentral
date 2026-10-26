import type { LucideIcon } from 'lucide-react'

import { cn } from '@/lib/utils'

export interface MapIconButtonProps {
  icon: LucideIcon
  /** The accessible name: what the button does. */
  label: string
  onClick: () => void
  disabled?: boolean
  className?: string
}

/** The dark-glass square map button that zoom, recentre and full screen
 * share. The satellite and 3D toggles use MapToggleButton, which adds an
 * on state. */
export function MapIconButton({ icon: Icon, label, onClick, disabled, className }: MapIconButtonProps) {
  return (
    <button
      type="button"
      onClick={onClick}
      aria-label={label}
      disabled={disabled}
      className={cn(
        'flex h-10 w-10 items-center justify-center rounded-lg bg-black/65 text-white shadow-sm backdrop-blur-sm hover:bg-black/80 active:scale-95 disabled:pointer-events-none disabled:opacity-50',
        className,
      )}
      style={{ transition: 'background-color 150ms ease-out' }}
    >
      <Icon className="h-4 w-4" />
    </button>
  )
}
