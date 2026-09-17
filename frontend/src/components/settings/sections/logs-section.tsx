import { useEffect, useMemo, useState } from 'react'
import { MessageSquareText } from 'lucide-react'

import { Button } from '@/components/ui/button'
import { useLogs } from '@/hooks/use-logs'

interface LogsSectionProps {
  onAskMate: (question: string, options?: { newConversation?: boolean }) => void
}

type LogFilter = 'all' | 'warnings' | 'errors'

function matchesLogFilter(message: string, filter: LogFilter): boolean {
  const normalized = message.toUpperCase()
  if (filter === 'all') return true
  if (filter === 'warnings') return normalized.includes('WARN') || normalized.includes('WARNING')
  if (filter === 'errors') return normalized.includes('ERROR') || normalized.includes('FATAL')
  return true
}

export function LogsSection({ onAskMate }: LogsSectionProps) {
  const { logs, isLive, setIsLive, clearLogs, connected, error } = useLogs()
  const [selectedText, setSelectedText] = useState<string>('')
  const [filter, setFilter] = useState<LogFilter>('all')

  const filteredLogs = useMemo(
    () => logs.filter((entry) => matchesLogFilter(entry.message, filter)),
    [logs, filter],
  )

  useEffect(() => {
    if (!filteredLogs.some((entry) => entry.message === selectedText)) {
      setSelectedText('')
    }
  }, [filteredLogs, selectedText])

  const askMateQuestion = useMemo(() => {
    if (!selectedText.trim()) return 'Please review the recent app logs and explain the recent status.'
    return `Please review these recent log lines and explain what is happening:\n\n${selectedText}`
  }, [selectedText])

  const handleAskMate = () => {
    if (!selectedText.trim()) return
    onAskMate(askMateQuestion, { newConversation: true })
  }

  return (
    <div className="mx-auto max-w-3xl space-y-4 rounded-lg border bg-background/60 p-4">
      <div className="flex items-center justify-between gap-3">
        <div>
          <h3 className="text-sm font-medium uppercase tracking-[0.12em] text-muted-foreground">Logs</h3>
        </div>

        <div className="flex items-center gap-2">
          <div className="flex items-center gap-1 rounded-md border bg-card p-1">
            {(['all', 'warnings', 'errors'] as const).map((value) => (
              <Button
                key={value}
                type="button"
                variant={filter === value ? 'default' : 'ghost'}
                size="sm"
                onClick={() => setFilter(value)}
                className="h-8 min-w-20 px-3 text-[10px] uppercase tracking-[0.08em]"
              >
                {value === 'all' ? 'All' : value === 'warnings' ? 'Warnings' : 'Errors'}
              </Button>
            ))}
          </div>

          <label className="flex items-center gap-2 text-xs uppercase tracking-[0.08em] text-muted-foreground">
            <input
              type="checkbox"
              checked={isLive}
              aria-label="Live update"
              onChange={(event) => setIsLive(event.target.checked)}
            />
            Live update
          </label>

          <Button type="button" variant="outline" size="sm" onClick={clearLogs}>
            Clear
          </Button>
        </div>
      </div>

      {error && (
        <div className="rounded-md border border-destructive/30 bg-destructive/5 px-3 py-2 text-xs text-destructive">
          {error}
        </div>
      )}

      <div className="rounded-md border bg-card p-2">
        <div className="mb-2 flex items-center justify-between text-[10px] uppercase tracking-[0.08em] text-muted-foreground">
          <span>{connected ? 'Connected' : 'Disconnected'}</span>
        </div>

        <div className="max-h-80 space-y-2 overflow-y-auto rounded-xs bg-background/40 p-2">
          {filteredLogs.length === 0 ? (
            <p className="text-sm text-muted-foreground">No matching log entries.</p>
          ) : (
            filteredLogs.map((entry) => (
              <button
                key={entry.id}
                type="button"
                onClick={() => setSelectedText(entry.message)}
                aria-label={entry.message}
                title={entry.message}
                className="block w-full rounded-md border border-transparent bg-transparent px-2 py-1 text-left text-sm text-foreground hover:border-border hover:bg-card"
              >
                <span className="mr-2 text-[10px] uppercase text-muted-foreground">{entry.timestamp}</span>
                <span>{entry.message}</span>
              </button>
            ))
          )}
        </div>
      </div>

      <div className="rounded-md border border-border bg-card p-2 text-sm">
        <div className="mb-2 flex items-center justify-between gap-2">
          <div className="text-[10px] uppercase tracking-[0.08em] text-muted-foreground">Selected log</div>
          <Button
            type="button"
            variant="ghost"
            size="sm"
            disabled={!selectedText.trim()}
            onClick={handleAskMate}
            aria-label="Ask Mate about these log lines"
          >
            <MessageSquareText className="mr-2 h-4 w-4" />
            Ask Mate about these log lines
          </Button>
        </div>
        {selectedText ? (
          <p className="whitespace-pre-wrap wrap-break-word">{selectedText}</p>
        ) : (
          <p className="text-muted-foreground">Select a log line to ask Mate about it.</p>
        )}
      </div>
    </div>
  )
}
