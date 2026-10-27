// Stable return values for tests that mock the trail hooks. A real reader gets
// the same reference until a trail changes, and the maps memoize on that, so a
// mock must not hand out a fresh array or Map on every call.
const NO_SELF_TRAIL: never[] = []
const NO_AIS_TRAILS = new Map<string, never[]>()

export const trailHooksMock = {
  useSelfTrail: () => NO_SELF_TRAIL,
  useAisTrails: () => NO_AIS_TRAILS,
}
