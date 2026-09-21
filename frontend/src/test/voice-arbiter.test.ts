import { describe, it, expect, beforeEach, vi } from 'vitest'

// ADR 0122. Fresh module per test (vi.resetModules pattern - same as
// mate-watch-store.test.ts) so the module-level refcount/listeners never
// leak between assertions.
async function loadArbiterModule() {
  vi.resetModules()
  return import('@/lib/voice-arbiter')
}

describe('voice-arbiter', () => {
  beforeEach(() => {
    vi.resetModules()
  })

  it('starts unclaimed', async () => {
    const { getVoiceClaimSnapshot } = await loadArbiterModule()
    expect(getVoiceClaimSnapshot()).toBe(false)
  })

  it('claimVoice() claims it', async () => {
    const { claimVoice, getVoiceClaimSnapshot } = await loadArbiterModule()

    claimVoice()

    expect(getVoiceClaimSnapshot()).toBe(true)
  })

  it('the release function returned by claimVoice() releases it', async () => {
    const { claimVoice, getVoiceClaimSnapshot } = await loadArbiterModule()

    const release = claimVoice()
    release()

    expect(getVoiceClaimSnapshot()).toBe(false)
  })

  it('stays claimed until every overlapping claim has released', async () => {
    const { claimVoice, getVoiceClaimSnapshot } = await loadArbiterModule()

    const releaseFirst = claimVoice()
    const releaseSecond = claimVoice()
    expect(getVoiceClaimSnapshot()).toBe(true)

    releaseFirst()
    expect(getVoiceClaimSnapshot()).toBe(true)

    releaseSecond()
    expect(getVoiceClaimSnapshot()).toBe(false)
  })

  it('calling a release function twice has no further effect', async () => {
    const { claimVoice, getVoiceClaimSnapshot } = await loadArbiterModule()

    const releaseFirst = claimVoice()
    claimVoice()
    releaseFirst()
    releaseFirst()

    // Still one outstanding claim - the double release did not
    // over-decrement past it.
    expect(getVoiceClaimSnapshot()).toBe(true)
  })

  it('notifies subscribers on claim and on release', async () => {
    const { claimVoice, subscribeVoiceClaim } = await loadArbiterModule()
    let calls = 0
    subscribeVoiceClaim(() => { calls += 1 })

    const release = claimVoice()
    expect(calls).toBe(1)

    release()
    expect(calls).toBe(2)
  })

  it('does not notify again for an overlapping claim/release that does not change the outcome', async () => {
    const { claimVoice, subscribeVoiceClaim } = await loadArbiterModule()
    const releaseFirst = claimVoice()
    let calls = 0
    subscribeVoiceClaim(() => { calls += 1 })

    const releaseSecond = claimVoice() // already claimed - no transition
    expect(calls).toBe(0)

    releaseSecond() // still one outstanding claim - no transition
    expect(calls).toBe(0)

    releaseFirst() // the last one - this is the real transition
    expect(calls).toBe(1)
  })

  it('stops notifying once unsubscribed', async () => {
    const { claimVoice, subscribeVoiceClaim } = await loadArbiterModule()
    let calls = 0
    const unsubscribe = subscribeVoiceClaim(() => { calls += 1 })

    unsubscribe()
    claimVoice()

    expect(calls).toBe(0)
  })
})

// ADR 0122 bug fix: push-to-talk preempting an active dictation claim
// (hooks/use-mate-voice.ts's pushToTalk, components/dictation.tsx's
// subscription). A separate, one-way signal from claim/release - preempting
// does not itself change refCount, only tells a claimant to let go.
describe('voice-arbiter preempt', () => {
  it('notifies preempt subscribers when preemptVoice() is called', async () => {
    const { preemptVoice, subscribeVoicePreempt } = await loadArbiterModule()
    let calls = 0
    subscribeVoicePreempt(() => { calls += 1 })

    preemptVoice()

    expect(calls).toBe(1)
  })

  it('preemptVoice() does not itself change the claim - only a subscriber releasing it does', async () => {
    const { claimVoice, preemptVoice, getVoiceClaimSnapshot } = await loadArbiterModule()
    claimVoice()

    preemptVoice()

    expect(getVoiceClaimSnapshot()).toBe(true)
  })

  it('a claimant that releases on preempt actually lifts the claim', async () => {
    const { claimVoice, preemptVoice, subscribeVoicePreempt, getVoiceClaimSnapshot } = await loadArbiterModule()
    const release = claimVoice()
    subscribeVoicePreempt(() => { release() })

    preemptVoice()

    expect(getVoiceClaimSnapshot()).toBe(false)
  })

  it('stops notifying preempt once unsubscribed', async () => {
    const { preemptVoice, subscribeVoicePreempt } = await loadArbiterModule()
    let calls = 0
    const unsubscribe = subscribeVoicePreempt(() => { calls += 1 })

    unsubscribe()
    preemptVoice()

    expect(calls).toBe(0)
  })
})
