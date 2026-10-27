import { vi } from 'vitest'

/**
 * A stand-in for '@/hooks/use-vessel-state' in tests that render the app with
 * fixed vessel readings. Every export the app imports reads the same object,
 * filled out with the store's own empty values, so a selector never meets a
 * missing field.
 *
 *   vi.mock('@/hooks/use-vessel-state', async () =>
 *     (await import('./vessel-state-mock')).vesselStateModule(() => ({ depth: 3.2 })))
 *
 * `read` is called on every hook call, so a test can change what it returns
 * between renders. The empty state, gust ladders included, is the store's own
 * INITIAL_VESSEL_STATE, read through importActual so the mock never drifts
 * from the fields the store has.
 */
export async function vesselStateModule(read: () => Record<string, unknown>) {
  const { INITIAL_VESSEL_STATE: EMPTY } = await vi.importActual<typeof import('@/hooks/use-vessel-state')>('@/hooks/use-vessel-state')
  // eslint-disable-next-line @typescript-eslint/no-explicit-any -- a test double: the app's own types check the callers
  const state = (): any => ({ ...EMPTY, ...read() })
  return {
    useVesselStateSelector: <T,>(select: (s: never) => T) => select(state() as never),
    useVesselFields: (fields: readonly string[]) => {
      const s = state()
      return Object.fromEntries(fields.map((f) => [f, s[f]]))
    },
    getVesselStateSnapshot: () => state(),
  }
}
