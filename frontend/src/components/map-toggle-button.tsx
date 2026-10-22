import type { LucideIcon } from 'lucide-react'

import { cn } from '@/lib/utils'

export interface MapToggleButtonProps {
  icon: LucideIcon
  /** The accessible name, which is what the button does, not its state. */
  label: string
  on: boolean
  onToggle: () => void
  className?: string
}

/** The round-cornered map button the satellite and 3D toggles share:
 * sky-blue when on, dark glass when off. */
export function MapToggleButton({ icon: Icon, label, on, onToggle, className }: MapToggleButtonProps) {
  return (
    <button
      type="button"
      onClick={onToggle}
      aria-label={label}
      aria-pressed={on}
      className={cn(
        'flex h-10 w-10 items-center justify-center rounded-lg text-white shadow-sm backdrop-blur-sm active:scale-95',
        on ? 'bg-sky-600/90 hover:bg-sky-500/90' : 'bg-black/65 hover:bg-black/80',
        className,
      )}
      style={{ transition: 'background-color 150ms ease-out' }}
    >
      <Icon className="h-4 w-4" />
    </button>
  )
}
