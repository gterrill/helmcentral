import { lazy, Suspense } from 'react'

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

/** One card under a Mate reply (ADR 0191), chosen by `kind`. An `error` entry
 * is shown, not hidden: the operator should know a card was meant to be here. */
export function AssistantCard({
  card,
  canWrite = false,
  onDecisionChange,
  onDecisionUnreadable,
}: {
  card: MateCard
  canWrite?: boolean
  onDecisionChange?: (cardId: number, decision: PassageDecision) => void
  onDecisionUnreadable?: () => void
}) {
  switch (card.kind) {
    case 'passage_window':
      return (
        <Suspense
          fallback={<div className="h-16 min-w-0 rounded-md border border-border bg-card" aria-hidden="true" />}
        >
          <AssistantPassageWindowCard card={card} canWrite={canWrite} onDecisionChange={onDecisionChange} onDecisionUnreadable={onDecisionUnreadable} />
        </Suspense>
      )
    case 'telemetry_chart':
      return (
        <Suspense
          fallback={<div className="h-16 min-w-0 rounded-md border border-border bg-card" aria-hidden="true" />}
        >
          <AssistantTelemetryChartCard card={card} />
        </Suspense>
      )
    case 'wind_rose':
      return (
        <Suspense
          fallback={<div className="h-16 min-w-0 rounded-md border border-border bg-card" aria-hidden="true" />}
        >
          <AssistantWindRoseCard card={card} />
        </Suspense>
      )
    case 'error':
      return (
        <section
          aria-label="Card error"
          data-testid="assistant-card-error"
          className="min-w-0 rounded-md border border-red-500/40 bg-red-50 p-3 text-xs text-red-800 dark:bg-red-950 dark:text-red-300"
        >
          This card couldn't be shown: {card.reason}
        </section>
      )
  }
}
