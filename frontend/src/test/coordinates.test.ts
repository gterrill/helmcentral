import { describe, expect, it } from 'vitest'

import { formatCoordinateDM, formatPositionDM, parseCoordinate } from '@/lib/coordinates'

describe('formatCoordinateDM', () => {
  it('writes degrees and decimal minutes with the hemisphere', () => {
    expect(formatCoordinateDM(-20.5446, 'lat')).toBe("20°32.676' S")
    expect(formatCoordinateDM(149.11893, 'lon')).toBe("149°07.136' E")
    expect(formatCoordinateDM(-5.5, 'lon')).toBe("005°30.000' W")
    expect(formatPositionDM(-20.5446, 149.1189)).toBe("20°32.676' S 149°07.134' E")
  })

  it('carries a rounded-up minute into the degrees', () => {
    expect(formatCoordinateDM(20.9999999, 'lat')).toBe("21°00.000' N")
  })
})

describe('parseCoordinate', () => {
  it("reads the operator's own format", () => {
    expect(parseCoordinate("020º32.676' S", 'lat')).toBeCloseTo(-20.5446, 6)
    expect(parseCoordinate("149º07.136' E", 'lon')).toBeCloseTo(149.118933, 5)
  })

  it('reads the spellings of the same position', () => {
    expect(parseCoordinate("20°32.676'S", 'lat')).toBeCloseTo(-20.5446, 6)
    expect(parseCoordinate('20 32.676 s', 'lat')).toBeCloseTo(-20.5446, 6)
    expect(parseCoordinate('20°32′40.56″ S', 'lat')).toBeCloseTo(-20.5446, 6)
    expect(parseCoordinate('-20.5446', 'lat')).toBe(-20.5446)
    expect(parseCoordinate('20.5446 S', 'lat')).toBe(-20.5446)
    expect(parseCoordinate('149.5', 'lon')).toBe(149.5)
    expect(parseCoordinate("5°30' W", 'lon')).toBe(-5.5)
  })

  it('round-trips what it writes', () => {
    const text = formatCoordinateDM(-20.8163, 'lat')
    expect(parseCoordinate(text, 'lat')).toBeCloseTo(-20.8163, 4)
  })

  it('refuses what it cannot read exactly', () => {
    for (const bad of ['', 'abc', "20°75' S", '91', '20 E', '-20 S', '1 2 3 4', '20.5°30\' S', 'SS 20']) {
      expect(parseCoordinate(bad, 'lat'), bad).toBeNull()
    }
    expect(parseCoordinate('181', 'lon')).toBeNull()
    expect(parseCoordinate('149 N', 'lon')).toBeNull()
  })
})
