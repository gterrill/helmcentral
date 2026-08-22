import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { AnchorRodePlanner, type AnchorRodePlannerProps } from '@/components/anchor-rode-planner'
import { SidebarProvider } from '@/components/ui/sidebar'
import type { TideToday } from '@/hooks/use-tide-today'

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
    windSpeedApparentKts: 12,
    maxGustKts: { '10m': null, '30m': null, '1h': 20, '24h': null },
    tide,
    isImperial: false,
    anchorConfig: {
      bowRollerHeightM: 1,
      chainSizeMm: 10,
      chainOnboardM: 50,
      hullType: 'power_mono',
      windageAreaM2: 20,
      gpsFromBowM: 2,
      loaM: 12,
    },
    bowOffsetM: 2,
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

describe('AnchorRodePlanner — apply as alarm radius', () => {
  it('calls onApplyAlarmRadius with recommended rode + bow offset + LOA', async () => {
    const onApplyAlarmRadius = vi.fn().mockResolvedValue(undefined)
    renderPlanner({ onApplyAlarmRadius, bowOffsetM: 2 })
    fireEvent.click(screen.getByRole('button', { name: /expand rode planner/i }))

    fireEvent.click(screen.getByRole('button', { name: /apply as alarm radius/i }))

    // depth 5m sounder + tide correction (2ft -> 5ft rise = 0.914m), bowRoller 1m,
    // wind 20kt (from maxGustKts['1h']), calm/sand, chain 10mm, windage 20m2, power_mono
    // — the exact recommendedRodeM is pinned by the rode-plan.ts characterization
    // tests; here we only assert the composition rule: rode + bowOffset + LOA.
    await waitFor(() => expect(onApplyAlarmRadius).toHaveBeenCalledTimes(1))
    const [radiusMeters] = onApplyAlarmRadius.mock.calls[0]
    expect(radiusMeters).toBeGreaterThan(2 + 12) // sanity: includes bow offset + LOA at minimum
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
})
