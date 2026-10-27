/**
 * AppShell re-renders on every live SignalK delta, so the routed pages it
 * renders must not re-render when it hands them equal props.
 */
import { useState } from 'react'
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import { PassagesRoutePanel } from '@/components/passages/passages-route-panel'
import { InventoryRoutePanel } from '@/components/inventory/inventory-route-panel'

const usePassageList = vi.fn()
const usePassagesRoute = vi.fn()
const useInventoryRoute = vi.fn()

vi.mock('@/hooks/use-passages', () => ({ usePassageList: () => usePassageList() }))
vi.mock('@/hooks/use-passages-route', () => ({ usePassagesRoute: () => usePassagesRoute() }))
vi.mock('@/hooks/use-inventory-route', () => ({ useInventoryRoute: () => useInventoryRoute() }))
vi.mock('@/components/passages/passages-panel', () => ({ PassagesPanel: () => <div /> }))
vi.mock('@/components/passages/passage-details-page', () => ({ PassageDetailsPage: () => <div /> }))
vi.mock('@/components/inventory/inventory-panel', () => ({ InventoryPanel: () => <div /> }))

// Builds the element on each render, as AppShell does; a fixed children prop
// would let React skip the child whatever memo says.
function Parent({ render: renderChild }: { render: () => React.ReactNode }) {
  const [, setTick] = useState(0)
  return (
    <>
      <button type="button" onClick={() => setTick((n) => n + 1)}>tick</button>
      {renderChild()}
    </>
  )
}

beforeEach(() => {
  vi.clearAllMocks()
  usePassageList.mockReturnValue({ passages: [], cruises: [], loading: false, error: null, refresh: vi.fn() })
  usePassagesRoute.mockReturnValue({
    route: { view: 'index', filter: { kind: 'all' } },
    showFilter: vi.fn(), openPassage: vi.fn(), openCruise: vi.fn(), openCombined: vi.fn(), close: vi.fn(),
  })
  useInventoryRoute.mockReturnValue({
    route: { view: 'index' }, go: vi.fn(), close: vi.fn(), goSection: vi.fn(), locationsHome: vi.fn(),
  })
})

describe('route panels under a re-rendering parent', () => {
  it('PassagesRoutePanel does not re-render for equal props', () => {
    render(<Parent render={() => <PassagesRoutePanel units="metric" canWrite isDarkTheme={false} />} />)
    const before = usePassageList.mock.calls.length
    fireEvent.click(screen.getByText('tick'))
    expect(usePassageList.mock.calls.length).toBe(before)
  })

  it('InventoryRoutePanel does not re-render for equal props', () => {
    render(<Parent render={() => <InventoryRoutePanel canWrite />} />)
    const before = useInventoryRoute.mock.calls.length
    fireEvent.click(screen.getByText('tick'))
    expect(useInventoryRoute.mock.calls.length).toBe(before)
  })
})
