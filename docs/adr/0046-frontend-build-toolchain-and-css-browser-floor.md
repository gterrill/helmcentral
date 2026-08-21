# ADR 0046: Vite 8 makes the CSS browser floor an explicit decision, set to Baseline 2024

## Status

Accepted.

## Context

The frontend build sat on Vite 5.4.21 for a long time. Vite supports only its `latest` and `previous` branches — currently 8.x and 7.x — so 5.x had stopped receiving fixes, and [GHSA-4w7w-66w2-5vf9](https://github.com/advisories/GHSA-4w7w-66w2-5vf9) (path traversal in optimized-deps `.map` handling, affecting `<=6.4.1`) had no fixed 5.x release to move to. The 6.x line was patched; the 5.x line simply ended inside the affected range.

That advisory and the esbuild dev-server one never shipped: `frontend/Dockerfile` serves a static `dist/` through `http-server` and runs no Vite. The exposure was to the development machine only.

Vite 7 was taken first, deliberately, as a bump with no engine change — still Rollup and esbuild, and `@vitejs/plugin-react` 4.7.0 already declared `^7.0.0`. Vite 8 was then evaluated separately because it replaces Rollup with Rolldown and esbuild with oxc, which is where real breakage would surface against a ~2.3 MB bundle carrying `maplibre-gl`, `recharts` and `react-grid-layout`.

**Vite 8 does not build this project as configured.** `build.target: 'ES2020'` is a JS language target. Vite 8 minifies CSS with lightningcss, which takes browser versions instead, and inheriting the JS target fails the build outright:

```
[plugin vite:css-post]
Error: [lightningcss minify] Unsupported target "ES2020"
```

Under Vite 5 and 7 this never came up — esbuild accepted the same value for both. So upgrading forces a question the build had never had to answer: **which browsers must the shipped CSS actually support?**

## Decision

### 1. Vite 8, with the CSS floor set explicitly to Baseline 2024

```ts
build: {
  target: 'ES2020',
  cssTarget: ['chrome111', 'edge111', 'firefox111', 'safari16.4'],
}
```

These versions encode the Baseline 2024 target that `AGENTS.md` already sets for this project, so the build now enforces the policy the repo had only stated. **This drops Safari below 16.4**, and with it iPads too old to run iPadOS 16.4.

Two alternatives were measured and rejected:

- **ES2020-era browsers** (`chrome80`, `safari13.1`) would have preserved whatever compatibility the old build incidentally had, at 78.89 kB gzip. Rejected because "whatever it happened to do before" is not a support policy, and the project already has one written down.
- **`esnext`**, no downlevelling at all, at 78.78 kB gzip. Rejected as relying on every viewing device being current, with silent breakage as the failure mode.

The spread across all three is 0.12 kB gzip. **The choice is about which devices are supported, not about output size** — which is precisely why it belongs in an ADR rather than in a build file alone.

### 2. `build.target` stays `ES2020` for JS

Only the CSS pipeline changed. The JS target is untouched, so this upgrade moves no JS compatibility boundary.

## Consequences

- Safari 16.3 and earlier no longer receive downlevelled CSS. On a boat this means an iPad that cannot reach iPadOS 16.4 is no longer a supported helm display. If one turns up, the fix is to widen `cssTarget`, not to pin Vite.
- Builds got substantially faster: ~6.3s to ~1.4s, Rolldown against Rollup.
- The client bundle shrank from 654 kB gzip to 620 kB.
- `@vitejs/plugin-react` moves to 6.x, whose `oxc-transform-react`, `@rolldown/plugin-babel` and `babel-plugin-react-compiler` peers are all declared optional and are not installed. React Fast Refresh was verified working without them.
- `frontend/src/test/toolchain-node-floor.test.ts` pins the Vite major against the node major in `frontend/Dockerfile`. Vite 7 raised the node floor to `^20.19.0 || >=22.12.0` while the production image was still `node:18-alpine`, a mismatch that would only have surfaced at release time; that test exists so the two cannot drift apart again.
