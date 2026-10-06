import { useUpdatedToast } from '@/hooks/use-updated-toast'

/**
 * Raises the "Helmcentral updated to …" message. Rendered just after the
 * shell's <Toaster>, never from App's top level: after the reload App first
 * shows "Checking sign-in…" with no Toaster, and sonner drops a toast raised
 * before a Toaster is listening. Sibling effects run in order, so the Toaster
 * has subscribed by the time this reads the marker. The wall display has no
 * Toaster and so no message; its marker waits for the next ordinary page.
 */
export function UpdatedToast() {
  useUpdatedToast()
  return null
}
