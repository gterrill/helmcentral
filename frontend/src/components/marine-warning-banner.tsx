import { TriangleAlert } from 'lucide-react'

import type { MarineWarnings } from '@/hooks/use-marine-warnings'

// A persistent, non-dismissing safety banner - not a toast. Renders nothing
// when there is no active warning for the vessel's own zone (see
// useMarineWarnings: bulletins are pre-filtered to has_active_warning_for_zone
// and non-cancellation sections only, so a warning active elsewhere in the
// same state never shows up here). Re-evaluated on every poll, so it clears
// itself automatically once the warning is no longer active for this zone.
export function MarineWarningBanner({ warnings }: { warnings: MarineWarnings | null }) {
  if (!warnings || warnings.bulletins.length === 0) return null

  const groups = warnings.bulletins
    .map((bulletin) => ({
      key: bulletin.productId,
      detailsUrl: bulletin.detailsUrl,
      rows: bulletin.sections.map((section) => ({
        key: `${bulletin.productId}-${section.warningType}-${section.day}`,
        warningType: section.warningType || bulletin.title,
        day: section.day,
      })),
    }))
    .filter((group) => group.rows.length > 0)

  const rowCount = groups.reduce((count, group) => count + group.rows.length, 0)
  if (rowCount === 0) return null

  return (
    <div
      role="alert"
      className="rounded-lg border border-destructive bg-destructive/10 px-4 py-3 text-destructive shadow-sm"
    >
      <div className="flex items-start gap-3">
        <TriangleAlert className="mt-0.5 h-5 w-5 shrink-0" />
        <div className="min-w-0 flex-1 space-y-1.5">
          <p className="font-display text-xs font-semibold uppercase tracking-[0.14em]">
            Marine Warning{rowCount > 1 ? 's' : ''} — {warnings.zone}
          </p>
          {groups.map((group) => (
            <ul key={group.key} className="space-y-0.5">
              {group.rows.map((row) => (
                <li key={row.key} className="text-[11px] font-medium leading-snug">
                  {row.warningType}
                  {row.day ? <span className="text-destructive/80"> · {row.day}</span> : null}
                </li>
              ))}
              {group.detailsUrl ? (
                <li>
                  <a
                    href={group.detailsUrl}
                    target="_blank"
                    rel="noopener noreferrer"
                    className="text-[11px] font-medium text-destructive/80 underline underline-offset-2 hover:text-destructive"
                  >
                    View on BOM
                  </a>
                </li>
              ) : null}
            </ul>
          ))}
        </div>
      </div>
    </div>
  )
}
