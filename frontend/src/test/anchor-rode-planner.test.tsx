import { useState } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { AnchorRodePlanner, type AnchorRodePlannerProps } from '@/components/anchor-rode-planner'
import { SidebarProvider } from '@/components/ui/sidebar'
import type { TideToday } from '@/hooks/use-tide-today'
import { AnchorRequestError } from '@/lib/anchor-request'
import { catenaryMethod, planningFigureM, ratioMethod, rawDepthFromPlanningFigureM, resolvePlanningWindBand, type RodePlanInput } from '@/lib/rode-plan'
import { toast } from 'sonner'

vi.mock('sonner', () => ({ toast: { error: vi.fn() } }))

const tide: TideToday = {
  datetime: new Date(0).toISOString(),
  current_tide_height_ft: 2,
  tide_direction: 'Rising',
  high_tide_time: new Date(0).toISOString(),
  high_tide_height_ft: 5,
  low_tide_time: new Date(0).toISOString(),
  low_tide_height_ft: 0.5,
  station_name: 'Test Station',
  provider: 'test',
}

function baseProps(overrides: Partial<AnchorRodePlannerProps> = {}): AnchorRodePlannerProps {
  return {
    anchorState: 'set',
    rodeDeployedM: 0,
    seaState: 'calm',
    seabedType: 'sand',
    depthM: 5,
    // anchorState is 'set' above, so resolvePlanningDepth prefers this over
    // depthM once the Depth cell wires up (ADR 0063) — same value as depthM
    // so every pre-existing expected figure in this file survives unchanged.
    planningDepthM: 5,
    planningTideHeightFt: 2,
    onPlanningDepthChange: vi.fn().mockResolvedValue(undefined),
    windSpeedApparentKts: 12,
    maxGustKts: { '10m': null, '30m': null, '1h': 20, '24h': null },
    tide,
    isImperial: false,
    anchorConfig: {
      bowRollerHeightM: 1,
      chainSizeMm: 10,
      chainOnboardM: 50,
      hullType: 'power_mono',
      scopeMethod: 'ratio',
      windageAreaM2: 20,
      gpsFromBowM: 2,
      loaM: 12,
    },
    bowOffsetM: 2,
    vesselLengthOverallM: null,
    windBandId: null,
    onWindBandChange: vi.fn(),
    onUpdateRodeAndConditions: vi.fn().mockResolvedValue(undefined),
    onApplyAlarmRadius: vi.fn().mockResolvedValue(undefined),
    ...overrides,
  }
}

// Renders inside the app's real SidebarProvider — AnchorRodePlanner must not
// nest a second one (ADR 0047 §2), so the test harness mirrors App.tsx's
// actual mount point instead of stubbing the provider away.
function renderPlanner(overrides: Partial<AnchorRodePlannerProps> = {}) {
  return render(
    <SidebarProvider>
      <AnchorRodePlanner {...baseProps(overrides)} />
    </SidebarProvider>,
  )
}

// The two rode methods share one tab strip, so only the selected method's
// panel is mounted at a time — every per-method assertion scopes to the live
// panel rather than to a sidebar group that no longer exists.
function methodPanel(): HTMLElement {
  return screen.getByRole('tabpanel')
}

function selectMethodTab(name: RegExp): HTMLElement {
  fireEvent.click(screen.getByRole('tab', { name }))
  return methodPanel()
}

// Mirrors exactly what the component's own planInput builds from
// baseProps() — depth 5m sounder + tide, 1h gust of 20kt (which seeds the
// 20-25 band, planning at its top of 25kt via resolvePlanningWindBand),
// calm/sand, chain 10mm, windage 20m2, power_mono, bow roller 1m — so the
// expected figures come from rode-plan.ts itself, not a hand-picked number.
const bandedWindKts = resolvePlanningWindBand(
  { '10m': null, '30m': null, '1h': 20, '24h': null },
  12,
  null,
)!.planKts

const expectedPlanInput: RodePlanInput = {
  // baseProps() has anchorState: 'set' with planningDepthM: 5, planningTideHeightFt: 2
  // — resolvePlanningDepth (ADR 0063) resolves that to this same datum.
  depth: { depthM: 5, tideHeightFt: 2 },
  isAnchored: true,
  bowRollerHeightM: 1,
  tide,
  windKts: bandedWindKts,
  seaState: 'calm',
  seabedType: 'sand',
  chainSizeMm: 10,
  chainOnboardM: 50,
  windageAreaM2: 20,
  hullType: 'power_mono',
}

beforeEach(() => {
  localStorage.clear()
})

afterEach(() => {
  vi.useRealTimers()
})

describe('AnchorRodePlanner — collapse state', () => {
  it('is collapsed by default', () => {
    renderPlanner()
    expect(screen.getByRole('button', { name: /expand rode planner/i })).toBeInTheDocument()
    expect(screen.queryByText('Rode Planner')).toBeNull()
  })

  it('expands when the collapsed rail is clicked', () => {
    renderPlanner()
    fireEvent.click(screen.getByRole('button', { name: /expand rode planner/i }))
    expect(screen.getByText('Rode Planner')).toBeInTheDocument()
  })

  it('persists open state across a remount via localStorage', () => {
    const { unmount } = renderPlanner()
    fireEvent.click(screen.getByRole('button', { name: /expand rode planner/i }))
    expect(screen.getByText('Rode Planner')).toBeInTheDocument()
    unmount()

    renderPlanner()
    expect(screen.getByText('Rode Planner')).toBeInTheDocument()
  })
})

describe('AnchorRodePlanner — no false-positive scope badge (regression guard)', () => {
  it('renders no "Scope Insufficient" badge when rodeDeployedM is 0', () => {
    renderPlanner({ rodeDeployedM: 0 })
    fireEvent.click(screen.getByRole('button', { name: /expand rode planner/i }))

    expect(screen.queryByText(/scope insufficient/i)).toBeNull()
    expect(screen.queryByText(/scope unknown/i)).toBeNull()
    expect(screen.queryByText(/scope ok/i)).toBeNull()
    expect(screen.queryByText(/scope low/i)).toBeNull()
  })

  it('shows the badge once a deployed rode is entered', () => {
    renderPlanner({ rodeDeployedM: 40 })
    fireEvent.click(screen.getByRole('button', { name: /expand rode planner/i }))

    const badges = [
      screen.queryByText(/scope insufficient/i),
      screen.queryByText(/scope unknown/i),
      screen.queryByText(/scope ok/i),
      screen.queryByText(/scope low/i),
    ].filter(Boolean)
    expect(badges.length).toBe(1)
  })
})

// Apply used to always use the catenary plan's swing regardless of the
// operator's anchor.scope_method setting — actively misleading now that both
// method groups show their own Swing figure. Apply must use the swing of
// whichever method is configured and refuse rather than substitute the
// other method's figure (ADR 0059 §4, ADR 0047).
describe('AnchorRodePlanner — apply as alarm radius follows the configured method', () => {
  it('applies the ratio-method swing when anchor.scope_method is "ratio"', async () => {
    const onApplyAlarmRadius = vi.fn().mockResolvedValue(undefined)
    const props = baseProps()
    renderPlanner({ onApplyAlarmRadius, anchorConfig: { ...props.anchorConfig, scopeMethod: 'ratio' } })
    fireEvent.click(screen.getByRole('button', { name: /expand rode planner/i }))

    fireEvent.click(screen.getByRole('button', { name: /apply as alarm radius/i }))

    await waitFor(() => expect(onApplyAlarmRadius).toHaveBeenCalledTimes(1))
    const expected = ratioMethod(expectedPlanInput)!.recommendedRodeM + props.bowOffsetM + props.anchorConfig.loaM
    expect(onApplyAlarmRadius.mock.calls[0][0]).toBeCloseTo(expected, 6)
  })

  it('applies the catenary-method swing when anchor.scope_method is "catenary"', async () => {
    const onApplyAlarmRadius = vi.fn().mockResolvedValue(undefined)
    const props = baseProps()
    renderPlanner({ onApplyAlarmRadius, anchorConfig: { ...props.anchorConfig, scopeMethod: 'catenary' } })
    fireEvent.click(screen.getByRole('button', { name: /expand rode planner/i }))

    fireEvent.click(screen.getByRole('button', { name: /apply as alarm radius/i }))

    await waitFor(() => expect(onApplyAlarmRadius).toHaveBeenCalledTimes(1))
    const expected = catenaryMethod(expectedPlanInput)!.recommendedRodeM + props.bowOffsetM + props.anchorConfig.loaM
    expect(onApplyAlarmRadius.mock.calls[0][0]).toBeCloseTo(expected, 6)
  })

  it('names the configured method and its figure in a caption above the button', () => {
    const props = baseProps()
    renderPlanner({ anchorConfig: { ...props.anchorConfig, scopeMethod: 'ratio' } })
    fireEvent.click(screen.getByRole('button', { name: /expand rode planner/i }))

    const expectedRodeM = ratioMethod(expectedPlanInput)!.recommendedRodeM
    const expectedRadius = Math.round(expectedRodeM + props.bowOffsetM + props.anchorConfig.loaM)
    expect(screen.getByText(`Applies the Ratio Method swing, ${expectedRadius} m`)).toBeInTheDocument()
  })

  it('disables Apply and names the reason when the configured method itself is unavailable, without falling back to the other method', () => {
    const onApplyAlarmRadius = vi.fn().mockResolvedValue(undefined)
    const props = baseProps()
    // windageAreaM2: 0 knocks out only the catenary method — ratioMethod
    // never reads it, so the ratio group still computes fine. Apply must stay
    // disabled anyway, never silently borrowing the ratio figure it isn't
    // showing as the configured one.
    renderPlanner({
      onApplyAlarmRadius,
      anchorConfig: { ...props.anchorConfig, scopeMethod: 'catenary', windageAreaM2: 0 },
    })
    fireEvent.click(screen.getByRole('button', { name: /expand rode planner/i }))

    expect(screen.getByText(/Catenary Method unavailable —/i)).toBeInTheDocument()

    const apply = screen.getByRole('button', { name: /apply as alarm radius/i })
    expect(apply).toBeDisabled()
    fireEvent.click(apply)
    expect(onApplyAlarmRadius).not.toHaveBeenCalled()
  })
})

describe('AnchorRodePlanner — disabled with no active watch', () => {
  it('disables record-deployed-rode and apply-as-alarm-radius, with a visible reason', () => {
    renderPlanner({ anchorState: 'none' })
    fireEvent.click(screen.getByRole('button', { name: /expand rode planner/i }))

    const rodeInput = screen.getByLabelText(/rode deployed/i)
    expect(rodeInput).toBeDisabled()

    const applyButton = screen.getByRole('button', { name: /apply as alarm radius/i })
    expect(applyButton).toBeDisabled()

    expect(screen.getAllByText(/set anchor watch to/i).length).toBeGreaterThan(0)
  })
})

describe('AnchorRodePlanner — sea-state persistence guard (no masking fallback)', () => {
  beforeEach(() => {
    vi.useFakeTimers()
  })

  it('makes no PATCH call when no watch is active', () => {
    const onUpdateRodeAndConditions = vi.fn().mockResolvedValue(undefined)
    renderPlanner({ anchorState: 'none', onUpdateRodeAndConditions })
    fireEvent.click(screen.getByRole('button', { name: /expand rode planner/i }))

    fireEvent.change(screen.getByLabelText(/sea state/i), { target: { value: 'storm' } })
    vi.advanceTimersByTime(2000)

    expect(onUpdateRodeAndConditions).not.toHaveBeenCalled()
  })

  it('PATCHes via onUpdateRodeAndConditions when a watch is active', () => {
    const onUpdateRodeAndConditions = vi.fn().mockResolvedValue(undefined)
    renderPlanner({ anchorState: 'set', onUpdateRodeAndConditions })
    fireEvent.click(screen.getByRole('button', { name: /expand rode planner/i }))

    fireEvent.change(screen.getByLabelText(/sea state/i), { target: { value: 'storm' } })
    vi.advanceTimersByTime(2000)

    expect(onUpdateRodeAndConditions).toHaveBeenCalledTimes(1)
    expect(onUpdateRodeAndConditions.mock.calls[0][1]).toBe('storm')
  })
})

// code-review finding: a failed PATCH used to leave the rejected sea
// state/seabed/rode (or, for the Depth field below, the rejected typed
// figure) on screen looking saved, since the toast fired but nothing put the
// input back to what the server actually holds.
describe('AnchorRodePlanner — reverts local state on a failed save', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    vi.mocked(toast.error).mockClear()
  })

  it('reverts sea state to the server value and shows the failure, with Retry for a retryable error', async () => {
    const onUpdateRodeAndConditions = vi.fn().mockRejectedValue(new Error('Connection lost'))
    renderPlanner({ anchorState: 'set', seaState: 'calm', onUpdateRodeAndConditions })
    fireEvent.click(screen.getByRole('button', { name: /expand rode planner/i }))

    fireEvent.change(screen.getByLabelText(/sea state/i), { target: { value: 'storm' } })
    expect(screen.getByLabelText(/sea state/i)).toHaveValue('storm')

    await act(async () => { await vi.advanceTimersByTimeAsync(800) })

    expect(screen.getByLabelText(/sea state/i)).toHaveValue('calm')
    expect(toast.error).toHaveBeenCalledWith('Could not save rode and conditions', {
      description: 'Connection lost',
      action: { label: 'Retry', onClick: expect.any(Function) },
    })
  })

  it('reverts seabed to the server value and offers no Retry for a non-retryable (4xx) error', async () => {
    const onUpdateRodeAndConditions = vi.fn().mockRejectedValue(new AnchorRequestError('no active anchor watch', 404))
    renderPlanner({ anchorState: 'set', seabedType: 'sand', onUpdateRodeAndConditions })
    fireEvent.click(screen.getByRole('button', { name: /expand rode planner/i }))

    fireEvent.change(screen.getByLabelText(/seabed/i), { target: { value: 'rock' } })
    await act(async () => { await vi.advanceTimersByTimeAsync(800) })

    expect(screen.getByLabelText(/seabed/i)).toHaveValue('sand')
    expect(toast.error).toHaveBeenCalledWith('Could not save rode and conditions', {
      description: 'no active anchor watch',
      action: undefined,
    })
  })

  it('Retry re-sends the same rejected values immediately, without waiting out another debounce', async () => {
    const onUpdateRodeAndConditions = vi.fn()
      .mockRejectedValueOnce(new Error('Connection lost'))
      .mockResolvedValueOnce(undefined)
    renderPlanner({ anchorState: 'set', seaState: 'calm', onUpdateRodeAndConditions })
    fireEvent.click(screen.getByRole('button', { name: /expand rode planner/i }))

    fireEvent.change(screen.getByLabelText(/sea state/i), { target: { value: 'storm' } })
    await act(async () => { await vi.advanceTimersByTimeAsync(800) })
    expect(onUpdateRodeAndConditions).toHaveBeenCalledTimes(1)

    const [, options] = vi.mocked(toast.error).mock.calls[0]
    const action = (options as unknown as { action: { onClick: () => void } }).action
    action.onClick()

    expect(onUpdateRodeAndConditions).toHaveBeenCalledTimes(2)
    expect(onUpdateRodeAndConditions.mock.calls[1][1]).toBe('storm')
  })

  it('reverts the Depth input to the server-resolved figure on a failed PATCH, with Retry', async () => {
    const onPlanningDepthChange = vi.fn().mockRejectedValue(new Error('Connection lost'))
    renderPlanner({ anchorState: 'set', planningDepthM: 5, planningTideHeightFt: 2, onPlanningDepthChange })
    fireEvent.click(screen.getByRole('button', { name: /expand rode planner/i }))

    const before = screen.getByLabelText(/depth/i).getAttribute('value')
    fireEvent.change(screen.getByLabelText(/depth/i), { target: { value: '9.9' } })
    expect(screen.getByLabelText(/depth/i)).toHaveValue(9.9)

    await act(async () => { await vi.advanceTimersByTimeAsync(800) })

    expect(screen.getByLabelText(/depth/i)).toHaveValue(Number(before))
    expect(toast.error).toHaveBeenCalledWith('Could not save planning depth', {
      description: 'Connection lost',
      action: { label: 'Retry', onClick: expect.any(Function) },
    })
  })
})

describe('AnchorRodePlanner — does not collide with the app left nav', () => {
  it('toggling the planner leaves localStorage["sidebar.open"] untouched', () => {
    localStorage.setItem('sidebar.open', 'true')
    renderPlanner()

    fireEvent.click(screen.getByRole('button', { name: /expand rode planner/i }))
    expect(localStorage.getItem('sidebar.open')).toBe('true')

    fireEvent.click(screen.getByRole('button', { name: /collapse rode planner/i }))
    expect(localStorage.getItem('sidebar.open')).toBe('true')
  })

  it('a Cmd/Ctrl+B keydown does not change the planner open state', () => {
    renderPlanner()
    expect(screen.queryByText('Rode Planner')).toBeNull()

    fireEvent.keyDown(window, { key: 'b', metaKey: true })

    expect(screen.queryByText('Rode Planner')).toBeNull()
  })
})

// A boat's swing circle is rode + bow offset + the hull's own length. loaM
// defaults to 0 (config/app-config.ts) and is genuinely unset on real installs,
// which would make the planner report a swing radius smaller than the boat and
// let "Apply as alarm radius" *shrink* an existing, correct alarm circle. Per
// the repo's fallback policy that must surface, not silently compute with zero.
describe('AnchorRodePlanner — LOA not configured', () => {
  it('warns and refuses to apply an alarm radius when loaM is 0', () => {
    const onApplyAlarmRadius = vi.fn().mockResolvedValue(undefined)
    const props = baseProps()
    renderPlanner({
      onApplyAlarmRadius,
      anchorConfig: { ...props.anchorConfig, loaM: 0 },
    })
    fireEvent.click(screen.getByRole('button', { name: /expand rode planner/i }))

    expect(screen.getByText(/boat length/i)).toBeInTheDocument()

    const apply = screen.getByRole('button', { name: /apply as alarm radius/i })
    expect(apply).toBeDisabled()
    fireEvent.click(apply)
    expect(onApplyAlarmRadius).not.toHaveBeenCalled()
  })

  it('does not show the swing radius as an authoritative number when loaM is 0', () => {
    const props = baseProps()
    renderPlanner({ anchorConfig: { ...props.anchorConfig, loaM: 0 } })
    fireEvent.click(screen.getByRole('button', { name: /expand rode planner/i }))

    expect(screen.queryByText(/^Swing \d/)).toBeNull()
  })

  it('still warns and disables when SignalK also has no LOA', () => {
    const onApplyAlarmRadius = vi.fn().mockResolvedValue(undefined)
    const props = baseProps()
    renderPlanner({
      onApplyAlarmRadius,
      anchorConfig: { ...props.anchorConfig, loaM: 0 },
      vesselLengthOverallM: null,
    })
    fireEvent.click(screen.getByRole('button', { name: /expand rode planner/i }))

    expect(screen.getByText(/boat length/i)).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /apply as alarm radius/i })).toBeDisabled()
    expect(screen.queryByText(/^Swing \d/)).toBeNull()
  })
})

// LOA can now also come from SignalK's design.length.overall (ADR 0047), since
// gpsFromBowM-style manual-only handling isn't appropriate here: unlike
// gps_from_bow_m, LOA doesn't depend on which sensor/antenna published it.
// Settings remains an explicit operator override and must still win when set.
describe('AnchorRodePlanner — LOA source precedence (settings vs SignalK)', () => {
  it('prefers settings LOA over SignalK when both are present', () => {
    const props = baseProps()
    renderPlanner({
      anchorConfig: { ...props.anchorConfig, loaM: 12 },
      vesselLengthOverallM: 17.9,
    })
    fireEvent.click(screen.getByRole('button', { name: /expand rode planner/i }))

    expect(screen.getByText(/LOA 12 m from settings/i)).toBeInTheDocument()
    expect(screen.queryByText(/from signalk/i)).toBeNull()
  })

  it('falls back to SignalK LOA when settings LOA is 0', () => {
    const props = baseProps()
    renderPlanner({
      anchorConfig: { ...props.anchorConfig, loaM: 0 },
      vesselLengthOverallM: 17.9,
    })
    fireEvent.click(screen.getByRole('button', { name: /expand rode planner/i }))

    expect(screen.getByText(/LOA 17\.9 m from SignalK/i)).toBeInTheDocument()
    expect(screen.queryByText(/from settings/i)).toBeNull()
  })

  it('shows a swing radius and enables Apply as alarm radius using SignalK-only LOA', async () => {
    const onApplyAlarmRadius = vi.fn().mockResolvedValue(undefined)
    const props = baseProps()
    renderPlanner({
      onApplyAlarmRadius,
      anchorConfig: { ...props.anchorConfig, loaM: 0 },
      vesselLengthOverallM: 17.9,
      bowOffsetM: 2,
    })
    fireEvent.click(screen.getByRole('button', { name: /expand rode planner/i }))

    // Scoped to the catenary panel: every method shows its own Swing figure
    // (see "Swing on both method tabs" below), and only the selected one is
    // mounted, so the tab has to be picked before the figure exists at all.
    const catenaryPanel = selectMethodTab(/catenary/i)
    expect(within(catenaryPanel).getByText(/^Swing \d/)).toBeInTheDocument()
    expect(screen.queryByText(/boat length/i)).toBeNull()

    const apply = screen.getByRole('button', { name: /apply as alarm radius/i })
    expect(apply).not.toBeDisabled()
    fireEvent.click(apply)

    await waitFor(() => expect(onApplyAlarmRadius).toHaveBeenCalledTimes(1))
    const [radiusMeters] = onApplyAlarmRadius.mock.calls[0]
    expect(radiusMeters).toBeGreaterThan(2 + 17.9) // includes bow offset + SignalK LOA at minimum
  })
})

// ADR 0047 §2a: the planner renders one entry per lib/rode-plan.ts RodeMethod
// result — today the catenary method and the ratio method. They are tabs, not
// two stacked groups: in a 20rem sidebar two full recommendations pushed the
// deployed-rode input and Apply below the fold. The method the operator
// configured (anchor.scope_method) leads and opens first, so the panel starts
// on the figure Apply will actually use.
describe('AnchorRodePlanner — rode methods are tabs', () => {
  it('renders one tab per method and mounts only the selected panel', () => {
    renderPlanner()
    fireEvent.click(screen.getByRole('button', { name: /expand rode planner/i }))

    expect(screen.getAllByRole('tab')).toHaveLength(2)
    expect(screen.getAllByRole('tabpanel')).toHaveLength(1)
    // The single strongest signal that the two recommendations no longer
    // stack: exactly one Pay Out figure is on screen at a time.
    expect(screen.getAllByText('Pay Out')).toHaveLength(1)
  })

  it('leads with the configured method and opens on it — ratio', () => {
    const props = baseProps()
    renderPlanner({ anchorConfig: { ...props.anchorConfig, scopeMethod: 'ratio' } })
    fireEvent.click(screen.getByRole('button', { name: /expand rode planner/i }))

    const [first, second] = screen.getAllByRole('tab')
    expect(first).toHaveTextContent('Ratio Method')
    expect(second).toHaveTextContent('Catenary Method')
    expect(first).toHaveAttribute('aria-selected', 'true')

    const expected = ratioMethod(expectedPlanInput)!
    expect(within(methodPanel()).getByText(`Scope ${expected.scopeRatio.toFixed(1)}:1`)).toBeInTheDocument()
  })

  it('leads with the configured method and opens on it — catenary', () => {
    const props = baseProps()
    renderPlanner({ anchorConfig: { ...props.anchorConfig, scopeMethod: 'catenary' } })
    fireEvent.click(screen.getByRole('button', { name: /expand rode planner/i }))

    const [first, second] = screen.getAllByRole('tab')
    expect(first).toHaveTextContent('Catenary Method')
    expect(second).toHaveTextContent('Ratio Method')
    expect(first).toHaveAttribute('aria-selected', 'true')

    const expected = catenaryMethod(expectedPlanInput)!
    expect(within(methodPanel()).getByText(`Scope ${expected.scopeRatio.toFixed(1)}:1`)).toBeInTheDocument()
  })

  it('swaps the panel to the other method when its tab is picked', () => {
    renderPlanner()
    fireEvent.click(screen.getByRole('button', { name: /expand rode planner/i }))

    const ratio = ratioMethod(expectedPlanInput)!
    const catenary = catenaryMethod(expectedPlanInput)!
    // The whole point of showing both: at these conditions they disagree.
    expect(catenary.scopeRatio.toFixed(1)).not.toBe(ratio.scopeRatio.toFixed(1))

    expect(within(methodPanel()).getByText(`Scope ${ratio.scopeRatio.toFixed(1)}:1`)).toBeInTheDocument()

    const catenaryPanel = selectMethodTab(/catenary/i)
    expect(within(catenaryPanel).getByText(`Scope ${catenary.scopeRatio.toFixed(1)}:1`)).toBeInTheDocument()
    expect(within(catenaryPanel).queryByText(`Scope ${ratio.scopeRatio.toFixed(1)}:1`)).toBeNull()
  })

  it('follows a later settings change of the method onto that tab', () => {
    const props = baseProps()
    const { rerender } = render(
      <SidebarProvider>
        <AnchorRodePlanner {...baseProps({ anchorConfig: { ...props.anchorConfig, scopeMethod: 'ratio' } })} />
      </SidebarProvider>,
    )
    fireEvent.click(screen.getByRole('button', { name: /expand rode planner/i }))
    expect(screen.getAllByRole('tab')[0]).toHaveTextContent('Ratio Method')

    rerender(
      <SidebarProvider>
        <AnchorRodePlanner {...baseProps({ anchorConfig: { ...props.anchorConfig, scopeMethod: 'catenary' } })} />
      </SidebarProvider>,
    )

    const [first] = screen.getAllByRole('tab')
    expect(first).toHaveTextContent('Catenary Method')
    expect(first).toHaveAttribute('aria-selected', 'true')
    const expected = catenaryMethod(expectedPlanInput)!
    expect(within(methodPanel()).getByText(`Scope ${expected.scopeRatio.toFixed(1)}:1`)).toBeInTheDocument()
  })

  it('keeps the methods independent — a catenary-only missing input still lets the ratio method compute', () => {
    const props = baseProps()
    renderPlanner({ anchorConfig: { ...props.anchorConfig, windageAreaM2: 0 } })
    fireEvent.click(screen.getByRole('button', { name: /expand rode planner/i }))

    // scopeMethod is 'ratio' in baseProps, so the ratio tab opens first.
    expect(within(methodPanel()).getByText('Pay Out')).toBeInTheDocument()
    expect(within(methodPanel()).queryByText(/unavailable —/i)).toBeNull()

    const catenaryPanel = selectMethodTab(/catenary/i)
    expect(within(catenaryPanel).getByText(/unavailable —/i)).toBeInTheDocument()
  })

  it('does not duplicate the LOA provenance line into a method panel', () => {
    renderPlanner()
    fireEvent.click(screen.getByRole('button', { name: /expand rode planner/i }))

    // LOA provenance is a shared-input fact, not a per-method one (see the
    // "shared LOA input lives in the footer" describe block below).
    expect(within(methodPanel()).queryByText(/from settings/i)).toBeNull()
    expect(within(selectMethodTab(/catenary/i)).queryByText(/from settings/i)).toBeNull()
  })
})

// LOA describes a single shared input (bow offset + hull length), not
// anything specific to a method, so its provenance line and its warning are
// stated once in SidebarFooter, directly above Apply — the control they
// gate — rather than repeated inside every method panel (ADR 0059 §4).
describe('AnchorRodePlanner — shared LOA input lives in the footer', () => {
  it('renders the LOA provenance line once, in the footer, not inside either method panel', () => {
    renderPlanner()
    fireEvent.click(screen.getByRole('button', { name: /expand rode planner/i }))

    expect(screen.getAllByText(/LOA .* from settings/i)).toHaveLength(1)

    expect(within(methodPanel()).queryByText(/from settings/i)).toBeNull()
    expect(within(selectMethodTab(/catenary/i)).queryByText(/from settings/i)).toBeNull()
  })

  it('renders the LOA warning once, in the footer, not inside either method panel', () => {
    const props = baseProps()
    renderPlanner({ anchorConfig: { ...props.anchorConfig, loaM: 0 }, vesselLengthOverallM: null })
    fireEvent.click(screen.getByRole('button', { name: /expand rode planner/i }))

    expect(screen.getAllByText(/boat length/i)).toHaveLength(1)

    expect(within(methodPanel()).queryByText(/boat length/i)).toBeNull()
    expect(within(selectMethodTab(/catenary/i)).queryByText(/boat length/i)).toBeNull()
  })
})

// Swing (rode + bow offset + LOA) used to be bound to the catenary plan only.
// Each method panel now shows its own swing figure, computed from that
// method's own recommended rode, so the ratio panel's circle isn't silently
// missing or borrowed from catenary's.
describe('AnchorRodePlanner — Swing on both method tabs', () => {
  it('shows a Swing figure in the Ratio Method panel, computed from its own recommended rode', () => {
    renderPlanner()
    fireEvent.click(screen.getByRole('button', { name: /expand rode planner/i }))

    const ratioPanel = selectMethodTab(/ratio/i)
    expect(within(ratioPanel).getByText(/^Swing \d/)).toBeInTheDocument()
  })

  it('shows different Swing figures per tab when the two methods recommend different rode', () => {
    renderPlanner()
    fireEvent.click(screen.getByRole('button', { name: /expand rode planner/i }))

    const ratioSwing = within(selectMethodTab(/ratio/i)).getByText(/^Swing \d/).textContent
    const catenarySwing = within(selectMethodTab(/catenary/i)).getByText(/^Swing \d/).textContent
    expect(catenarySwing).not.toBe(ratioSwing)
  })

  it('shows Swing in neither panel when LOA is unresolved', () => {
    const props = baseProps()
    renderPlanner({
      anchorConfig: { ...props.anchorConfig, loaM: 0 },
      vesselLengthOverallM: null,
    })
    fireEvent.click(screen.getByRole('button', { name: /expand rode planner/i }))

    expect(within(methodPanel()).queryByText(/^Swing \d/)).toBeNull()
    expect(within(selectMethodTab(/catenary/i)).queryByText(/^Swing \d/)).toBeNull()
  })

  it('warns in whichever method panel its own recommendation exceeds chain aboard — chain-onboard is per-method, not catenary-only', () => {
    // At these defaults (baseProps): catenary recommends ~40.3m, ratio ~48.4m
    // (the 1h gust of 20kt seeds the 20-25 band, which plans at 25kt and
    // clears the ratio method's 7:1 threshold). 45m aboard is short for
    // ratio but not for catenary — a real gap the old catenary-only gating
    // missed entirely, since the ratio method can recommend more chain than
    // the boat carries and used to say nothing about it.
    const props = baseProps()
    renderPlanner({ anchorConfig: { ...props.anchorConfig, chainOnboardM: 45 } })
    fireEvent.click(screen.getByRole('button', { name: /expand rode planner/i }))

    expect(within(selectMethodTab(/ratio/i)).getByText(/only .* aboard/i)).toBeInTheDocument()
    expect(within(selectMethodTab(/catenary/i)).queryByText(/only .* aboard/i)).toBeNull()
  })
})

describe('AnchorRodePlanner — forecast wind band', () => {
  // The select is now a controlled input — App.tsx owns windBandId so the
  // tile and drawer can share it — so driving it here needs a small
  // stateful host that plays App.tsx's part: hold the band and feed the
  // operator's choice back in as a prop, same round trip the real app does.
  function ControlledPlanner(overrides: Partial<AnchorRodePlannerProps>) {
    const [windBandId, setWindBandId] = useState<string | null>(null)
    return (
      <SidebarProvider>
        <AnchorRodePlanner {...baseProps({ ...overrides, windBandId, onWindBandChange: setWindBandId })} />
      </SidebarProvider>
    )
  }

  function expandPlanner(overrides: Partial<AnchorRodePlannerProps> = {}) {
    render(<ControlledPlanner {...overrides} />)
    fireEvent.click(screen.getByRole('button', { name: /expand rode planner/i }))
    return screen.getByLabelText('Forecast wind') as HTMLSelectElement
  }

  it('preselects the band containing the 1h gust', () => {
    const select = expandPlanner({ maxGustKts: { '10m': null, '30m': null, '1h': 12.3, '24h': null } })
    expect(select.value).toBe('10-15')
  })

  it('falls back to apparent wind for the seed when there is no gust reading', () => {
    const select = expandPlanner({
      maxGustKts: { '10m': null, '30m': null, '1h': null, '24h': null },
      windSpeedApparentKts: 27,
    })
    expect(select.value).toBe('25-30')
  })

  it('drops the seed-provenance helper text', () => {
    expandPlanner()
    expect(screen.queryByText(/seeded from/i)).toBeNull()
  })

  it('plans at the top of the chosen band, so switching band changes the recommendation', () => {
    const select = expandPlanner({ maxGustKts: { '10m': null, '30m': null, '1h': 5, '24h': null } })
    expect(select.value).toBe('0-10')

    // baseProps' scopeMethod is 'ratio', so the ratio tab is the open one.
    // 0-10 plans at 10 kts -> under the 20kt threshold -> 5:1 on 6m hawse = 30m.
    expect(within(methodPanel()).getByText(/Scope 5.0:1/)).toBeInTheDocument()

    fireEvent.change(select, { target: { value: '20-25' } })
    // 20-25 plans at 25 kts -> at/over the threshold -> 7:1 = 42m.
    expect(within(methodPanel()).getByText(/Scope 7.0:1/)).toBeInTheDocument()
  })

  it('offers no band and reports no wind data when nothing is reading', () => {
    const select = expandPlanner({
      maxGustKts: { '10m': null, '30m': null, '1h': null, '24h': null },
      windSpeedApparentKts: null,
    })
    expect(select.value).toBe('')
    expect(screen.getAllByText(/no wind data/i).length).toBeGreaterThan(0)
  })
})

// ADR 0063 (amended): the Depth cell is a real, editable input, seeded with
// the figure the plan is actually computed against — depth at the next high
// tide plus bow roller height (planningFigureM) — not the raw sounder/
// recorded reading. The highest-consequence trap here is double tide
// correction on override: these tests pin that typing over the corrected
// figure persists the *raw* depth that figure implies
// (rawDepthFromPlanningFigureM), not the typed figure itself, which is what
// avoids feeding the rise back in and compounding it on every render.
describe('AnchorRodePlanner — Depth cell', () => {
  function depthInput() {
    return screen.getByLabelText('Depth') as HTMLInputElement
  }

  it('is a real, editable input', () => {
    renderPlanner()
    fireEvent.click(screen.getByRole('button', { name: /expand rode planner/i }))

    expect(depthInput()).toBeInTheDocument()
    expect(depthInput()).not.toBeDisabled()
  })

  it('shows the tide+bow corrected planning figure computed from the recorded planning depth, not the live sounder, while anchored', () => {
    renderPlanner({ depthM: 99, planningDepthM: 5, planningTideHeightFt: 2 })
    fireEvent.click(screen.getByRole('button', { name: /expand rode planner/i }))

    // datum {depthM: 5, tideHeightFt: 2}, tide (high 5) -> rise 3ft/3.28084,
    // + bowRollerHeightM 1 from baseProps' anchorConfig.
    const expected = planningFigureM({ depthM: 5, tideHeightFt: 2 }, tide, 1)!
    expect(depthInput().value).toBe(expected.toFixed(1))
    expect(depthInput().value).toBe('6.9')
  })

  it('renders an empty input and names the reason when anchored with nothing recorded — never a live-depth fallback', () => {
    renderPlanner({ depthM: 42, planningDepthM: null, planningTideHeightFt: null })
    fireEvent.click(screen.getByRole('button', { name: /expand rode planner/i }))

    expect(depthInput().value).toBe('')
    // Exact string, not a loose regex: the open method panel also shows
    // "Unavailable — no depth entered" for the same reason, so a looser
    // match would find two hits instead of the Depth cell's own caption.
    // Names the figure the field expects: typing the bare sounder reading
    // here would have bow height and tide rise subtracted from it.
    expect(screen.getByText('No depth entered, type depth at high tide + 1.0 m bow')).toBeInTheDocument()
  })

  it('names the sounder-plus-bow figure when anchored with nothing recorded and no tide station', () => {
    renderPlanner({ depthM: 42, planningDepthM: null, planningTideHeightFt: null, tide: { ...tide, current_tide_height_ft: -1, high_tide_height_ft: -1 } })
    fireEvent.click(screen.getByRole('button', { name: /expand rode planner/i }))

    expect(screen.getByText('No depth entered, type depth + 1.0 m bow')).toBeInTheDocument()
  })

  // The double-correction guard: baseProps' tide has current_tide_height_ft 2,
  // high_tide_height_ft 5 -> a 3ft rise -> 3/3.28084 = 0.914m corrected onto the
  // 5m raw reading, plus the 1m bow roller height. The input shows that
  // corrected 6.9 figure, and the caption names both the correction and the
  // bow height baked into it.
  it('holds the tide- and bow-corrected figure in the input, and the caption names both', () => {
    renderPlanner()
    fireEvent.click(screen.getByRole('button', { name: /expand rode planner/i }))

    expect(depthInput().value).toBe('6.9')
    expect(screen.getByText('High tide + 1.0 m bow')).toBeInTheDocument()
  })

  it('shows the sounder-plus-bow caption when the datum carries no tide stamp', () => {
    renderPlanner({ planningDepthM: 5, planningTideHeightFt: null })
    fireEvent.click(screen.getByRole('button', { name: /expand rode planner/i }))

    // No tide stamp on the datum -> maxExpectedDepthM is null -> figure is
    // just the raw 5 + bow 1 = 6.
    expect(depthInput().value).toBe('6.0')
    expect(screen.getByText('Sounder + 1.0 m bow, no tide station')).toBeInTheDocument()
  })

  describe('persistence', () => {
    beforeEach(() => {
      vi.useFakeTimers()
    })

    it('debounces the change to metres, 800ms, when a watch is anchored, persisting the raw depth the typed figure implies', () => {
      const onPlanningDepthChange = vi.fn().mockResolvedValue(undefined)
      renderPlanner({ onPlanningDepthChange })
      fireEvent.click(screen.getByRole('button', { name: /expand rode planner/i }))

      fireEvent.change(depthInput(), { target: { value: '8' } })
      expect(onPlanningDepthChange).not.toHaveBeenCalled()

      vi.advanceTimersByTime(800)
      // The typed "8" is the corrected figure (high tide + bow height), not
      // the raw depth — rawDepthFromPlanningFigureM inverts it back to what
      // gets persisted, stamped with the tide right now (2).
      const expectedRawDepthM = rawDepthFromPlanningFigureM(8, 2, tide, 1)!
      expect(onPlanningDepthChange).toHaveBeenCalledTimes(1)
      const [depthMArg, tideArg] = onPlanningDepthChange.mock.calls[0]
      expect(depthMArg).toBeCloseTo(expectedRawDepthM, 6)
      expect(tideArg).toBe(2)
    })

    it('converts a typed feet value to metres under imperial, then inverts to the raw depth before persisting', () => {
      const onPlanningDepthChange = vi.fn().mockResolvedValue(undefined)
      renderPlanner({ onPlanningDepthChange, isImperial: true })
      fireEvent.click(screen.getByRole('button', { name: /expand rode planner/i }))

      fireEvent.change(depthInput(), { target: { value: '20' } })
      vi.advanceTimersByTime(800)

      expect(onPlanningDepthChange).toHaveBeenCalledTimes(1)
      const [depthMArg] = onPlanningDepthChange.mock.calls[0]
      const figureM = 20 / 3.28084
      const expectedRawDepthM = rawDepthFromPlanningFigureM(figureM, 2, tide, 1)!
      expect(depthMArg).toBeCloseTo(expectedRawDepthM, 6)
    })

    it('fires immediately, with no debounce, when there is no active watch', () => {
      const onPlanningDepthChange = vi.fn().mockResolvedValue(undefined)
      renderPlanner({ anchorState: 'none', onPlanningDepthChange })
      fireEvent.click(screen.getByRole('button', { name: /expand rode planner/i }))

      fireEvent.change(depthInput(), { target: { value: '8' } })
      const expectedRawDepthM = rawDepthFromPlanningFigureM(8, 2, tide, 1)!
      expect(onPlanningDepthChange).toHaveBeenCalledTimes(1)
      const [depthMArg, tideArg] = onPlanningDepthChange.mock.calls[0]
      expect(depthMArg).toBeCloseTo(expectedRawDepthM, 6)
      expect(tideArg).toBe(2)
    })

    it('clears both the sea-state and depth debounce timers on unmount, so a stray PATCH cannot fire after Raise', () => {
      const onPlanningDepthChange = vi.fn().mockResolvedValue(undefined)
      const onUpdateRodeAndConditions = vi.fn().mockResolvedValue(undefined)
      const { unmount } = renderPlanner({ onPlanningDepthChange, onUpdateRodeAndConditions })
      fireEvent.click(screen.getByRole('button', { name: /expand rode planner/i }))

      fireEvent.change(depthInput(), { target: { value: '8' } })
      fireEvent.change(screen.getByLabelText(/sea state/i), { target: { value: 'storm' } })
      unmount()
      vi.advanceTimersByTime(2000)

      expect(onPlanningDepthChange).not.toHaveBeenCalled()
      expect(onUpdateRodeAndConditions).not.toHaveBeenCalled()
    })

    // The new fail-visibly path: a typed figure that doesn't clear bow
    // height plus tide rise implies a raw depth <= 0, which is not a
    // sensible reading — it must not be persisted, and the caption must say
    // why instead of silently keeping the last good value's caption.
    it('does not persist and shows a caption when the typed figure is below bow height plus tide rise', () => {
      const onPlanningDepthChange = vi.fn().mockResolvedValue(undefined)
      renderPlanner({ onPlanningDepthChange })
      fireEvent.click(screen.getByRole('button', { name: /expand rode planner/i }))

      // rise (3ft/3.28084 ~= 0.914m) + bow (1m) ~= 1.914m; 1 m clears neither.
      fireEvent.change(depthInput(), { target: { value: '1' } })
      vi.advanceTimersByTime(800)

      expect(onPlanningDepthChange).not.toHaveBeenCalled()
      expect(screen.getByText('Below bow height plus tide rise, not saved')).toBeInTheDocument()
    })

    it('shows the refusal caption when anchored with nothing recorded', () => {
      const onPlanningDepthChange = vi.fn().mockResolvedValue(undefined)
      renderPlanner({ onPlanningDepthChange, depthM: 42, planningDepthM: null, planningTideHeightFt: null })
      fireEvent.click(screen.getByRole('button', { name: /expand rode planner/i }))

      fireEvent.change(depthInput(), { target: { value: '1' } })
      vi.advanceTimersByTime(800)

      expect(onPlanningDepthChange).not.toHaveBeenCalled()
      expect(screen.getByText('Below bow height plus tide rise, not saved')).toBeInTheDocument()
    })

    it('shows the refusal caption for zero rather than silently ignoring it', () => {
      const onPlanningDepthChange = vi.fn().mockResolvedValue(undefined)
      renderPlanner({ onPlanningDepthChange })
      fireEvent.click(screen.getByRole('button', { name: /expand rode planner/i }))

      fireEvent.change(depthInput(), { target: { value: '0' } })
      vi.advanceTimersByTime(800)

      expect(onPlanningDepthChange).not.toHaveBeenCalled()
      expect(screen.getByText('Below bow height plus tide rise, not saved')).toBeInTheDocument()
    })
  })
})
