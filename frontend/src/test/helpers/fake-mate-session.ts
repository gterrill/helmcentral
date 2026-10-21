import { vi } from 'vitest'

import type { MateSession } from '@/hooks/use-mate-session'

type FakeMateSessionOverrides = Partial<Omit<MateSession, 'status'>> & {
  status?: Partial<MateSession['status']>
}

/**
 * A Mate session (use-mate-session.ts) for testing a component that is a
 * view over one, such as AssistantThread: an open conversation `c1` with no
 * messages, nothing sending, every action a mock. `status` overrides merge
 * into the idle status rather than replacing it.
 */
export function fakeMateSession(overrides: FakeMateSessionOverrides = {}): MateSession {
  const { status, ...rest } = overrides
  return {
    conversations: [],
    activeId: 'c1',
    messages: [],
    summaryNoteId: null,
    setSummaryNoteId: vi.fn(),
    conversationLoading: false,
    loading: false,
    errorMessage: null,
    send: vi.fn().mockResolvedValue({ answered: false, message: null }),
    stop: vi.fn(),
    select: vi.fn().mockResolvedValue(undefined),
    startNew: vi.fn(),
    remove: vi.fn().mockResolvedValue(undefined),
    reload: vi.fn().mockResolvedValue(undefined),
    rejoin: vi.fn().mockResolvedValue(undefined),
    isAnswering: vi.fn().mockReturnValue(false),
    updateProposal: vi.fn(),
    updateFormDraft: vi.fn(),
    liveAnswerIds: new Set<string>(),
    ratings: {},
    rateAnswer: vi.fn().mockResolvedValue(null),
    ...rest,
    status: {
      sending: false,
      statusText: null,
      draft: null,
      error: null,
      stopped: false,
      deliveredMessageId: null,
      ...status,
    },
  }
}
