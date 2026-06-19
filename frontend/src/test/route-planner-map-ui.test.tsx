import { act, fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it, vi, beforeEach } from 'vitest'
import { RoutePlannerMap } from '@/components/route-planner-map'

vi.mock('maplibre-gl', () => ({
  default: {},
}))

let lastMapClickHandler: ((e: { lngLat: { lat: number; lng: number } }) => void) | null = null
let lastZoomHandler: (() => void) | null = null
let lastInitialViewState: { latitude: number; longitude: number; zoom: number } | null = null
const easeToMock = vi.fn()
let mockZoom = 12

vi.mock('react-map-gl/maplibre', async () => {
  const React = await import('react')
  return {
    Map: React.forwardRef(
      (
        {
          children,
          onClick,
          onZoom,
          initialViewState,
        }: {
          children?: React.ReactNode
          onClick?: (e: { lngLat: { lat: number; lng: number } }) => void
          onZoom?: () => void
          initialViewState?: { latitude: number; longitude: number; zoom: number }
        },
        ref: React.Ref<unknown>,
      ) => {
        lastMapClickHandler = onClick ?? null
        lastZoomHandler = onZoom ?? null
        lastInitialViewState = initialViewState ?? null
        React.useImperativeHandle(ref, () => ({
          getCanvas: () => ({ style: { cursor: 'grab' } }),
          getZoom: () => mockZoom,
          easeTo: easeToMock,
          fitBounds: () => undefined,
        }))
        return <div data-testid="map-root">{children}</div>
      },
    ),
    Marker: ({
      children,
      onClick,
    }: {
      children?: React.ReactNode
      onClick?: (e: React.MouseEvent) => void
    }) => (
      <div onClick={onClick as React.MouseEventHandler}>{children}</div>
    ),
    Source: ({ children, id }: { children?: React.ReactNode; id: string }) => (
      <div data-testid={`source-${id}`}>{children}</div>
    ),
    Layer: ({ id }: { id: string }) => <div data-testid={`layer-${id}`} />,
  }
})

function clickMap(lat: number, lng: number) {
  fireEvent.click(screen.getByTestId('map-root'))
  lastMapClickHandler?.({ lngLat: { lat, lng } })
}

describe('RoutePlannerMap', () => {
  beforeEach(() => {
    easeToMock.mockClear()
    localStorage.clear()
    mockZoom = 12
  })

  it('centers the initial view on the vessel position when there are no waypoints', () => {
    render(
      <RoutePlannerMap
        waypoints={[]}
        onWaypointsChange={() => undefined}
        isDarkTheme={false}
        vesselLat={-25.29}
        vesselLon={152.91}
      />,
    )

    expect(lastInitialViewState).toEqual({ latitude: -25.29, longitude: 152.91, zoom: 11 })
  })

  it('prefers the first waypoint over the vessel position for the initial view', () => {
    render(
      <RoutePlannerMap
        waypoints={[{ lat: 1, lon: 2 }]}
        onWaypointsChange={() => undefined}
        isDarkTheme={false}
        vesselLat={-25.29}
        vesselLon={152.91}
      />,
    )

    expect(lastInitialViewState).toEqual({ latitude: 1, longitude: 2, zoom: 12 })
  })

  it('recenters on the vessel when the "center on current position" control is used', () => {
    render(
      <RoutePlannerMap
        waypoints={[]}
        onWaypointsChange={() => undefined}
        isDarkTheme={false}
        vesselLat={-25.29}
        vesselLon={152.91}
      />,
    )

    fireEvent.click(screen.getByRole('button', { name: 'Center on current position' }))

    expect(easeToMock).toHaveBeenCalledWith({ center: [152.91, -25.29], zoom: 11, duration: 400 })
  })

  it('auto-recenters once if the vessel GPS fix arrives after mount', () => {
    const { rerender } = render(
      <RoutePlannerMap waypoints={[]} onWaypointsChange={() => undefined} isDarkTheme={false} />,
    )

    expect(easeToMock).not.toHaveBeenCalled()

    rerender(
      <RoutePlannerMap
        waypoints={[]}
        onWaypointsChange={() => undefined}
        isDarkTheme={false}
        vesselLat={-25.29}
        vesselLon={152.91}
      />,
    )

    expect(easeToMock).toHaveBeenCalledWith({ center: [152.91, -25.29], zoom: 11, duration: 400 })
  })

  it('does not auto-recenter once the user has started placing waypoints', () => {
    const { rerender } = render(
      <RoutePlannerMap
        waypoints={[{ lat: 5, lon: 6 }]}
        onWaypointsChange={() => undefined}
        isDarkTheme={false}
      />,
    )

    rerender(
      <RoutePlannerMap
        waypoints={[{ lat: 5, lon: 6 }]}
        onWaypointsChange={() => undefined}
        isDarkTheme={false}
        vesselLat={-25.29}
        vesselLon={152.91}
      />,
    )

    expect(easeToMock).not.toHaveBeenCalled()
  })

  it('shows a hint when there are no waypoints', () => {
    render(<RoutePlannerMap waypoints={[]} onWaypointsChange={() => undefined} isDarkTheme={false} />)
    expect(screen.getByText('Click the map to add your first waypoint')).toBeInTheDocument()
  })

  it('appends a waypoint when the map is clicked', () => {
    const onWaypointsChange = vi.fn()
    render(<RoutePlannerMap waypoints={[]} onWaypointsChange={onWaypointsChange} isDarkTheme={false} />)

    clickMap(10, 20)

    expect(onWaypointsChange).toHaveBeenCalledWith([{ lat: 10, lon: 20 }])
  })

  it('renders a numbered marker per waypoint', () => {
    render(
      <RoutePlannerMap
        waypoints={[{ lat: 0, lon: 0 }, { lat: 1, lon: 1 }]}
        onWaypointsChange={() => undefined}
        isDarkTheme={false}
      />,
    )

    expect(screen.getByRole('button', { name: /Waypoint 1/ })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /Waypoint 2/ })).toBeInTheDocument()
  })

  it('shows the route line source once there are 2+ waypoints', () => {
    render(
      <RoutePlannerMap
        waypoints={[{ lat: 0, lon: 0 }, { lat: 1, lon: 1 }]}
        onWaypointsChange={() => undefined}
        isDarkTheme={false}
      />,
    )

    expect(screen.getByTestId('source-route-line')).toBeInTheDocument()
  })

  it('selecting then deleting a waypoint removes it', () => {
    const onWaypointsChange = vi.fn()
    render(
      <RoutePlannerMap
        waypoints={[{ lat: 0, lon: 0 }, { lat: 1, lon: 1 }]}
        onWaypointsChange={onWaypointsChange}
        isDarkTheme={false}
      />,
    )

    fireEvent.click(screen.getByRole('button', { name: /Waypoint 1/ }))
    fireEvent.click(screen.getByRole('button', { name: 'Delete' }))

    expect(onWaypointsChange).toHaveBeenCalledWith([{ lat: 1, lon: 1 }])
  })

  it('does not show the satellite imagery source until toggled on', () => {
    render(<RoutePlannerMap waypoints={[]} onWaypointsChange={() => undefined} isDarkTheme={false} />)

    expect(screen.queryByTestId('source-world-imagery')).not.toBeInTheDocument()
  })

  it('does not show satellite imagery below the zoom handoff even when toggled on', () => {
    render(<RoutePlannerMap waypoints={[]} onWaypointsChange={() => undefined} isDarkTheme={false} />)

    fireEvent.click(screen.getByRole('button', { name: 'Toggle satellite imagery' }))

    expect(screen.queryByTestId('source-world-imagery')).not.toBeInTheDocument()
  })

  it('shows the Esri World Imagery layer once zoomed in past the handoff', () => {
    render(<RoutePlannerMap waypoints={[]} onWaypointsChange={() => undefined} isDarkTheme={false} />)

    fireEvent.click(screen.getByRole('button', { name: 'Toggle satellite imagery' }))
    mockZoom = 12
    act(() => {
      lastZoomHandler?.()
    })

    expect(screen.getByTestId('source-world-imagery')).toBeInTheDocument()
  })

  it('hides satellite imagery again when toggled off', () => {
    render(<RoutePlannerMap waypoints={[]} onWaypointsChange={() => undefined} isDarkTheme={false} />)

    const toggle = screen.getByRole('button', { name: 'Toggle satellite imagery' })
    fireEvent.click(toggle)
    mockZoom = 12
    act(() => {
      lastZoomHandler?.()
    })
    expect(screen.getByTestId('source-world-imagery')).toBeInTheDocument()

    fireEvent.click(toggle)
    expect(screen.queryByTestId('source-world-imagery')).not.toBeInTheDocument()
  })
})
