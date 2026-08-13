import { Send } from 'lucide-react'
import { memo, useEffect, useState } from 'react'

import { Button } from '@/components/ui/button'
import { Field, FieldError, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Tile } from '@/components/ui/tile'
import {
  emptyTransportConfig,
  useAlarmTransports,
  type AlarmTransportConfig,
  type TransportSecretKey,
} from '@/hooks/use-alarm-transports'

function Toggle({ id, checked, label, onChange }: { id: string; checked: boolean; label: string; onChange: (v: boolean) => void }) {
  return (
    <label htmlFor={id} className="flex items-center gap-2 text-[11px] uppercase tracking-[0.16em] text-muted-foreground">
      <input id={id} type="checkbox" checked={checked} onChange={(e) => onChange(e.target.checked)} />
      {label}
    </label>
  )
}

/**
 * Notification delivery. Every transport here is self-hosted or free — no
 * subscription — which is why a cloud relay and SMS are absent (ADR 0038).
 */
export const AlarmTransportsForm = memo(function AlarmTransportsForm() {
  const { config, secretsPresent, loading, error, save, test, testResults, testing } = useAlarmTransports()
  const [draft, setDraft] = useState<AlarmTransportConfig>(emptyTransportConfig)
  const [secrets, setSecrets] = useState<Partial<Record<TransportSecretKey, string>>>({})
  const [saveError, setSaveError] = useState<string | null>(null)
  const [saved, setSaved] = useState(false)

  // Re-seed only when the server's copy changes, so typing is never clobbered.
  useEffect(() => { setDraft(config) }, [config])

  const setSection = <K extends keyof AlarmTransportConfig>(key: K, value: Partial<AlarmTransportConfig[K]>) =>
    setDraft((current) => ({ ...current, [key]: { ...current[key], ...value } }))

  const submit = async () => {
    setSaveError(null)
    setSaved(false)
    try {
      await save(draft, secrets)
      setSecrets({})
      setSaved(true)
    } catch (err) {
      setSaveError(err instanceof Error ? err.message : String(err))
    }
  }

  if (loading) {
    return (
      <Tile title="Notifications" icon={<Send className="h-3.5 w-3.5 text-gauge-secondary" />}>
        <p className="text-[11px] uppercase tracking-[0.16em] text-muted-foreground">Loading…</p>
      </Tile>
    )
  }

  return (
    <Tile
      title="Notifications"
      icon={<Send className="h-3.5 w-3.5 text-gauge-secondary" />}
      titleExtra={
        <Button size="sm" variant="outline" onClick={() => void test()} disabled={testing}>
          {testing ? 'Sending…' : 'Send Test'}
        </Button>
      }
    >
      {error && <p className="mb-2 text-[11px] text-destructive">{error}</p>}

      {testResults && (
        <div className="mb-3 rounded-md border bg-background/60 px-3 py-2">
          {Object.entries(testResults).map(([transport, result]) => (
            <p key={transport} className="truncate text-[11px]">
              <span className="uppercase tracking-[0.16em] text-muted-foreground">{transport}</span>{' '}
              <span className={result === 'ok' ? 'text-emerald-600' : 'text-destructive'}>{result}</span>
            </p>
          ))}
        </div>
      )}

      <div className="flex flex-col gap-4">
        <section className="rounded-md border bg-background/60 p-3">
          <Toggle id="ntfy-enabled" checked={draft.ntfy.enabled} label="ntfy (phone push)"
            onChange={(enabled) => setSection('ntfy', { enabled })} />
          <p className="mt-1 text-[10px] text-muted-foreground">
            Self-host it, or use ntfy.sh free with no account. Subscribe to the same topic in the ntfy app.
          </p>
          {draft.ntfy.enabled && (
            <div className="mt-3 grid gap-3 sm:grid-cols-2">
              <Field>
                <FieldLabel htmlFor="ntfy-server">Server</FieldLabel>
                <Input id="ntfy-server" value={draft.ntfy.server} placeholder="https://ntfy.sh"
                  onChange={(e) => setSection('ntfy', { server: e.target.value })} />
              </Field>
              <Field>
                <FieldLabel htmlFor="ntfy-topic">Topic</FieldLabel>
                <Input id="ntfy-topic" value={draft.ntfy.topic} placeholder="pikorua-alarms"
                  onChange={(e) => setSection('ntfy', { topic: e.target.value })} />
              </Field>
              <Field>
                <FieldLabel htmlFor="ntfy-token">Access token (optional)</FieldLabel>
                <Input id="ntfy-token" type="password" autoComplete="off"
                  placeholder={secretsPresent.NTFY_TOKEN ? 'Saved — enter to replace' : 'Only for a protected topic'}
                  value={secrets.NTFY_TOKEN ?? ''}
                  onChange={(e) => setSecrets((s) => ({ ...s, NTFY_TOKEN: e.target.value }))} />
              </Field>
            </div>
          )}
        </section>

        <section className="rounded-md border bg-background/60 p-3">
          <Toggle id="smtp-enabled" checked={draft.smtp.enabled} label="Email"
            onChange={(enabled) => setSection('smtp', { enabled })} />
          {draft.smtp.enabled && (
            <div className="mt-3 grid gap-3 sm:grid-cols-2">
              <Field>
                <FieldLabel htmlFor="smtp-host">Server</FieldLabel>
                <Input id="smtp-host" value={draft.smtp.host} placeholder="smtp.example.com"
                  onChange={(e) => setSection('smtp', { host: e.target.value })} />
              </Field>
              <Field>
                <FieldLabel htmlFor="smtp-port">Port</FieldLabel>
                <Input id="smtp-port" type="number" value={draft.smtp.port}
                  onChange={(e) => setSection('smtp', { port: Number(e.target.value) })} />
              </Field>
              <Field>
                <FieldLabel htmlFor="smtp-from">From</FieldLabel>
                <Input id="smtp-from" value={draft.smtp.from} placeholder="boat@example.com"
                  onChange={(e) => setSection('smtp', { from: e.target.value })} />
              </Field>
              <Field>
                <FieldLabel htmlFor="smtp-to">To (comma separated)</FieldLabel>
                <Input id="smtp-to" value={(draft.smtp.to ?? []).join(', ')} placeholder="skipper@example.com"
                  onChange={(e) => setSection('smtp', { to: e.target.value.split(',').map((v) => v.trim()).filter(Boolean) })} />
              </Field>
              <Field>
                <FieldLabel htmlFor="smtp-username">Username</FieldLabel>
                <Input id="smtp-username" value={draft.smtp.username} autoComplete="off"
                  onChange={(e) => setSection('smtp', { username: e.target.value })} />
              </Field>
              <Field>
                <FieldLabel htmlFor="smtp-password">Password</FieldLabel>
                <Input id="smtp-password" type="password" autoComplete="off"
                  placeholder={secretsPresent.SMTP_PASSWORD ? 'Saved — enter to replace' : ''}
                  value={secrets.SMTP_PASSWORD ?? ''}
                  onChange={(e) => setSecrets((s) => ({ ...s, SMTP_PASSWORD: e.target.value }))} />
              </Field>
            </div>
          )}
        </section>

        <section className="rounded-md border bg-background/60 p-3">
          <Toggle id="webhook-enabled" checked={draft.webhook.enabled} label="Webhook"
            onChange={(enabled) => setSection('webhook', { enabled })} />
          {draft.webhook.enabled && (
            <Field className="mt-3">
              <FieldLabel htmlFor="webhook-url">URL</FieldLabel>
              <Input id="webhook-url" value={draft.webhook.url} placeholder="https://home-assistant.local/api/webhook/…"
                onChange={(e) => setSection('webhook', { url: e.target.value })} />
            </Field>
          )}
        </section>

        <section className="rounded-md border bg-background/60 p-3">
          <Toggle id="signalk-enabled" checked={draft.signalk.enabled} label="Publish to SignalK"
            onChange={(enabled) => setSection('signalk', { enabled })} />
          <p className="mt-1 text-[10px] text-muted-foreground">
            Writes alarms back to notifications.* so a buzzer or MFD reacts. Needs no internet.
          </p>
        </section>

        <section className="rounded-md border bg-background/60 p-3">
          <p className="text-[11px] uppercase tracking-[0.16em] text-muted-foreground">Watchdog</p>
          <div className="mt-3 grid gap-3 sm:grid-cols-2">
            <Field>
              <FieldLabel htmlFor="watchdog-silence">Alarm if no SignalK data for (seconds)</FieldLabel>
              <Input id="watchdog-silence" type="number" value={draft.watchdog.stream_silence_seconds}
                onChange={(e) => setSection('watchdog', { stream_silence_seconds: Number(e.target.value) })} />
              <p className="text-[10px] text-muted-foreground">0 uses the default (120s).</p>
            </Field>
            <Field>
              <FieldLabel htmlFor="watchdog-heartbeat">Heartbeat every (minutes)</FieldLabel>
              <Input id="watchdog-heartbeat" type="number" value={draft.watchdog.heartbeat_minutes}
                onChange={(e) => setSection('watchdog', { heartbeat_minutes: Number(e.target.value) })} />
              <p className="text-[10px] text-muted-foreground">
                0 disables. A periodic "still alive" makes its absence the alarm.
              </p>
            </Field>
          </div>
        </section>
      </div>

      {saveError && <FieldError className="mt-3">{saveError}</FieldError>}
      {saved && !saveError && (
        <p className="mt-3 text-[11px] uppercase tracking-[0.16em] text-muted-foreground">Saved</p>
      )}

      <div className="mt-3 flex justify-end">
        <Button size="sm" onClick={() => void submit()}>Save Notifications</Button>
      </div>
    </Tile>
  )
})
