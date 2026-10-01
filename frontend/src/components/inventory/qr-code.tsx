import { lazy, Suspense } from 'react'

// Same split as help-markdown.tsx: the encoder lives behind a dynamic import
// so the wall kiosk's older WebKit, which parses the entry chunk, never loads
// it. Labels are printed from the details pages, not the kiosk.
const QrCodeImpl = lazy(() => import('./qr-code-impl'))

interface QrCodeProps {
  value: string
  className?: string
}

export function QrCode({ value, className }: QrCodeProps) {
  return (
    <Suspense fallback={<div className={className} aria-hidden="true" />}>
      <QrCodeImpl value={value} className={className} />
    </Suspense>
  )
}
