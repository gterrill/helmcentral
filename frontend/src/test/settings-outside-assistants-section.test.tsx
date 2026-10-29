import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { OutsideAssistantsSection } from '@/components/settings/sections/outside-assistants-section'
import { SETTINGS_SECTIONS } from '@/components/settings/settings-nav'
import { SETTINGS_HELP_TARGETS } from '@/lib/help-links'
import type { OutsideAssistantActivity, OutsideAssistantToken, OutsideAssistantTool } from '@/hooks/use-outside-assistants'
import { installFakeTelemetryStream } from './helpers/fake-telemetry-stream'

// Times on this page are the boat's (ADR 0230). The runner is on UTC.
installFakeTelemetryStream({ seeds: { 'vessel-state': { timezone: 'UTC', timezone_source: 'fix' } } })

const hook = vi.hoisted(() => ({
  tokens: [] as unknown[],
  tools: [] as unknown[],
  activity: [] as unknown[],
  loading: false,
  error: null as string | null,
  activityError: null as string | null,
  create: vi.fn(),
  setTools: vi.fn(),
  revoke: vi.fn(),
  refreshActivity: vi.fn(),
  refreshError: null as string | null,
}))

vi.mock('@/hooks/use-outside-assistants', () => ({
  useOutsideAssistants: () => hook,
}))

const tools: OutsideAssistantTool[] = [
  { name: 'get_vessel_status', plain_name: 'Vessel status', default_on: true },
  { name: 'get_active_alarms', plain_name: 'Active alarms', default_on: true },
  { name: 'check_signalk_paths', plain_name: 'Live SignalK paths', default_on: false },
]

const laptop: OutsideAssistantToken = {
  id: 't1',
  name: 'Laptop',
  created_at: '2026-10-29T09:00:00Z',
  last_used_at: '2026-10-30T08:30:00Z',
  tools: ['get_active_alarms', 'get_vessel_status'],
}

beforeEach(() => {
  hook.tokens = []
  hook.tools = tools
  hook.activity = []
  hook.loading = false
  hook.error = null
  hook.activityError = null
  hook.refreshError = null
  hook.create = vi.fn().mockResolvedValue({ token: laptop, secret: 'hcmcp_SECRETVALUE' })
  hook.setTools = vi.fn().mockResolvedValue(undefined)
  hook.revoke = vi.fn().mockResolvedValue(undefined)
  Object.defineProperty(navigator, 'clipboard', { value: { writeText: vi.fn().mockResolvedValue(undefined) }, configurable: true })
})

describe('Outside assistants section', () => {
  it('is a settings section with a help target, worded for the operator', () => {
    expect(SETTINGS_SECTIONS.find((s) => s.id === 'outside-assistants')?.label).toBe('Outside assistants')
    expect(SETTINGS_HELP_TARGETS['outside-assistants']).toEqual({ page: 'features/outside-assistants' })
  })

  it('says plainly there are no tokens yet and that /mcp is closed', () => {
    render(<OutsideAssistantsSection />)
    expect(screen.getByText(/no outside assistant can connect/i)).toBeInTheDocument()
  })

  it('creates a token from a name and shows the secret once with the ready-to-paste commands', async () => {
    render(<OutsideAssistantsSection />)
    fireEvent.change(screen.getByLabelText('Name'), { target: { value: 'Laptop' } })
    fireEvent.click(screen.getByRole('button', { name: 'Create token' }))
    await waitFor(() => expect(hook.create).toHaveBeenCalledWith('Laptop'))

    const dialog = await screen.findByRole('dialog')
    expect(within(dialog).getByText(/won't be shown again/i)).toBeInTheDocument()
    expect(within(dialog).getByDisplayValue('hcmcp_SECRETVALUE')).toBeInTheDocument()
    const command = `claude mcp add --transport http helmcentral ${window.location.origin}/mcp --header "Authorization: Bearer hcmcp_SECRETVALUE"`
    expect(within(dialog).getByText(command)).toBeInTheDocument()
    expect(within(dialog).getByText(/"mcpServers"/)).toBeInTheDocument()

    fireEvent.click(within(dialog).getByRole('button', { name: 'Copy token' }))
    await waitFor(() => expect(navigator.clipboard.writeText).toHaveBeenCalledWith('hcmcp_SECRETVALUE'))
    fireEvent.click(within(dialog).getByRole('button', { name: 'Copy Claude Code command' }))
    await waitFor(() => expect(navigator.clipboard.writeText).toHaveBeenCalledWith(command))
  })

  it('will not create a token without a name', () => {
    render(<OutsideAssistantsSection />)
    expect(screen.getByRole('button', { name: 'Create token' })).toBeDisabled()
  })

  it('lists each token with its last use and plain-named tool switches', () => {
    hook.tokens = [laptop]
    render(<OutsideAssistantsSection />)
    expect(screen.getByText('Laptop')).toBeInTheDocument()
    expect(screen.getByText(/last used 30 Oct 2026 08:30/i)).toBeInTheDocument()
    expect(screen.getByRole('switch', { name: 'Vessel status for Laptop' })).toBeChecked()
    expect(screen.getByRole('switch', { name: 'Live SignalK paths for Laptop' })).not.toBeChecked()
  })

  it('says a token has never been used', () => {
    hook.tokens = [{ ...laptop, last_used_at: null }]
    render(<OutsideAssistantsSection />)
    expect(screen.getByText(/never used/i)).toBeInTheDocument()
  })

  it('switching a tool sends the token its whole new list', async () => {
    hook.tokens = [laptop]
    render(<OutsideAssistantsSection />)
    fireEvent.click(screen.getByRole('switch', { name: 'Live SignalK paths for Laptop' }))
    await waitFor(() => expect(hook.setTools).toHaveBeenCalledWith('t1', ['get_active_alarms', 'get_vessel_status', 'check_signalk_paths']))
    fireEvent.click(screen.getByRole('switch', { name: 'Vessel status for Laptop' }))
    await waitFor(() => expect(hook.setTools).toHaveBeenLastCalledWith('t1', ['get_active_alarms']))
  })

  it('two quick switches both persist, the second built from the first', async () => {
    hook.tokens = [laptop]
    let releaseFirst: () => void = () => {}
    hook.setTools = vi
      .fn()
      .mockImplementationOnce(() => new Promise<void>((resolve) => { releaseFirst = resolve }))
      .mockResolvedValue(undefined)
    render(<OutsideAssistantsSection />)
    fireEvent.click(screen.getByRole('switch', { name: 'Live SignalK paths for Laptop' }))
    fireEvent.click(screen.getByRole('switch', { name: 'Active alarms for Laptop' }))
    // Both switches already show the operator's two choices.
    expect(screen.getByRole('switch', { name: 'Live SignalK paths for Laptop' })).toBeChecked()
    expect(screen.getByRole('switch', { name: 'Active alarms for Laptop' })).not.toBeChecked()
    await waitFor(() => expect(hook.setTools).toHaveBeenCalledTimes(1))
    releaseFirst()
    await waitFor(() => expect(hook.setTools).toHaveBeenCalledTimes(2))
    expect(hook.setTools).toHaveBeenNthCalledWith(1, 't1', ['get_active_alarms', 'get_vessel_status', 'check_signalk_paths'])
    expect(hook.setTools).toHaveBeenNthCalledWith(2, 't1', ['get_vessel_status', 'check_signalk_paths'])
  })

  it('shows a failed list refresh without losing the secret dialog', async () => {
    hook.refreshError = 'Could not refresh the token list: network down'
    render(<OutsideAssistantsSection />)
    expect(screen.getByRole('alert')).toHaveTextContent('network down')
  })

  it('revokes only after a confirmation', async () => {
    hook.tokens = [laptop]
    render(<OutsideAssistantsSection />)
    fireEvent.click(screen.getByRole('button', { name: 'Revoke Laptop' }))
    expect(hook.revoke).not.toHaveBeenCalled()
    const dialog = await screen.findByRole('alertdialog')
    fireEvent.click(within(dialog).getByRole('button', { name: 'Revoke' }))
    await waitFor(() => expect(hook.revoke).toHaveBeenCalledWith('t1'))
  })

  it('lists recent activity with the client, what was asked and the outcome', () => {
    const entry: OutsideAssistantActivity = {
      at: '2026-10-30T08:30:00Z', client: 'Laptop', kind: 'tool', name: 'get_vessel_status', outcome: 'ok', duration_ms: 42,
    }
    hook.activity = [entry, { ...entry, name: 'search_web', outcome: 'refused' }]
    render(<OutsideAssistantsSection />)
    const log = screen.getByRole('list', { name: 'Recent activity' })
    expect(within(log).getAllByRole('listitem')).toHaveLength(2)
    expect(within(log).getByText(/Vessel status/)).toBeInTheDocument()
    expect(within(log).getByText(/refused/i)).toBeInTheDocument()
  })

  it('shows a server error, such as a role that may not manage tokens', () => {
    hook.error = 'this action requires admin access; your session has readonly'
    render(<OutsideAssistantsSection />)
    expect(screen.getByRole('alert')).toHaveTextContent('requires admin access')
  })

  it('never says widget', () => {
    hook.tokens = [laptop]
    const { container } = render(<OutsideAssistantsSection />)
    expect(container.textContent?.toLowerCase()).not.toContain('widget')
  })
})
