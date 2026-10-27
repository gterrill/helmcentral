import { useCallback, useEffect, useMemo, useRef, useState, memo } from 'react'

import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog'
import { Button } from '@/components/ui/button'
import { useSettingsForm } from '@/hooks/use-settings-form'
import { useTelemetryStatus } from '@/hooks/use-telemetry-stream'
import { useSignalKDiscovery, type DiscoveredServer } from '@/hooks/use-signalk-discovery'
import { useVesselStateSelector, type VesselStateValue } from '@/hooks/use-vessel-state'

const DISMISSED_KEY = 'helmcentral.signalk-discovery-dismissed-for'
const UNCONFIGURED_KEY = '__unconfigured__'

/**
 * How long vessel state must stay `signalk-unreachable`, without a single
 * other reading in between and while the vessel-state stream is connected,
 * before the saved server counts as gone. An update restart answers
 * "unreachable" for a few seconds while the new backend connects, and that must
 * not look like a moved server. Vessel state is pushed on change, not polled,
 * so this is a timer and not a count of polls.
 */
export const UNREACHABLE_GRACE_MS = 30_000

/** How long to wait before searching again after a search failed outright. */
export const SEARCH_RETRY_MS = 60_000

/** The port the app uses when the saved settings carry none. */
const DEFAULT_SIGNALK_PORT = 3000

interface SignalKDiscoveryPromptProps {
  /** `source` from /api/vessel-state — "signalk-unreachable" means the saved server isn't answering. */
  vesselStateSource: string | null
}

function isUnconfigured(address: string): boolean {
  const trimmed = address.trim()
  return trimmed === '' || trimmed === 'localhost'
}

/** The saved server, by the server's own verdict or, failing that, by plain address and port. */
function isConfiguredServer(server: DiscoveredServer, address: string, port: number): boolean {
  return (
    server.configured === true ||
    (server.address.trim().toLowerCase() === address.trim().toLowerCase() && server.port === port)
  )
}

/**
 * Offers to find SignalK when the operator would otherwise be stuck typing an
 * IP address: on a fresh install, and when a previously-working server stops
 * answering (it moved, or was replaced).
 *
 * Deliberately narrow about when it speaks up. The search runs quietly in the
 * background and interrupts only once it has something actionable — a server
 * the operator can accept. It does not put a modal over the dashboard to
 * announce that it is looking (a sweep takes seconds), nor to report that it
 * found nothing: neither is worth blocking the app for, and manual entry in
 * Settings → SignalK is always there.
 *
 * Rules:
 * - It reads the saved address and port from the same useSettingsForm
 *   instance it saves through, and does nothing until that has loaded, so a
 *   failed settings read can never look like an unconfigured boat.
 * - Unconfigured (empty address or "localhost"): searches at once.
 * - Unreachable: searches only after the source has stayed unreachable for
 *   UNREACHABLE_GRACE_MS while the vessel-state stream is connected. Any other
 *   reading, or a dropped stream, restarts the clock.
 * - The server already saved is never offered, as marked by the backend (which
 *   resolves a saved hostname) or matched by address and port. If nothing else
 *   is found there is no dialog and no dismissal.
 * - A search that completes counts as done for this situation, whatever it
 *   found. One that fails is tried again after SEARCH_RETRY_MS, while the
 *   situation persists.
 * - A dismissal is remembered against address:port. An older address-only
 *   dismissal still counts for port 3000.
 *
 * Accepting saves immediately via useSettingsForm().save(), the single write
 * path (ADR 0028). That is consistent with removing the old Connect button:
 * what was wrong there was saving as a side effect of a connectivity *check*,
 * not saving in response to a deliberate "yes, store this".
 */
export function SignalKDiscoveryPrompt({ vesselStateSource }: SignalKDiscoveryPromptProps) {
  const { servers, discover } = useSignalKDiscovery()
  const { settings, loaded, save } = useSettingsForm()
  const streamConnected = useTelemetryStatus() === 'connected'

  const [open, setOpen] = useState(false)
  const [saving, setSaving] = useState(false)
  const [saveError, setSaveError] = useState<string | null>(null)
  // The situation (dismissal key) a search is running or has completed for.
  const searchedFor = useRef<string | null>(null)
  // Set when a search failed outright; makes the next attempt wait SEARCH_RETRY_MS.
  const failedFor = useRef<string | null>(null)
  const [failures, setFailures] = useState(0)

  const configuredAddress = settings.signalk?.address ?? ''
  const configuredPort = settings.signalk?.port ?? DEFAULT_SIGNALK_PORT

  const unconfigured = isUnconfigured(configuredAddress)
  const unreachable = vesselStateSource === 'signalk-unreachable'
  // Without settings an empty address says nothing about the boat.
  const shouldOffer = loaded && (unconfigured || unreachable)

  // Scoped to the situation the dismissal was about.
  const trimmedAddress = configuredAddress.trim()
  const dismissalKey = unconfigured ? UNCONFIGURED_KEY : `${trimmedAddress}:${configuredPort}`
  const legacyDismissalKey = !unconfigured && configuredPort === DEFAULT_SIGNALK_PORT ? trimmedAddress : null

  const offered = useMemo(
    () => (servers ?? []).filter((server) => !isConfiguredServer(server, configuredAddress, configuredPort)),
    [servers, configuredAddress, configuredPort],
  )

  useEffect(() => {
    if (!shouldOffer) return
    // Only the unreachable case waits, and only while the stream is delivering.
    if (!unconfigured && !streamConnected) return
    if (searchedFor.current === dismissalKey) return
    const dismissed = globalThis.localStorage?.getItem(DISMISSED_KEY)
    if (dismissed === dismissalKey || (legacyDismissalKey !== null && dismissed === legacyDismissalKey)) return

    const search = () => {
      searchedFor.current = dismissalKey
      void discover()
        .then((found) => {
          failedFor.current = null
          // Only interrupt if there is something to accept.
          if (found.some((server) => !isConfiguredServer(server, configuredAddress, configuredPort))) setOpen(true)
        })
        .catch(() => {
          // A background search the operator never asked for shouldn't raise a
          // modal when it fails; Settings → SignalK remains the manual path.
          // It may be a restart still in progress, so allow another go.
          searchedFor.current = null
          failedFor.current = dismissalKey
          setFailures((count) => count + 1)
        })
    }

    const delay = failedFor.current === dismissalKey ? SEARCH_RETRY_MS : unconfigured ? 0 : UNREACHABLE_GRACE_MS
    if (delay === 0) {
      search()
      return
    }
    // Cleanup runs when the source, the stream or the settings change, which is what restarts the clock.
    const timer = setTimeout(search, delay)
    return () => clearTimeout(timer)
  }, [shouldOffer, unconfigured, streamConnected, dismissalKey, legacyDismissalKey, discover, configuredAddress, configuredPort, failures])

  const dismiss = useCallback(() => {
    globalThis.localStorage?.setItem(DISMISSED_KEY, dismissalKey)
    setOpen(false)
  }, [dismissalKey])

  const accept = useCallback(
    async (server: DiscoveredServer) => {
      // Never save over settings that have not been read.
      if (!loaded) return
      setSaving(true)
      setSaveError(null)
      try {
        await save({ signalk: { address: server.address, port: server.port } })
        setOpen(false)
      } catch (err) {
        setSaveError(err instanceof Error ? err.message : 'Unable to save these settings')
      } finally {
        setSaving(false)
      }
    },
    [save, loaded],
  )

  if (!open) return null

  const found = offered
  const single = found.length === 1 ? found[0] : null

  return (
    <AlertDialog open onOpenChange={(next) => { if (!next) dismiss() }}>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>SignalK found</AlertDialogTitle>
          <AlertDialogDescription>
            {single ? (
              <>
                We found SignalK for <strong>{single.vessel_name || 'an unnamed vessel'}</strong> at{' '}
                <strong>{`${single.address}:${single.port}`}</strong>. Want to store these connection settings?
              </>
            ) : (
              'Several SignalK servers answered. Choose the one for this vessel:'
            )}
          </AlertDialogDescription>
        </AlertDialogHeader>

        {found.length > 1 && (
          <div className="flex flex-col gap-2">
            {found.map((server) => (
              <Button
                key={`${server.address}:${server.port}`}
                variant="outline"
                className="justify-between"
                disabled={saving || !loaded}
                onClick={() => void accept(server)}
              >
                <span>{server.vessel_name || 'Unnamed vessel'}</span>
                <span className="text-muted-foreground">{`${server.address}:${server.port}`}</span>
              </Button>
            ))}
          </div>
        )}

        {saveError && (
          <div className="rounded-md border border-destructive/30 bg-destructive/5 px-3 py-2 text-xs uppercase tracking-[0.08em] text-destructive">
            {saveError}
          </div>
        )}

        <AlertDialogFooter>
          <AlertDialogCancel onClick={dismiss}>Not now</AlertDialogCancel>
          {single && (
            <AlertDialogAction disabled={saving || !loaded} onClick={() => void accept(single)}>
              {saving ? 'Saving…' : 'Store these settings'}
            </AlertDialogAction>
          )}
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  )
}

const selectSource = (state: VesselStateValue) => state.source

/** Offers to look for a SignalK server that has moved, from which source the server answered. */
export const LiveSignalKDiscoveryPrompt = memo(function LiveSignalKDiscoveryPrompt() {
  const vesselStateSource = useVesselStateSelector(selectSource)
  return <SignalKDiscoveryPrompt vesselStateSource={vesselStateSource} />
})
