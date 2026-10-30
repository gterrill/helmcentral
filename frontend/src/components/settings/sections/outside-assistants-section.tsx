import { errorMessage } from '@/lib/errors'
import { useRef, useState } from 'react'
import { toast } from 'sonner'

import { ConfirmDelete, FormSection, SettingsLayout } from '@/components/patterns'
import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Switch } from '@/components/ui/switch'
import {
  useOutsideAssistants,
  type CreatedOutsideAssistantToken,
  type OutsideAssistantActivity,
  type OutsideAssistantToken,
  type OutsideAssistantTool,
} from '@/hooks/use-outside-assistants'
import { useVesselStateSelector } from '@/hooks/use-vessel-state'
import { copyTextFrom } from '@/lib/copy-text'
import { formatDayTimeIn, selectTimeZone } from '@/lib/vessel-time'

function endpointUrl(): string {
  return `${window.location.origin}/mcp`
}

function claudeCodeCommand(secret: string): string {
  return `claude mcp add --transport http helmcentral ${endpointUrl()} --header "Authorization: Bearer ${secret}"`
}

// Claude Desktop reads stdio servers from its config file, so the boat's
// address goes through the mcp-remote bridge. The header value is passed in
// the environment because Claude Desktop mangles spaces in arguments.
function claudeDesktopJson(secret: string): string {
  return JSON.stringify(
    {
      mcpServers: {
        helmcentral: {
          command: 'npx',
          args: ['-y', 'mcp-remote', endpointUrl(), '--allow-http', '--header', 'Authorization:${HELMCENTRAL_AUTH}'],
          env: { HELMCENTRAL_AUTH: `Bearer ${secret}` },
        },
      },
    },
    null,
    2,
  )
}

async function copy(source: HTMLElement | null, what: string) {
  if (await copyTextFrom(source)) {
    toast.success(`${what} copied`)
  } else {
    toast.error(`Could not copy the ${what.toLowerCase()}`, { description: 'Select it and copy it by hand.' })
  }
}

function describeOutcome(outcome: string): string {
  switch (outcome) {
    case 'ok':
      return 'ok'
    case 'refused':
      return 'refused: not offered to this token'
    case 'tool_error':
      return 'the tool could not answer'
    default:
      return outcome.replace(/_/g, ' ')
  }
}

function SecretDialog({ created, onClose }: { created: CreatedOutsideAssistantToken | null; onClose: () => void }) {
  const secret = created?.secret ?? ''
  const command = claudeCodeCommand(secret)
  const desktop = claudeDesktopJson(secret)
  const secretField = useRef<HTMLInputElement>(null)
  const commandBlock = useRef<HTMLPreElement>(null)
  const desktopBlock = useRef<HTMLPreElement>(null)
  return (
    <Dialog open={created !== null} onOpenChange={(open) => !open && onClose()}>
      <DialogContent className="max-h-[90vh] max-w-2xl overflow-y-auto">
        <DialogHeader>
          <DialogTitle>Token created for {created?.token.name}</DialogTitle>
          <DialogDescription>
            Copy it now. It won&apos;t be shown again. If you lose it, revoke this token and make another.
          </DialogDescription>
        </DialogHeader>
        <div className="flex min-w-0 flex-col gap-4">
          <div className="flex min-w-0 items-center gap-2">
            <Input ref={secretField} readOnly value={secret} aria-label="Token" className="min-w-0 font-mono text-xs" onFocus={(e) => e.currentTarget.select()} />
            <Button type="button" variant="outline" onClick={() => void copy(secretField.current, 'Token')} aria-label="Copy token">
              Copy
            </Button>
          </div>
          <div className="flex min-w-0 flex-col gap-1.5">
            <p className="text-xs font-semibold uppercase tracking-[0.16em] text-muted-foreground">Claude Code</p>
            <pre ref={commandBlock} className="overflow-x-auto rounded-md border bg-card p-2 font-mono text-xs whitespace-pre-wrap break-all">{command}</pre>
            <div>
              <Button type="button" size="sm" variant="outline" onClick={() => void copy(commandBlock.current, 'Command')} aria-label="Copy Claude Code command">
                Copy command
              </Button>
            </div>
          </div>
          <div className="flex min-w-0 flex-col gap-1.5">
            <p className="text-xs font-semibold uppercase tracking-[0.16em] text-muted-foreground">Claude Desktop</p>
            <p className="text-xs text-muted-foreground">
              Add this to claude_desktop_config.json. It needs Node.js on the computer, for the npx command.
            </p>
            <pre ref={desktopBlock} className="overflow-x-auto rounded-md border bg-card p-2 font-mono text-xs">{desktop}</pre>
            <div>
              <Button type="button" size="sm" variant="outline" onClick={() => void copy(desktopBlock.current, 'Snippet')} aria-label="Copy Claude Desktop snippet">
                Copy snippet
              </Button>
            </div>
          </div>
          <div className="flex justify-end">
            <Button type="button" onClick={onClose}>
              Done
            </Button>
          </div>
        </div>
      </DialogContent>
    </Dialog>
  )
}

function TokenCard({
  token,
  tools,
  timeZone,
  onSwitch,
  onRevoke,
}: {
  token: OutsideAssistantToken
  tools: OutsideAssistantTool[]
  timeZone: string | undefined
  onSwitch: (token: OutsideAssistantToken, tool: string, on: boolean) => void
  onRevoke: (token: OutsideAssistantToken) => void
}) {
  const lastUsed = token.last_used_at ? formatDayTimeIn(token.last_used_at, timeZone) : null
  return (
    <div className="flex min-w-0 flex-col gap-3 rounded-md border border-border/60 bg-background/40 p-3">
      <div className="flex min-w-0 items-center justify-between gap-2">
        <div className="min-w-0">
          <p className="truncate text-sm font-medium text-foreground">{token.name}</p>
          <p className="truncate text-xs text-muted-foreground">
            {lastUsed ? `Last used ${lastUsed}` : 'Never used'}
          </p>
        </div>
        <Button type="button" size="sm" variant="ghost" aria-label={`Revoke ${token.name}`} onClick={() => onRevoke(token)}>
          Revoke
        </Button>
      </div>
      <ul className="grid grid-cols-[repeat(auto-fill,minmax(14rem,1fr))] gap-x-4 gap-y-2" aria-label={`Tools for ${token.name}`}>
        {tools.map((tool) => (
          <li key={tool.name} className="flex min-w-0 items-center gap-2">
            <Switch
              checked={token.tools.includes(tool.name)}
              onCheckedChange={(on) => onSwitch(token, tool.name, on)}
              aria-label={`${tool.plain_name} for ${token.name}`}
            />
            <span className="min-w-0 truncate text-sm text-foreground">{tool.plain_name}</span>
          </li>
        ))}
      </ul>
    </div>
  )
}

function ActivityRow({ entry, tools, timeZone }: { entry: OutsideAssistantActivity; tools: OutsideAssistantTool[]; timeZone: string | undefined }) {
  const plain = tools.find((t) => t.name === entry.name)?.plain_name
  const asked = entry.kind === 'tool' ? (plain ?? entry.name) : entry.kind === 'resource' ? 'Boat context' : entry.name
  return (
    <li className="flex min-w-0 flex-wrap items-baseline gap-x-2 gap-y-0.5 rounded-md border border-border/60 bg-background/40 px-2.5 py-1.5 text-sm">
      <span className="text-xs text-muted-foreground tabular-nums">{formatDayTimeIn(entry.at, timeZone)}</span>
      <span className="font-medium text-foreground">{entry.client}</span>
      <span className="min-w-0 truncate text-foreground">{asked}</span>
      <span className={entry.outcome === 'ok' ? 'text-xs text-muted-foreground' : 'text-xs text-destructive'}>
        {describeOutcome(entry.outcome)} · {entry.duration_ms} ms
      </span>
    </li>
  )
}

/**
 * Outside assistants (ADR 0231): the tokens that let a program such as Claude
 * Code read the boat, the tools each token may use, and what they asked for.
 * Everything here saves as it is done, so the section has no Save bar.
 */
export function OutsideAssistantsSection() {
  const timeZone = useVesselStateSelector(selectTimeZone)
  const { tokens, tools, activity, loading, error, activityError, refreshError, create, setTools, revoke } = useOutsideAssistants()
  const [name, setName] = useState('')
  const [creating, setCreating] = useState(false)
  const [created, setCreated] = useState<CreatedOutsideAssistantToken | null>(null)
  const [revoking, setRevoking] = useState<OutsideAssistantToken | null>(null)
  const [revokeBusy, setRevokeBusy] = useState(false)

  const handleCreate = async () => {
    setCreating(true)
    try {
      const result = await create(name.trim())
      setName('')
      setCreated(result)
    } catch (err) {
      toast.error('Could not create the token', { description: errorMessage(err) })
    } finally {
      setCreating(false)
    }
  }

  // The operator's latest choice for each token, shown at once. Each change is
  // built from the latest list and sent in order, so two quick switches both land.
  const [choices, setChoices] = useState<Record<string, string[]>>({})
  const latest = useRef<Record<string, string[]>>({})
  const chain = useRef<Record<string, Promise<void>>>({})
  const inFlight = useRef<Record<string, number>>({})

  const setChoice = (id: string, tools: string[] | null) => {
    if (tools === null) delete latest.current[id]
    else latest.current[id] = tools
    setChoices({ ...latest.current })
  }

  const handleSwitch = (token: OutsideAssistantToken, tool: string, on: boolean) => {
    const base = latest.current[token.id] ?? token.tools
    const next = on ? [...base.filter((t) => t !== tool), tool] : base.filter((t) => t !== tool)
    setChoice(token.id, next)
    inFlight.current[token.id] = (inFlight.current[token.id] ?? 0) + 1
    chain.current[token.id] = (chain.current[token.id] ?? Promise.resolve()).then(async () => {
      try {
        await setTools(token.id, next)
      } catch (err) {
        toast.error('Could not change the tools', { description: errorMessage(err) })
        // The server kept the earlier list; show that.
        setChoice(token.id, null)
      } finally {
        inFlight.current[token.id] -= 1
        if (inFlight.current[token.id] === 0 && token.id in latest.current) setChoice(token.id, null)
      }
    })
  }

  const handleRevoke = async () => {
    if (!revoking) return
    setRevokeBusy(true)
    try {
      await revoke(revoking.id)
      setRevoking(null)
    } catch (err) {
      toast.error('Could not revoke the token', { description: errorMessage(err) })
    } finally {
      setRevokeBusy(false)
    }
  }

  return (
    <SettingsLayout title="Outside assistants">
      {(error || refreshError) && (
        <p className="text-sm text-destructive" role="alert">
          {error ?? refreshError}
        </p>
      )}

      <FormSection
        title="Tokens"
        description="A token lets one program on your network read the boat: Claude Code on a laptop, for example. It can only read. Each program gets its own token, so you can revoke one without touching the others."
      >
        <form
          className="flex min-w-0 flex-wrap items-end gap-2"
          onSubmit={(event) => {
            event.preventDefault()
            if (name.trim()) void handleCreate()
          }}
        >
          <div className="flex min-w-0 grow flex-col gap-1.5">
            <label htmlFor="outside-assistant-name" className="text-xs font-medium text-muted-foreground">
              Name
            </label>
            <Input
              id="outside-assistant-name"
              value={name}
              placeholder="Laptop"
              maxLength={80}
              onChange={(event) => setName(event.target.value)}
            />
          </div>
          <Button type="submit" disabled={creating || name.trim() === ''}>
            Create token
          </Button>
        </form>

        {loading && <p className="text-sm text-muted-foreground">Loading…</p>}
        {!loading && !error && tokens.length === 0 && (
          <p className="text-sm text-muted-foreground">
            There are no tokens, so no outside assistant can connect. Create one to let a program read the boat.
          </p>
        )}
        <div className="flex flex-col gap-3">
          {tokens.map((token) => (
            <TokenCard
              key={token.id}
              token={{ ...token, tools: choices[token.id] ?? token.tools }}
              tools={tools}
              timeZone={timeZone}
              onSwitch={handleSwitch}
              onRevoke={setRevoking}
            />
          ))}
        </div>
      </FormSection>

      <FormSection title="Recent activity" description="What outside assistants asked for in the last 30 days, newest first.">
        {activityError && (
          <p className="text-sm text-destructive" role="alert">
            {activityError}
          </p>
        )}
        {!activityError && activity.length === 0 && <p className="text-sm text-muted-foreground">Nothing yet.</p>}
        {activity.length > 0 && (
          <ul className="flex max-h-96 flex-col gap-1.5 overflow-y-auto" aria-label="Recent activity">
            {activity.map((entry, i) => (
              <ActivityRow key={`${entry.at}-${i}`} entry={entry} tools={tools} timeZone={timeZone} />
            ))}
          </ul>
        )}
      </FormSection>

      <SecretDialog created={created} onClose={() => setCreated(null)} />
      <ConfirmDelete
        open={revoking !== null}
        onOpenChange={(open) => !open && setRevoking(null)}
        title={`Revoke ${revoking?.name ?? 'this token'}?`}
        description="The program using it stops being able to connect straight away. This can't be undone. You can make a new token."
        confirmLabel="Revoke"
        deleting={revokeBusy}
        onConfirm={() => void handleRevoke()}
      />
    </SettingsLayout>
  )
}
