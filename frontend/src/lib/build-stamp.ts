export interface AppVersion {
  version: string
  revision: string
}

declare const __APP_VERSION__: string
declare const __APP_REVISION__: string

/**
 * The build this page's own bundle was made from. The release build passes
 * `APP_VERSION` and `APP_REVISION` to Vite (see the Dockerfile and the
 * GoReleaser hook); anything built without them is "dev" / "unknown".
 *
 * The page must know its own build from the bundle, not from `/api/health`:
 * the backend answers for whatever is deployed now, which after an update is
 * the new build, so a page that took its baseline from the first health read
 * would believe it was current even when loaded from a stale cache.
 */
export const BUNDLE_BUILD: AppVersion = { version: __APP_VERSION__, revision: __APP_REVISION__ }

/** A bundle built without a stamp (local dev, an ad hoc `npm run build`). */
export const isUnstamped = (build: AppVersion): boolean => build.version === 'dev'
