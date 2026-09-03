import { useAppVersion } from '@/hooks/use-app-version'

/**
 * The build stamp, parked in the sidebar footer so "what version am I on?" is
 * answerable from any screen without opening Settings.
 *
 * Only the version gets width — the git revision rides along in the title
 * attribute, where it's there for a support conversation without crowding a
 * sidebar that also has to survive being collapsed to icons. When the probe
 * fails this says so rather than showing a stale or invented version, per the
 * fallback policy in AGENTS.md.
 */
export function SidebarVersion() {
  const { build, loading, error } = useAppVersion()

  if (loading) return null

  const label = build ? build.version : 'version unavailable'
  const title = build
    ? `Helmcentral ${build.version} (${build.revision})`
    : `Helmcentral version unavailable: ${error ?? 'unknown error'}`

  return (
    <div
      data-testid="sidebar-version"
      title={title}
      className="truncate px-2 pb-1 text-center font-mono text-[0.6875rem] leading-none text-sidebar-foreground/50 group-data-[collapsible=icon]:hidden"
    >
      {label}
    </div>
  )
}
