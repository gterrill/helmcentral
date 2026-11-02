// Signs in the blog card (ADR 0205): pins, cards on a leader line and the photo
// dialog, built with DOM calls and textContent only. The rules for which cards
// are up come from the app (lib/replay-signs.ts); this file only draws them.
// Everything is scoped to one card: its elements live inside the card's root
// and carry ids made from the card's own id.

import type * as MapLibre from 'maplibre-gl'

import { isExportPhoto } from '@/lib/passage-export-signs'
import { SIGN_LIFT_PX, placeSignCard, type CardMode, type ResolvedSign } from '@/lib/replay-signs'

/** What a card measures before the browser can say. */
const CARD_W = 256
const CARD_H_PHOTO = 250
const CARD_H_TEXT = 72

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
  /** Where the card was put last; it stays there for as long as it fits. */
  mode: CardMode | undefined
  cardShown: boolean
  pinShown: boolean
}

export function createSignLayer(o: SignLayerOptions): SignLayer {
  const { doc, root, stage, map, lib, id, still } = o
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
    // point in place(), so a card can slide, go beside the point or shrink to
    // stay whole inside the map.
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
    box.appendChild(surface)
    // The card, the leader and the dot are placed from the point, by place().
    card.append(box, line, dot)

    const item: Item = { r, photo, pin, card, box, surface, photoBtn, close, line, mode: undefined, cardShown: false, pinShown: false }
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

  // Each shown card is put where lib/replay-signs says, the same as on the app's
  // map and in a video: above its point on a leader line, or below it, or beside
  // it, or made smaller if the map is too short, so it is always whole inside the map.
  function place(up: Map<number, number>) {
    const c = map.getContainer()
    const W = c.clientWidth
    const H = c.clientHeight
    for (const [i, slot] of up) {
      const it = items[i]
      const lift: number = SIGN_LIFT_PX[Math.min(slot, SIGN_LIFT_PX.length - 1)]
      const p = W > 0 && H > 0 ? map.project([it.r.lon, it.r.lat]) : { x: 0, y: 0 }
      const r = placeSignCard({
        point: { x: p.x, y: p.y },
        container: { w: W, h: H },
        card: { w: it.surface.offsetWidth || CARD_W, h: it.surface.offsetHeight || (it.photo ? CARD_H_PHOTO : CARD_H_TEXT) },
        lift,
        previous: it.mode,
      })
      it.mode = r.mode
      // The card's top left, from the point, and how much smaller than its own size it is.
      it.box.style.left = `${Math.round(r.rect.x - p.x)}px`
      it.box.style.top = `${Math.round(r.rect.y - p.y)}px`
      it.box.style.transform = r.scale === 1 ? '' : `scale(${Math.round(r.scale * 1000) / 1000})`
      it.box.style.visibility = r.hidden ? 'hidden' : ''
      it.line.hidden = r.leader === null || r.hidden
      if (r.leader) {
        const dx = r.leader.x2 - r.leader.x1
        const dy = r.leader.y2 - r.leader.y1
        it.line.style.width = `${Math.round(Math.hypot(dx, dy))}px`
        it.line.style.transform = `rotate(${Math.round((Math.atan2(dy, dx) * 1800) / Math.PI) / 10}deg)`
      }
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
