// Where each sign's card was last put against its point (above, below or beside
// it), by the page's map. The card on the map keeps its place while it still
// fits, and the photo moment grows from the card where it stands, so it asks
// here which place that is.

import type { CardMode } from '@/lib/replay-signs'

export const cardModes = new Map<string, CardMode>()
