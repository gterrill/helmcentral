// The four boats of the passage replay, drawn from three.js geometry: no
// model files to source, licence or download. Metres, bow along +y, up is +z,
// centred on the middle of the hull, waterline at z = 0. Used only by the
// lazily loaded boat layer. The three.js namespace is handed in rather than
// imported: the app loads it from its own chunk, and the blog card loads it
// from a CDN, so this file draws the same boat in both.

import type { BufferGeometry, Group, Material, Mesh, Shape } from 'three'

import type { HullType } from '@/config/app-config'

// Light hull and a darker deck, so the boat reads from above on the pale chart
// and on dark satellite water alike.
export const BOAT_COLOURS = {
  hull: 0xf2f4f6,
  deck: 0x3f5a6e,
  cabin: 0xe6e9ec,
  windows: 0x161c24,
  roof: 0xf4f6f8,
  nonskid: 0xb4bcc2,
  teak: 0x9a7b55,
  post: 0x2b2f34,
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


// ---------------------------------------------------------------------------
// Power catamaran. Proportions are read off the Granocean W-55 plan and
// profile drawings and held as fractions of LOA and beam, so any power cat
// of any size gets the same shape. u runs along the boat from the stern
// (-0.5) to the bow (+0.5); heights are fractions of LOA above the waterline.

const PC_HULL_BEAM = 0.32 // demihull width as a fraction of beam
const PC_DRAFT = 0.045 // below the waterline, fraction of LOA
const PC_SHEER_FWD = 0.117 // gunwale height at the stem
const PC_SHEER_AFT = 0.105 // and at the transom
const PC_DECK_Z = 0.108 // non-skid deck plates, just inside the gunwale
const PC_SOLE_Z = 0.09 // cockpit sole
const PC_SALON_AFT = -0.129 // the sliding doors, 0.371 LOA from the stern (GA plan)
const PC_SALON_FWD = 0.225 // glazing front, 0.725 LOA from the stern
const PC_FOREDECK_END = 0.42 // the bridgedeck stops here; the bows run on alone
const PC_ROOF_Z = 0.18 // underside of the flybridge deck
const PC_ROOF_T = 0.018 // the slab's lower layer; a wedge sits on top aft
const PC_FLY_TOP = 0.22 // top of the flybridge deck, aft
const PC_HARDTOP_Z = 0.31
const PC_RAKE = 0.01 // the salon front is near-vertical

// Half-width of a demihull as a share of its widest point, from the bow
// (0) to the stern (1): a fine entry forward, full aft. From 0.78 aft the
// hull top steps down to the swim platforms.
const PC_STATIONS: ReadonlyArray<readonly [number, number]> = [
  [0, 0.03], [0.02, 0.2], [0.06, 0.45], [0.12, 0.66], [0.18, 0.8],
  [0.29, 0.93], [0.43, 0.99], [0.65, 1], [0.78, 0.992], [0.8, 0.99], [0.86, 0.98], [0.9, 0.977], [1, 0.97],
]

const sheer = (tb: number) => PC_SHEER_FWD + (PC_SHEER_AFT - PC_SHEER_FWD) * tb

// Aft of the cockpit the whole hull top steps down to the swim platforms,
// with the transom cap at platform height.
const AFT_TOP: ReadonlyArray<readonly [number, number]> = [[0.78, PC_SHEER_AFT], [0.8, 0.07], [0.86, 0.05], [0.9, 0.03], [1, 0.027]]
const hullTop = (tb: number) => (tb <= 0.78 ? sheer(tb) : knots(AFT_TOP, tb))

/**
 * An open-topped demihull lofted through the stations, flat-shaded, its top
 * stepping down at the stern.
 */
function demihull(T: Three, loa: number, hullW: number, x: number): Mesh {
  const hw = hullW / 2
  const draft = PC_DRAFT * loa
  const pos: number[] = []
  const rings: number[][][] = PC_STATIONS.map(([tb, f]) => {
    const y = loa / 2 - tb * loa
    const w = f * hw
    const zt = hullTop(tb) * loa
    // The keel lifts out of the water towards the stem, giving the near-plumb bow.
    const zb = -draft * (1 - 0.85 * Math.max(0, 1 - tb / 0.1))
    return [[-w, y, zt], [-w * 0.92, y, 0], [-w * 0.45, y, zb], [w * 0.45, y, zb], [w * 0.92, y, 0], [w, y, zt]]
  })
  const tri = (a: number[], b: number[], c: number[]) => pos.push(...a, ...b, ...c)
  for (let i = 0; i + 1 < rings.length; i++) {
    for (let k = 0; k < 5; k++) {
      const a = rings[i][k], b = rings[i][k + 1], c = rings[i + 1][k + 1], d = rings[i + 1][k]
      tri(a, b, c)
      tri(a, c, d)
    }
  }
  for (const ring of [rings[0], rings[rings.length - 1]]) {
    for (let k = 1; k < 5; k++) tri(ring[0], ring[k], ring[k + 1])
  }
  const geo = new T.BufferGeometry()
  geo.setAttribute('position', new T.Float32BufferAttribute(pos, 3))
  geo.computeVertexNormals()
  const mesh = new T.Mesh(geo, lambert(T, BOAT_COLOURS.hull, true))
  mesh.position.x = x
  mesh.name = 'hull'
  return mesh
}

/** Piecewise-linear lookup over (s, v) knots. */
function knots(pts: ReadonlyArray<readonly [number, number]>, s: number): number {
  for (let i = 1; i < pts.length; i++) {
    if (s <= pts[i][0]) {
      const [s0, v0] = pts[i - 1]
      const [s1, v1] = pts[i]
      return v0 + ((v1 - v0) * (s - s0)) / (s1 - s0 || 1)
    }
  }
  return pts[pts.length - 1][1]
}

/**
 * A dark window band laid on a demihull's outboard topside, from s0 to s1
 * (fractions of LOA from the stern), between a bottom edge given as knots and
 * a straight top edge.
 */
function hullWindows(T: Three, loa: number, hullW: number, x: number, s0: number, s1: number, bottom: ReadonlyArray<readonly [number, number]>, top: number): Mesh {
  const hw = hullW / 2
  const side = x < 0 ? -1 : 1
  const n = 16
  const pos: number[] = []
  const pt = (i: number, isTop: boolean): number[] => {
    const s = s0 + ((s1 - s0) * i) / n
    const u = s - 0.5
    const z = (isTop ? top : knots(bottom, s)) * loa
    const zd = sheer(0.5 - u) * loa
    // The topside flares from 0.92 of the half-width at the waterline to full at the gunwale.
    const edge = knots(PC_STATIONS, 0.5 - u) * hw * (0.92 + 0.08 * (z / zd)) + loa * 0.0008
    return [x + side * edge, u * loa, z]
  }
  for (let i = 0; i < n; i++) {
    const a = pt(i, true), b = pt(i + 1, true), c = pt(i + 1, false), d = pt(i, false)
    pos.push(...a, ...b, ...c, ...a, ...c, ...d)
  }
  const geo = new T.BufferGeometry()
  geo.setAttribute('position', new T.Float32BufferAttribute(pos, 3))
  geo.computeVertexNormals()
  const mesh = new T.Mesh(geo, lambert(T, BOAT_COLOURS.windows, true))
  mesh.name = 'hull-windows'
  return mesh
}

/** A prism whose side profile is given as (u, z) fractions, spanning `width` across. */
function profilePrism(T: Three, name: string, pts: ReadonlyArray<readonly [number, number]>, loa: number, width: number, colour: number): Mesh {
  const s = new T.Shape()
  pts.forEach(([u, z], i) => (i ? s.lineTo(u * loa, z * loa) : s.moveTo(u * loa, z * loa)))
  s.closePath()
  const geo: BufferGeometry = new T.ExtrudeGeometry(s, { depth: width, bevelEnabled: false })
  // (shape x, shape y, extrusion) -> (y, z, x)
  geo.applyMatrix4(new T.Matrix4().set(0, 0, 1, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 0, 1))
  geo.translate(-width / 2, 0, 0)
  const mesh = new T.Mesh(geo, lambert(T, colour))
  mesh.name = name
  return mesh
}

/**
 * A flat plan slab from u0 (aft) to u1 (fore): aft corners cut by `aftCut`,
 * with a rounded front of depth `foreCut` (both fractions of LOA).
 */
function planSlab(T: Three, name: string, u0: number, u1: number, halfW: number, aftCut: number, foreCut: number, z0: number, thick: number, loa: number, colour: number): Mesh {
  const a = u0 * loa, b = u1 * loa
  const ca = aftCut * loa, cf = foreCut * loa
  const s = new T.Shape()
  s.moveTo(-halfW + ca, a)
  s.lineTo(halfW - ca, a)
  s.lineTo(halfW, a + ca)
  s.lineTo(halfW, b - cf)
  s.lineTo(halfW * 0.93, b - cf * 0.45)
  s.lineTo(halfW * 0.7, b - cf * 0.15)
  s.lineTo(halfW * 0.35, b - cf * 0.03)
  s.lineTo(0, b)
  s.lineTo(-halfW * 0.35, b - cf * 0.03)
  s.lineTo(-halfW * 0.7, b - cf * 0.15)
  s.lineTo(-halfW * 0.93, b - cf * 0.45)
  s.lineTo(-halfW, b - cf)
  s.lineTo(-halfW, a + ca)
  s.closePath()
  const mesh = new T.Mesh(new T.ExtrudeGeometry(s, { depth: thick, bevelEnabled: false }), lambert(T, colour))
  mesh.position.z = z0
  mesh.name = name
  return mesh
}

/** A box between two plan stations (u) and two heights, centred across at x. */
function span(T: Three, name: string, loa: number, u0: number, u1: number, width: number, x: number, z0: number, z1: number, colour: number): Mesh {
  return box(T, name, width, (u1 - u0) * loa, z1 - z0, x, ((u0 + u1) / 2) * loa, z0, colour)
}

/** A slim post from (x, u, zBase) up to zTop, its head leaning aft by `lean` of LOA. */
function post(T: Three, loa: number, x: number, u: number, zBase: number, zTop: number, lean: number, thick: number): Mesh {
  const h = zTop - zBase
  const dy = lean * loa
  const len = Math.hypot(h, dy)
  const mesh = new T.Mesh(new T.BoxGeometry(thick, thick, len), lambert(T, BOAT_COLOURS.post))
  mesh.position.set(x, u * loa - dy / 2, zBase + h / 2)
  mesh.rotation.x = Math.atan2(dy, h)
  mesh.name = 'post'
  return mesh
}

function addPowerCat(T: Three, group: Group, loa: number, beam: number) {
  const hullW = beam * PC_HULL_BEAM
  const centre = (beam - hullW) / 2
  const zd = PC_DECK_Z * loa
  const sole = PC_SOLE_Z * loa
  const H = (z: number) => z * loa
  const hullWindowTop = 0.085
  const mainBottom: ReadonlyArray<readonly [number, number]> = [
    [0.27, 0.05], [0.32, 0.016], [0.4, 0.008], [0.55, 0.02], [0.7, 0.045], [0.85, 0.075], [0.9, hullWindowTop],
  ]
  const slotBottom: ReadonlyArray<readonly [number, number]> = [[0.21, 0.072], [0.27, 0.072]]

  for (const x of [-centre, centre]) {
    group.add(demihull(T, loa, hullW, x))
    group.add(hullWindows(T, loa, hullW, x, 0.27, 0.9, mainBottom, hullWindowTop))
    group.add(hullWindows(T, loa, hullW, x, 0.21, 0.27, slotBottom, hullWindowTop))
  }

  // Non-skid deck on each hull, from the stem back to the salon doors.
  for (const x of [-centre, centre]) {
    const hw = (hullW / 2) * 0.95
    const deck = new T.Shape()
    const tbEnd = 0.5 - PC_SALON_AFT
    const fore = PC_STATIONS.filter(([tb]) => tb <= tbEnd && tb >= 0.02)
    fore.forEach(([tb, f], i) => (i ? deck.lineTo(-f * hw, loa / 2 - tb * loa) : deck.moveTo(-f * hw, loa / 2 - tb * loa)))
    deck.lineTo(-hw, PC_SALON_AFT * loa)
    deck.lineTo(hw, PC_SALON_AFT * loa)
    for (let i = fore.length - 1; i >= 0; i--) deck.lineTo(fore[i][1] * hw, loa / 2 - fore[i][0] * loa)
    deck.closePath()
    const plate = new T.Mesh(new T.ExtrudeGeometry(deck, { depth: H(0.004), bevelEnabled: false }), lambert(T, BOAT_COLOURS.nonskid, true))
    plate.position.set(x, 0, zd - H(0.004))
    plate.name = 'deck'
    group.add(plate)
  }

  // Bridgedeck between the hulls: a long open foredeck, no structure on it.
  // Its plan follows each hull's inboard edge, so it meets them all the way forward.
  const gap = beam - 2 * hullW + beam * 0.04
  const bridge = new T.Shape()
  const tbA = 0.5 - PC_FOREDECK_END, tbB = 0.5 - PC_SALON_AFT
  const edgeX = (tb: number) => centre - knots(PC_STATIONS, tb) * (hullW / 2) * 0.9
  const run = [tbA, ...PC_STATIONS.map(([tb]) => tb).filter((tb) => tb > tbA && tb < tbB), tbB]
  run.forEach((tb, i) => (i ? bridge.lineTo(-edgeX(tb), loa / 2 - tb * loa) : bridge.moveTo(-edgeX(tb), loa / 2 - tb * loa)))
  for (let i = run.length - 1; i >= 0; i--) bridge.lineTo(edgeX(run[i]), loa / 2 - run[i] * loa)
  bridge.closePath()
  const deckSlab = new T.Mesh(new T.ExtrudeGeometry(bridge, { depth: zd - H(0.05), bevelEnabled: false }), lambert(T, BOAT_COLOURS.nonskid, true))
  deckSlab.position.z = H(0.05)
  deckSlab.name = 'bridgedeck'
  group.add(deckSlab)

  // Cockpit: a teak sole from the salon doors aft to 0.2 LOA from the stern,
  // steps down each hull's inboard side to low swim platforms, and the tender
  // lift between the hulls just below sole level.
  const soleW = beam - hullW * 0.24
  group.add(span(T, 'cockpit', loa, -0.3, PC_SALON_AFT, soleW, 0, sole - H(0.012), sole, BOAT_COLOURS.teak))
  const inner = (x: number) => (x < 0 ? 1 : -1)
  for (const x of [-centre, centre]) {
    const sx = x + inner(x) * hullW * 0.22
    group.add(span(T, 'swim-platform', loa, -0.5, -0.3, hullW * 0.9, x, H(0.013), H(0.025), BOAT_COLOURS.teak))
    group.add(span(T, 'stern-step', loa, -0.4, -0.3, hullW * 0.46, sx, H(0.025), H(0.05), BOAT_COLOURS.hull))
    group.add(span(T, 'stern-step', loa, -0.36, -0.3, hullW * 0.46, sx, H(0.05), H(0.07), BOAT_COLOURS.hull))
  }
  group.add(span(T, 'tender-lift', loa, -0.5, -0.337, gap, 0, H(0.063), H(0.075), BOAT_COLOURS.nonskid))

  // Salon: dark glazing from the sliding doors to a near-vertical front.
  const salonW = beam * 0.73
  const rake = (z: number) => PC_SALON_FWD - PC_RAKE * ((z - PC_DECK_Z) / (PC_ROOF_Z - PC_DECK_Z))
  group.add(profilePrism(T, 'cabin', [[PC_SALON_AFT, PC_DECK_Z], [PC_SALON_FWD, PC_DECK_Z], [rake(PC_ROOF_Z), PC_ROOF_Z], [PC_SALON_AFT, PC_ROOF_Z]], loa, salonW, BOAT_COLOURS.cabin))
  const gl0 = 0.12, gl1 = 0.178
  group.add(profilePrism(T, 'windows', [[PC_SALON_AFT - 0.002, gl0], [rake(gl0) + 0.003, gl0], [rake(gl1) + 0.003, gl1], [PC_SALON_AFT - 0.002, gl1]], loa, salonW * 1.01, BOAT_COLOURS.windows))

  // Flybridge deck: a thick white slab, 0.08 to 0.755 LOA from the stern, full
  // beam with a curved front, flat under the helm house, thinning forward over the salon windscreen.
  group.add(planSlab(T, 'flybridge', -0.42, 0.255, beam * 0.49, 0.02, 0.12, H(PC_ROOF_Z), H(PC_ROOF_T), loa, BOAT_COLOURS.roof))
  const wedgeZ0 = PC_ROOF_Z + PC_ROOF_T
  group.add(profilePrism(T, 'flybridge-top', [[-0.42, wedgeZ0], [0.18, wedgeZ0], [0.18, wedgeZ0 + 0.003], [0.06, PC_FLY_TOP], [-0.42, PC_FLY_TOP]], loa, beam * 0.86, BOAT_COLOURS.roof))
  // One slim post each side overhanging the cockpit, leaning forward as it rises.
  for (const sx of [-1, 1]) group.add(post(T, loa, sx * beam * 0.42, -0.34, sole, H(PC_ROOF_Z), -0.03, loa * 0.006))

  // Enclosed helm house on the flybridge deck, glazed all round.
  const houseW = beam * 0.5
  const houseTop = PC_HARDTOP_Z - 0.002
  const houseFront = (z: number) => 0.05 - 0.025 * ((z - PC_FLY_TOP) / (houseTop - PC_FLY_TOP))
  group.add(profilePrism(T, 'helm', [[-0.23, PC_FLY_TOP], [houseFront(PC_FLY_TOP), PC_FLY_TOP], [houseFront(houseTop), houseTop], [-0.23, houseTop]], loa, houseW, BOAT_COLOURS.cabin))
  const hg0 = PC_FLY_TOP + 0.02, hg1 = houseTop - 0.012
  group.add(profilePrism(T, 'helm-windows', [[-0.232, hg0], [houseFront(hg0) + 0.003, hg0], [houseFront(hg1) + 0.003, hg1], [-0.232, hg1]], loa, houseW * 1.01, BOAT_COLOURS.windows))

  // Hardtop: a thin white roof, 0.22 to 0.56 LOA from the stern, front edge upswept.
  const htHalf = beam * 0.34
  group.add(planSlab(T, 'hardtop', -0.28, 0.06, htHalf, 0.02, 0.05, H(PC_HARDTOP_Z), H(0.009), loa, BOAT_COLOURS.roof))
  const lip = new T.Mesh(new T.BoxGeometry(htHalf * 1.7, H(0.03), H(0.007)), lambert(T, BOAT_COLOURS.roof))
  lip.position.set(0, H(0.074), H(PC_HARDTOP_Z + 0.007))
  lip.rotation.x = 0.3
  lip.name = 'hardtop-lip'
  group.add(lip)

  // A tall thin antenna mast at the aft end of the hardtop, and the radar dome forward.
  const hardtopTop = H(PC_HARDTOP_Z + 0.009)
  group.add(post(T, loa, 0, -0.27, hardtopTop, H(0.4), 0, loa * 0.008))
  const dome = new T.Mesh(new T.SphereGeometry(H(0.011), 8, 5), lambert(T, BOAT_COLOURS.roof))
  dome.position.set(htHalf * 0.5, 0.04 * loa, hardtopTop + H(0.006))
  dome.name = 'radar-dome'
  group.add(dome)
}

export function buildBoat(type: HullType, { loaM, beamM }: BoatDimensions, T: Three): BoatModel {
  const group = new T.Group()
  group.name = `boat-${type}`
  const height = loaM * HULL_DEPTH
  const sail = type.startsWith('sail')
  const cat = type.endsWith('cat')

  if (type === 'power_cat') {
    addPowerCat(T, group, loaM, beamM)
    return { group, hullCount: 2 }
  }

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
  addMastAndSail(T, group, loaM, loaM * 0.1, deckZ)
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
