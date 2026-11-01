// Map line colours cannot be CSS tokens (MapLibre paints with literal
// colours), so the passages share one fixed palette, light enough for the
// dark basemap and dark enough for Positron. A single passage uses the first.
export const PASSAGE_COLOURS = ['#0284c7', '#d97706', '#059669', '#c026d3', '#e11d48', '#4f46e5'] as const
export const passageColour = (index: number) => PASSAGE_COLOURS[index % PASSAGE_COLOURS.length]
