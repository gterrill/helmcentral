// Checks that a frontend's node_modules holds exactly what its
// package-lock.json pins, by comparing the lockfile with the record npm keeps
// of the last install (node_modules/.package-lock.json). A worktree's
// node_modules is a link to the main checkout's, which lags whenever
// origin/main changes a dependency. An optional package built for another
// platform is never installed here, so its absence is skipped.
// Usage: node scripts/check-frontend-deps.mjs <frontend dir>
import { lstatSync, readFileSync } from 'node:fs'
import { join } from 'node:path'

const dir = process.argv[2]
if (!dir) {
  console.error('usage: node scripts/check-frontend-deps.mjs <frontend dir>')
  process.exit(2)
}

function packages(path) {
  try {
    return JSON.parse(readFileSync(join(dir, path), 'utf8')).packages
  } catch (err) {
    console.error(`cannot read ${join(dir, path)}: ${err.message}`)
    process.exit(1)
  }
}

// npm's os/cpu fields list allowed values, or excluded ones prefixed with "!".
const allows = (list, value) =>
  !list || (list.some((v) => !v.startsWith('!')) ? list.includes(value) : !list.includes(`!${value}`))
const forThisPlatform = (pkg) => allows(pkg.os, process.platform) && allows(pkg.cpu, process.arch)

const locked = packages('package-lock.json')
const installed = packages('node_modules/.package-lock.json')

const stale = []
for (const [path, pkg] of Object.entries(locked)) {
  if (path === '') continue
  const have = installed[path]
  if (!have && pkg.optional && !forThisPlatform(pkg)) continue
  if (have?.version !== pkg.version) {
    stale.push(`${path}: locked ${pkg.version}, installed ${have?.version ?? 'none'}`)
  } else if (pkg.resolved && have.resolved && pkg.resolved !== have.resolved) {
    stale.push(`${path}: locked from ${pkg.resolved}, installed from ${have.resolved}`)
  }
}
for (const path of Object.keys(installed)) {
  if (!(path in locked)) stale.push(`${path}: installed ${installed[path].version}, not in the lockfile`)
}

if (stale.length) {
  console.error(`${dir}/node_modules does not match package-lock.json (${stale.length}):`)
  for (const line of stale.slice(0, 20)) console.error(`  ${line}`)
  if (stale.length > 20) console.error(`  ...and ${stale.length - 20} more`)
  const linked = lstatSync(join(dir, 'node_modules')).isSymbolicLink()
  console.error(
    linked
      ? 'node_modules is a shared link: run `rm node_modules && npm ci` here, never npm ci or npm install through the link.'
      : 'Run `npm ci` here.',
  )
  process.exit(1)
}
