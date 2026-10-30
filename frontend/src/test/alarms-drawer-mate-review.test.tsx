import { fireEvent, render, screen, within } from '@testing-library/react'
import { describe, expect, it, vi, type Mock } from 'vitest'

import { AlarmsDrawer } from '@/components/alarms-drawer'
import { formatAppLocation } from '@/lib/app-location'
import { newAlarmRuleDraft, type AlarmRule, type AlarmRuleDraft } from '@/hooks/use-alarm-rules'
import type { ActiveAlarm } from '@/hooks/use-alarms'
import { installFakeTelemetryStream } from './helpers/fake-telemetry-stream'

installFakeTelemetryStream()

vi.mock('@/hooks/use-alarm-rules', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/hooks/use-alarm-rules')>()),
  useAlarmLog: () => ({ entries: [], refresh: vi.fn().mockResolvedValue(undefined) }),
}))

vi.mock('@/hooks/use-signalk-paths', () => ({ useSignalKPaths: () => ({ paths: [] }) }))

function rule(overrides: Partial<AlarmRule> = {}): AlarmRule {
  return {
    id: 'rule-1', enabled: true, path: 'environment.pressureRate', label: 'Barometer falling', op: 'below', value: -0.03,
    hysteresis: 0, dwell_seconds: 10, stale_after_seconds: 0, state: 'warn', methods: ['visual', 'sound'], notify: null,
    escalate_after_seconds: 0, ...overrides,
  }
}

function alarm(overrides: Partial<ActiveAlarm> = {}): ActiveAlarm {
  return {
    rule_id: 'rule-1', label: 'House bank low', path: 'electrical.batteries.house.voltage', phase: 'active', state: 'alarm', value: 11.6,
    message: 'House bank low', silenced: false, can_silence: false, can_acknowledge: true, ...overrides,
  }
}

type UpdateRuleMock = Mock<(id: string, draft: AlarmRuleDraft) => Promise<void>>

function renderDrawer(props: { rules?: AlarmRule[]; alarms?: ActiveAlarm[]; mateOn?: boolean; updateRule?: UpdateRuleMock; onOpenMateReview?: (id: string) => void } = {}) {
  render(
    <AlarmsDrawer
      alarms={props.alarms ?? []}
      onAcknowledge={vi.fn()}
      onSilence={vi.fn()}
      rules={props.rules ?? []}
      loading={false}
      error={null}
      createRule={vi.fn().mockResolvedValue(undefined)}
      updateRule={props.updateRule ?? vi.fn().mockResolvedValue(undefined)}
      deleteRule={vi.fn()}
      collisionTuningUrl={null}
      forecastWarnings={null}
      mateOn={props.mateOn}
      onOpenMateReview={props.onOpenMateReview}
    />,
  )
}

describe('the Mate reviews it switch', () => {
  it('is off in a new rule', () => {
    expect(newAlarmRuleDraft().mate_reviews).toBe(false)
  })

  it('is shown but cannot be turned on while Mate is off, with a line saying why', () => {
    renderDrawer({ rules: [rule()], mateOn: false })
    fireEvent.click(screen.getByRole('button', { name: /edit barometer falling/i }))
    const box = screen.getByLabelText(/mate reviews it/i) as HTMLInputElement
    expect(box.checked).toBe(false)
    expect(box.disabled).toBe(true)
    expect(screen.getByText("Mate is off, so it won't review this")).toBeInTheDocument()
  })

  it('can still be turned off while Mate is off', async () => {
    const updateRule: UpdateRuleMock = vi.fn<(id: string, draft: AlarmRuleDraft) => Promise<void>>().mockResolvedValue(undefined)
    renderDrawer({ rules: [rule({ mate_reviews: true })], mateOn: false, updateRule })
    fireEvent.click(screen.getByRole('button', { name: /edit barometer falling/i }))
    const box = screen.getByLabelText(/mate reviews it/i) as HTMLInputElement
    expect(box.checked).toBe(true)
    expect(box.disabled).toBe(false)
    fireEvent.click(box)
    fireEvent.click(screen.getByRole('button', { name: /save rule/i }))
    await vi.waitFor(() => expect(updateRule).toHaveBeenCalled())
    expect(updateRule.mock.calls[0][1]).toMatchObject({ mate_reviews: false })
  })

  it('shows no "Mate is off" line while Mate is on', () => {
    renderDrawer({ rules: [rule()], mateOn: true })
    fireEvent.click(screen.getByRole('button', { name: /edit barometer falling/i }))
    expect(screen.queryByText(/Mate is off/)).toBeNull()
  })

  it('is shown while Mate is on, and is saved with the rule', async () => {
    const updateRule: UpdateRuleMock = vi.fn<(id: string, draft: AlarmRuleDraft) => Promise<void>>().mockResolvedValue(undefined)
    renderDrawer({ rules: [rule()], mateOn: true, updateRule })
    fireEvent.click(screen.getByRole('button', { name: /edit barometer falling/i }))

    const box = screen.getByLabelText(/mate reviews it/i) as HTMLInputElement
    expect(box.checked).toBe(false)
    fireEvent.click(box)
    fireEvent.click(screen.getByRole('button', { name: /save rule/i }))

    await vi.waitFor(() => expect(updateRule).toHaveBeenCalled())
    expect(updateRule.mock.calls[0][0]).toBe('rule-1')
    expect(updateRule.mock.calls[0][1]).toMatchObject({ mate_reviews: true })
  })

  it('shows a rule that already has it on as checked', () => {
    renderDrawer({ rules: [rule({ mate_reviews: true })], mateOn: true })
    fireEvent.click(screen.getByRole('button', { name: /edit barometer falling/i }))
    expect((screen.getByLabelText(/mate reviews it/i) as HTMLInputElement).checked).toBe(true)
  })

  it('keeps a rule\'s setting when it is saved while Mate is off', async () => {
    const updateRule: UpdateRuleMock = vi.fn<(id: string, draft: AlarmRuleDraft) => Promise<void>>().mockResolvedValue(undefined)
    renderDrawer({ rules: [rule({ mate_reviews: true })], mateOn: false, updateRule })
    fireEvent.click(screen.getByRole('button', { name: /edit barometer falling/i }))
    fireEvent.click(screen.getByRole('button', { name: /save rule/i }))
    await vi.waitFor(() => expect(updateRule).toHaveBeenCalled())
    expect(updateRule.mock.calls[0][1]).toMatchObject({ mate_reviews: true })
  })
})

describe('the alarm card and Mate\'s review', () => {
  it('shows "Mate reviewed this" with the first sentence, linking to the conversation', () => {
    const open = vi.fn()
    renderDrawer({
      alarms: [alarm({ mate_review: { state: 'done', conversation_id: 'conv-9', summary: 'The bank is at 11.6 V and still falling.', cost_usd: 0.012, cost_known: true } })],
      onOpenMateReview: open,
    })
    const link = screen.getByRole('link', { name: /mate reviewed this/i })
    expect(link).toHaveAttribute('href', formatAppLocation({ panel: 'assistant', conversationId: 'conv-9' }, { firstPageId: null }))
    expect(link).toHaveAttribute('href', '/mate/conv-9')
    expect(screen.getByText(/The bank is at 11.6 V and still falling\./)).toBeInTheDocument()
    expect(screen.getByText('$0.01')).toBeInTheDocument()
    fireEvent.click(link)
    expect(open).toHaveBeenCalledWith('conv-9')
  })

  it('says the review failed, with why', () => {
    renderDrawer({ alarms: [alarm({ mate_review: { state: 'failed', problem: 'Mate is not ready (the assistant is switched off)', cost_known: true } })] })
    const card = screen.getByText('House bank low').closest('div[class*="rounded-md"]') as HTMLElement
    expect(within(card).getByText(/Mate's review failed/)).toBeInTheDocument()
    expect(within(card).getByText(/the assistant is switched off/)).toBeInTheDocument()
    expect(within(card).queryByRole('link', { name: /mate reviewed this/i })).toBeNull()
  })

  it('says Mate is reviewing while it runs', () => {
    renderDrawer({ alarms: [alarm({ mate_review: { state: 'running', cost_known: false } })] })
    expect(screen.getByText(/Mate is reviewing this/)).toBeInTheDocument()
  })

  it('draws nothing for an alarm no one reviewed', () => {
    renderDrawer({ alarms: [alarm()] })
    expect(screen.queryByText(/mate reviewed this/i)).toBeNull()
  })
})
