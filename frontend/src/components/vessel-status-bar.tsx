import { Circle, Moon, Sun } from 'lucide-react'

import { useVesselIdentity } from '@/hooks/use-vessel-identity'

interface VesselStatusBarProps {
  isDark?: boolean
  onToggleDarkMode?: () => void
}

export function VesselStatusBar({ isDark = false, onToggleDarkMode }: VesselStatusBarProps) {
  const { currentDate, clock, signalkConnected } = useVesselIdentity()
  const [hh = '--', mm = '--', ss = '--'] = clock.timePart.split(':')

  return (
    <div className="flex shrink-0 items-center gap-1">
      <div className={`inline-flex items-center gap-1 rounded-md border px-2 py-1 text-[10px] font-semibold uppercase tracking-[0.08em] md:text-[11px] ${signalkConnected === true ? 'border-border bg-background/70 text-muted-foreground' : signalkConnected === false ? 'border-red-300/60 bg-red-50/60 text-red-600 dark:border-red-800/60 dark:bg-red-950/40 dark:text-red-400' : 'border-border bg-background/70 text-muted-foreground'}`}>
        <Circle className={`h-2.5 w-2.5 ${signalkConnected === true ? 'fill-secondary text-secondary' : signalkConnected === false ? 'fill-red-500 text-red-500' : 'fill-muted-foreground text-muted-foreground'}`} />
        <span className="hidden sm:inline">{signalkConnected === false ? 'No Signal' : 'Live'}</span>
      </div>
      <button
        onClick={onToggleDarkMode}
        className="inline-flex items-center gap-1 rounded-md border border-border bg-background/70 px-2 py-1 text-[10px] font-semibold uppercase tracking-[0.08em] text-muted-foreground transition-colors hover:border-primary/40 hover:text-primary md:text-[11px]"
        aria-label={isDark ? 'Switch to day mode' : 'Switch to night mode'}
      >
        {isDark ? <Moon className="h-3.5 w-3.5" /> : <Sun className="h-3.5 w-3.5" />}
        <span className="hidden sm:inline">{isDark ? 'Night' : 'Day'}</span>
      </button>
      <div className="flex shrink-0 flex-col items-end pl-2">
        <span className="whitespace-nowrap text-[10px] font-semibold uppercase tracking-[0.1em] text-foreground/85 md:text-[11px]">
          {currentDate}
        </span>
        <time className="inline-flex items-baseline whitespace-nowrap font-display leading-[1.08] tracking-[0.02em] text-gauge-secondary">
          <span className="inline-block w-[5ch] text-right tabular-nums text-[1.65rem] sm:text-[1.8rem] md:text-[2rem] lg:text-[2.15rem]">
            {hh}:{mm}
          </span>
          <span className="inline-block w-[3ch] text-left tabular-nums text-[1.65rem] sm:text-[1.8rem] md:text-[2rem] lg:text-[2.15rem]">
            :{ss}
          </span>
          <span className="ml-1 text-[1.05rem] tracking-[0.06em] sm:text-[1.15rem] md:text-[1.3rem] lg:text-[1.4rem]">
            {clock.meridiem}
          </span>
        </time>
      </div>
    </div>
  )
}
