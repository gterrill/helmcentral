import maplibregl from 'maplibre-gl'

import type { ExportData } from '@/lib/passage-export'
import { IMAGE_SIZES, composeImageLayout, paintOverlay, roundRectPath, type ImageKind, type ImageLayout, type Measure } from '@/lib/passage-export-layout'

// Static images of a passage for the blog's feature image or a social card
// (ADR 0174). The map is drawn offscreen with MapLibre in the same Voyager and
// seamark style as the embed, then the title and figures are painted over it
// with Canvas 2D.

// ── the map ──────────────────────────────────────────────────────────────

const RENDER_TIMEOUT_MS = 30_000

interface OffscreenMapOptions {
  width: number
  height: number
  pixelRatio: number
  bounds?: ExportData['bounds']
  padding?: ImageLayout['padding']
  center?: [number, number]
  zoom?: number
  withTrack: boolean
  withBox: boolean
}

function bboxPolygon(b: ExportData['bounds']): GeoJSON.Feature<GeoJSON.Polygon> {
  return {
    type: 'Feature', properties: {},
    geometry: { type: 'Polygon', coordinates: [[[b[0], b[1]], [b[2], b[1]], [b[2], b[3]], [b[0], b[3]], [b[0], b[1]]]] },
  }
}

function trackCollection(data: ExportData): GeoJSON.FeatureCollection {
  const features: GeoJSON.Feature[] = []
  for (const p of data.passages) {
    for (const seg of p.s) {
      if (seg.length < 2) continue
      features.push({ type: 'Feature', properties: { c: p.c }, geometry: { type: 'LineString', coordinates: seg.map((pt) => [pt[1], pt[2]]) } })
    }
  }
  return { type: 'FeatureCollection', features }
}

/** Draws one map into a hidden element and hands back a copy of its canvas once
 * every tile has loaded. Fails, rather than returns a half-drawn map. */
async function renderMap(data: ExportData, o: OffscreenMapOptions): Promise<HTMLCanvasElement> {
  const host = document.createElement('div')
  host.style.cssText = `position:fixed;left:-99999px;top:0;width:${o.width}px;height:${o.height}px;`
  document.body.appendChild(host)
  let map: maplibregl.Map | null = null
  try {
    const m = new maplibregl.Map({
      container: host,
      style: data.map.style,
      interactive: false,
      attributionControl: false,
      fadeDuration: 0,
      pixelRatio: o.pixelRatio,
      canvasContextAttributes: { preserveDrawingBuffer: true },
      ...(o.bounds ? { bounds: o.bounds, fitBoundsOptions: { padding: o.padding, maxZoom: 14 } } : { center: o.center, zoom: o.zoom }),
    })
    map = m
    await new Promise<void>((resolve, reject) => {
      const timer = setTimeout(() => reject(new Error('The map did not finish drawing. Check the internet connection and try again.')), RENDER_TIMEOUT_MS)
      m.on('error', (e) => {
        // A missing tile is not fatal; a style that never loads is, and the timeout reports it.
        console.warn('Passage image map:', e.error?.message ?? e)
      })
      m.once('load', () => {
        const seamarks = data.map.seamarks
        m.addSource(seamarks.id, { type: 'raster', tiles: [seamarks.tiles], tileSize: 256 })
        m.addLayer({ id: seamarks.layerId, type: 'raster', source: seamarks.id, paint: { 'raster-opacity': 0.85 } })
        if (o.withTrack) {
          m.addSource(data.map.trackSource, { type: 'geojson', data: trackCollection(data) })
          for (const layer of data.map.layers) {
            if ((layer as { source: string }).source === data.map.trackSource) m.addLayer(layer as maplibregl.LayerSpecification)
          }
        }
        if (o.withBox) {
          m.addSource('hcp-box', { type: 'geojson', data: bboxPolygon(data.bounds) })
          m.addLayer({ id: 'hcp-box-fill', type: 'fill', source: 'hcp-box', paint: { 'fill-color': '#e11d48', 'fill-opacity': 0.2 } })
          m.addLayer({ id: 'hcp-box-line', type: 'line', source: 'hcp-box', paint: { 'line-color': '#e11d48', 'line-width': 2 } })
        }
        m.once('idle', () => { clearTimeout(timer); resolve() })
        m.triggerRepaint()
      })
    })
    return copyCanvas(m.getCanvas())
  } finally {
    map?.remove()
    host.remove()
  }
}

function copyCanvas(src: HTMLCanvasElement): HTMLCanvasElement {
  const out = document.createElement('canvas')
  out.width = src.width
  out.height = src.height
  const ctx = out.getContext('2d')
  if (!ctx) throw new Error('This browser cannot draw the image')
  ctx.drawImage(src, 0, 0)
  return out
}

/** The finished PNG for one image size. */
export async function renderPassageImage(data: ExportData, kind: ImageKind): Promise<Blob> {
  const size = IMAGE_SIZES[kind]
  const layout = composeImageLayout(
    { width: size.width, height: size.height, title: data.title, dates: data.dates, stats: data.stats, inset: data.inset },
    measureWithCanvas(),
  )
  const mapPromise = renderMap(data, {
    width: size.width, height: size.height, pixelRatio: 2, bounds: data.bounds, padding: layout.padding,
    withTrack: true, withBox: false,
  })
  const insetPromise = layout.inset
    ? renderMap(data, {
      width: layout.inset.w, height: layout.inset.h, pixelRatio: 2,
      center: [(data.bounds[0] + data.bounds[2]) / 2, (data.bounds[1] + data.bounds[3]) / 2],
      zoom: data.insetZoom, withTrack: false, withBox: true,
    })
    : Promise.resolve(null)
  const [mapCanvas, insetCanvas] = await Promise.all([mapPromise, insetPromise])

  const out = document.createElement('canvas')
  out.width = size.width * size.outScale
  out.height = size.height * size.outScale
  const ctx = out.getContext('2d')
  if (!ctx) throw new Error('This browser cannot draw the image')
  ctx.scale(size.outScale, size.outScale)
  ctx.imageSmoothingQuality = 'high'
  ctx.drawImage(mapCanvas, 0, 0, size.width, size.height)
  if (insetCanvas && layout.inset) {
    const r = layout.inset
    ctx.save()
    ctx.shadowColor = 'rgba(0, 0, 0, 0.35)'
    ctx.shadowBlur = 8
    roundRectPath(ctx, r, 8)
    ctx.fillStyle = '#ffffff'
    ctx.fill()
    ctx.restore()
    ctx.save()
    roundRectPath(ctx, { x: r.x + 3, y: r.y + 3, w: r.w - 6, h: r.h - 6 }, 6)
    ctx.clip()
    ctx.drawImage(insetCanvas, r.x + 3, r.y + 3, r.w - 6, r.h - 6)
    ctx.restore()
  }
  paintOverlay(ctx, layout)
  return await new Promise<Blob>((resolve, reject) => {
    out.toBlob((b) => (b ? resolve(b) : reject(new Error('Could not encode the image'))), 'image/png')
  })
}

function measureWithCanvas(): Measure {
  const ctx = document.createElement('canvas').getContext('2d')
  if (!ctx) throw new Error('This browser cannot draw the image')
  return (text, f) => {
    ctx.font = f
    return ctx.measureText(text).width
  }
}
