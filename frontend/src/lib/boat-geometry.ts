// The four boats of the passage replay, drawn from three.js geometry: no
// model files to source, licence or download. Metres, bow along +y, up is +z,
// centred on the middle of the hull, waterline at z = 0. Used only by the
// lazily loaded boat layer. The three.js namespace is handed in rather than
// imported: the app loads it from its own chunk, and the blog card loads it
// from a CDN, so this file draws the same boat in both.

import type { Group, Material, Mesh, Shape } from 'three'

import type { HullType } from '@/config/app-config'

// Light hull and a darker deck, so the boat reads from above on the pale chart
// and on dark satellite water alike.
export const BOAT_COLOURS = {
  hull: 0xf2f4f6,
  deck: 0x3f5a6e,
  cabin: 0xe6e9ec,
  windows: 0x161c24,
  mast: 0xaab4be,
  sail: 0xfdfdfd,
} as const

/** The three.js namespace, from wherever the caller loaded it. */
export type Three = typeof import('three')

export interface BoatDimensions {
  loaM: number
  beamM: number
}

export interface BoatModel {
  group: Group
  hullCount: number
}

const HULL_DEPTH = 0.09 // hull height as a fraction of LOA
const DRAFT = 0.4 // share of that below the waterline
const DEMIHULL_BEAM = 0.22 // catamaran demihull width as a fraction of beam

const lambert = (T: Three, color: number, doubleSided = false) =>
  new T.MeshLambertMaterial(doubleSided ? { color, side: T.DoubleSide } : { color })

/** A pointed plan, bow along +y. */
function plan(T: Three, length: number, width: number, bowShare: number): Shape {
  const bow = length * bowShare
  const s = new T.Shape()
  s.moveTo(-width / 2, -length / 2)
  s.lineTo(width / 2, -length / 2)
  s.lineTo(width / 2, length / 2 - bow)
  s.lineTo(0, length / 2)
  s.lineTo(-width / 2, length / 2 - bow)
  s.closePath()
  return s
}

function slab(T: Three, shape: Shape, depth: number, z: number, name: string, colour: number): Mesh {
  const mesh = new T.Mesh(new T.ExtrudeGeometry(shape, { depth, bevelEnabled: false }), lambert(T, colour, true))
  mesh.position.z = z
  mesh.name = name
  return mesh
}

function box(T: Three, name: string, w: number, l: number, h: number, x: number, y: number, z: number, colour: number): Mesh {
  const mesh = new T.Mesh(new T.BoxGeometry(w, l, h), lambert(T, colour))
  mesh.position.set(x, y, z + h / 2)
  mesh.name = name
  return mesh
}

/** One hull with its own deck, its top edge at z = freeboard. */
function addHull(T: Three, group: Group, length: number, width: number, x: number, y: number, height: number) {
  const draft = height * DRAFT
  const hull = slab(T, plan(T, length, width, 0.32), height, -draft, 'hull', BOAT_COLOURS.hull)
  const deck = slab(T, plan(T, length * 0.94, width * 0.78, 0.3), height * 0.12, height - draft, 'deck', BOAT_COLOURS.deck)
  for (const m of [hull, deck]) { m.position.x = x; m.position.y = y }
  group.add(hull, deck)
  return height - draft + height * 0.12 // z of the deck surface
}

/** A cabin with a dark band of windows around it. */
function addCabin(T: Three, group: Group, w: number, l: number, h: number, x: number, y: number, z: number) {
  group.add(box(T, 'cabin', w, l, h, x, y, z, BOAT_COLOURS.cabin))
  group.add(box(T, 'windows', w * 1.02, l * 1.02, h * 0.32, x, y, z + h * 0.38, BOAT_COLOURS.windows))
}

function addMastAndSail(T: Three, group: Group, loa: number, mastY: number, z: number) {
  const mastH = loa * 1.15
  const mast = new T.Mesh(new T.CylinderGeometry(loa * 0.008, loa * 0.012, mastH, 6), lambert(T, BOAT_COLOURS.mast))
  mast.rotation.x = Math.PI / 2
  mast.position.set(0, mastY, z + mastH / 2)
  mast.name = 'mast'
  group.add(mast)

  // A triangle in the boat's centre plane, aft of the mast.
  const boomZ = z + loa * 0.07
  const geo = new T.BufferGeometry()
  geo.setAttribute('position', new T.Float32BufferAttribute([
    0, mastY - loa * 0.02, boomZ,
    0, mastY - loa * 0.02, z + mastH * 0.97,
    0, mastY - loa * 0.4, boomZ,
  ], 3))
  geo.computeVertexNormals()
  const sail = new T.Mesh(geo, lambert(T, BOAT_COLOURS.sail, true))
  sail.name = 'sail'
  group.add(sail)
}

export function buildBoat(type: HullType, { loaM, beamM }: BoatDimensions, T: Three): BoatModel {
  const group = new T.Group()
  group.name = `boat-${type}`
  const height = loaM * HULL_DEPTH
  const sail = type.startsWith('sail')
  const cat = type.endsWith('cat')

  if (!cat) {
    const deckZ = addHull(T, group, loaM, beamM, 0, 0, height)
    if (sail) addMastAndSail(T, group, loaM, loaM * 0.08, deckZ)
    else addCabin(T, group, beamM * 0.62, loaM * 0.42, loaM * 0.12, 0, -loaM * 0.06, deckZ)
    return { group, hullCount: 1 }
  }

  const hullW = beamM * DEMIHULL_BEAM
  const offset = (beamM - hullW) / 2
  const deckZ = addHull(T, group, loaM, hullW, -offset, 0, height)
  addHull(T, group, loaM, hullW, offset, 0, height)
  // Spans the hulls, forward of the stern, at the height of their decks.
  const bridgeL = loaM * 0.52
  const bridgeH = height * 0.18
  const bridge = box(T, 'bridgedeck', beamM - hullW, bridgeL, bridgeH, 0, -loaM * 0.04, deckZ - bridgeH, BOAT_COLOURS.deck)
  group.add(bridge)
  if (sail) {
    addMastAndSail(T, group, loaM, loaM * 0.1, deckZ)
  } else {
    const cabinH = loaM * 0.1
    addCabin(T, group, (beamM - hullW) * 0.9, bridgeL * 0.8, cabinH, 0, -loaM * 0.04, deckZ)
    group.add(box(T, 'flybridge', (beamM - hullW) * 0.5, bridgeL * 0.35, loaM * 0.045, 0, -loaM * 0.1, deckZ + cabinH, BOAT_COLOURS.cabin))
  }
  return { group, hullCount: 2 }
}

/** Frees every geometry and material the boat holds on the GPU. */
export function disposeBoat(boat: BoatModel) {
  boat.group.traverse((o) => {
    const mesh = o as Mesh
    if (!mesh.isMesh) return
    mesh.geometry.dispose()
    const mats: Material[] = Array.isArray(mesh.material) ? mesh.material : [mesh.material]
    mats.forEach((m) => m.dispose())
  })
}
