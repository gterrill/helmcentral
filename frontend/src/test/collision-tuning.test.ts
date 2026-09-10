import { describe, expect, it } from 'vitest'

import { collisionTuningUrl } from '@/lib/collision-tuning'

describe('collisionTuningUrl', () => {
  it('builds an http URL from a bare host and port', () => {
    expect(collisionTuningUrl('192.168.50.240', 3000)).toBe(
      'http://192.168.50.240:3000/signalk-ais-target-prioritizer/',
    )
  })

  it('keeps an address that already has a scheme, ignores the port, and avoids a double slash', () => {
    expect(collisionTuningUrl('http://pikorua.local/', 3000)).toBe(
      'http://pikorua.local/signalk-ais-target-prioritizer/',
    )
  })

  it('falls back to the default SignalK port when none is given', () => {
    expect(collisionTuningUrl('192.168.50.240', undefined)).toBe(
      'http://192.168.50.240:3000/signalk-ais-target-prioritizer/',
    )
  })

  it('returns null for an empty address', () => {
    expect(collisionTuningUrl('', 3000)).toBeNull()
  })

  it('returns null for an undefined address', () => {
    expect(collisionTuningUrl(undefined, 3000)).toBeNull()
  })

  it('returns null for a whitespace-only address', () => {
    expect(collisionTuningUrl('   ', 3000)).toBeNull()
  })
})
