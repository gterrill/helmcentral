import { findActiveWindBulletin, type ForecastWarnings } from '@/hooks/use-forecast-warnings'

// An inline sentence appended to the forecast summary paragraph - not a
// duplicate of ForecastWarningsBanner. Purely a function of props: no local
// dismiss state, so it stays visible for as long as a wind warning (category
// === 'wind') is active for the vessel's own region, even if the user
// already dismissed the main dashboard banner. Renders nothing for surf-only
// warnings or when there's no active warning at all - reuses the
// already-filtered data useForecastWarnings provides rather than re-deriving
// that filtering here. Returns a fragment (no block wrapper) so it can sit
// inline within an existing paragraph of text.
export function WindWarningNotice({ warnings }: { warnings: ForecastWarnings | null }) {
  const windBulletin = findActiveWindBulletin(warnings)
  if (!windBulletin) return null

  return (
    <>
      {' '}
      <span className="font-semibold text-destructive">Wind warning in effect.</span>
      {windBulletin.detailsUrl ? (
        <>
          {' '}
          <a
            href={windBulletin.detailsUrl}
            target="_blank"
            rel="noopener noreferrer"
            className="text-destructive underline underline-offset-2 hover:text-destructive/80"
          >
            View details →
          </a>
        </>
      ) : null}
    </>
  )
}
