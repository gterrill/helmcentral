// How nights are counted. One rule, used by the cruise page's figures and the
// blog card (lib/passage-export) and by the replay's stays (lib/replay-stay),
// so a stay's label can never disagree with the cruise's nights.

const localDay = (unix: number, offsetHours: number) => Math.floor((unix + offsetHours * 3600) / 86400)

/** Boat-local midnights from the first departure up to an instant: the rule
 * the cruise page uses for its nights. An instant exactly on midnight does not
 * count its own midnight. */
export function midnightsSince(firstStartUnix: number, unix: number, offsetHours: number): number {
  return Math.max(0, localDay(unix - 0.001, offsetHours) - localDay(firstStartUnix, offsetHours))
}
