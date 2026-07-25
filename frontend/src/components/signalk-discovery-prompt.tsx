import { useCallback, useEffect, useRef, useState } from 'react'

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
import { useSignalKDiscovery, type DiscoveredServer } from '@/hooks/use-signalk-discovery'

const DISMISSED_KEY = 'helmcentral.signalk-discovery-dismissed-for'

interface SignalKDiscoveryPromptProps {
  /** Currently persisted SignalK address; empty or "localhost" means unconfigured. */
  configuredAddress: string
  /** `source` from /api/vessel-state — "signalk-unreachable" means the saved server isn't answering. */
  vesselStateSource: string | null
}

function isUnconfigured(address: string): boolean {
  const trimmed = address.trim()
  return trimmed === '' || trimmed === 'localhost'
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
 * Settings → SignalK is always there. It searches once per mount, and
 * remembers a dismissal against the address it was dismissed for — so
 * declining doesn't gag it when a *different* server later fails, which would
 * be a different problem going unreported.
 *
 * Accepting saves immediately via useSettingsForm().save(), the single write
 * path (ADR 0028). That is consistent with removing the old Connect button:
 * what was wrong there was saving as a side effect of a connectivity *check*,
 * not saving in response to a deliberate "yes, store this".
 */
export function SignalKDiscoveryPrompt({ configuredAddress, vesselStateSource }: SignalKDiscoveryPromptProps) {
  const { servers, discover } = useSignalKDiscovery()
  const { save } = useSettingsForm()

  const [open, setOpen] = useState(false)
  const [saving, setSaving] = useState(false)
  const [saveError, setSaveError] = useState<string | null>(null)
  // One search per mount: the unreachable state persists across every poll, and
  // re-sweeping the network on each one would be a scan storm.
  const searchedFor = useRef<string | null>(null)

  const unconfigured = isUnconfigured(configuredAddress)
  const unreachable = vesselStateSource === 'signalk-unreachable'
  const shouldOffer = unconfigured || unreachable

  // Keyed by address so a dismissal is scoped to the situation it was about.
  const dismissalKey = unconfigured ? '__unconfigured__' : configuredAddress.trim()

  useEffect(() => {
    if (!shouldOffer) return
    if (searchedFor.current === dismissalKey) return
    if (globalThis.localStorage?.getItem(DISMISSED_KEY) === dismissalKey) return

    searchedFor.current = dismissalKey
    void discover()
      .then((found) => {
        // Only interrupt if there is something to accept.
        if (found.length > 0) setOpen(true)
      })
      .catch(() => {
        // A background search the operator never asked for shouldn't raise a
        // modal when it fails. Settings → SignalK remains the manual path.
      })
  }, [shouldOffer, dismissalKey, discover])

  const dismiss = useCallback(() => {
    globalThis.localStorage?.setItem(DISMISSED_KEY, dismissalKey)
    setOpen(false)
  }, [dismissalKey])

  const accept = useCallback(
    async (server: DiscoveredServer) => {
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
    [save],
  )

  if (!open) return null

  const found = servers ?? []
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
                disabled={saving}
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
            <AlertDialogAction disabled={saving} onClick={() => void accept(single)}>
              {saving ? 'Saving…' : 'Store these settings'}
            </AlertDialogAction>
          )}
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  )
}
