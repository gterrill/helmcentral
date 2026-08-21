import { memo, useState } from 'react'

import { Button } from '@/components/ui/button'
import { Field, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { useWebPush } from '@/hooks/use-web-push'

/**
 * Web push has two independent levels, and conflating them is the obvious
 * usability failure: the transport is on or off for the whole boat, while each
 * browser separately registers itself as a recipient. Turning the transport on
 * does not make this phone receive anything.
 */
interface WebPushSectionProps {
  enabled: boolean
  onEnabledChange: (enabled: boolean) => void
}

function defaultDeviceName(): string {
  if (typeof navigator === 'undefined') return ''
  const ua = navigator.userAgent
  if (/iPhone/.test(ua)) return 'iPhone'
  if (/iPad/.test(ua)) return 'iPad'
  if (/Android/.test(ua)) return 'Android phone'
  if (/Macintosh/.test(ua)) return 'Mac'
  if (/Windows/.test(ua)) return 'PC'
  return 'This device'
}

export const WebPushSection = memo(function WebPushSection({ enabled, onEnabledChange }: WebPushSectionProps) {
  const { support, subscribed, deviceCount, busy, error, enableOnThisDevice, disableOnThisDevice } = useWebPush()
  const [label, setLabel] = useState(defaultDeviceName)

  return (
    <section className="rounded-md border bg-background/60 p-3">
      <label
        htmlFor="webpush-enabled"
        className="flex items-center gap-2 text-[11px] uppercase tracking-[0.16em] text-muted-foreground"
      >
        <input
          id="webpush-enabled"
          type="checkbox"
          checked={enabled}
          onChange={(e) => onEnabledChange(e.target.checked)}
        />
        Web push (browser &amp; phone)
      </label>
      <p className="mt-1 text-[10px] text-muted-foreground">
        Alarms on the lock screen of any browser registered below, with the dashboard closed. No app to install.
      </p>

      {enabled && (
        <div className="mt-3 rounded-md border bg-card p-3">
          <p className="text-[10px] uppercase tracking-[0.16em] text-muted-foreground">This device</p>
          <div className="mt-2">{renderDeviceState()}</div>
          {error && <p className="mt-2 text-[10px] text-destructive">{error}</p>}
        </div>
      )}
    </section>
  )

  function renderDeviceState() {
    switch (support.kind) {
      case 'insecure-context':
        return (
          <div className="text-[10px] text-muted-foreground">
            <p className="text-foreground">Web push needs a secure connection.</p>
            <p className="mt-1">
              You are on an http:// address, and browsers only allow push over https. Helmcentral ships no
              certificate of its own — the supported route is Tailscale:
            </p>
            <pre className="mt-2 overflow-x-auto rounded bg-background/60 p-2 font-mono text-[11px] text-foreground">
              tailscale serve --bg 8080
            </pre>
            <p className="mt-2">
              Then open Helmcentral at the https://…ts.net address it prints. That is a real certificate and needs no
              public DNS. Tailscale Funnel is not required and should not be used.
            </p>
          </div>
        )

      case 'ios-not-installed':
        return (
          <p className="text-[10px] text-muted-foreground">
            <span className="text-foreground">On iPhone and iPad, add Helmcentral to your Home Screen first.</span>{' '}
            Tap Share, then Add to Home Screen, then open Helmcentral from the new icon and come back here. iOS only
            allows notifications for installed web apps — and it must be installed from the https:// address, not a
            LAN one.
          </p>
        )

      case 'blocked':
        return (
          <p className="text-[10px] text-muted-foreground">
            Notifications are blocked for this site. Re-enable them in your browser&apos;s site settings, then reload.
          </p>
        )

      case 'no-service-worker':
      case 'unsupported':
        return (
          <p className="text-[10px] text-muted-foreground">
            This browser cannot receive web push. ntfy or email will still reach this device.
          </p>
        )

      case 'ok':
      default:
        return (
          <div className="flex flex-col gap-2">
            {subscribed ? (
              <div className="flex items-center justify-between gap-3">
                <p className="text-[10px] text-muted-foreground">Subscribed on this device.</p>
                <Button size="sm" variant="outline" disabled={busy} onClick={() => void disableOnThisDevice()}>
                  Unsubscribe
                </Button>
              </div>
            ) : (
              <>
                <Field>
                  <FieldLabel htmlFor="webpush-label">Device name</FieldLabel>
                  <Input
                    id="webpush-label"
                    value={label}
                    placeholder="Helm tablet"
                    onChange={(e) => setLabel(e.target.value)}
                  />
                </Field>
                <div>
                  <Button size="sm" variant="outline" disabled={busy} onClick={() => void enableOnThisDevice(label)}>
                    Enable on this device
                  </Button>
                </div>
              </>
            )}

            <p className="text-[10px] text-muted-foreground">
              {deviceCount === 0
                ? 'No devices are subscribed yet, so this transport cannot deliver anything.'
                : `${deviceCount} device${deviceCount === 1 ? '' : 's'} subscribed.`}
            </p>
          </div>
        )
    }
  }
})
