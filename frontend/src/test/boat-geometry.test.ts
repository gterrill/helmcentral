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

  describe('power_cat', () => {
    const build = (loa: number, beam: number) => buildBoat('power_cat', { loaM: loa, beamM: beam }, THREE)
    const named = (loa: number, beam: number) => {
      const out = new Map<string, Mesh[]>()
      build(loa, beam).group.traverse((o) => {
        if ((o as Mesh).isMesh) out.set(o.name, [...(out.get(o.name) ?? []), o as Mesh])
      })
      return out
    }

    it('has the superstructure of a flybridge catamaran', () => {
      const parts = named(16.8, 8.2)
      for (const n of ['hull', 'hull-windows', 'bridgedeck', 'deck', 'cockpit', 'stern-step', 'swim-platform', 'tender-lift', 'cabin', 'windows', 'flybridge', 'flybridge-top', 'helm', 'helm-windows', 'hardtop', 'post', 'hardtop-lip', 'radar-dome']) {
        expect(parts.has(n), n).toBe(true)
      }
      expect(parts.get('hull')).toHaveLength(2)
    })

    it('stacks salon, flybridge deck and hardtop, set back from the bows', () => {
      const parts = named(16.8, 8.2)
      const box = (n: string) => new Box3().setFromObject(parts.get(n)![0])
      const loa = 16.8
      // Salon from just forward of the transom doors to about 0.71 LOA from the stern.
      expect(box('cabin').min.y / loa).toBeCloseTo(-0.129, 2)
      expect(box('cabin').max.y / loa).toBeCloseTo(0.225, 2)
      expect(box('flybridge').min.z).toBeGreaterThanOrEqual(box('cabin').max.z - 1e-6)
      expect(box('flybridge').min.y / loa).toBeCloseTo(-0.42, 2)
      expect(box('flybridge').max.y / loa).toBeCloseTo(0.255, 2)
      expect(box('hardtop').min.z).toBeGreaterThan(box('flybridge-top').max.z)
      expect(box('helm').min.z).toBeGreaterThanOrEqual(box('flybridge-top').max.z - 1e-6)
      expect(box('helm').max.z).toBeLessThanOrEqual(box('hardtop').min.z + 1e-6)
      expect(box('helm-windows').getSize(new Vector3()).x).toBeGreaterThan(box('helm').getSize(new Vector3()).x)
      // The foredeck is solid out to about 0.92 LOA from the stern.
      expect(box('bridgedeck').max.y / loa).toBeCloseTo(0.42, 2)
      expect(box('hardtop').min.z / loa).toBeCloseTo(0.31, 2)
      expect(box('hardtop').max.z / loa).toBeLessThan(0.33)
    })

    it('keeps its proportions as fractions of LOA and beam', () => {
      const a = new Box3().setFromObject(build(10, 5).group).getSize(new Vector3())
      const b = new Box3().setFromObject(build(30, 15).group).getSize(new Vector3())
      expect(b.x / a.x).toBeCloseTo(3, 2)
      expect(b.y / a.y).toBeCloseTo(3, 2)
      expect(b.z / a.z).toBeCloseTo(3, 2)
    })

    it('draws slender demihulls with nothing deeper than the draft', () => {
      const loa = 16.8
      const box = new Box3().setFromObject(build(loa, 8.2).group)
      expect(box.min.z).toBeGreaterThanOrEqual(-loa * 0.05)
      expect(box.min.z).toBeLessThan(-loa * 0.03)
      const hull = new Box3().setFromObject(named(loa, 8.2).get('hull')![0]).getSize(new Vector3())
      expect(hull.x).toBeLessThan(8.2 * 0.4)
      expect(named(loa, 8.2).has('hull-windows')).toBe(true)
      expect(hull.y).toBeCloseTo(loa, 1)
    })

    it('stays a light marker', () => {
      let tris = 0
      build(16.8, 8.2).group.traverse((o) => {
        const m = o as Mesh
        if (!m.isMesh) return
        tris += (m.geometry.index ? m.geometry.index.count : m.geometry.getAttribute('position').count) / 3
      })
      expect(tris).toBeLessThan(2000)
    })
  })
})
