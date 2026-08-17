import { act, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { AutopilotTile } from '@/components/autopilot-tile'
import type { AutopilotState } from '@/hooks/use-autopilot'

const ABSENT: AutopilotState = {
  present: false,
  engaged: false,
  state: null,
  mode: null,
  target: null,
  availableActions: [],
  stale: false,
}

const ENGAGED: AutopilotState = {
  present: true,
  engaged: true,
  state: 'auto',
  mode: 'compass',
  target: 135,
  availableActions: ['disengage', 'engage', 'tack', 'gybe', 'adjustHeading'],
  stale: false,
}

const STANDBY: AutopilotState = {
  present: true,
  engaged: false,
  state: 'standby',
  mode: 'compass',
  target: 0,
  availableActions: ['engage', 'adjustHeading'],
  stale: false,
}

function baseProps() {
  return {
    state: STANDBY,
    pending: new Set<string>(),
    error: null,
    availableModes: ['compass', 'wind', 'gps'],
    capabilityError: null,
    onEngage: vi.fn(),
    onDisengage: vi.fn(),
    onTack: vi.fn(),
    onGybe: vi.fn(),
    onAdjustHeading: vi.fn(),
    onSetMode: vi.fn(),
    onDodge: vi.fn(),
    onClearDodge: vi.fn(),
  }
}

beforeEach(() => {
  vi.useFakeTimers()
})

afterEach(() => {
  vi.useRealTimers()
})

describe('AutopilotTile', () => {
  it('states plainly that there is no autopilot rather than rendering dead controls', () => {
    render(<AutopilotTile {...baseProps()} state={ABSENT} />)

    expect(screen.getByText(/no autopilot/i)).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /engage/i })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /tack/i })).not.toBeInTheDocument()
  })

  it('renders an unadvertised action as disabled, not hidden', () => {
    const props = baseProps()
    // STANDBY does not advertise 'tack' or 'gybe'.
    render(<AutopilotTile {...props} />)

    const tackPort = screen.getByRole('button', { name: /tack.*port/i })
    expect(tackPort).toBeInTheDocument()
    expect(tackPort).toBeDisabled()
  })

  it('enables an advertised action', () => {
    const props = baseProps()
    render(<AutopilotTile {...props} state={ENGAGED} />)

    const tackPort = screen.getByRole('button', { name: /tack.*port/i })
    expect(tackPort).not.toBeDisabled()
  })

  it('does not fire the hold-to-confirm engage/disengage action on a short tap', () => {
    const props = baseProps()
    render(<AutopilotTile {...props} />)

    const holdButton = screen.getByRole('button', { name: /hold to engage/i })
    fireEvent.pointerDown(holdButton)
    act(() => { vi.advanceTimersByTime(300) })
    fireEvent.pointerUp(holdButton)
    act(() => { vi.advanceTimersByTime(1000) })

    expect(props.onEngage).not.toHaveBeenCalled()
  })

  it('fires the hold-to-confirm action after ~800ms of holding', () => {
    const props = baseProps()
    render(<AutopilotTile {...props} />)

    const holdButton = screen.getByRole('button', { name: /hold to engage/i })
    fireEvent.pointerDown(holdButton)
    act(() => { vi.advanceTimersByTime(800) })

    expect(props.onEngage).toHaveBeenCalledTimes(1)
  })

  it('shows a hold-to-DISENGAGE control while engaged, not engage', () => {
    const props = baseProps()
    render(<AutopilotTile {...props} state={ENGAGED} />)

    const holdButton = screen.getByRole('button', { name: /hold to disengage/i })
    fireEvent.pointerDown(holdButton)
    act(() => { vi.advanceTimersByTime(800) })

    expect(props.onDisengage).toHaveBeenCalledTimes(1)
    expect(props.onEngage).not.toHaveBeenCalled()
  })

  it('fires heading adjust buttons immediately, with no hold required', () => {
    const props = baseProps()
    render(<AutopilotTile {...props} state={ENGAGED} />)

    fireEvent.click(screen.getByRole('button', { name: '+1°' }))
    fireEvent.click(screen.getByRole('button', { name: '+10°' }))
    fireEvent.click(screen.getByRole('button', { name: '-1°' }))
    fireEvent.click(screen.getByRole('button', { name: '-10°' }))

    expect(props.onAdjustHeading).toHaveBeenCalledWith(1)
    expect(props.onAdjustHeading).toHaveBeenCalledWith(10)
    expect(props.onAdjustHeading).toHaveBeenCalledWith(-1)
    expect(props.onAdjustHeading).toHaveBeenCalledWith(-10)
  })

  it('disables heading adjust buttons when adjustHeading is not advertised', () => {
    const props = baseProps()
    render(<AutopilotTile {...props} state={{ ...STANDBY, availableActions: ['engage'] }} />)

    expect(screen.getByRole('button', { name: '+1°' })).toBeDisabled()
  })

  it('greys the tile and says so when steering data has gone stale', () => {
    const props = baseProps()
    render(<AutopilotTile {...props} state={{ ...ENGAGED, stale: true }} />)

    expect(screen.getByText(/stale/i)).toBeInTheDocument()
  })

  it('disables commands while stale, since the last-known state cannot be trusted', () => {
    const props = baseProps()
    render(<AutopilotTile {...props} state={{ ...ENGAGED, stale: true }} />)

    expect(screen.getByRole('button', { name: /hold to disengage/i })).toBeDisabled()
  })

  it('still shows the last-known engaged reading while stale rather than hiding it', () => {
    const props = baseProps()
    render(<AutopilotTile {...props} state={{ ...ENGAGED, stale: true }} />)

    // Last-known target of 135 should still be visible.
    expect(screen.getByText(/135/)).toBeInTheDocument()
  })

  it('disables a control that is mid-flight (pending) without changing the displayed engaged/target', () => {
    const props = baseProps()
    render(<AutopilotTile {...props} state={ENGAGED} pending={new Set(['disengage'])} />)

    expect(screen.getByRole('button', { name: /hold to disengage/i })).toBeDisabled()
    // Still shows the server-confirmed engaged state, not an optimistic one.
    expect(screen.getByText(/135/)).toBeInTheDocument()
  })

  it('surfaces a command error from the hook', () => {
    const props = baseProps()
    render(<AutopilotTile {...props} error="signalk returned status 503: provider offline" />)

    expect(screen.getByText(/provider offline/)).toBeInTheDocument()
  })

  it('offers tack and gybe independently, each gated on its own advertised action', () => {
    const props = baseProps()
    // Advertises tack but not gybe, while steering in compass mode.
    const tackOnly = { ...ENGAGED, availableActions: ['disengage', 'tack', 'adjustHeading'] }
    render(<AutopilotTile {...props} state={tackOnly} />)

    expect(screen.getByRole('button', { name: /tack.*port/i })).not.toBeDisabled()
    expect(screen.getByRole('button', { name: /tack.*starboard/i })).not.toBeDisabled()
    // Present but disabled — a missing button reads as a bug, a greyed one
    // reads as "this pilot can't".
    expect(screen.getByRole('button', { name: /gybe.*port/i })).toBeDisabled()
    expect(screen.getByRole('button', { name: /gybe.*starboard/i })).toBeDisabled()
  })

  it('fires gybe, not tack, from the gybe control', () => {
    const props = baseProps()
    render(<AutopilotTile {...props} state={ENGAGED} />)

    const gybeStarboard = screen.getByRole('button', { name: /gybe.*starboard/i })
    fireEvent.pointerDown(gybeStarboard)
    act(() => { vi.advanceTimersByTime(900) })

    expect(props.onGybe).toHaveBeenCalledWith('starboard')
    expect(props.onTack).not.toHaveBeenCalled()
  })

  it('renders a control for each supported mode and marks the active one', () => {
    const props = baseProps()
    render(<AutopilotTile {...props} state={ENGAGED} />)

    expect(screen.getByRole('button', { name: /switch to wind mode/i })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /switch to gps mode/i })).toBeInTheDocument()
    // ENGAGED is already in compass mode, so switching to it is a no-op.
    expect(screen.getByRole('button', { name: /compass mode/i })).toBeDisabled()
  })

  it('requires a hold to change mode, since it changes what the pilot steers to', () => {
    const props = baseProps()
    render(<AutopilotTile {...props} state={ENGAGED} />)

    const windMode = screen.getByRole('button', { name: /switch to wind mode/i })

    fireEvent.pointerDown(windMode)
    act(() => { vi.advanceTimersByTime(300) })
    fireEvent.pointerUp(windMode)
    act(() => { vi.advanceTimersByTime(1000) })
    expect(props.onSetMode).not.toHaveBeenCalled()

    fireEvent.pointerDown(windMode)
    act(() => { vi.advanceTimersByTime(900) })
    expect(props.onSetMode).toHaveBeenCalledWith('wind')
  })

  it('disables mode switching and says why when the capability probe failed', () => {
    const props = baseProps()
    render(
      <AutopilotTile
        {...props}
        state={ENGAGED}
        availableModes={[]}
        capabilityError="signalk unreachable"
      />,
    )

    expect(screen.getByText(/signalk unreachable/)).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /switch to .* mode/i })).not.toBeInTheDocument()
  })

  it('fires dodge immediately with no hold, like the heading nudges', () => {
    const props = baseProps()
    const dodgeable = { ...ENGAGED, availableActions: [...ENGAGED.availableActions, 'dodge'] }
    render(<AutopilotTile {...props} state={dodgeable} />)

    fireEvent.click(screen.getByRole('button', { name: /dodge 5° to port/i }))
    expect(props.onDodge).toHaveBeenCalledWith(-5)

    fireEvent.click(screen.getByRole('button', { name: /cancel dodge/i }))
    expect(props.onClearDodge).toHaveBeenCalled()
  })

  it('disables the dodge controls when the pilot does not advertise dodge', () => {
    const props = baseProps()
    render(<AutopilotTile {...props} state={ENGAGED} />)

    expect(screen.getByRole('button', { name: /dodge 5° to port/i })).toBeDisabled()
    expect(screen.getByRole('button', { name: /cancel dodge/i })).toBeDisabled()
  })

  it('locks mode and dodge controls while stale, like every other command', () => {
    const props = baseProps()
    const staleDodgeable = { ...ENGAGED, availableActions: [...ENGAGED.availableActions, 'dodge'], stale: true }
    render(<AutopilotTile {...props} state={staleDodgeable} />)

    expect(screen.getByRole('button', { name: /switch to wind mode/i })).toBeDisabled()
    expect(screen.getByRole('button', { name: /dodge 5° to port/i })).toBeDisabled()
  })

  // Role-gating (ADR 0040 §frontend): below readwrite, every control is
  // disabled even when the pilot advertises the action and steering data is
  // fresh — cosmetic only, the server is the actual enforcement point.
  describe('readOnly', () => {
    it('disables engage/disengage, heading nudges, tack/gybe and dodge on an otherwise fully-capable pilot', () => {
      const props = baseProps()
      const dodgeable = { ...ENGAGED, availableActions: [...ENGAGED.availableActions, 'dodge'] }
      render(<AutopilotTile {...props} state={dodgeable} readOnly />)

      expect(screen.getByRole('button', { name: /hold to disengage/i })).toBeDisabled()
      expect(screen.getByRole('button', { name: '+1°' })).toBeDisabled()
      expect(screen.getByRole('button', { name: /tack.*port/i })).toBeDisabled()
      expect(screen.getByRole('button', { name: /gybe.*starboard/i })).toBeDisabled()
      expect(screen.getByRole('button', { name: /dodge 5° to port/i })).toBeDisabled()
    })

    it('disables mode switching', () => {
      const props = baseProps()
      render(<AutopilotTile {...props} state={ENGAGED} readOnly />)

      expect(screen.getByRole('button', { name: /switch to wind mode/i })).toBeDisabled()
    })

    it('does not grey the tile or claim steering data is stale — this is a permission gate, not a data problem', () => {
      const props = baseProps()
      render(<AutopilotTile {...props} state={ENGAGED} readOnly />)

      expect(screen.queryByText(/stale/i)).not.toBeInTheDocument()
    })

    it('leaves controls enabled when readOnly is false (the default)', () => {
      const props = baseProps()
      render(<AutopilotTile {...props} state={ENGAGED} />)

      expect(screen.getByRole('button', { name: /hold to disengage/i })).not.toBeDisabled()
    })
  })
})
