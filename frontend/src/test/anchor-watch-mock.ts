import { vi } from 'vitest'

/**
 * A stand-in for '@/hooks/use-anchor-watch' in tests that render the app with
 * a fixed watch record. Every reader (the shell's selector, the tile's and
 * page's useAnchorWatch) and every write reads the same object, so a test that
 * passes `setAnchorHere: spy` sees the shell's Drop anchor call it.
 *
 *   vi.mock('@/hooks/use-anchor-watch', async () =>
 *     (await import('./anchor-watch-mock')).anchorWatchModule(() => ({ active: true })))
 *
 * `read` is called on every hook call, so a test can change what it returns
 * between renders. The record is loaded unless `read` says `loaded: false`,
 * which is how a test walks the shell through the first answer landing. The empty record is the module's own parse of no answer,
 * read through importActual so the mock never drifts from the fields the
 * record has. anchorWatchAt hands the record back as it is: a test that wants
 * a distance or a drag state puts it in the record.
 */
export async function anchorWatchModule(read: () => Record<string, unknown>) {
  const actual = await vi.importActual<typeof import('@/hooks/use-anchor-watch')>('@/hooks/use-anchor-watch')
  const EMPTY = actual.parseAnchorWatch(undefined)
  const writes = ['setAnchorHere', 'updateRadius', 'adjustAnchor', 'updateRodeAndConditions', 'updatePlanningDepth', 'clearAnchor'] as const
  // eslint-disable-next-line @typescript-eslint/no-explicit-any -- a test double: the app's own types check the callers
  const record = (): any => {
    const overrides = read()
    const defaults = Object.fromEntries(writes.map((name) => [name, vi.fn()]))
    // Loaded unless the test says otherwise: most tests render a watch that
    // has answered. One that passes `loaded: false` gets the record before
    // the first answer, as the real hook hands it out.
    return { ...EMPTY, loaded: true, ...defaults, ...overrides }
  }
  // The module's own write functions forward to whatever the record holds now.
  const write = (name: (typeof writes)[number]) => (...args: unknown[]) => {
    const fn = read()[name]
    return typeof fn === 'function' ? fn(...args) : Promise.resolve()
  }
  return {
    ...actual,
    anchorWatchAt: (watch: unknown) => watch,
    useAnchorWatch: () => record(),
    useAnchorWatchSelector: <T,>(select: (fields: never) => T) => select(record() as never),
    setAnchorHere: write('setAnchorHere'),
    updateAnchorRadius: write('updateRadius'),
    adjustAnchor: write('adjustAnchor'),
    updateRodeAndConditions: write('updateRodeAndConditions'),
    updatePlanningDepth: write('updatePlanningDepth'),
    clearAnchor: write('clearAnchor'),
  }
}
