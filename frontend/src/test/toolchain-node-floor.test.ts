import { describe, expect, it } from 'vitest'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'

// Vite 7 requires node ^20.19.0 || >=22.12.0. Two images build the frontend and
// neither is exercised by a normal dev loop, so a base image below that floor
// breaks somewhere a developer will not notice locally:
//
//   - ../Dockerfile        the release image, built by the Release workflow
//   - ./frontend/Dockerfile  the `frontend` service of compose's prod profile
//
// Pin both against the vite major here.
const packageJson = JSON.parse(readFileSync(resolve('package.json'), 'utf8')) as {
  devDependencies?: Record<string, string>
}

const dockerfiles = {
  'Dockerfile (release image)': readFileSync(resolve('..', 'Dockerfile'), 'utf8'),
  'frontend/Dockerfile (compose prod)': readFileSync(resolve('Dockerfile'), 'utf8'),
}

function majorOf(range: string | undefined) {
  const m = range?.match(/(\d+)/)
  return m ? Number(m[1]) : 0
}

describe('frontend toolchain', () => {
  it.each(Object.entries(dockerfiles))('%s builds on a node new enough for the pinned vite', (_name, contents) => {
    const bases = [...contents.matchAll(/^FROM\s+(?:--platform=\S+\s+)?node:(\d+)/gm)].map((m) => Number(m[1]))
    expect(bases.length).toBeGreaterThan(0)
    for (const major of bases) {
      expect(major).toBeGreaterThanOrEqual(20)
    }
  })

  it('is on a vite branch that still receives security fixes', () => {
    // Vite supports `latest` and `previous` only; 5.x and 6.x are both EOL,
    // and the path-traversal advisory GHSA-4w7w-66w2-5vf9 (<=6.4.1) has no
    // fixed 5.x release to move to.
    expect(majorOf(packageJson.devDependencies?.vite)).toBeGreaterThanOrEqual(8)
  })
})
