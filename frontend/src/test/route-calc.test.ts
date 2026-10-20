import { describe, it, expect } from 'vitest'
import { formatEtaHours, formatNm } from '@/lib/route-calc'

describe('formatNm', () => {
  it('shows two decimals under 10 nm and one above', () => {
    expect(formatNm(1852 * 2.5)).toBe('2.50 nm')
    expect(formatNm(1852 * 12.34)).toBe('12.3 nm')
  })
})

describe('formatEtaHours', () => {
  it('formats minutes, and hours with padded minutes', () => {
    expect(formatEtaHours(0.5)).toBe('30m')
    expect(formatEtaHours(10)).toBe('10h 00m')
  })

  it('has no time for an infinite duration', () => {
    expect(formatEtaHours(Infinity)).toBe('—')
  })
})
