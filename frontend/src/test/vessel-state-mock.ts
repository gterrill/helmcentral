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
 * between renders.
 */
const EMPTY_LADDER = { '10m': null, '30m': null, '1h': null, '24h': null }

const EMPTY: Record<string, unknown> = {
  depth: null, depthLastUpdateAgeS: null, positionLastUpdateAgeS: null, windLastUpdateAgeS: null,
  vesselLengthOverallM: null, vesselDraftM: null,
  currentDriftKts: null, currentSetDeg: null, currentDriftImpactKts: null,
  navigationState: null, latitude: null, longitude: null,
  gnssQualityIndicator: null, gnssHdop: null, gnssSatellites: null,
  gnssValidationState: null, gnssValidationReason: null, gnssCriticalAlert: false,
  headingTrue: null, speedOverGroundKts: null,
  windSpeedApparentKts: null, windAngleApparentDeg: null, windSide: null, windAngleRelativeDeg: null,
  windSpeedTrueKts: null, windAngleTrueDeg: null, windSideTrue: null, windAngleTrueRelativeDeg: null, windDirectionTrueDeg: null,
  maxGustKts: EMPTY_LADDER, maxGustTrueKts: EMPTY_LADDER,
  generatorState: null, generatorManualStart: false, generatorManualStartTimer: 0,
  generatorRunningByCondition: null, generatorRuntime: null,
  enginesRunning: null, source: null,
}

export function vesselStateModule(read: () => Record<string, unknown>) {
  // eslint-disable-next-line @typescript-eslint/no-explicit-any -- a test double: the app's own types check the callers
  const state = (): any => ({ ...EMPTY, ...read() })
  return {
    useVesselState: () => state(),
    useVesselStateSelector: <T,>(select: (s: never) => T) => select(state() as never),
    useVesselFields: (fields: readonly string[]) => {
      const s = state()
      return Object.fromEntries(fields.map((f) => [f, s[f]]))
    },
    getVesselStateSnapshot: () => state(),
    useRetainVesselState: () => {},
  }
}
