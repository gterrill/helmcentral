// ADR 0142: where SaveBar renders. The app's header provides one of these
// (App.tsx, inside its `relative` <header>), and SaveBar portals into it, so
// a dirty form takes over the header - Polaris' contextual save bar - rather
// than floating over the page. Kept in its own tiny module so App.tsx can
// import it without pulling the whole patterns barrel (TanStack Table and
// all) into the entry chunk.

export const SAVE_BAR_SLOT_ID = 'save-bar-slot'

/** Must sit inside a `relative` element that is as tall and wide as the bar
 * should be (the header). Absolutely covers it while a SaveBar is portalled
 * in; `empty:hidden` means it covers nothing at all the rest of the time. */
export function SaveBarSlot() {
  return (
    <div
      id={SAVE_BAR_SLOT_ID}
      data-testid="save-bar-slot"
      className="absolute inset-0 z-10 empty:hidden"
    />
  )
}
