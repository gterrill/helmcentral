// The replay player that ships inside a blog export (ADR 0174). It runs in a
// reader's browser on a blog page, with MapLibre already loaded, and knows
// nothing about Helmcentral. This file is read as text by passage-export.ts and
// pasted into the snippet, so it must stay plain script: no imports, no
// modules, and every visible string is set with textContent.
function hcPassageCardInit(id, maplibregl, doc) {
  var root = doc.getElementById(id)
  var data = JSON.parse(doc.getElementById(id + '-data').textContent)
  function part(name) { return root.querySelector('.hcp-' + name) }
  var els = {
    title: part('title'), dates: part('dates'), stats: part('stats'), map: part('map'),
    inset: part('inset'), readout: part('readout'), caption: part('caption'),
    passage: part('passage'), time: part('time'), btn: part('btn'), label: part('btn-label'),
    error: part('error'),
  }
  var MONTHS = ['Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun', 'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec']

  els.title.textContent = data.title
  els.dates.textContent = data.dates
  data.stats.forEach(function (s) {
    var item = doc.createElement('div')
    item.className = 'hcp-stat'
    var k = doc.createElement('span')
    k.className = 'hcp-k'
    k.textContent = s.l
    var v = doc.createElement('span')
    v.className = 'hcp-v'
    v.textContent = s.v
    item.appendChild(k)
    item.appendChild(v)
    els.stats.appendChild(item)
  })
  if (data.stats.length === 0) els.stats.hidden = true

  // ── time and position ──────────────────────────────────────────────────

  function pad(n) { return (n < 10 ? '0' : '') + n }
  function fmtLocal(unix) {
    var d = new Date((unix + data.zone.o * 3600) * 1000)
    return d.getUTCDate() + ' ' + MONTHS[d.getUTCMonth()] + ' ' + pad(d.getUTCHours()) + ':' + pad(d.getUTCMinutes()) + ' ' + data.zone.l
  }

  // Where the boat is at t inside one passage: interpolated within a segment,
  // holding at the last fix across a gap in the log.
  function positionIn(p, t) {
    var held = null
    for (var si = 0; si < p.s.length; si++) {
      var seg = p.s[si]
      if (seg.length === 0) continue
      if (held === null) held = seg[0]
      if (t < seg[0][0]) return [held[1], held[2]]
      if (t <= seg[seg.length - 1][0]) {
        for (var i = 1; i < seg.length; i++) {
          if (t <= seg[i][0]) {
            var a = seg[i - 1], b = seg[i]
            var span = b[0] - a[0]
            var f = span > 0 ? (t - a[0]) / span : 1
            return [a[1] + (b[1] - a[1]) * f, a[2] + (b[2] - a[2]) * f]
          }
        }
      }
      held = seg[seg.length - 1]
    }
    return held ? [held[1], held[2]] : null
  }

  // The line covered so far in one passage, up to t.
  function travelled(p, t) {
    var lines = []
    for (var si = 0; si < p.s.length; si++) {
      var seg = p.s[si]
      if (seg.length === 0 || t < seg[0][0]) break
      var line = []
      for (var i = 0; i < seg.length; i++) {
        if (seg[i][0] <= t) line.push([seg[i][1], seg[i][2]])
        else {
          var a = seg[i - 1], b = seg[i]
          var f = (t - a[0]) / (b[0] - a[0])
          line.push([a[1] + (b[1] - a[1]) * f, a[2] + (b[2] - a[2]) * f])
          break
        }
      }
      if (line.length >= 2) lines.push(line)
    }
    return lines
  }

  function lastFix(p) {
    for (var si = p.s.length - 1; si >= 0; si--) {
      var seg = p.s[si]
      if (seg.length) return [seg[seg.length - 1][1], seg[seg.length - 1][2]]
    }
    return null
  }

  // ── the plan: passages end to end, each night a short pause ───────────

  var underway = 0
  data.passages.forEach(function (p) { underway += Math.max(1, p.b - p.a) })
  var scale = data.play / underway
  var steps = []
  var total = 0
  data.passages.forEach(function (p, i) {
    if (p.nb > 0) {
      var prev = data.passages[i - 1]
      var each = Math.min(data.nightSeconds, data.nightCap / p.nb)
      for (var k = 0; k < p.nb; k++) {
        steps.push({
          kind: 'night', at: total, dur: each, i: i - 1, night: p.n0 + k, place: p.pl,
          t0: prev.b + ((p.a - prev.b) * k) / p.nb, t1: prev.b + ((p.a - prev.b) * (k + 1)) / p.nb,
        })
        total += each
      }
    }
    var dur = Math.max(1, p.b - p.a) * scale
    steps.push({ kind: 'run', at: total, dur: dur, i: i, t0: p.a, t1: p.b })
    total += dur
  })

  function stepAt(clock) {
    for (var i = steps.length - 1; i >= 0; i--) if (clock >= steps[i].at) return steps[i]
    return steps[0]
  }

  // The whole state of the replay at a clock value in seconds.
  function stateAt(clock) {
    var c = Math.max(0, Math.min(total, clock))
    var s = stepAt(c)
    var f = s.dur > 0 ? Math.min(1, (c - s.at) / s.dur) : 1
    var t = s.t0 + (s.t1 - s.t0) * f
    var p = data.passages[s.i]
    var pos = s.kind === 'night' ? lastFix(p) : positionIn(p, t)
    return {
      pos: pos, t: t, passage: p.n, index: s.i,
      night: s.kind === 'night' ? { n: s.night, place: s.place } : null,
      done: c >= total,
    }
  }

  function geoFor(state) {
    var features = []
    for (var i = 0; i <= state.index; i++) {
      var p = data.passages[i]
      var through = i < state.index || state.night || state.done ? p.b : state.t
      travelled(p, through).forEach(function (coords) {
        features.push({ type: 'Feature', properties: { c: p.c }, geometry: { type: 'LineString', coordinates: coords } })
      })
    }
    return { type: 'FeatureCollection', features: features }
  }

  function fullGeo() {
    var features = []
    data.passages.forEach(function (p) {
      p.s.forEach(function (seg) {
        if (seg.length < 2) return
        features.push({
          type: 'Feature', properties: { c: p.c },
          geometry: { type: 'LineString', coordinates: seg.map(function (pt) { return [pt[1], pt[2]] }) },
        })
      })
    })
    return { type: 'FeatureCollection', features: features }
  }

  var api = { data: data, steps: steps, total: total, stateAt: stateAt, fmtLocal: fmtLocal }

  // ── the map ─────────────────────────────────────────────────────────────

  if (!maplibregl) {
    els.error.textContent = 'The map could not load. Check your connection and reload the page.'
    els.error.hidden = false
    els.btn.hidden = true
    return api
  }

  var bounds = [[data.bounds[0], data.bounds[1]], [data.bounds[2], data.bounds[3]]]
  var centre = [(data.bounds[0] + data.bounds[2]) / 2, (data.bounds[1] + data.bounds[3]) / 2]
  var reduced = !!(root.ownerDocument.defaultView && root.ownerDocument.defaultView.matchMedia &&
    root.ownerDocument.defaultView.matchMedia('(prefers-reduced-motion: reduce)').matches)
  var padding = { top: 56, bottom: 56, left: 40, right: 40 }

  var map = new maplibregl.Map({
    container: els.map,
    style: data.map.style,
    bounds: bounds,
    fitBoundsOptions: { padding: padding, maxZoom: 14 },
    attributionControl: { compact: true },
    cooperativeGestures: true,
    dragRotate: false,
    pitchWithRotate: false,
  })
  map.addControl(new maplibregl.NavigationControl({ showCompass: false }), 'top-left')
  var loaded = false
  var wantStart = false

  map.on('style.load', function () {
    map.setProjection({ type: 'globe' })
    map.addSource(data.map.seamarks.id, {
      type: 'raster', tiles: [data.map.seamarks.tiles], tileSize: 256, attribution: data.map.seamarks.attribution,
    })
    map.addLayer({ id: data.map.seamarks.layerId, type: 'raster', source: data.map.seamarks.id, paint: { 'raster-opacity': 0.85 } })
    map.addSource(data.map.trackSource, { type: 'geojson', data: fullGeo() })
    map.addSource(data.map.doneSource, { type: 'geojson', data: { type: 'FeatureCollection', features: [] } })
    data.map.layers.forEach(function (layer) { map.addLayer(layer) })
    loaded = true
    if (wantStart) intro()
  })
  map.on('error', function (e) {
    if (e && e.error && !loaded) {
      els.error.textContent = 'The map could not load. Check your connection and reload the page.'
      els.error.hidden = false
    }
  })

  var boatEl = doc.createElement('div')
  boatEl.className = 'hcp-boat'
  var boat = new maplibregl.Marker({ element: boatEl })
  var boatShown = false
  var insetMap = null

  function dimTrack(on) {
    if (!loaded) return
    var o = on ? 0.3 : 1
    map.setPaintProperty('hcp-track-casing', 'line-opacity', on ? 0.3 : 0.95)
    map.setPaintProperty('hcp-track-line', 'line-opacity', o)
  }

  function setMode(mode, label) {
    root.className = 'hcp hcp-' + mode
    els.label.textContent = label
  }

  function showState(s) {
    if (s.pos) {
      boat.setLngLat(s.pos)
      if (!boatShown) { boat.addTo(map); boatShown = true }
    }
    els.passage.textContent = s.passage
    els.time.textContent = fmtLocal(s.t)
    els.caption.hidden = !s.night
    els.caption.textContent = s.night ? 'Night ' + s.night.n + (s.night.place ? ' · ' + s.night.place : '') : ''
    var src = map.getSource(data.map.doneSource)
    if (src) src.setData(geoFor(s))
  }

  function makeInset() {
    if (!data.inset || insetMap) return
    els.inset.hidden = false
    insetMap = new maplibregl.Map({
      container: els.inset, style: data.map.style, center: centre, zoom: data.insetZoom,
      interactive: false, attributionControl: false,
    })
    insetMap.on('style.load', function () {
      var b = data.bounds
      insetMap.addSource('hcp-box', {
        type: 'geojson',
        data: { type: 'Feature', properties: {}, geometry: { type: 'Polygon', coordinates: [[[b[0], b[1]], [b[2], b[1]], [b[2], b[3]], [b[0], b[3]], [b[0], b[1]]]] } },
      })
      insetMap.addLayer({ id: 'hcp-box-fill', type: 'fill', source: 'hcp-box', paint: { 'fill-color': '#e11d48', 'fill-opacity': 0.2 } })
      insetMap.addLayer({ id: 'hcp-box-line', type: 'line', source: 'hcp-box', paint: { 'line-color': '#e11d48', 'line-width': 2 } })
    })
  }

  // ── playing ─────────────────────────────────────────────────────────────

  var clock = 0
  var playing = false
  var paused = false
  var started = false
  var raf = 0
  var introTimer = 0
  var lastTick = 0

  function frame(now) {
    if (!playing) return
    if (!paused) {
      clock = Math.min(total, clock + (now - lastTick) / 1000)
      var s = stateAt(clock)
      showState(s)
      if (s.done) { finish(); return }
    }
    lastTick = now
    raf = requestAnimationFrame(frame)
  }

  function finish() {
    playing = false
    paused = false
    dimTrack(false)
    setMode('done', 'Replay')
  }

  function begin() {
    if (started) return
    started = true
    clock = 0
    playing = true
    paused = false
    setMode('playing', 'Pause')
    els.readout.hidden = false
    makeInset()
    showState(stateAt(0))
    lastTick = performance.now()
    raf = requestAnimationFrame(frame)
  }

  // Start high over the globe so a reader who does not know the coast sees
  // where in the world this is, then come down onto the track.
  function intro() {
    started = false
    wantStart = false
    dimTrack(true)
    map.getSource(data.map.doneSource).setData({ type: 'FeatureCollection', features: [] })
    setMode('flying', 'Play')
    if (reduced) {
      map.fitBounds(bounds, { padding: padding, maxZoom: 14, duration: 0 })
      begin()
      return
    }
    map.jumpTo({ center: centre, zoom: 1.5, bearing: 0, pitch: 0 })
    map.once('moveend', begin)
    map.fitBounds(bounds, { padding: padding, maxZoom: 14, duration: 3200 })
    clearTimeout(introTimer)
    introTimer = setTimeout(begin, 5000)
  }

  els.btn.addEventListener('click', function () {
    if (playing) {
      paused = !paused
      els.label.textContent = paused ? 'Resume' : 'Pause'
      lastTick = performance.now()
      return
    }
    cancelAnimationFrame(raf)
    if (loaded) intro()
    else { wantStart = true; setMode('flying', 'Play') }
  })

  api.map = map
  return api
}
