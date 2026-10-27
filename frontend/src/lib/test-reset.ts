/**
 * The registry behind the per-test reset of module-level live state (ADR
 * 0215). It has no import of the live stream, so src/test/setup.ts can load it
 * statically without pulling the stream in ahead of a test file's vi.mock of it.
 */

const resets = new Set<() => void>()

/**
 * Returns every store to its initial state and drops its subscription, so the
 * next reader starts it afresh. Tests only: src/test/setup.ts calls it after
 * each test, because a store otherwise lives as long as the module.
 */
export function resetTelemetryStoresForTest(): void {
  for (const reset of resets) reset()
}

/**
 * Registers module-level live state (a store, the autopilot's pending
 * commands, the shared trail pollers) for the same reset after each test.
 */
export function onTelemetryTestReset(reset: () => void): void {
  resets.add(reset)
}
