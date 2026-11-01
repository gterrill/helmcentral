import { useEffect, useMemo, useState } from 'react'

import { signPhotoSrc } from '@/components/passages/sign-card'
import { momentsOf, photoMomentOn, type ResolvedSign } from '@/lib/replay-signs'

/** The signs whose photos hold the replay for a moment, as the timing wants
 * them: where the boat meets each, in a list that keeps its identity while
 * the moments themselves are the same (a note edited does not rebuild the
 * replay's timing).
 *
 * The photos are fetched ahead. One that fails to load is left out, so the
 * replay does not stop for 2.3 seconds over a broken picture; it is logged,
 * and the sign's card still says "Photo not available". */
export function useSignMoments(placed: readonly ResolvedSign[]): { clock: number; id: string }[] {
  const wanted = placed.filter((r) => r.sign.title !== '' && photoMomentOn(r.sign))
  // A photo is known by the sign and the address it was asked for, so a sign given a different photo is tried afresh.
  const keyOf = (r: ResolvedSign) => `${r.sign.id}|${signPhotoSrc(r.sign)}`
  const wantedKey = wanted.map(keyOf).join(',')
  const [failed, setFailed] = useState<ReadonlySet<string>>(new Set())

  useEffect(() => {
    let live = true
    const images = wanted.map((r) => {
      const image = new Image()
      image.onerror = () => {
        if (!live) return
        console.warn(`Photo moment left out for sign "${r.sign.title}" (${r.sign.id}): its photo did not load`)
        setFailed((prev) => new Set(prev).add(keyOf(r)))
      }
      image.src = signPhotoSrc(r.sign) ?? ''
      return image
    })
    return () => {
      live = false
      for (const image of images) image.onerror = null
    }
    // The photos asked for are named by wantedKey.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [wantedKey])

  const left = momentsOf(placed, new Set(placed.filter((r) => failed.has(keyOf(r))).map((r) => r.sign.id)))
  const key = left.map((m) => `${m.id}:${m.clock}`).join('|')
  // eslint-disable-next-line react-hooks/exhaustive-deps
  return useMemo(() => left, [key])
}
