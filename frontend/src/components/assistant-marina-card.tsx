import { Anchor } from 'lucide-react'
import type { ReactNode } from 'react'

import { AssistantMarinaMap } from '@/components/assistant-marina-map'
import { useAppConfig } from '@/hooks/use-app-config'
import {
  dataDateText,
  distanceText,
  facilityLine,
  hasMarinaGeometry,
  headerFacts,
  kindLabel,
  limitBadge,
  markLine,
  phoneHref,
  safeHref,
  websiteLabel,
  type LimitTone,
} from '@/lib/marina-card'
import { documentViewerHref } from '@/lib/document-citation'
import type { MarinaCard } from '@/lib/mate-cards'
import { cn } from '@/lib/utils'

// "exceeds" is the one alarm colour. "ok" is plain, and a limit that was not
// charted or a boat figure that was not set is muted: neither says the boat fits.
const BADGE_TONE: Record<LimitTone, string> = {
  critical: 'border-red-500/40 bg-red-50 text-red-800 dark:bg-red-950 dark:text-red-300',
  neutral: 'border-border bg-card text-foreground',
  muted: 'border-border bg-muted/40 text-muted-foreground',
}

function Section({ title, children }: { title: string; children: ReactNode }) {
  return (
    <details className="group min-w-0 border-t border-border pt-2">
      <summary className="cursor-pointer list-none text-xs font-medium text-foreground marker:hidden [&::-webkit-details-marker]:hidden">
        <span className="mr-1 inline-block text-muted-foreground transition-transform group-open:rotate-90" aria-hidden="true">
          ▸
        </span>
        {title}
      </summary>
      <div className="pt-2">{children}</div>
    </details>
  )
}

function ContactLinks({ contact }: { contact: MarinaCard['contact'] }) {
  const items: ReactNode[] = []
  if (contact.website) {
    const href = safeHref(contact.website)
    items.push(
      href ? (
        <a key="web" href={href} target="_blank" rel="noopener noreferrer" className="min-w-0 truncate text-primary underline-offset-2 hover:underline">
          {websiteLabel(href)}
        </a>
      ) : (
        <span key="web" className="min-w-0 truncate">
          {contact.website}
        </span>
      ),
    )
  }
  if (contact.phone) {
    const href = phoneHref(contact.phone)
    items.push(
      href ? (
        <a key="tel" href={href} className="shrink-0 text-primary underline-offset-2 hover:underline">
          {contact.phone}
        </a>
      ) : (
        <span key="tel" className="min-w-0 truncate">
          {contact.phone}
        </span>
      ),
    )
  }
  if (contact.vhf) items.push(<span key="vhf" className="shrink-0">VHF {contact.vhf}</span>)
  if (contact.email) items.push(<span key="email" className="min-w-0 truncate">{contact.email}</span>)
  if (items.length === 0) return null
  return <div className="flex min-w-0 flex-wrap items-center gap-x-3 gap-y-1 text-xs text-foreground">{items}</div>
}

/**
 * The marina under a Mate reply (ADR 0240): the facts the map records, a drawn
 * map of the pontoons and facilities, and how the boat compares with the
 * charted limits. The server built it from what the tool read, and a reopened
 * thread draws the same card.
 */
export function AssistantMarinaCard({ card }: { card: MarinaCard }) {
  const { ui } = useAppConfig()
  const facts = headerFacts(card)
  const dataDate = dataDateText(card.dataAsOf)

  return (
    <section
      aria-label="Marina"
      data-testid="assistant-marina-card"
      className="flex min-w-0 flex-col gap-2 rounded-md border border-border bg-card p-3"
    >
      <div className="flex min-w-0 items-center gap-2">
        <Anchor className="h-3.5 w-3.5 shrink-0 text-muted-foreground" aria-hidden="true" />
        <span className="shrink-0 text-[10px] font-medium uppercase tracking-wider text-muted-foreground">Marina</span>
        <span className="min-w-0 flex-1 truncate text-sm font-medium text-foreground" title={card.name}>
          {card.name || 'Unnamed marina'}
        </span>
      </div>

      {(facts.length > 0 || card.vesselLimitCheck.length > 0) && (
        <div className="flex min-w-0 flex-wrap items-center gap-x-3 gap-y-1">
          {facts.length > 0 && (
            <div data-testid="marina-facts" className="flex min-w-0 flex-wrap items-center gap-x-3 gap-y-1 text-xs tabular-nums text-foreground">
              {facts.map((f) => (
                <span key={f} className="min-w-0 truncate">
                  {f}
                </span>
              ))}
            </div>
          )}
          {card.vesselLimitCheck.map((c) => {
            const badge = limitBadge(c)
            return (
              <span
                key={c.particular}
                title={badge.title}
                className={cn('min-w-0 truncate rounded border px-1.5 py-0.5 text-xs tabular-nums', BADGE_TONE[badge.tone])}
              >
                {badge.text}
              </span>
            )
          })}
        </div>
      )}

      {hasMarinaGeometry(card) && <AssistantMarinaMap card={card} />}

      {card.facilities.length > 0 && (
        <ul aria-label="Facilities" className="flex min-w-0 flex-wrap gap-1.5">
          {card.facilities.map((f, i) => {
            const line = facilityLine(f)
            return (
              <li key={`${f.kind}-${i}`} className="flex min-w-0 max-w-full items-baseline gap-1.5 rounded border border-border px-1.5 py-0.5 text-xs">
                <span className="shrink-0 font-medium text-foreground">{line.label}</span>
                {line.detail && <span className="min-w-0 truncate text-muted-foreground">{line.detail}</span>}
              </li>
            )
          })}
        </ul>
      )}

      {card.entranceMarks.length > 0 && (
        <Section title={`Entrance marks (${card.entranceMarks.length})`}>
          <ul className="flex flex-col gap-1 text-xs text-foreground">
            {card.entranceMarks.map((m, i) => (
              <li key={i} className="min-w-0 truncate">
                {markLine(m)}
              </li>
            ))}
          </ul>
        </Section>
      )}

      {card.pontoonArms.length > 0 && (
        <Section title={`Pontoon arms (${card.pontoonArms.length})`}>
          <ul className="flex flex-wrap gap-1.5 text-xs text-foreground">
            {card.pontoonArms.map((a) => (
              <li key={a} className="rounded border border-border px-1.5 py-0.5">
                {a}
              </li>
            ))}
          </ul>
        </Section>
      )}

      {card.ashore.length > 0 && (
        <Section title={`Shops and services ashore (${card.ashore.length})`}>
          <ul className="flex flex-col gap-1.5 text-xs">
            {card.ashore.map((a, i) => (
              <li key={i} className="flex min-w-0 flex-wrap items-baseline gap-x-2">
                <span className="min-w-0 truncate font-medium text-foreground">{a.name}</span>
                <span className="shrink-0 text-muted-foreground">{kindLabel(a.kind)}</span>
                <span className="shrink-0 tabular-nums text-muted-foreground">{distanceText(a.distanceM, ui.distanceUnits)}</span>
                {a.openingHours && <span className="min-w-0 truncate text-muted-foreground">{a.openingHours}</span>}
              </li>
            ))}
          </ul>
        </Section>
      )}

      <div className="flex min-w-0 flex-col gap-1 border-t border-border pt-2">
        {card.savedDocuments.length > 0 && (
          <p data-testid="marina-saved-documents" className="flex min-w-0 flex-wrap items-baseline gap-x-1.5 gap-y-1 text-xs text-muted-foreground">
            <span className="shrink-0">In Documents:</span>
            {card.savedDocuments.map((doc) => (
              <a
                key={doc.id}
                href={documentViewerHref(doc.id)}
                title={doc.title}
                className="min-w-0 max-w-full truncate text-primary underline-offset-2 hover:underline"
              >
                {doc.title}
              </a>
            ))}
          </p>
        )}
        <ContactLinks contact={card.contact} />
        <p data-testid="marina-source" className="min-w-0 truncate text-xs text-muted-foreground">
          {dataDate ? `OpenStreetMap · data as of ${dataDate}` : 'OpenStreetMap'}
        </p>
        {card.geometryTruncated && <p className="text-xs text-muted-foreground">The map is simplified: some shapes were thinned or left out.</p>}
        {card.truncated && <p className="text-xs text-muted-foreground">The map has more than this card lists: lists may be incomplete.</p>}
      </div>
    </section>
  )
}
