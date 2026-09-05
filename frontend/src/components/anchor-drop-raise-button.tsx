import { Anchor } from 'lucide-react'
import { useCallback, useState } from 'react'

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
import { cn } from '@/lib/utils'
import { primeAudioContextForAlarm } from '@/lib/audio-utils'

interface AnchorDropRaiseButtonProps {
  anchorActive: boolean
  canDrop: boolean
  onDrop: () => void
  onRaise: () => Promise<void> | void
  /** Sizing/alignment is the host's call: Drop is a state's primary action
      and gets width; Raise is a departure chore and shouldn't dominate. */
  className?: string
}

/**
 * Drop/Raise control shared by the anchor-watch tile and the fullscreen
 * drawer, mounted below their respective maps. Keeping this in one place
 * also fixes a gap the two callers used to have independently: the drawer's
 * drop path never primed the audio context, so a drag alarm set from the
 * drawer could fail to sound until some other gesture unlocked audio first.
 */
export function AnchorDropRaiseButton({ anchorActive, canDrop, onDrop, onRaise, className }: AnchorDropRaiseButtonProps) {
  const [confirmOpen, setConfirmOpen] = useState(false)

  const handleDrop = useCallback(() => {
    void primeAudioContextForAlarm() // Unlock audio context for a later alarm
    onDrop()
  }, [onDrop])

  const handleConfirmRaise = useCallback(() => {
    void onRaise()
  }, [onRaise])

  if (!anchorActive) {
    return (
      <Button
        className={cn('h-11', className)}
        disabled={!canDrop}
        onClick={handleDrop}
      >
        <Anchor className="h-4 w-4" />
        Drop
      </Button>
    )
  }

  return (
    <>
      <Button className={cn('h-11', className)} variant="outline" onClick={() => setConfirmOpen(true)}>
        <Anchor className="h-4 w-4" />
        Raise
      </Button>
      <AlertDialog open={confirmOpen} onOpenChange={setConfirmOpen}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Raise anchor?</AlertDialogTitle>
            <AlertDialogDescription>
              This stops the anchor watch and clears the vessel trail and this session&apos;s placemarks.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Cancel</AlertDialogCancel>
            <AlertDialogAction onClick={handleConfirmRaise}>Raise</AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  )
}
