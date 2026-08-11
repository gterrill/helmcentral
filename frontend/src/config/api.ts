/**
 * Base URL every `/api` call is prefixed with.
 *
 * Empty by default, i.e. same-origin relative requests. That is correct for
 * every way this app actually ships: the release binary serves the go:embed-ed
 * SPA and the API from one Echo instance on `PORT`, and the Docker image does
 * the same behind whatever port the operator published. Pinning a port here
 * would break any install not on 8080, and any install behind a reverse proxy.
 *
 * `npm run dev` keeps working because vite.config.ts proxies /api to the
 * backend, so relative paths resolve from the dev server too.
 *
 * VITE_API_BASE_URL remains as a build-time escape hatch for the one case
 * relative paths can't express: serving the frontend from a genuinely
 * different origin than the backend.
 */
export const apiBaseUrl = import.meta.env.VITE_API_BASE_URL ?? ''
