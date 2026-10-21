import { ThumbsDown, ThumbsUp } from 'lucide-react'
import { useEffect, useState, type FormEvent } from 'react'

import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import type { MateAnswerRating } from '@/hooks/use-mate-session'
import { cn } from '@/lib/utils'

interface AssistantAnswerFeedbackProps {
  messageId: string
  /** The verdict held for this answer, if any. */
  rating: MateAnswerRating | undefined
  /** Saves a verdict (with its note), or clears it when null. Resolves null on
   * success or the server's words when it refused. */
  /** Shows the verdict and note without offering to change them. */
  readOnly?: boolean
  onRate: (messageId: string, verdict: 'up' | 'down' | null, note?: string) => Promise<string | null>
}

/**
 * Thumbs up and down under an answer Mate has just given, with a short note
 * once a verdict is held. A tap saves at once; tapping the active thumb takes
 * the verdict back. Offered only on answers delivered in the live chat.
 */
export function AssistantAnswerFeedback({ messageId, rating, onRate, readOnly = false }: AssistantAnswerFeedbackProps) {
  const [note, setNote] = useState(rating?.note ?? '')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const savedNote = rating?.note ?? ''

  useEffect(() => {
    setNote(savedNote)
  }, [savedNote, rating?.verdict])

  const rate = async (verdict: 'up' | 'down' | null, text?: string) => {
    setBusy(true)
    const failure = verdict === null ? await onRate(messageId, null) : await onRate(messageId, verdict, text ?? '')
    setError(failure)
    setBusy(false)
  }

  const tap = (verdict: 'up' | 'down') => {
    void rate(rating?.verdict === verdict ? null : verdict)
  }

  const saveNote = (event: FormEvent) => {
    event.preventDefault()
    if (rating) void rate(rating.verdict, note.trim())
  }

  const thumb = (verdict: 'up' | 'down') => {
    const pressed = rating?.verdict === verdict
    const Icon = verdict === 'up' ? ThumbsUp : ThumbsDown
    return (
      <Button
        type="button"
        variant="ghost"
        size="icon"
        aria-label={verdict === 'up' ? 'Good answer' : 'Poor answer'}
        aria-pressed={pressed}
        disabled={busy || readOnly}
        onClick={() => tap(verdict)}
        className={cn(pressed ? 'text-foreground' : 'text-muted-foreground')}
      >
        <Icon className="h-4 w-4" fill={pressed ? 'currentColor' : 'none'} aria-hidden="true" />
      </Button>
    )
  }

  const noteLabel = rating?.verdict === 'down' ? 'What was wrong' : 'What was good'

  return (
    <div className="flex min-w-0 flex-col gap-1" data-testid="assistant-answer-feedback">
      <div className="flex items-center">
        {thumb('up')}
        {thumb('down')}
      </div>
      {rating && readOnly && rating.note !== '' && <p className="text-xs text-muted-foreground">{rating.note}</p>}
      {rating && !readOnly && (
        <form onSubmit={saveNote} className="flex min-w-0 items-center gap-2">
          <Input
            value={note}
            onChange={(event) => setNote(event.target.value)}
            maxLength={1000}
            aria-label={noteLabel}
            placeholder={`${noteLabel} (optional)`}
            className="min-w-0 flex-1 text-xs"
          />
          <Button type="submit" variant="ghost" size="sm" disabled={busy || note.trim() === savedNote}>
            Save note
          </Button>
        </form>
      )}
      {error !== null && (
        <p role="alert" className="text-xs text-destructive">
          {error}
        </p>
      )}
    </div>
  )
}
