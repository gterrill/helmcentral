import { describe, it, expect } from 'vitest'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { getUiConfig, getAnchorConfig } from '@/config/app-config'

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
    expect(getUiConfig()).toEqual({
      distanceUnits: 'metric',
      vesselStateRefreshSeconds: 10,
      autoCloseAnchorWatchOnEngine: true,
    })
    expect(getAnchorConfig()).toEqual({
      bowRollerHeightM: 1.5,
      chainSizeMm: 12,
      chainOnboardM: 150,
      hullType: 'power_cat',
      windageAreaM2: 35,
    })
  })
})
