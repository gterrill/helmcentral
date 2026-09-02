import { describe, expect, it } from 'vitest'
import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

// The spoke relay is a WebSocket on the same /api prefix as every REST call.
// Vite's dev proxy does not forward upgrade requests unless the entry opts in
// with `ws: true`, and it fails silently: the socket never opens, no error is
// logged, and the overlay simply shows nothing.
//
// Measured 2026-09-02 before the fix: through :5173 the browser got 0 frames
// in 8 s, while the same request straight to the Go backend on :8080 got 19.
// Production is unaffected, because there the backend serves the frontend
// itself and no proxy sits in the path, which is exactly what makes this the
// kind of gap that reaches a boat undetected.
describe('vite dev proxy', () => {
  it('forwards websocket upgrades on /api, or the spoke relay is dead in dev', () => {
    const configPath = resolve(dirname(fileURLToPath(import.meta.url)), '../../vite.config.ts')
    const config = readFileSync(configPath, 'utf8')

    const start = config.indexOf("'/api'")
    expect(start).toBeGreaterThan(-1)
    // To the end of the proxy entry, not a fixed window: the explanatory
    // comment above `ws: true` is long, and a fixed slice silently stops
    // covering the thing it is meant to assert.
    const apiBlock = config.slice(start, config.indexOf('build:', start))
    expect(apiBlock).toContain('ws: true')
  })
})
