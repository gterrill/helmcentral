import { useState } from 'react'
import { toast } from 'sonner'

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
import { useIgnoredAlarms } from '@/hooks/use-ignored-alarms'
import { errorMessage } from '@/lib/errors'

interface StopShowingAlarmProps {
  /** The alarm's kind: its bus path without the "notifications." prefix. */
  kind: string
  title: string
}

/**
 * "Stop showing" on a network alarm card. Only rendered when the server says
 * the alarm is ignorable, so a safety alarm never gets the button. Confirms
 * first because the alarm then disappears from the board and every
 * notification until it is restored in Settings.
 */
export function StopShowingAlarm({ kind, title }: StopShowingAlarmProps) {
  const [open, setOpen] = useState(false)
  const { ignore } = useIgnoredAlarms()

  const confirm = async () => {
    try {
      await ignore(kind)
    } catch (err) {
      toast.error('Could not stop showing this alarm', { description: errorMessage(err) })
    }
  }

  return (
    <>
      <Button size="sm" variant="ghost" aria-label={`Stop showing ${title}`} onClick={() => setOpen(true)}>
        Stop showing
      </Button>
      <AlertDialog open={open} onOpenChange={setOpen}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Stop showing {title}?</AlertDialogTitle>
            <AlertDialogDescription>
              It won&apos;t appear on the board or in notifications until you restore it in Settings.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Cancel</AlertDialogCancel>
            <AlertDialogAction onClick={() => void confirm()}>Stop showing</AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  )
}
