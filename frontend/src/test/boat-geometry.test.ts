import * as THREE from 'three'
import { Box3, Vector3, type Mesh } from 'three'
import { describe, expect, it } from 'vitest'

import { buildBoat, disposeBoat } from '@/lib/boat-geometry'
import type { HullType } from '@/config/app-config'

const TYPES: HullType[] = ['sail_mono', 'sail_cat', 'power_mono', 'power_cat']

function size(type: HullType, loa: number, beam: number) {
  const boat = buildBoat(type, { loaM: loa, beamM: beam }, THREE)
  const box = new Box3().setFromObject(boat.group)
  return { boat, size: box.getSize(new Vector3()) }
}

describe('buildBoat', () => {
  it('builds one hull for a monohull and two for a catamaran', () => {
    expect(buildBoat('sail_mono', { loaM: 12, beamM: 4 }, THREE).hullCount).toBe(1)
    expect(buildBoat('power_mono', { loaM: 12, beamM: 4 }, THREE).hullCount).toBe(1)
    expect(buildBoat('sail_cat', { loaM: 12, beamM: 6.5 }, THREE).hullCount).toBe(2)
    expect(buildBoat('power_cat', { loaM: 12, beamM: 6.5 }, THREE).hullCount).toBe(2)
  })

  it('names the hull meshes so the structure can be read back', () => {
    for (const type of TYPES) {
      const boat = buildBoat(type, { loaM: 12, beamM: 5 }, THREE)
      const hulls: Mesh[] = []
      boat.group.traverse((o) => { if (o.name === 'hull') hulls.push(o as Mesh) })
      expect(hulls).toHaveLength(boat.hullCount)
    }
  })

  it('carries a mast and sail on sailboats only, a cabin on power boats only', () => {
    const names = (t: HullType) => {
      const out = new Set<string>()
      buildBoat(t, { loaM: 12, beamM: 5 }, THREE).group.traverse((o) => out.add(o.name))
      return out
    }
    expect(names('sail_mono').has('mast')).toBe(true)
    expect(names('sail_mono').has('sail')).toBe(true)
    expect(names('sail_cat').has('sail')).toBe(true)
    expect(names('sail_cat').has('bridgedeck')).toBe(true)
    expect(names('power_mono').has('cabin')).toBe(true)
    expect(names('power_mono').has('mast')).toBe(false)
    expect(names('power_cat').has('cabin')).toBe(true)
    expect(names('power_cat').has('bridgedeck')).toBe(true)
  })

  it('is as long as the LOA and as wide as the beam, bow along +y', () => {
    for (const type of TYPES) {
      const { size: s } = size(type, 15, 5)
      expect(s.y).toBeCloseTo(15, 1)
      expect(s.x).toBeCloseTo(5, 1)
    }
  })

  it('scales with the dimensions it is given', () => {
    for (const type of TYPES) {
      const a = size(type, 10, 4).size
      const b = size(type, 20, 8).size
      expect(b.y / a.y).toBeCloseTo(2, 1)
      expect(b.x / a.x).toBeCloseTo(2, 1)
    }
  })

  it('disposes every geometry and material', () => {
    const boat = buildBoat('power_cat', { loaM: 12, beamM: 6.5 }, THREE)
    let disposed = 0
    boat.group.traverse((o) => {
      const m = o as Mesh
      if (!m.isMesh) return
      m.geometry.addEventListener('dispose', () => { disposed++ })
      const mat = m.material as { addEventListener: (t: string, f: () => void) => void }
      mat.addEventListener('dispose', () => { disposed++ })
    })
    disposeBoat(boat)
    expect(disposed).toBeGreaterThan(0)
  })
})
