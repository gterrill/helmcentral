import { describe, it, expect } from 'vitest'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { normalizeUiConfig, normalizeAnchorConfig, normalizeMayaraConfig } from '@/config/app-config'

// Vitest runs with the frontend package root as cwd; the jsdom environment
// leaves import.meta.url as a non-file URL, so resolve from cwd instead.
const appConfigSource = readFileSync(resolve('src/config/app-config.ts'), 'utf8')

// app-config used to read the repo-root settings.yaml through an eager
// `import.meta.glob('?raw')`, which inlined that file's *contents* into the JS
// bundle at build time. On a machine with a real settings.yaml that meant every
// artifact built from source carried the builder's private InfluxDB URL, LAN
// addresses and vessel details — verified by grepping a `npm run build` bundle.
// Shipping open-source release archives made that a disclosure risk, not just a
// staleness one, so the bake is gone for good.
describe('app-config', () => {
  // Matches the mechanisms that inline a file's contents into the bundle, not
  // the string "settings.yaml" itself — the comments in app-config.ts name the
  // file precisely so this stays removed.
  it('never inlines settings.yaml at build time', () => {
    expect(appConfigSource).not.toMatch(/import\.meta\.glob/)
    expect(appConfigSource).not.toMatch(/\?raw/)
  })

  it('resolves to documented defaults with no bundled config', () => {
    expect(normalizeUiConfig(null)).toEqual({
      distanceUnits: 'metric',
      autoCloseAnchorWatchOnEngine: true,
    })
    expect(normalizeAnchorConfig(null)).toEqual({
      bowRollerHeightM: 1.5,
      chainSizeMm: 12,
      chainOnboardM: 150,
      hullType: 'power_cat',
      scopeMethod: 'ratio',
      windageAreaM2: 35,
      gpsFromBowM: 0,
      loaM: 0,
    })
  })

  // The map reads mayara's address through useAppConfig(), not through the
  // settings form, so this is the most likely place the address silently
  // never reaches the map — normalizeMayaraConfig needs the same per-field
  // validation discipline as normalizeUiConfig/normalizeAnchorConfig above.
  describe('normalizeMayaraConfig', () => {
    it('defaults a missing block to a blank address and port 6502', () => {
      expect(normalizeMayaraConfig(null)).toEqual({ address: '', port: 6502 })
      expect(normalizeMayaraConfig({})).toEqual({ address: '', port: 6502 })
    })

    it('passes through a configured address and port', () => {
      expect(normalizeMayaraConfig({ mayara: { address: '192.168.50.81', port: 6503 } })).toEqual({
        address: '192.168.50.81',
        port: 6503,
      })
    })

    it('rejects a non-string address rather than propagating it', () => {
      expect(normalizeMayaraConfig({ mayara: { address: 12345 as unknown as string, port: 6503 } })).toEqual({
        address: '',
        port: 6503,
      })
    })

    it('defaults a bad port rather than propagating it', () => {
      expect(normalizeMayaraConfig({ mayara: { address: '192.168.50.81', port: 0 } }).port).toBe(6502)
      expect(normalizeMayaraConfig({ mayara: { address: '192.168.50.81', port: 70000 } }).port).toBe(6502)
      expect(normalizeMayaraConfig({ mayara: { address: '192.168.50.81', port: Number.NaN } }).port).toBe(6502)
      expect(normalizeMayaraConfig({ mayara: { address: '192.168.50.81', port: '6503' as unknown as number } }).port).toBe(6502)
    })
  })
})
