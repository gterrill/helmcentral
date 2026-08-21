import { describe, expect, it } from 'vitest'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'

// Vite 7 requires node ^20.19.0 || >=22.12.0. The production image builds and
// serves the dashboard itself, so if its base image drops below that floor the
// break shows up as a failed release build, not as anything a dev would notice
// locally — the dev container is on node 20. Pin the two together here.
const dockerfile = readFileSync(resolve('Dockerfile'), 'utf8')
const packageJson = JSON.parse(readFileSync(resolve('package.json'), 'utf8')) as {
  devDependencies?: Record<string, string>
}

function majorOf(range: string | undefined) {
  const m = range?.match(/(\d+)/)
  return m ? Number(m[1]) : 0
}

describe('frontend toolchain', () => {
  it('builds on a node new enough for the pinned vite', () => {
    const bases = [...dockerfile.matchAll(/^FROM\s+node:(\d+)/gm)].map((m) => Number(m[1]))
    expect(bases.length).toBeGreaterThan(0)
    for (const major of bases) {
      expect(major).toBeGreaterThanOrEqual(20)
    }
  })

  it('is on a vite branch that still receives security fixes', () => {
    // Vite supports `latest` and `previous` only; 5.x and 6.x are both EOL,
    // and the path-traversal advisory GHSA-4w7w-66w2-5vf9 (<=6.4.1) has no
    // fixed 5.x release to move to.
    expect(majorOf(packageJson.devDependencies?.vite)).toBeGreaterThanOrEqual(7)
  })
})
