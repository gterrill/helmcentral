/**
 * Utility to initialize and unlock AudioContext early.
 * This must be called from a user gesture handler (click, touch) to comply with browser autoplay policy.
 * Once called, the AudioContext is ready for audio playback even after the user gesture ends.
 */
export async function primeAudioContextForAlarm(): Promise<void> {
  try {
    const ctx = new (window.AudioContext || (window as any).webkitAudioContext)()
    if (ctx.state === 'suspended') {
      await ctx.resume()
      console.log('[audio-utils] AudioContext primed and resumed')
    } else {
      console.log('[audio-utils] AudioContext already in', ctx.state, 'state')
    }
  } catch (err) {
    console.error('[audio-utils] Failed to prime AudioContext:', err)
  }
}
