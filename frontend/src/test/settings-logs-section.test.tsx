import { describe, it, expect, vi } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import { LogsSection } from '@/components/settings/sections/logs-section'

vi.mock('@/hooks/use-logs', () => ({
  useLogs: vi.fn(),
}))

import { useLogs } from '@/hooks/use-logs'

describe('LogsSection', () => {
  it('renders log entries and live pause toggle', () => {
    vi.mocked(useLogs).mockReturnValue({
      logs: [
        { id: 1, timestamp: '2026-09-13T10:00:00Z', message: 'Starting server' },
        { id: 2, timestamp: '2026-09-13T10:00:01Z', message: 'ERROR: connection failed' },
      ],
      isLive: true,
      setIsLive: vi.fn(),
      clearLogs: vi.fn(),
      connected: true,
      error: null,
    })

    render(<LogsSection onAskMate={vi.fn()} />)

    expect(screen.getByText(/Live update/i)).toBeInTheDocument()
    expect(screen.getByText('Starting server')).toBeInTheDocument()
    expect(screen.getByText('ERROR: connection failed')).toBeInTheDocument()
  })

  it('toggles live updates when checkbox is clicked', () => {
    const setIsLive = vi.fn()
    vi.mocked(useLogs).mockReturnValue({
      logs: [{ id: 1, timestamp: '2026-09-13T10:00:00Z', message: 'test' }],
      isLive: true,
      setIsLive,
      clearLogs: vi.fn(),
      connected: true,
      error: null,
    })

    render(<LogsSection onAskMate={vi.fn()} />)

    const checkbox = screen.getByRole('checkbox', { name: /Live update/i })
    fireEvent.click(checkbox)

    expect(setIsLive).toHaveBeenCalledWith(false)
  })

  it('filters log entries by All, Warnings, and Errors', () => {
    vi.mocked(useLogs).mockReturnValue({
      logs: [
        { id: 1, timestamp: '2026-09-13T10:00:00Z', message: 'Starting server' },
        { id: 2, timestamp: '2026-09-13T10:00:01Z', message: 'WARN: cache near limit' },
        { id: 3, timestamp: '2026-09-13T10:00:02Z', message: 'ERROR: connection failed' },
      ],
      isLive: true,
      setIsLive: vi.fn(),
      clearLogs: vi.fn(),
      connected: true,
      error: null,
    })

    render(<LogsSection onAskMate={vi.fn()} />)

    const allButton = screen.getByRole('button', { name: /^All$/i })
    const warningsButton = screen.getByRole('button', { name: /^Warnings$/i })
    const errorsButton = screen.getByRole('button', { name: /^Errors$/i })

    expect(allButton).toBeInTheDocument()
    expect(warningsButton).toBeInTheDocument()
    expect(errorsButton).toBeInTheDocument()

    fireEvent.click(warningsButton)
    expect(screen.getByText('WARN: cache near limit')).toBeInTheDocument()
    expect(screen.queryByText('ERROR: connection failed')).not.toBeInTheDocument()

    fireEvent.click(errorsButton)
    expect(screen.getByText('ERROR: connection failed')).toBeInTheDocument()
    expect(screen.queryByText('WARN: cache near limit')).not.toBeInTheDocument()
  })

  it('only enables Ask Mate when a log is selected and uses the selected-log wording', () => {
    const onAskMate = vi.fn()
    vi.mocked(useLogs).mockReturnValue({
      logs: [
        { id: 1, timestamp: '2026-09-13T10:00:00Z', message: 'ERROR: something broke in subsystem X' },
      ],
      isLive: true,
      setIsLive: vi.fn(),
      clearLogs: vi.fn(),
      connected: true,
      error: null,
    })

    render(<LogsSection onAskMate={onAskMate} />)

    const askButton = screen.getByRole('button', { name: /Ask Mate about these log lines/i })
    expect(askButton).toBeDisabled()

    fireEvent.click(screen.getByRole('button', { name: 'ERROR: something broke in subsystem X' }))

    expect(screen.getByRole('button', { name: /Ask Mate about these log lines/i })).toBeEnabled()
    fireEvent.click(screen.getByRole('button', { name: /Ask Mate about these log lines/i }))
    expect(onAskMate).toHaveBeenCalledWith(
      expect.stringContaining('something broke in subsystem X'),
      expect.objectContaining({ newConversation: true }),
    )
  })
})
