import { describe, expect, it } from 'vitest'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'

// Asserted against the config *source*, the same way
// app-config-no-build-time-bake.test.ts does, rather than by importing
// vite.config.ts. Importing it would pull in @vitejs/plugin-react and
// therefore esbuild, which asserts that
// `new TextEncoder().encode('') instanceof Uint8Array` — false under this
// suite's jsdom environment, because that typed array comes from another
// realm. Switching this one file to the node environment does not help
// either: the suite-wide setupFiles entry touches HTMLElement.prototype.
const viteConfigSource = readFileSync(resolve('vite.config.ts'), 'utf8')
const packageJson = JSON.parse(readFileSync(resolve('package.json'), 'utf8')) as {
  devDependencies?: Record<string, string>
}

// Vite has no React component boundary to swap without this plugin, so every
// save under src/ degrades to a full page reload and drops component state —
// an open drawer, a half-filled settings form, map pan and zoom. The config
// went without it for a long time; this keeps it from silently going missing
// again.
describe('vite react plugin', () => {
  it('imports @vitejs/plugin-react', () => {
    expect(viteConfigSource).toMatch(/from '@vitejs\/plugin-react'/)
  })

  it('registers it in the plugins array', () => {
    expect(viteConfigSource).toMatch(/plugins:\s*\[[^\]]*react\(\)/)
  })

  it('declares the dependency', () => {
    expect(packageJson.devDependencies?.['@vitejs/plugin-react']).toBeDefined()
  })
})
