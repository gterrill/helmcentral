import { vi } from 'vitest'

/**
 * A stand-in for '@/hooks/use-electrical-state' in tests that render the app
 * with fixed electrical readings, the same shape as vessel-state-mock.ts.
 *
 *   vi.mock('@/hooks/use-electrical-state', async () =>
 *     (await import('./electrical-state-mock')).electricalStateModule(() => ({ batterySocPercent: 90 })))
 *
 * `read` is called on every hook call. The empty state is the store's own
 * INITIAL_ELECTRICAL_STATE, read through importActual so the mock never drifts
 * from the fields the store has.
 */
export async function electricalStateModule(read: () => Record<string, unknown>) {
  const actual = await vi.importActual<typeof import('@/hooks/use-electrical-state')>('@/hooks/use-electrical-state')
  // eslint-disable-next-line @typescript-eslint/no-explicit-any -- a test double: the app's own types check the callers
  const state = (): any => ({ ...actual.INITIAL_ELECTRICAL_STATE, ...read() })
  return {
    ...actual,
    useElectricalSelector: <T,>(select: (s: never) => T) => select(state() as never),
    useElectricalFields: (fields: readonly string[]) => {
      const s = state()
      return Object.fromEntries(fields.map((f) => [f, s[f]]))
    },
  }
}
