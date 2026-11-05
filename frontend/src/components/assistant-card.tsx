import { lazy, Suspense, type ReactNode } from 'react'

import { AssistantTileCardFallback } from '@/components/assistant-tile-card-fallback'
import type { MateCard, PassageDecision } from '@/lib/mate-cards'

// Lazy for the same reason as AssistantMarkdown: the kiosk entry chunk should
// not carry card code for a thread it never mounts.
const AssistantPassageWindowCard = lazy(() =>
  import('./assistant-passage-window-card').then((m) => ({ default: m.AssistantPassageWindowCard })),
)

const AssistantTelemetryChartCard = lazy(() =>
  import('./assistant-telemetry-chart-card').then((m) => ({ default: m.AssistantTelemetryChartCard })),
)

const AssistantWindRoseCard = lazy(() => import('./assistant-wind-rose-card').then((m) => ({ default: m.AssistantWindRoseCard })))

// The marina card's map loads apart again (assistant-marina-map.tsx), so the
// card's text does not wait for maplibre.
const AssistantMarinaCard = lazy(() => import('./assistant-marina-card').then((m) => ({ default: m.AssistantMarinaCard })))

const AssistantAnchorageCard = lazy(() => import('./assistant-anchorage-card').then((m) => ({ default: m.AssistantAnchorageCard })))

// The tiles pull in the stores and hooks they read, so they load only when a
// reply carries one.
const AssistantTileCard = lazy(() => import('./assistant-tile-card').then((m) => ({ default: m.AssistantTileCard })))

const PLAIN_FALLBACK = <div className="h-16 min-w-0 rounded-md border border-border bg-card" aria-hidden="true" />

function CardSuspense({ fallback = PLAIN_FALLBACK, children }: { fallback?: ReactNode; children: ReactNode }) {
  return <Suspense fallback={fallback}>{children}</Suspense>
}

/** One card under a Mate reply (ADR 0191), chosen by `kind`. An `error` entry
 * is shown, not hidden: the operator should know a card was meant to be here. */
export function AssistantCard({
  card,
  canWrite = false,
  onDecisionChange,
  onDecisionUnreadable,
  liveTile = false,
}: {
  card: MateCard
  canWrite?: boolean
  onDecisionChange?: (cardId: number, decision: PassageDecision) => void
  onDecisionUnreadable?: () => void
  /** A tile card draws the live tile when its reply is the newest (ADR 0229). */
  liveTile?: boolean
}) {
  switch (card.kind) {
    case 'passage_window':
      return <CardSuspense><AssistantPassageWindowCard card={card} canWrite={canWrite} onDecisionChange={onDecisionChange} onDecisionUnreadable={onDecisionUnreadable} /></CardSuspense>
    case 'telemetry_chart':
      return <CardSuspense><AssistantTelemetryChartCard card={card} /></CardSuspense>
    case 'wind_rose':
      return <CardSuspense><AssistantWindRoseCard card={card} /></CardSuspense>
    case 'marina':
      return <CardSuspense><AssistantMarinaCard card={card} /></CardSuspense>
    case 'anchorage':
      return <CardSuspense><AssistantAnchorageCard card={card} /></CardSuspense>
    case 'tile':
      return (
        <CardSuspense fallback={<AssistantTileCardFallback tile={card.tile} />}>
          <AssistantTileCard card={card} live={liveTile} />
        </CardSuspense>
      )
    case 'error':
      return <CardError reason={card.reason} />
    default: {
      // A kind added to MateCard without a case above stops the build. A stale
      // bundle or hand-built entry can still reach here, and must not blank the thread.
      const unhandled: never = card
      return <CardError reason={`unknown card kind "${String((unhandled as { kind?: unknown }).kind)}"`} />
    }
  }
}

function CardError({ reason }: { reason: string }) {
  return (
    <section
      aria-label="Card error"
      data-testid="assistant-card-error"
      className="min-w-0 rounded-md border border-red-500/40 bg-red-50 p-3 text-xs text-red-800 dark:bg-red-950 dark:text-red-300"
    >
      This card couldn't be shown: {reason}
    </section>
  )
}
