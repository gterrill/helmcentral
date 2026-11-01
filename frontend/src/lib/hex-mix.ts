const channels = (hex: string) => [1, 3, 5].map((i) => parseInt(hex.slice(i, i + 2), 16))

/** The colour `t` of the way from one `#rrggbb` colour to another. */
export function mixHex(a: string, b: string, t: number): string {
  const [ar, ag, ab] = channels(a)
  const [br, bg, bb] = channels(b)
  return `#${[ar + (br - ar) * t, ag + (bg - ag) * t, ab + (bb - ab) * t].map((v) => Math.round(v).toString(16).padStart(2, '0')).join('')}`
}

/** A `#rrggbb` colour as `rgba()` with the given opacity. */
export function hexAlpha(hex: string, alpha: number): string {
  const [r, g, b] = channels(hex)
  return `rgba(${r}, ${g}, ${b}, ${alpha})`
}
