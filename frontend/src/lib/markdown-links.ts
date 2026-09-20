// Shared by help-links.ts and note-links.ts: both resolve relative markdown
// links found in rendered content (help pages, note bodies) and both need a
// heading-anchor slugger and a posix path normaliser to do it. Pulled out of
// help-links.ts rather than left there and imported by note-links.ts, because
// a note is not help content and importing a help module from note-links.ts
// would be the wrong dependency direction - help-links.ts re-exports both
// names so nothing that already imports them from there has to change.
//
// Pure and React-free, like help-links.ts itself: no DOM API, nothing that
// can't run in a plain vitest table.

// GitHub's own heading-slug algorithm (what every ##/### heading's anchor id
// is on github.com, and so what every relative doc link's #hash already
// assumes): lowercase, drop anything that isn't a letter, digit, space or
// hyphen, then turn each space into a hyphen. Consecutive hyphens are NOT
// collapsed - "Battery & Power" loses only the "&", leaving the space on
// each side, which is why its slug is "battery--power" with two hyphens.
//
// No duplicate heading text exists anywhere in docs/ today, so the
// "-1", "-2", ... suffix GitHub (and github-slugger) appends to a repeated
// heading is never exercised and deliberately not implemented here - adding
// a second heading with the same text anywhere in the help pages needs this
// revisited, not silently mismatched.
export function slugifyHeading(text: string): string {
  return text
    .toLowerCase()
    .replace(/[^a-z0-9 -]/g, '')
    .replace(/ /g, '-')
}

// A tiny posix path normaliser (no node:path - this module stays
// browser-safe): resolves "." and ".." segments against the segments before
// them. Never throws on a ".." that runs off the front; it just stops
// popping, which is exactly what leaving docs/ entirely from a shallow page
// needs to do.
export function normalizePath(path: string): string {
  const segments: string[] = []
  for (const part of path.split('/')) {
    if (part === '' || part === '.') continue
    if (part === '..') segments.pop()
    else segments.push(part)
  }
  return segments.join('/')
}
