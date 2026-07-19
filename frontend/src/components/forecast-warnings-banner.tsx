import { useState } from 'react'

import { TriangleAlert, X } from 'lucide-react'

import type { ForecastWarnings } from '@/hooks/use-forecast-warnings'

const DISMISSED_SIGNATURE_STORAGE_KEY = 'helmcentral.forecastWarningsBanner.dismissedSignature'

// A stable string representing the currently-active warning content, e.g.
// "IDQ20085:Sunday 5 July:Strong Wind Warning|IDQ21285:Sunday 5 July:Hazardous Surf Warning".
// Used to make dismissal content-aware: dismissing the banner only suppresses
// it for this exact combination of warnings. If any warning changes (new
// day, new type, or an entirely different bulletin), the signature changes
// and the banner reappears automatically, even though the user already
// dismissed a previous, different warning.
function buildWarningSignature(warnings: ForecastWarnings): string {
  const parts = warnings.bulletins.flatMap((bulletin) =>
    bulletin.sections.map((section) => `${bulletin.id}:${section.day}:${section.warningType}`),
  )
  return parts.sort().join('|')
}

function readStoredSignature(): string | null {
  try {
    return localStorage.getItem(DISMISSED_SIGNATURE_STORAGE_KEY)
  } catch {
    return null
  }
}

// A persistent safety banner - not a toast - that clears itself
// automatically once the warning is no longer active (see
// useForecastWarnings: the provider plugin already scopes bulletins to the
// vessel's own region and drops cancelled/inactive sections, so only
// currently-relevant warnings ever reach this component). The only other way
// it disappears is a manual dismiss click; nothing here is time-based.
// Dismissal is content-aware and persisted in localStorage keyed by a
// signature of the currently-active warnings, so a page reload mid-passage
// won't immediately re-surface a warning already acknowledged this session -
// but a genuinely new/different warning still shows regardless of any prior
// dismissal.
export function ForecastWarningsBanner({ warnings }: { warnings: ForecastWarnings | null }) {
  const [dismissedSignature, setDismissedSignature] = useState<string | null>(() => readStoredSignature())

  if (!warnings || warnings.bulletins.length === 0) return null

  const groups = warnings.bulletins
    .map((bulletin) => ({
      key: bulletin.id,
      detailsUrl: bulletin.detailsUrl,
      rows: bulletin.sections.map((section) => ({
        key: `${bulletin.id}-${section.warningType}-${section.day}`,
        warningType: section.warningType || bulletin.title,
        day: section.day,
      })),
    }))
    .filter((group) => group.rows.length > 0)

  const rowCount = groups.reduce((count, group) => count + group.rows.length, 0)
  if (rowCount === 0) return null

  const signature = buildWarningSignature(warnings)
  if (signature && signature === dismissedSignature) return null

  const handleDismiss = () => {
    setDismissedSignature(signature)
    try {
      localStorage.setItem(DISMISSED_SIGNATURE_STORAGE_KEY, signature)
    } catch {
      // Best-effort persistence only - dismissal still works for this
      // session even if localStorage is unavailable (e.g. private mode).
    }
  }

  return (
    <div
      role="alert"
      className="relative rounded-lg border border-destructive bg-destructive/10 px-4 py-3 pr-9 text-destructive shadow-sm"
    >
      <button
        type="button"
        onClick={handleDismiss}
        aria-label="Dismiss"
        className="absolute right-2 top-2 rounded-full p-1 text-destructive/70 hover:bg-destructive/10 hover:text-destructive"
      >
        <X className="h-4 w-4" />
      </button>
      <div className="flex items-start gap-3">
        <TriangleAlert className="mt-0.5 h-5 w-5 shrink-0" />
        <div className="min-w-0 flex-1 space-y-1.5">
          <p className="font-display text-xs font-semibold uppercase tracking-[0.14em]">
            Forecast Warning{rowCount > 1 ? 's' : ''} — {warnings.region}
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
                    View details
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
