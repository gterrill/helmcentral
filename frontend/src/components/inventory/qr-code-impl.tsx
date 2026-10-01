import qrcode from 'qrcode-generator'

// Draws a QR code as inline SVG so it prints crisply at any size. Black on
// white with the 4-module quiet zone the spec asks for; error correction M.
// Loaded only through qr-code.tsx, which keeps this module (and the encoder)
// out of the entry chunk.

const QUIET_ZONE = 4

interface QrCodeImplProps {
  value: string
  className?: string
}

export default function QrCodeImpl({ value, className }: QrCodeImplProps) {
  const qr = qrcode(0, 'M')
  qr.addData(value)
  qr.make()
  const n = qr.getModuleCount()
  const size = n + QUIET_ZONE * 2
  let d = ''
  for (let r = 0; r < n; r++) {
    for (let c = 0; c < n; c++) {
      if (qr.isDark(r, c)) d += `M${c + QUIET_ZONE} ${r + QUIET_ZONE}h1v1h-1z`
    }
  }
  return (
    <svg
      role="img"
      aria-label={`QR code for ${value}`}
      data-value={value}
      viewBox={`0 0 ${size} ${size}`}
      shapeRendering="crispEdges"
      className={className}
    >
      <rect width={size} height={size} fill="white" />
      <path d={d} fill="black" />
    </svg>
  )
}
