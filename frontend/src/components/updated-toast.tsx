import { useUpdatedToast } from '@/hooks/use-updated-toast'
import type { AppVersion } from '@/lib/build-stamp'

/**
 * Raises the "Helmcentral updated to …" message. Rendered just after the
 * shell's <Toaster>, never from App's top level: sonner drops a toast raised
 * before a Toaster is listening, and sibling effects run in order. The wall
 * display has no Toaster and shows DisplayUpdatedNotice instead.
 */
export function UpdatedToast({ current }: { current: AppVersion | null }) {
  useUpdatedToast(current)
  return null
}
