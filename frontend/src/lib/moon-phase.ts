export const MOON_PHASE_LABELS: Record<string, string> = {
  new: 'New Moon',
  waxingCrescent: 'Waxing Crescent',
  firstQuarter: 'First Quarter',
  waxingGibbous: 'Waxing Gibbous',
  full: 'Full Moon',
  waningGibbous: 'Waning Gibbous',
  lastQuarter: 'Last Quarter',
  waningCrescent: 'Waning Crescent',
}

export const MOON_PHASE_EMOJI: Record<string, string> = {
  new: '🌑',
  waxingCrescent: '🌒',
  firstQuarter: '🌓',
  waxingGibbous: '🌔',
  full: '🌕',
  waningGibbous: '🌖',
  lastQuarter: '🌗',
  waningCrescent: '🌘',
}

export function moonPhaseLabel(phase: string) {
  return MOON_PHASE_LABELS[phase] ?? phase
}

export function moonPhaseEmoji(phase: string) {
  return MOON_PHASE_EMOJI[phase] ?? '🌙'
}
