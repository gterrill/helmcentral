import { ImageOff, X } from 'lucide-react'
import { useEffect, useState } from 'react'

import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { apiBaseUrl } from '@/config/api'
import {
  SIGN_CARD_GAP_PX, SIGN_CARD_PAD_PX, SIGN_CARD_PHOTO_W_PX, SIGN_CARD_TEXT_W_PX, SIGN_NOTE_LINE_PX, SIGN_TITLE_LINE_PX, type ReplaySign,
} from '@/lib/replay-signs'
import { PHOTO_ASPECT } from '@/lib/replay-time'
import { cn } from '@/lib/utils'

/** The address of a sign's photo, or null when it has none (or the document is gone). */
export const signPhotoSrc = (sign: Pick<ReplaySign, 'photo_url'>): string | null =>
  sign.photo_url ? `${apiBaseUrl}${sign.photo_url}` : null

const logged = new Set<string>()
/** Says once per sign, in the console, that its photo did not load. */
function logPhotoFailure(sign: ReplaySign) {
  if (logged.has(sign.id)) return
  logged.add(sign.id)
  console.error(`Sign "${sign.title}" (${sign.id}): its photo did not load`)
}

export interface SignCardProps {
  sign: ReplaySign
  /** The replay is paused, so a tap opens the photo full size. */
  canOpenPhoto: boolean
  /** No entrance animation (reduced motion). */
  still: boolean
  onOpenPhoto: (sign: ReplaySign) => void
  /** Present on a card opened from a pin: closes it again. */
  onClose?: () => void
  /** The most the card may be wide, in pixels: the map's width less its margins. */
  maxWidthPx?: number
  className?: string
}

/** A sign on the chart: the photo above the title and note. */
export function SignCard({ sign, canOpenPhoto, still, onOpenPhoto, onClose, maxWidthPx, className }: SignCardProps) {
  const src = signPhotoSrc(sign)
  // The photo is there on the server but would not load: say so, do not hide it.
  const [failed, setFailed] = useState(false)
  useEffect(() => { setFailed(false) }, [src])
  const [entered, setEntered] = useState(still)
  useEffect(() => {
    if (still) return
    const id = requestAnimationFrame(() => setEntered(true))
    return () => cancelAnimationFrame(id)
  }, [still])
  const visible = still || entered
  const body = (
    <>
      {src && !failed && (
        <img src={src} alt="" loading="lazy" style={{ aspectRatio: PHOTO_ASPECT }} className="w-full shrink-0 rounded-sm bg-muted object-cover" onError={() => { logPhotoFailure(sign); setFailed(true) }} />
      )}
      {src && failed && (
        <span style={{ aspectRatio: PHOTO_ASPECT }} className="flex w-full shrink-0 items-center justify-center rounded-sm bg-muted text-muted-foreground" data-testid="sign-photo-missing">
          <ImageOff className="h-6 w-6" aria-hidden="true" />
        </span>
      )}
      <span className="flex min-w-0 flex-col text-left">
        <span style={{ lineHeight: `${SIGN_TITLE_LINE_PX}px` }} className="truncate text-sm font-semibold text-foreground">{sign.title}</span>
        {sign.note && <span style={{ lineHeight: `${SIGN_NOTE_LINE_PX}px` }} className="line-clamp-2 whitespace-pre-line text-xs text-muted-foreground">{sign.note}</span>}
        {src && failed && <span className="text-xs text-muted-foreground">Photo not available</span>}
      </span>
    </>
  )
  // The photo leads, full width; a note-only sign stays a compact text card.
  // Its size comes from lib/replay-signs, which the photo moment grows from.
  const surface = 'flex max-w-full flex-col items-stretch rounded-md border border-border bg-card text-card-foreground shadow-md'
  const surfaceStyle = { width: src ? SIGN_CARD_PHOTO_W_PX : SIGN_CARD_TEXT_W_PX, padding: SIGN_CARD_PAD_PX, gap: SIGN_CARD_GAP_PX }
  return (
    <div
      style={maxWidthPx === undefined ? undefined : { maxWidth: maxWidthPx }}
      className={cn('relative', !still && 'transition-opacity duration-300', visible ? 'opacity-100' : 'opacity-0', className)}
      data-testid="sign-card"
      data-sign-id={sign.id}
    >
      {canOpenPhoto && src && !failed ? (
        <button type="button" aria-label={`Open photo: ${sign.title}`} style={surfaceStyle} className={cn(surface, 'cursor-zoom-in focus-visible:outline-hidden focus-visible:ring-2 focus-visible:ring-ring')} onClick={() => onOpenPhoto(sign)}>
          {body}
        </button>
      ) : (
        <div style={surfaceStyle} className={surface} role="group" aria-label={`Sign: ${sign.title}`}>{body}</div>
      )}
      {onClose && (
        <button
          type="button"
          aria-label={`Close sign: ${sign.title}`}
          className="absolute -right-2 -top-2 flex h-5 w-5 items-center justify-center rounded-full border border-border bg-card text-foreground shadow-sm focus-visible:outline-hidden focus-visible:ring-2 focus-visible:ring-ring"
          onClick={onClose}
        >
          <X className="h-3 w-3" aria-hidden="true" />
        </button>
      )}
    </div>
  )
}

/** A small mark on the sign's point on the track, when the replay is not running. */
export function SignPin({ sign, onClick }: { sign: ReplaySign; onClick: () => void }) {
  return (
    <button
      type="button"
      aria-label={`Sign: ${sign.title}`}
      title={sign.title}
      data-testid="sign-pin"
      data-sign-id={sign.id}
      className="group flex h-7 w-7 items-center justify-center rounded-full focus-visible:outline-hidden focus-visible:ring-2 focus-visible:ring-ring"
      onClick={onClick}
    >
      <span aria-hidden="true" className="h-4 w-4 rotate-45 rounded-xs border-2 border-background bg-primary shadow-sm ring-1 ring-primary/50" />
    </button>
  )
}

/** The photo full size. Escape or the close button dismisses it. */
export function SignPhotoDialog({ sign, onClose }: { sign: ReplaySign | null; onClose: () => void }) {
  const src = sign ? signPhotoSrc(sign) : null
  const [failed, setFailed] = useState(false)
  useEffect(() => { setFailed(false) }, [src])
  return (
    <Dialog open={sign !== null} onOpenChange={(open) => { if (!open) onClose() }}>
      <DialogContent className="max-w-3xl" data-testid="sign-photo-dialog">
        <DialogHeader>
          <DialogTitle>{sign?.title}</DialogTitle>
          <DialogDescription className={cn(!sign?.note && 'sr-only')}>{sign?.note || 'Photo for this sign'}</DialogDescription>
        </DialogHeader>
        {src && !failed ? (
          <img src={src} alt={sign?.title ?? ''} className="max-h-[70vh] w-full rounded-md object-contain" onError={() => { if (sign) logPhotoFailure(sign); setFailed(true) }} />
        ) : (
          <p className="flex items-center gap-2 text-sm text-muted-foreground"><ImageOff className="h-4 w-4" aria-hidden="true" />The photo is not available.</p>
        )}
      </DialogContent>
    </Dialog>
  )
}
