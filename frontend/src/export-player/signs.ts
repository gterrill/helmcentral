// Signs in the blog card (ADR 0205): pins, cards on a leader line and the photo
// dialog, built with DOM calls and textContent only. The rules for which cards
// are up come from the app (lib/replay-signs.ts); this file only draws them.
// Everything is scoped to one card: its elements live inside the card's root
// and carry ids made from the card's own id.

import type * as MapLibre from 'maplibre-gl'

import { isExportPhoto } from '@/lib/passage-export-signs'
import type { ResolvedSign } from '@/lib/replay-signs'

/** How far above its point a card floats, by the place it holds, so two cards
 * for nearby points do not sit on top of each other. */
const LIFT_PX = [20, 104] as const
/** What a card measures before the browser can say, and the shortest leader. */
const CARD_W = 220
const CARD_H = 72
const MIN_LEADER = 8
/** Room kept clear at the top for the Play, Pause and zoom controls. */
const TOP_MARGIN = 48

export interface SignView {
  /** The cards on screen now, in order; they win over a pin. */
  cards: ResolvedSign[]
  /** Every sign shows as a pin that is not a card (rest, fly-in, the end). */
  pins: boolean
  /** The replay is not running, so a tap on a photo opens it. */
  canOpenPhoto: boolean
}

export interface SignLayer {
  update(view: SignView): void
}

export interface SignLayerOptions {
  doc: Document
  root: HTMLElement
  stage: HTMLElement
  map: MapLibre.Map
  lib: Pick<typeof MapLibre, 'Marker'>
  id: string
  signs: readonly ResolvedSign[]
  /** Reduced motion: cards appear without an entrance animation. */
  still: boolean
  /** Told once when a sign's photo is not an image this card may show. */
  onProblem: (message: string) => void
}

interface Item {
  r: ResolvedSign
  photo: string | null
  pin: HTMLButtonElement
  card: HTMLElement
  box: HTMLElement
  surface: HTMLElement
  photoBtn: HTMLButtonElement | null
  close: HTMLButtonElement
  line: HTMLElement
  cardShown: boolean
  pinShown: boolean
}

export function createSignLayer(o: SignLayerOptions): SignLayer {
  const { doc, root, stage, map, lib, id, still } = o
  const MARGIN = 6
  const el = <K extends keyof HTMLElementTagNameMap>(tag: K, cls: string, text?: string) => {
    const e = doc.createElement(tag)
    e.className = cls
    if (text !== undefined) e.textContent = text
    return e
  }

  // ── the photo dialog ────────────────────────────────────────────────────

  const dialog = el('div', 'hcp-dlg')
  dialog.hidden = true
  dialog.setAttribute('role', 'dialog')
  dialog.setAttribute('aria-modal', 'true')
  dialog.setAttribute('aria-labelledby', `${id}-dlg-title`)
  const panel = el('div', 'hcp-dlg-panel')
  const dlgTitle = el('h4', 'hcp-dlg-title')
  dlgTitle.id = `${id}-dlg-title`
  const dlgNote = el('p', 'hcp-dlg-note')
  const dlgImg = el('img', 'hcp-dlg-img') as HTMLImageElement
  const dlgClose = el('button', 'hcp-dlg-close', 'Close') as HTMLButtonElement
  dlgClose.type = 'button'
  panel.append(dlgTitle, dlgNote, dlgImg, dlgClose)
  dialog.appendChild(panel)
  stage.appendChild(dialog)
  let opener: HTMLElement | null = null

  const onKey = (e: KeyboardEvent) => {
    if (e.key === 'Escape') { e.preventDefault(); closeDialog() }
    // The close button is the dialog's one control: Tab stays on it.
    else if (e.key === 'Tab') { e.preventDefault(); dlgClose.focus() }
  }
  function openDialog(item: Item, from: HTMLElement) {
    if (!item.photo) return
    opener = from
    dlgTitle.textContent = item.r.sign.title
    dlgNote.textContent = item.r.sign.note
    dlgNote.hidden = item.r.sign.note === ''
    dlgImg.src = item.photo
    dlgImg.alt = item.r.sign.title
    dialog.hidden = false
    doc.addEventListener('keydown', onKey)
    dlgClose.focus()
  }
  function closeDialog() {
    if (dialog.hidden) return
    dialog.hidden = true
    dlgImg.removeAttribute('src')
    doc.removeEventListener('keydown', onKey)
    if (opener && root.contains(opener)) opener.focus()
    opener = null
  }
  dlgClose.addEventListener('click', closeDialog)
  dialog.addEventListener('click', (e) => { if (e.target === dialog) closeDialog() })

  // ── pins and cards ──────────────────────────────────────────────────────

  let openIndex: number | null = null
  const items: Item[] = o.signs.map((r, i) => {
    const sign = r.sign
    let photo: string | null = null
    if (sign.photo_url !== null) {
      if (isExportPhoto(sign.photo_url)) photo = sign.photo_url
      else {
        console.error(`The photo for the sign "${sign.title}" is not an image this card may show; the sign is shown without it`)
        o.onProblem('A sign photo in this card is damaged')
      }
    }

    const pin = el('button', 'hcp-sign-pin') as HTMLButtonElement
    pin.type = 'button'
    pin.hidden = true
    pin.title = sign.title
    pin.setAttribute('aria-label', `Sign: ${sign.title}`)

    // The marker element is a point; the box inside it is placed round that
    // point in place(), so a card can slide or flip to stay inside the map.
    const card = el('div', 'hcp-sign')
    card.hidden = true
    const box = el('div', 'hcp-sign-box')
    const surface = el('div', 'hcp-sign-card')
    surface.setAttribute('role', 'group')
    surface.setAttribute('aria-label', `Sign: ${sign.title}`)
    let photoBtn: HTMLButtonElement | null = null
    if (photo) {
      photoBtn = el('button', 'hcp-sign-photo') as HTMLButtonElement
      photoBtn.type = 'button'
      photoBtn.disabled = true
      photoBtn.setAttribute('aria-label', `Open photo: ${sign.title}`)
      const img = el('img', 'hcp-sign-img') as HTMLImageElement
      img.src = photo
      img.alt = ''
      photoBtn.appendChild(img)
      surface.appendChild(photoBtn)
    }
    const text = el('span', 'hcp-sign-text')
    text.appendChild(el('span', 'hcp-sign-title', sign.title))
    if (sign.note) text.appendChild(el('span', 'hcp-sign-note', sign.note))
    surface.appendChild(text)
    const close = el('button', 'hcp-sign-close', '×') as HTMLButtonElement
    close.type = 'button'
    close.hidden = true
    close.setAttribute('aria-label', `Close sign: ${sign.title}`)
    const line = el('span', 'hcp-sign-line')
    line.setAttribute('aria-hidden', 'true')
    const dot = el('span', 'hcp-sign-dot')
    dot.setAttribute('aria-hidden', 'true')
    surface.appendChild(close)
    box.append(surface, line, dot)
    card.appendChild(box)

    const item: Item = { r, photo, pin, card, box, surface, photoBtn, close, line, cardShown: false, pinShown: false }
    pin.addEventListener('click', () => { openIndex = i; render(); close.focus() })
    close.addEventListener('click', () => { openIndex = null; render(); pin.focus() })
    photoBtn?.addEventListener('click', () => openDialog(item, photoBtn as HTMLElement))

    // MapLibre positions a marker with its own transform, which would undo the
    // pin's turn, so the pin sits in a holder that takes it instead.
    const holder = el('span', 'hcp-sign-holder')
    holder.appendChild(pin)
    // The z-index belongs to the marker's own element, which is what MapLibre
    // positions: pins draw under every card.
    holder.style.zIndex = '5'
    new lib.Marker({ element: holder, anchor: 'center' }).setLngLat([r.lon, r.lat]).addTo(map)
    new lib.Marker({ element: card, anchor: 'center' }).setLngLat([r.lon, r.lat]).addTo(map)
    // MapLibre labels every marker element a "Map marker" button. These hold
    // real controls of their own, so they are plain containers to a screen reader.
    for (const e of [holder, card]) { e.setAttribute('role', 'presentation'); e.removeAttribute('aria-label') }
    return item
  })

  let slots = new Map<number, number>()
  let view: SignView = { cards: [], pins: true, canOpenPhoto: true }

  function render() {
    // A card opened from a pin lasts only while pins are showing.
    if (!view.pins) openIndex = null
    const up = new Map<number, number>()
    view.cards.forEach((c, place) => {
      const at = items.findIndex((it) => it.r === c)
      if (at >= 0) up.set(at, place)
    })
    if (openIndex !== null && !up.has(openIndex)) up.set(openIndex, up.size)
    items.forEach((it, i) => {
      const place = up.get(i)
      const cardOn = place !== undefined
      const pinOn = view.pins && !cardOn
      if (it.pinShown !== pinOn) { it.pin.hidden = !pinOn; it.pinShown = pinOn }
      if (cardOn) {
        it.card.style.zIndex = String(10 + place)
        it.close.hidden = openIndex !== i
        if (it.photoBtn) it.photoBtn.disabled = !view.canOpenPhoto
      }
      if (it.cardShown !== cardOn) {
        it.cardShown = cardOn
        it.card.hidden = !cardOn
        it.card.classList.remove('hcp-in')
        if (cardOn) {
          // The entrance is a fade from the next frame on; reduced motion has none.
          const win = doc.defaultView
          if (still || !win) it.card.classList.add('hcp-in')
          else win.requestAnimationFrame(() => { if (it.cardShown) it.card.classList.add('hcp-in') })
        }
      }
    })
    slots = up
    place(up)
  }

  // Each shown card sits above its point on a leader line. Near an edge it
  // slides sideways (the leader stays under it), is shortened, or goes below
  // its point, so it is always whole inside the map.
  function place(up: Map<number, number>) {
    const c = map.getContainer()
    const W = c.clientWidth
    const H = c.clientHeight
    for (const [i, slot] of up) {
      const it = items[i]
      const lift: number = LIFT_PX[Math.min(slot, LIFT_PX.length - 1)]
      let dx = 0
      let below = false
      let leader = lift
      if (W > 0 && H > 0) {
        const p = map.project([it.r.lon, it.r.lat])
        const w = it.surface.offsetWidth || CARD_W
        const h = it.surface.offsetHeight || CARD_H
        const slide = w / 2 - 6
        const left = p.x - w / 2
        if (left < MARGIN) dx = Math.min(slide, MARGIN - left)
        else if (left + w > W - MARGIN) dx = -Math.min(slide, left + w - (W - MARGIN))
        const room = p.y - h - TOP_MARGIN
        if (room >= MIN_LEADER) leader = Math.min(lift, room)
        else {
          const under = H - MARGIN - p.y - h
          if (under > room) { below = true; leader = Math.max(MIN_LEADER, Math.min(lift, under)) } else leader = MIN_LEADER
        }
      }
      it.surface.style.transform = dx === 0 ? '' : `translateX(${Math.round(dx)}px)`
      it.box.classList.toggle('hcp-below', below)
      it.line.style.height = `${Math.round(leader)}px`
    }
  }
  // A map that moves under a paused card moves its anchor, so it is placed again.
  map.on('move', () => place(slots))

  return {
    update(next) {
      view = next
      render()
    },
  }
}
