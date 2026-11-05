import { describe, it, expect } from 'vitest'
import { renderHook, act } from '@testing-library/react'

import { useMateSheetRequest } from '@/hooks/use-mate-sheet-request'
import type { AssistantScreenContext } from '@/hooks/use-assistant-chat'

// ADR 0257: the open request behind the Mate sheet - what it was opened for
// and how long that lasts. App.tsx holds one of these; the app-*.test.tsx
// files prove the wiring, these prove the lifecycle.
const SCREEN: AssistantScreenContext = { panel: 'dashboard' }
const ALARM = { id: 'alarm-1', title: 'Bilge high' }
const RUN = { id: 'run-9', name: 'Morning watch' }

function setup(screen: AssistantScreenContext | undefined = SCREEN) {
  return renderHook(({ s }) => useMateSheetRequest(s), { initialProps: { s: screen } })
}

describe('useMateSheetRequest', () => {
  it('starts closed, empty and unmounted', () => {
    const { result } = setup()
    expect(result.current.sheetProps.open).toBe(false)
    expect(result.current.hasOpened).toBe(false)
    expect(result.current.sheetProps.initialQuestion).toBeUndefined()
    expect(result.current.sheetProps.screen).toEqual(SCREEN)
  })

  it('opens with a plain question', () => {
    const { result } = setup()
    act(() => result.current.ask('How is the bank?'))
    expect(result.current.sheetProps).toMatchObject({
      open: true, initialQuestion: 'How is the bank?', newConversation: false, draft: false, spoken: false,
    })
    expect(result.current.hasOpened).toBe(true)
  })

  it('opens with no question at all', () => {
    const { result } = setup()
    act(() => result.current.ask())
    expect(result.current.sheetProps.open).toBe(true)
    expect(result.current.sheetProps.initialQuestion).toBeUndefined()
  })

  it('carries the new-conversation, draft and spoken flags', () => {
    const { result } = setup()
    act(() => result.current.ask('q', { newConversation: true, draft: true, spoken: true }))
    expect(result.current.sheetProps).toMatchObject({ newConversation: true, draft: true, spoken: true })
  })

  it('puts an alarm on the screen context', () => {
    const { result } = setup()
    act(() => result.current.ask('Why?', { alarm: ALARM }))
    expect(result.current.sheetProps.screen).toEqual({ panel: 'dashboard', alarm_id: 'alarm-1', alarm_title: 'Bilge high' })
  })

  it('puts a watch run on the screen context', () => {
    const { result } = setup()
    act(() => result.current.ask('Why?', { watchRun: RUN }))
    expect(result.current.sheetProps.screen).toEqual({ panel: 'dashboard', watch_run_id: 'run-9', watch_name: 'Morning watch' })
  })

  it('follows the screen the operator moves to while the context stays', () => {
    const { result, rerender } = setup()
    act(() => result.current.ask('Why?', { alarm: ALARM }))
    rerender({ s: { panel: 'alarms' } })
    expect(result.current.sheetProps.screen).toMatchObject({ panel: 'alarms', alarm_id: 'alarm-1' })
  })

  it('closing resets the question, flags and both contexts', () => {
    const { result } = setup()
    act(() => result.current.ask('q', { newConversation: true, draft: true, spoken: true, alarm: ALARM, watchRun: RUN }))
    act(() => result.current.sheetProps.onOpenChange(false))
    expect(result.current.sheetProps).toMatchObject({
      open: false, initialQuestion: undefined, newConversation: false, draft: false, spoken: false,
    })
    expect(result.current.sheetProps.screen).toEqual(SCREEN)
  })

  it('reopening with nothing never re-sends a stale question', () => {
    const { result } = setup()
    act(() => result.current.ask('old question', { alarm: ALARM }))
    act(() => result.current.sheetProps.onOpenChange(false))
    act(() => result.current.ask())
    expect(result.current.sheetProps.open).toBe(true)
    expect(result.current.sheetProps.initialQuestion).toBeUndefined()
    expect(result.current.sheetProps.screen).toEqual(SCREEN)
  })

  it('an unsent draft does not tie the next chat to its run', () => {
    const { result } = setup()
    act(() => result.current.ask('Draft text', { draft: true, watchRun: RUN }))
    act(() => result.current.sheetProps.onOpenChange(false))
    act(() => result.current.ask('Something else'))
    expect(result.current.sheetProps.screen).toEqual(SCREEN)
  })

  it('opening again replaces the previous request whole', () => {
    const { result } = setup()
    act(() => result.current.ask('first', { alarm: ALARM, spoken: true }))
    act(() => result.current.ask('second', { watchRun: RUN }))
    expect(result.current.sheetProps).toMatchObject({ initialQuestion: 'second', spoken: false })
    expect(result.current.sheetProps.screen).toEqual({ panel: 'dashboard', watch_run_id: 'run-9', watch_name: 'Morning watch' })
  })

  it('drops the context once the sheet reports it done, keeping the sheet open', () => {
    const { result } = setup()
    act(() => result.current.ask('Why?', { alarm: ALARM, watchRun: RUN }))
    act(() => result.current.sheetProps.onContextDone(result.current.sheetProps.requestId))
    expect(result.current.sheetProps.open).toBe(true)
    expect(result.current.sheetProps.screen).toEqual(SCREEN)
  })

  it('stays mounted once opened, across closes', () => {
    const { result } = setup()
    act(() => result.current.ask())
    act(() => result.current.sheetProps.onOpenChange(false))
    expect(result.current.hasOpened).toBe(true)
  })

  it('keeps the same screen object while the base screen and context are unchanged', () => {
    const { result, rerender } = setup()
    act(() => result.current.ask('q', { alarm: ALARM }))
    const before = result.current.sheetProps.screen
    rerender({ s: SCREEN })
    expect(result.current.sheetProps.screen).toBe(before)
  })

  it('gives every ask its own request id, even for the same words', () => {
    const { result } = setup()
    act(() => result.current.ask('same words'))
    const first = result.current.sheetProps.requestId
    act(() => result.current.ask('same words'))
    expect(result.current.sheetProps.requestId).not.toBe(first)
  })

  it('ignores a context-done from a request that is no longer current', () => {
    const { result } = setup()
    act(() => result.current.ask('about run A', { watchRun: RUN }))
    const staleId = result.current.sheetProps.requestId
    act(() => result.current.sheetProps.onOpenChange(false))
    const runB = { id: 'run-B', name: 'Evening watch' }
    act(() => result.current.ask('Draft for B', { draft: true, watchRun: runB }))
    act(() => result.current.sheetProps.onContextDone(staleId))
    expect(result.current.sheetProps.screen).toMatchObject({ watch_run_id: 'run-B' })
  })

  it('ignores a context-done from the request before a replacing ask', () => {
    const { result } = setup()
    act(() => result.current.ask('first', { alarm: ALARM }))
    const staleId = result.current.sheetProps.requestId
    act(() => result.current.ask('second', { alarm: { id: 'alarm-2', title: 'Other' } }))
    act(() => result.current.sheetProps.onContextDone(staleId))
    expect(result.current.sheetProps.screen).toMatchObject({ alarm_id: 'alarm-2' })
  })

  it('does not re-render for a context-done when the request has no context', () => {
    let renders = 0
    const { result } = renderHook(() => {
      renders += 1
      return useMateSheetRequest(SCREEN)
    })
    act(() => result.current.ask('plain'))
    const before = renders
    act(() => result.current.sheetProps.onContextDone(result.current.sheetProps.requestId))
    expect(renders).toBe(before)
  })

  it('shares the context of an ask only through the kinds it declares', () => {
    const { result } = setup()
    act(() => result.current.ask('q', { alarm: ALARM, watchRun: RUN }))
    expect(result.current.sheetProps.screen).toMatchObject({ alarm_id: 'alarm-1', watch_run_id: 'run-9' })
  })
})
