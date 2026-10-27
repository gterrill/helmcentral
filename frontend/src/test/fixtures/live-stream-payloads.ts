/**
 * Stream payloads captured from the boat's own server (GET /api/stream on the
 * dev backend, 2026-10-27, at anchor in Airlie), for the ADR 0215 phase 5
 * tests. Trimmed where noted; field names and values are the server's.
 *
 * What moved between consecutive ticks in that capture:
 * - electrical-state (every 5 s): charging current and power, DC current and
 *   power, solar output and datetime. The alternators sat at -1 throughout.
 * - solar-state (every 10 s): each controller's watts and share, the array
 *   total, today's kWh, datetime, and trend_24h_total, which nothing reads.
 * - tanks-state (every 10 s): datetime, every age, and one black water level.
 * - nearby-vessels (every 5 s): every vessel's age_seconds and bearing, most
 *   ranges and CPAs, the feed age and datetime.
 * - autopilot and radar-targets: no pilot and no radar on that server, so one
 *   seed each and nothing after. The hub only sends a payload that changed.
 */

/** electrical-state, the first of six. */
export const ELECTRICAL = {
  ac_loads_w: -1, ac_output_w: -1,
  alternator_0_current_a: -1, alternator_0_power_w: -1, alternator_0_temperature_c: -1, alternator_0_voltage_v: -1,
  alternator_1_current_a: -1, alternator_1_power_w: -1, alternator_1_temperature_c: -1, alternator_1_voltage_v: -1,
  battery_capacity_ah: -1, battery_soc_percent: 82,
  charger_0_acin_1_current_a: -1, charger_0_charging_mode: '', charger_0_current_a: -1, charger_0_error: '',
  charging_current_a: -27.4, charging_power_w: -725, datetime: '2026-10-27T04:20:11Z',
  dc_12v_current_a: 17.1, dc_12v_power_w: 453.5, dc_current_a: 17.1, dc_power_w: 453.5,
  generator_configured: true, generator_none: false, generator_real_power_w: 0,
  house_bank_configured: true, house_bank_unresolved: false, last_update_age_s: 0,
  soc_path: 'electrical.batteries.512.capacity.stateOfCharge', solar_output_w: 1364.4, source: 'signalk',
  voltage_readout_label: 'Voltage readout battery', voltage_readout_v: 26.5,
}

/** The tick after it: only the bank, DC and solar figures moved. */
export const ELECTRICAL_NEXT = {
  ...ELECTRICAL,
  charging_current_a: -29.6, charging_power_w: -784, datetime: '2026-10-27T04:20:16Z',
  dc_12v_current_a: 17, dc_12v_power_w: 449.4, dc_current_a: 17, dc_power_w: 449.4, solar_output_w: 1366.9,
}

/** solar-state, the first of four, without trend_24h_total. */
export const SOLAR = {
  controllers: [
    { contribution_pct: 17.3, current_w: 237.3, error: '', id: '288', label: 'Array 288', last_update_age_s: -1, mode: 'on', today_kwh: 2.1, yesterday_kwh: 2.21 },
    { contribution_pct: 28.8, current_w: 394.6, error: '', id: '289', label: 'Array 289', last_update_age_s: -1, mode: 'on', today_kwh: 2.53, yesterday_kwh: 2.62 },
    { contribution_pct: 26.5, current_w: 364, error: '', id: '290', label: 'Array 290', last_update_age_s: -1, mode: 'on', today_kwh: 2.72, yesterday_kwh: 2.7 },
    { contribution_pct: 29.3, current_w: 402.4, error: '', id: '291', label: 'Array 291', last_update_age_s: -1, mode: 'on', today_kwh: 2.96, yesterday_kwh: 2.85 },
  ],
  current_w: 1372.3, datetime: '2026-10-27T04:20:06Z', last_update_age_s: -1, peak_today_w: 2083.3,
  source: 'signalk', today_kwh: 10.287, yesterday_kwh: 10.338,
}

/**
 * tanks-state, the first of four. The dev server that day predated the tank
 * alarm levels (ADR 0214), so warn_percent, alarm_percent and alarms_off are
 * added here at the kind defaults this branch's backend sends.
 */
const LEVELS = {
  water: { warn_percent: 25, alarm_percent: 10, alarms_off: false },
  fuel: { warn_percent: 25, alarm_percent: 10, alarms_off: false },
  waste: { warn_percent: 75, alarm_percent: 90, alarms_off: false },
}
export const TANKS = {
  datetime: '2026-10-27T04:20:06Z', fuel_derived_age_s: -1, fuel_range_m: null, fuel_time_to_empty_s: null,
  fuel_volume_age_s: 2.5, fuel_volume_m3: 2.190176, last_update_age_s: 0.7, source: 'signalk',
  tanks: [
    { id: 'freshWater.0', label: 'Fresh water PORT', category: 'freshWater', kind: 'water', level_percent: 100, last_update_age_s: 0.7, ...LEVELS.water },
    { id: 'freshWater.3', label: 'Fresh water STBD', category: 'freshWater', kind: 'water', level_percent: 56.1, last_update_age_s: 0.9, ...LEVELS.water },
    { id: 'fuel.2', label: 'Fuel STBD FWD', category: 'fuel', kind: 'fuel', level_percent: 24.7, last_update_age_s: 0.9, ...LEVELS.fuel },
    { id: 'fuel.4', label: 'Fuel PORT AFT', category: 'fuel', kind: 'fuel', level_percent: 55.7, last_update_age_s: 0.7, ...LEVELS.fuel },
    { id: 'blackWater.1', label: 'Black water PORT', category: 'blackWater', kind: 'waste', level_percent: 11.3, last_update_age_s: 0.7, ...LEVELS.waste },
  ],
}

/** The tick after it: datetime and every age moved, and no level did. */
export const TANKS_AGES_ONLY = {
  ...TANKS,
  datetime: '2026-10-27T04:20:16Z', fuel_volume_age_s: 2.4, last_update_age_s: 0.6,
  tanks: TANKS.tanks.map((tank) => ({ ...tank, last_update_age_s: Math.max(0, tank.last_update_age_s - 0.1) })),
}

/** nearby-vessels, the first of six, with three of its eleven vessels. */
export const NEARBY = {
  datetime: '2026-10-27T04:20:12Z', last_update_age_s: 82, max_range_m: 5000, source: 'signalk',
  vessels: [
    { id: 'urn:mrn:imo:mmsi:503050940', name: 'BELLA BREEZA', mmsi: '503050940', range_m: 101.2, age_seconds: 121, sog_knots: 0.1, lat: -20.275698333333334, lon: 149.07368, seen_count: 0, bearing_rad: 3.1126549761139484 },
    { id: 'urn:mrn:imo:mmsi:503188370', name: "IT'S THE VIBE", mmsi: '503188370', range_m: 291.1, age_seconds: 125, sog_knots: 0, lat: -20.2723, lon: 149.072785, seen_count: 5, last_seen_at: '2026-10-25T23:47:55Z', bearing_rad: 5.967346476412313 },
    { id: 'urn:mrn:imo:mmsi:503049330', name: 'WHITSUNDAY BULLET', mmsi: '503049330', range_m: 1391.1, age_seconds: 138, sog_knots: 0.1, lat: -20.263123333333333, lon: 149.07847166666667, seen_count: 2, last_seen_at: '2026-09-06T05:54:35Z', bearing_rad: 0.36974619262235564 },
  ],
}

/** The tick after it, as the server sent it: every age and bearing moved. */
export const NEARBY_NEXT = {
  ...NEARBY,
  datetime: '2026-10-27T04:20:17Z', last_update_age_s: 87,
  vessels: [
    { ...NEARBY.vessels[0], age_seconds: 126, bearing_rad: 3.112813352533708 },
    { ...NEARBY.vessels[1], range_m: 291.2, age_seconds: 130, bearing_rad: 5.967295275871154 },
    { ...NEARBY.vessels[2], age_seconds: 143, bearing_rad: 0.369735114387743 },
  ],
}

/**
 * The live server had no pilot ({"present":false}) and no radar
 * ({"radars":[],"source":"disabled","targets":[]}). These follow the backend's
 * builders instead: buildAutopilotPayload (autopilot.go) and
 * buildRadarTargetsPayload (radar_source.go).
 */
export const AUTOPILOT = {
  present: true, engaged: true, state: 'auto', mode: 'compass', target: 5.742,
  available_actions: ['dodge', 'tack', 'gybe'], stale: false,
}

export const RADAR = {
  datetime: '2026-10-27T04:20:13Z', source: 'mayara',
  radars: [{ id: 'fur6424A', name: 'Furuno DRS4D-NXT', transmitting: true }],
  targets: [
    {
      id: 'fur6424A:3', radar_id: 'fur6424A', target_id: 3, status: 'tracking', bearing_rad: 0.733, range_m: 1852,
      lat: -20.262, lon: 149.085, position_derived: true, course_rad: 3.9, sog_knots: 6.2, cpa_m: 420, tcpa_seconds: 610,
      is_dangerous: false, acquisition: 'auto', age_seconds: 1.2,
    },
  ],
}
