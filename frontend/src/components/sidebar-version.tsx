import { useAppVersion } from '@/hooks/use-app-version'
import { releaseNotesUrl } from '@/lib/help-links'

/**
 * The build stamp, parked in the sidebar footer so "what version am I on?" is
 * answerable from any screen without opening Settings.
 *
 * Only the version gets width — the git revision rides along in the title
 * attribute, where it's there for a support conversation without crowding a
 * sidebar that also has to survive being collapsed to icons. When the probe
 * fails this says so rather than showing a stale or invented version, per the
 * fallback policy in AGENTS.md. A release tag links to that release's notes;
 * dev and between-release builds have none, so they stay plain text.
 */
export function SidebarVersion() {
  const { build, loading, error } = useAppVersion()

  if (loading) return null

  const label = build ? build.version : 'version unavailable'
  const title = build
    ? `Helmcentral ${build.version} (${build.revision})`
    : `Helmcentral version unavailable: ${error ?? 'unknown error'}`

  const className =
    'truncate px-2 pb-1 text-center font-mono text-xs leading-none text-muted-foreground group-data-[collapsible=icon]:hidden'
  const notesUrl = build ? releaseNotesUrl(build.version) : null

  if (notesUrl) {
    return (
      <a
        data-testid="sidebar-version"
        href={notesUrl}
        target="_blank"
        rel="noopener noreferrer"
        title={title}
        aria-label={`Release notes for ${label}`}
        className={`block hover:underline focus-visible:rounded-sm focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring ${className}`}
      >
        {label}
      </a>
    )
  }

  return (
    <div data-testid="sidebar-version" title={title} className={className}>
      {label}
    </div>
  )
}
