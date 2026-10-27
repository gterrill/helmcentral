import { createContext, type ReactNode } from 'react'
import { cn } from '@/lib/utils'

/**
 * True for the page a wall display is showing, and for every tile outside a
 * wall. A tile that holds something live and heavy (a camera stream in an
 * embed) reads this to let go of it while its page rests.
 */
export const WallPageActiveContext = createContext(true)

interface WallPageStackProps<P extends { id: string }> {
  pages: readonly P[]
  activeId: string | null
  renderPage: (page: P) => ReactNode
}

/**
 * Every page of a wall display's feed, mounted once and kept mounted, with
 * only the active one visible.
 *
 * Swapping the page by unmounting and remounting made a lap build and destroy
 * every tile on it, and a map tile builds a WebGL context each time. The
 * kiosk's WPE WebKit never gives that native memory back, so a wall with a map
 * page grew by hundreds of MB an hour until the board froze.
 *
 * A resting page stays laid out at the width of the stack, absolutely
 * positioned over the active one and hidden with `visibility`. `display: none`
 * would give a map and the grid zero size, and they would have to be
 * re-measured on every lap. `inert` takes the resting page out of focus and
 * out of the accessibility tree, and `pointer-events-none` keeps it from
 * catching a tap.
 */
export function WallPageStack<P extends { id: string }>({ pages, activeId, renderPage }: WallPageStackProps<P>) {
  return (
    <div className="relative">
      {pages.map((page) => {
        const active = page.id === activeId
        return (
          <div
            key={page.id}
            data-wall-page={page.id}
            aria-hidden={active ? undefined : true}
            inert={!active}
            className={cn(!active && 'pointer-events-none invisible absolute left-0 top-0 w-full')}
          >
            <WallPageActiveContext.Provider value={active}>
              {renderPage(page)}
            </WallPageActiveContext.Provider>
          </div>
        )
      })}
    </div>
  )
}
