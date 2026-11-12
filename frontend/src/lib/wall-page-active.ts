import { createContext, useContext } from 'react'

/**
 * True for the page a wall display is showing, and for every tile outside a
 * wall. A tile that holds something live and heavy (a camera stream in an
 * embed) reads this to let go of it while its page rests, and every live
 * reader (`useSelection`) stops taking updates while it is false.
 */
export const WallPageActiveContext = createContext(true)

/**
 * Whether this tile's wall page is the one showing. A tile that owns a timer
 * drives it from this (ADR 0217): it runs only while the page is active.
 */
export function useWallPageActive(): boolean {
  return useContext(WallPageActiveContext)
}
