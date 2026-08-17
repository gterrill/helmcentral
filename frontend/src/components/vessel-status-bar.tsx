import { Circle, LogOut, Moon, Sun } from 'lucide-react'

import { useVesselIdentity } from '@/hooks/use-vessel-identity'
import { useTelemetryStatus } from '@/hooks/use-telemetry-stream'
import { BREAKPOINTS, useMinWidth } from '@/lib/breakpoints'

interface VesselStatusBarProps {
  isDark?: boolean
  onToggleDarkMode?: () => void
  /**
   * The SignalK username of the current session, or null under mode:none
   * (no auth) or before mode has resolved. Only mode:signalk ever passes a
   * value here — App.tsx is what decides that, not this component.
   */
  username?: string | null
  /** Present only under mode:signalk; its absence is what hides the control. */
  onLogout?: () => void
}

export function VesselStatusBar({ isDark = false, onToggleDarkMode, username = null, onLogout }: VesselStatusBarProps) {
  const { currentDate, clock, signalkConnected } = useVesselIdentity()
  const telemetryStatus = useTelemetryStatus()
  const [hh = '--', mm = '--', ss = '--'] = clock.timePart.split(':')
  const showFullClock = useMinWidth(BREAKPOINTS.sm)

  // Two independent things can go wrong: the backend can't reach SignalK
  // (signalkConnected, polled via /api/vessel-state's `source` field), or the
  // browser's own push stream to the backend is down (telemetryStatus - see
  // use-telemetry-stream.ts). Either one freezes the dashboard's live tiles,
  // including the anchor drag alarm's position feed, so both have to be
  // healthy for this badge to say "Live".
  const noSignal = signalkConnected === false || telemetryStatus === 'disconnected'
  const reconnecting = !noSignal && telemetryStatus === 'reconnecting'
  const label = noSignal ? 'No Signal' : reconnecting ? 'Reconnecting' : 'Live'

  return (
    <div className="flex shrink-0 items-center gap-1">
      <div className={`inline-flex items-center gap-1 rounded-md border px-2 py-1 text-[10px] font-semibold uppercase tracking-[0.08em] md:text-[11px] ${noSignal ? 'border-red-300/60 bg-red-50/60 text-red-600 dark:border-red-800/60 dark:bg-red-950/40 dark:text-red-400' : reconnecting ? 'border-amber-300/60 bg-amber-50/60 text-amber-700 dark:border-amber-800/60 dark:bg-amber-950/40 dark:text-amber-400' : 'border-border bg-background/70 text-muted-foreground'}`}>
        <Circle className={`h-2.5 w-2.5 ${noSignal ? 'fill-red-500 text-red-500' : reconnecting ? 'fill-amber-500 text-amber-500' : 'fill-secondary text-secondary'}`} />
        <span className="hidden sm:inline">{label}</span>
      </div>
      <button
        onClick={onToggleDarkMode}
        className="inline-flex items-center gap-1 rounded-md border border-border bg-background/70 px-2 py-1 text-[10px] font-semibold uppercase tracking-[0.08em] text-muted-foreground transition-colors hover:border-primary/40 hover:text-primary md:text-[11px]"
        aria-label={isDark ? 'Switch to day mode' : 'Switch to night mode'}
      >
        {isDark ? <Moon className="h-3.5 w-3.5" /> : <Sun className="h-3.5 w-3.5" />}
        <span className="hidden sm:inline">{isDark ? 'Night' : 'Day'}</span>
      </button>
      {onLogout && (
        <button
          onClick={onLogout}
          className="inline-flex items-center gap-1 rounded-md border border-border bg-background/70 px-2 py-1 text-[10px] font-semibold uppercase tracking-[0.08em] text-muted-foreground transition-colors hover:border-primary/40 hover:text-primary md:text-[11px]"
          aria-label={username ? `Log out ${username}` : 'Log out'}
        >
          <LogOut className="h-3.5 w-3.5" />
          {username && <span className="hidden sm:inline">{username}</span>}
        </button>
      )}
      <div className="flex min-w-0 shrink-0 flex-col items-end pl-2">
        {/* Below `sm` the date and seconds are dropped rather than CSS-hidden. Both
            are the least-informative parts of this cluster — the date already
            appears on the Today & Now tile — and the seconds span re-renders every
            second, so hiding it in CSS would keep paying for a widget nobody can
            see. The `ch` widths and `tabular-nums` stay: they stop the clock
            jittering as digits change. */}
        {showFullClock && (
          <span className="whitespace-nowrap text-[10px] font-semibold uppercase tracking-[0.1em] text-foreground/85 md:text-[11px]">
            {currentDate}
          </span>
        )}
        <time className="inline-flex items-baseline whitespace-nowrap font-display leading-[1.08] tracking-[0.02em] text-gauge-secondary">
          <span className="inline-block w-[5ch] text-right tabular-nums text-[1.35rem] sm:text-[1.8rem] md:text-[2rem] lg:text-[2.15rem]">
            {hh}:{mm}
          </span>
          {showFullClock && (
            <span className="inline-block w-[3ch] text-left tabular-nums text-[1.8rem] md:text-[2rem] lg:text-[2.15rem]">
              :{ss}
            </span>
          )}
          <span className="ml-1 text-[10px] tracking-[0.06em] sm:text-[1.15rem] md:text-[1.3rem] lg:text-[1.4rem]">
            {clock.meridiem}
          </span>
        </time>
      </div>
    </div>
  )
}
