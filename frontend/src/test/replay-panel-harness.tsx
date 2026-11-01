import { useState } from 'react'

import { ReplayPanel as Panel, type ReplayPanelProps } from '@/components/passages/replay-panel'
import { DEFAULT_REPLAY_LENGTH, type ReplaySpeedId } from '@/hooks/use-replay'

/** The panel with the page's job done for it: it holds the replay's length. */
export function ReplayPanel(props: Omit<ReplayPanelProps, 'length' | 'onLengthChange'> & { onLength?: (length: ReplaySpeedId) => void }) {
  const { onLength, ...rest } = props
  const [length, setLength] = useState<ReplaySpeedId>(DEFAULT_REPLAY_LENGTH)
  return <Panel {...rest} length={length} onLengthChange={(l) => { setLength(l); onLength?.(l) }} />
}
