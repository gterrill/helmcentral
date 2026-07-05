import { TriangleAlert } from 'lucide-react'

import type { MarineWarnings } from '@/hooks/use-marine-warnings'

// A small, non-dismissable reminder inside the forecast drawer - not a
// duplicate of MarineWarningBanner. Purely a function of props: no local
// dismiss state, so it stays visible for as long as a wind warning (category
// === 'wind') is active for the vessel's own zone, even if the user already
// dismissed the main dashboard banner. Renders nothing for surf-only
// warnings or when there's no active warning at all - reuses the zone- and
// cancellation-filtered data useMarineWarnings already provides rather than
// re-deriving that filtering here.
export function WindWarningNotice({ warnings }: { warnings: MarineWarnings | null }) {
  if (!warnings) return null

  const windBulletin = warnings.bulletins.find(
    (bulletin) => bulletin.category === 'wind' && bulletin.sections.length > 0,
  )
  if (!windBulletin) return null

  return (
    <p className="flex items-center gap-1.5 text-[12px] font-medium text-destructive">
      <TriangleAlert className="h-3.5 w-3.5 shrink-0" />
      <span>Wind warning in effect</span>
      {windBulletin.detailsUrl ? (
        <a
          href={windBulletin.detailsUrl}
          target="_blank"
          rel="noopener noreferrer"
          className="underline underline-offset-2 hover:text-destructive/80"
        >
          View on BOM →
        </a>
      ) : null}
    </p>
  )
}
