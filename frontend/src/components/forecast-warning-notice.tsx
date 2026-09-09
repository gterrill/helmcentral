import { findActiveSurfBulletin, findActiveWindBulletin, type ForecastWarnings } from '@/hooks/use-forecast-warnings'

// The forecast summary's own warning line(s). Purely a function of props: no
// local dismiss state, so a wind or surf line stays visible for exactly as
// long as the plugin reports that category active for the vessel's own
// region. Wind and surf are independent ladders (ADR 0087 -
// forecastWindWarningLevel and forecastSurfWarning are separate derived
// paths), so either line can appear alone, both together, or neither -
// there is no dependency between them the way there would be if this were
// one combined "is anything active" flag. Each line carries its own block
// wrapper rather than the inline fragment this used to return: this is the
// highest-stakes text on the forecast page, and set inline it had no more
// weight than the sentence it was appended to.
export function ForecastWarningNotice({ warnings }: { warnings: ForecastWarnings | null }) {
  const windBulletin = findActiveWindBulletin(warnings)
  const surfBulletin = findActiveSurfBulletin(warnings)
  if (!windBulletin && !surfBulletin) return null

  return (
    <>
      {windBulletin ? (
        <p data-testid="forecast-wind-warning" className="mt-1.5">
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
        </p>
      ) : null}
      {surfBulletin ? (
        <p data-testid="forecast-surf-warning" className="mt-1.5">
          <span className="font-semibold text-destructive">Hazardous surf warning in effect.</span>
          {surfBulletin.detailsUrl ? (
            <>
              {' '}
              <a
                href={surfBulletin.detailsUrl}
                target="_blank"
                rel="noopener noreferrer"
                className="text-destructive underline underline-offset-2 hover:text-destructive/80"
              >
                View details →
              </a>
            </>
          ) : null}
        </p>
      ) : null}
    </>
  )
}
