import { fireEvent, render, screen, within } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { AnchorWatchTile } from '@/components/anchor-watch-tile'
import type { AnchorWatchResult } from '@/hooks/use-anchor-watch'
import type { TideToday } from '@/hooks/use-tide-today'

// The tile computes `now` itself (`new Date()`, live at render time — see
// ADR 0135), so datetime/low_tide_time below are built relative to the real
// clock rather than a fixed calendar date: a hardcoded date would eventually
// (or immediately, depending when the suite runs) fall foul of the 30-minute
// staleness rule or the "low already passed" rule this same ADR adds.
function makeTide(overrides: Partial<TideToday> = {}): TideToday {
  const now = Date.now()
  return {
    datetime: new Date(now - 5 * 60 * 1000).toISOString(),
    current_tide_height_ft: 3,
    tide_direction: 'Falling',
    high_tide_time: new Date(0).toISOString(),
    high_tide_height_ft: 5,
    low_tide_time: new Date(now + 60 * 60 * 1000).toISOString(),
    low_tide_height_ft: 1,
    station_name: 'Test Station',
    provider: 'test',
    ...overrides,
  }
}

// Same shallow-mock approach as anchor-imagery-toggle.test.tsx (~lines 22-37):
// the map itself is covered by its own dedicated tests, so here it's stubbed
// down to the props this tile is responsible for feeding it.
//
// anchor-watch-tile.tsx now lazy-loads the real AnchorWatchMap (kiosk
// bundle-split follow-up), but vi.mock intercepts the module regardless of
// whether it's reached through a static or a dynamic import, so this mock
// still takes effect. Only the tests that assert on the mock's own rendered
// content need to await its (near-instant, but still async) resolution —
// see the findByTestId calls below; everything else in this tile renders
// eagerly, outside the map's own Suspense boundary.
vi.mock('@/components/anchor-watch-map', () => ({
  AnchorWatchMap: (props: {
    vesselLat: number
    vesselLon: number
    aisCollisionAlarms?: ReadonlyMap<string, string>
    interactive?: boolean
    anchorSetAt?: string | null
    onAnchorReposition?: (lat: number, lon: number) => void
    onRadiusChange?: (radiusMeters: number) => void
  }) => (
    <div
      data-testid="anchor-watch-map"
      data-collision-vessels={[...(props.aisCollisionAlarms?.keys() ?? [])].join(',')}
      data-interactive={String(props.interactive)}
      data-anchor-set-at={props.anchorSetAt ?? ''}
      data-has-reposition={String(props.onAnchorReposition !== undefined)}
      data-has-radius-change={String(props.onRadiusChange !== undefined)}
    >
      {`${props.vesselLat},${props.vesselLon}`}
    </div>
  ),
}))

vi.mock('@/hooks/use-alarms', () => ({
  useAnchorDragAlarm: () => null,
  acknowledgeAlarm: vi.fn(),
}))

// A plain object each test can mutate via anchorAlarmMock.silence / .unsilence
// (both vi.fn()) and reassign the booleans on — simpler than juggling
// mockReturnValueOnce across the describe blocks below, and every existing
// test keeps the pre-P0-fix "nothing going on" defaults.
const anchorAlarmMock: {
  isAlarming: boolean
  isSilenced: boolean
  isActive: boolean
  silence: ReturnType<typeof vi.fn>
  unsilence: ReturnType<typeof vi.fn>
} = {
  isAlarming: false,
  isSilenced: false,
  isActive: false,
  silence: vi.fn(),
  unsilence: vi.fn(),
}

vi.mock('@/hooks/use-anchor-alarm', () => ({
  useAnchorAlarm: () => anchorAlarmMock,
}))

vi.mock('@/lib/audio-utils', () => ({
  primeAudioContextForAlarm: vi.fn().mockResolvedValue(undefined),
}))

function baseWatch(overrides: Partial<AnchorWatchResult> = {}): AnchorWatchResult {
  return {
    active: false,
    anchorState: 'none',
    gnssCritical: false,
    anchorLat: null,
    anchorLon: null,
    radiusMeters: 20,
    rodeDeployedM: 0,
    seaState: 'calm',
    seabedType: 'sand',
    distanceMeters: null,
    bearingDeg: null,
    setAt: null,
    loaded: true,
    error: null,
    bowOffsetM: 0,
    bowOffsetApplied: false,
    bowOffsetReason: '',
    headingAtSetDeg: -1,
    placeName: '',
    planningDepthM: null,
    planningTideHeightFt: null,
    lastAutoRaise: null,
    setAnchorHere: vi.fn(),
    updateRadius: vi.fn(),
    adjustAnchor: vi.fn(),
    updateRodeAndConditions: vi.fn(),
    updatePlanningDepth: vi.fn(),
    clearAnchor: vi.fn(),
    ...overrides,
  }
}

function baseProps(overrides: Record<string, unknown> = {}) {
  return {
    watch: baseWatch(),
    lat: -25.1,
    lon: 152.9,
    depthMeters: 5,
    currentDriftKts: null,
    currentSetDeg: null,
    isImperial: false,
    vesselHeadingDeg: null,
    vesselTrail: [],
    aisVessels: [],
    aisTrails: new Map(),
    isDarkTheme: false,
    showImageryLayer: false,
    onImageryToggle: vi.fn(),
    showRadarEcho: false,
    onRadarEchoToggle: vi.fn(),
    onFullscreen: vi.fn(),
    placemarks: [],
    onPlacemarkCreate: vi.fn(),
    onPlacemarkRemove: vi.fn(),
    tide: null,
    lastUpdateAgeS: null,
    ...overrides,
  }
}

describe('AnchorWatchTile', () => {
  beforeEach(() => {
    anchorAlarmMock.isAlarming = false
    anchorAlarmMock.isSilenced = false
    anchorAlarmMock.isActive = false
    anchorAlarmMock.silence = vi.fn()
    anchorAlarmMock.unsilence = vi.fn()
  })

  it('renders the map when no anchor is set but a GPS fix is available', async () => {
    render(<AnchorWatchTile {...baseProps()} />)

    expect(await screen.findByTestId('anchor-watch-map')).toBeInTheDocument()
  })

  it('passes aisCollisionAlarms straight through to the map', async () => {
    render(
      <AnchorWatchTile
        {...baseProps({ aisCollisionAlarms: new Map([['urn:mrn:imo:mmsi:100000001', 'warn']]) })}
      />,
    )

    expect(await screen.findByTestId('anchor-watch-map')).toHaveAttribute(
      'data-collision-vessels',
      'urn:mrn:imo:mmsi:100000001',
    )
  })

  // Kiosk maps are display-only (ADR: kiosk maps are display-only) - the
  // tile's own job here is just to forward the prop, since AnchorWatchMap
  // is what actually hides controls and drops maplibre's interactive
  // handlers (covered in anchor-watch-map-ui.test.tsx).
  describe('interactive', () => {
    it('defaults to an interactive map', async () => {
      render(<AnchorWatchTile {...baseProps()} />)

      expect(await screen.findByTestId('anchor-watch-map')).toHaveAttribute('data-interactive', 'true')
    })

    it('passes interactive={false} straight through when told to', async () => {
      render(<AnchorWatchTile {...baseProps({ interactive: false })} />)

      expect(await screen.findByTestId('anchor-watch-map')).toHaveAttribute('data-interactive', 'false')
    })
  })

  // The tile is view-only: editing the anchor point and swing radius lives
  // only on the full-page Anchor Watch view (anchor-watch-drawer.tsx). See
  // anchor-watch-map-view-only.test.tsx for what AnchorWatchMap itself does
  // once these callbacks are absent.
  describe('anchor editing', () => {
    it('does not pass onAnchorReposition or onRadiusChange to the map', async () => {
      render(
        <AnchorWatchTile
          {...baseProps({
            watch: baseWatch({ anchorState: 'set', anchorLat: -25.1, anchorLon: 152.9 }),
          })}
        />,
      )

      const map = await screen.findByTestId('anchor-watch-map')
      expect(map).toHaveAttribute('data-has-reposition', 'false')
      expect(map).toHaveAttribute('data-has-radius-change', 'false')
    })
  })

  // A fresh drop mints a new set_at; AnchorWatchMap eases the camera to the
  // anchor whenever this session id changes (anchor-session-recenter.test.tsx
  // covers that behaviour at the map level) — this only has to prove the
  // tile hands the session id down at all.
  describe('anchorSetAt', () => {
    it("passes the watch's set_at through to the map as anchorSetAt", async () => {
      render(
        <AnchorWatchTile
          {...baseProps({
            watch: baseWatch({
              anchorState: 'set',
              anchorLat: -25.1,
              anchorLon: 152.9,
              setAt: '2026-09-25T06:30:00Z',
            }),
          })}
        />,
      )

      expect(await screen.findByTestId('anchor-watch-map')).toHaveAttribute(
        'data-anchor-set-at',
        '2026-09-25T06:30:00Z',
      )
    })

    it('passes null through when no watch session is running', async () => {
      render(<AnchorWatchTile {...baseProps({ watch: baseWatch({ setAt: null }) })} />)

      expect(await screen.findByTestId('anchor-watch-map')).toHaveAttribute('data-anchor-set-at', '')
    })
  })

  it('shows a No GPS fix placeholder — never a map — when position and anchor are both unavailable', () => {
    render(<AnchorWatchTile {...baseProps({ lat: null, lon: null })} />)

    expect(screen.queryByTestId('anchor-watch-map')).toBeNull()
    expect(screen.getByText('No GPS fix')).toBeInTheDocument()
  })

  it('disables Drop when there is no GPS fix to drop the anchor at', () => {
    render(<AnchorWatchTile {...baseProps({ lat: null, lon: null })} />)

    expect(screen.getByRole('button', { name: 'Drop' })).toBeDisabled()
  })

  it('still renders the map from the anchor point alone when the live GPS fix drops after the anchor is set', async () => {
    render(
      <AnchorWatchTile
        {...baseProps({
          lat: null,
          lon: null,
          watch: baseWatch({ anchorState: 'set', anchorLat: -25.2, anchorLon: 152.8 }),
        })}
      />,
    )

    expect(await screen.findByTestId('anchor-watch-map')).toHaveTextContent('-25.2,152.8')
  })

  it('shows Drop when no anchor is set', () => {
    render(<AnchorWatchTile {...baseProps()} />)

    expect(screen.getByRole('button', { name: 'Drop' })).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Raise' })).toBeNull()
  })

  it('has no Raise button when an anchor is set; raising belongs to the Anchor Watch page', () => {
    render(
      <AnchorWatchTile
        {...baseProps({
          watch: baseWatch({ anchorState: 'set', anchorLat: -25.1, anchorLon: 152.9 }),
        })}
      />,
    )

    expect(screen.queryByRole('button', { name: 'Drop' })).toBeNull()
    expect(screen.queryByRole('button', { name: 'Raise' })).toBeNull()
  })

  describe('banners', () => {
    it('never prompts to set a watch from the SignalK navigation state', () => {
      render(<AnchorWatchTile {...baseProps({ watch: baseWatch({ anchorState: 'none' }) })} />)

      expect(screen.queryByText(/set anchor watch\?/i)).toBeNull()
    })

    it('shows the gnssCritical banner only once a watch is active', () => {
      const { rerender } = render(
        <AnchorWatchTile {...baseProps({ watch: baseWatch({ anchorState: 'none', gnssCritical: true }) })} />,
      )
      expect(screen.queryByText(/GPS signal degraded/)).toBeNull()

      rerender(
        <AnchorWatchTile
          {...baseProps({
            watch: baseWatch({ anchorState: 'set', anchorLat: -25.1, anchorLon: 152.9, gnssCritical: true }),
          })}
        />,
      )
      expect(screen.getByText(/GPS signal degraded/)).toBeInTheDocument()
    })

    // The backend puts a damaged anchor_watch.json into an explicit error
    // state rather than an invented or empty watch (GET /api/anchor-watch
    // reports it as `error`, naming the file path and the parse error). The
    // tile must show that plainly, using "tile" wording, not silently render
    // the ordinary empty "no watch set" tile as if nothing were wrong.
    it('shows the load-error banner naming the problem when the watch is unreadable', () => {
      render(
        <AnchorWatchTile
          {...baseProps({
            watch: baseWatch({
              anchorState: 'none',
              error: 'parsing anchor watch state (data/anchor_watch.json): unexpected end of JSON input',
            }),
          })}
        />,
      )

      expect(screen.getByText(/saved anchor watch unreadable/i)).toBeInTheDocument()
      expect(screen.getByText(/data\/anchor_watch\.json/)).toBeInTheDocument()
    })

    it('does not show the load-error banner once the watch loads normally', () => {
      render(<AnchorWatchTile {...baseProps({ watch: baseWatch({ anchorState: 'none', error: null }) })} />)

      expect(screen.queryByText(/saved anchor watch unreadable/i)).toBeNull()
    })
  })

  // P0: a silenced drag used to render nothing anywhere on this tile — the
  // red strip was gated on isAlarming alone, which silencing flips straight
  // to false with no replacement. These tests cover both the surviving red
  // strip and the amber one that must now stand in for it once silenced.
  describe('drag alarm strip', () => {
    it('shows a red, audible strip naming the live distance and radius, and Silence stops it', () => {
      anchorAlarmMock.isAlarming = true
      anchorAlarmMock.isActive = true
      render(
        <AnchorWatchTile
          {...baseProps({
            watch: baseWatch({
              anchorState: 'dragging',
              anchorLat: -25.1,
              anchorLon: 152.9,
              distanceMeters: 38,
              radiusMeters: 30,
            }),
          })}
        />,
      )

      const strip = screen.getByTestId('drag-alarm-strip')
      expect(strip).toHaveAttribute('role', 'alert')
      expect(strip.textContent).toContain('38')
      expect(strip.textContent).toContain('30')
      // The old copy said only "ALARM", with no indication of how far or
      // past what radius.
      expect(strip.textContent).not.toBe('ALARM')
      expect(screen.queryByTestId('drag-silenced-strip')).toBeNull()

      fireEvent.click(within(strip).getByRole('button', { name: /silence/i }))
      expect(anchorAlarmMock.silence).toHaveBeenCalledTimes(1)
    })

    it('shows a persistent amber strip — never nothing — once the drag is silenced', () => {
      anchorAlarmMock.isSilenced = true
      anchorAlarmMock.isActive = true
      render(
        <AnchorWatchTile
          {...baseProps({
            watch: baseWatch({
              anchorState: 'dragging',
              anchorLat: -25.1,
              anchorLon: 152.9,
              distanceMeters: 38,
              radiusMeters: 30,
            }),
          })}
        />,
      )

      expect(screen.queryByTestId('drag-alarm-strip')).toBeNull()
      const strip = screen.getByTestId('drag-silenced-strip')
      expect(strip).toHaveAttribute('role', 'alert')
      expect(strip.textContent).toContain('38')
      expect(strip.textContent).toContain('30')
      expect(strip.textContent?.toLowerCase()).toContain('silenced')

      fireEvent.click(within(strip).getByRole('button', { name: /unsilence/i }))
      expect(anchorAlarmMock.unsilence).toHaveBeenCalledTimes(1)
    })

    it('renders neither strip when there is no drag alarm at all', () => {
      render(<AnchorWatchTile {...baseProps({ watch: baseWatch({ anchorState: 'set', anchorLat: -25.1, anchorLon: 152.9 }) })} />)

      expect(screen.queryByTestId('drag-alarm-strip')).toBeNull()
      expect(screen.queryByTestId('drag-silenced-strip')).toBeNull()
    })
  })

  // The map's own overlay already carries distance, and the low-water
  // clearance line belongs to the Anchor Watch page (the drawer), so the
  // tile stays a map.
  describe('page-only readouts', () => {
    it('does not render the distance KPI stack even once anchored', () => {
      render(
        <AnchorWatchTile
          {...baseProps({
            watch: baseWatch({
              anchorState: 'set',
              anchorLat: -25.1,
              anchorLon: 152.9,
              distanceMeters: 12,
              radiusMeters: 20,
            }),
          })}
        />,
      )

      expect(screen.queryByTestId('anchor-distance-kpi')).toBeNull()
    })

    it('does not render the low water clearance line, quiet or warning', () => {
      for (const depthMeters of [4, 2]) {
        const { unmount } = render(
          <AnchorWatchTile
            {...baseProps({
              depthMeters,
              tide: makeTide(),
            })}
          />,
        )
        expect(screen.queryByTestId('low-water-clearance')).toBeNull()
        unmount()
      }
    })
  })

  describe('staleness', () => {
    it('marks the tile stale once the feed age passes the threshold', () => {
      render(<AnchorWatchTile {...baseProps({ lastUpdateAgeS: 500 })} />)

      const badge = screen.getByTestId('tile-stale-badge')
      expect(badge.textContent).toContain('8m')
    })

    it('does not mark the tile stale for a fresh age', () => {
      render(<AnchorWatchTile {...baseProps({ lastUpdateAgeS: 5 })} />)

      expect(screen.queryByTestId('tile-stale-badge')).toBeNull()
    })

    it('does not fabricate staleness when the age is unknown (null)', () => {
      render(<AnchorWatchTile {...baseProps({ lastUpdateAgeS: null })} />)

      expect(screen.queryByTestId('tile-stale-badge')).toBeNull()
    })
  })
})
