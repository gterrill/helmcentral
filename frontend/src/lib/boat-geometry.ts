// The four boats of the passage replay, drawn from three.js geometry: no
// model files to source, licence or download. Metres, bow along +y, up is +z,
// centred on the middle of the hull, waterline at z = 0. Imported only by the
// lazily loaded boat layer.

import {
  BoxGeometry,
  BufferGeometry,
  CylinderGeometry,
  DoubleSide,
  ExtrudeGeometry,
  Float32BufferAttribute,
  Group,
  Mesh,
  MeshLambertMaterial,
  Shape,
  type Material,
} from 'three'

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

const lambert = (color: number, doubleSided = false) =>
  new MeshLambertMaterial(doubleSided ? { color, side: DoubleSide } : { color })

/** A pointed plan, bow along +y. */
function plan(length: number, width: number, bowShare: number): Shape {
  const bow = length * bowShare
  const s = new Shape()
  s.moveTo(-width / 2, -length / 2)
  s.lineTo(width / 2, -length / 2)
  s.lineTo(width / 2, length / 2 - bow)
  s.lineTo(0, length / 2)
  s.lineTo(-width / 2, length / 2 - bow)
  s.closePath()
  return s
}

function slab(shape: Shape, depth: number, z: number, name: string, colour: number): Mesh {
  const mesh = new Mesh(new ExtrudeGeometry(shape, { depth, bevelEnabled: false }), lambert(colour, true))
  mesh.position.z = z
  mesh.name = name
  return mesh
}

function box(name: string, w: number, l: number, h: number, x: number, y: number, z: number, colour: number): Mesh {
  const mesh = new Mesh(new BoxGeometry(w, l, h), lambert(colour))
  mesh.position.set(x, y, z + h / 2)
  mesh.name = name
  return mesh
}

/** One hull with its own deck, its top edge at z = freeboard. */
function addHull(group: Group, length: number, width: number, x: number, y: number, height: number) {
  const draft = height * DRAFT
  const hull = slab(plan(length, width, 0.32), height, -draft, 'hull', BOAT_COLOURS.hull)
  const deck = slab(plan(length * 0.94, width * 0.78, 0.3), height * 0.12, height - draft, 'deck', BOAT_COLOURS.deck)
  for (const m of [hull, deck]) { m.position.x = x; m.position.y = y }
  group.add(hull, deck)
  return height - draft + height * 0.12 // z of the deck surface
}

/** A cabin with a dark band of windows around it. */
function addCabin(group: Group, w: number, l: number, h: number, x: number, y: number, z: number) {
  group.add(box('cabin', w, l, h, x, y, z, BOAT_COLOURS.cabin))
  group.add(box('windows', w * 1.02, l * 1.02, h * 0.32, x, y, z + h * 0.38, BOAT_COLOURS.windows))
}

function addMastAndSail(group: Group, loa: number, mastY: number, z: number) {
  const mastH = loa * 1.15
  const mast = new Mesh(new CylinderGeometry(loa * 0.008, loa * 0.012, mastH, 6), lambert(BOAT_COLOURS.mast))
  mast.rotation.x = Math.PI / 2
  mast.position.set(0, mastY, z + mastH / 2)
  mast.name = 'mast'
  group.add(mast)

  // A triangle in the boat's centre plane, aft of the mast.
  const boomZ = z + loa * 0.07
  const geo = new BufferGeometry()
  geo.setAttribute('position', new Float32BufferAttribute([
    0, mastY - loa * 0.02, boomZ,
    0, mastY - loa * 0.02, z + mastH * 0.97,
    0, mastY - loa * 0.4, boomZ,
  ], 3))
  geo.computeVertexNormals()
  const sail = new Mesh(geo, lambert(BOAT_COLOURS.sail, true))
  sail.name = 'sail'
  group.add(sail)
}

export function buildBoat(type: HullType, { loaM, beamM }: BoatDimensions): BoatModel {
  const group = new Group()
  group.name = `boat-${type}`
  const height = loaM * HULL_DEPTH
  const sail = type.startsWith('sail')
  const cat = type.endsWith('cat')

  if (!cat) {
    const deckZ = addHull(group, loaM, beamM, 0, 0, height)
    if (sail) addMastAndSail(group, loaM, loaM * 0.08, deckZ)
    else addCabin(group, beamM * 0.62, loaM * 0.42, loaM * 0.12, 0, -loaM * 0.06, deckZ)
    return { group, hullCount: 1 }
  }

  const hullW = beamM * DEMIHULL_BEAM
  const offset = (beamM - hullW) / 2
  const deckZ = addHull(group, loaM, hullW, -offset, 0, height)
  addHull(group, loaM, hullW, offset, 0, height)
  // Spans the hulls, forward of the stern, at the height of their decks.
  const bridgeL = loaM * 0.52
  const bridgeH = height * 0.18
  const bridge = box('bridgedeck', beamM - hullW, bridgeL, bridgeH, 0, -loaM * 0.04, deckZ - bridgeH, BOAT_COLOURS.deck)
  group.add(bridge)
  if (sail) {
    addMastAndSail(group, loaM, loaM * 0.1, deckZ)
  } else {
    const cabinH = loaM * 0.1
    addCabin(group, (beamM - hullW) * 0.9, bridgeL * 0.8, cabinH, 0, -loaM * 0.04, deckZ)
    group.add(box('flybridge', (beamM - hullW) * 0.5, bridgeL * 0.35, loaM * 0.045, 0, -loaM * 0.1, deckZ + cabinH, BOAT_COLOURS.cabin))
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
