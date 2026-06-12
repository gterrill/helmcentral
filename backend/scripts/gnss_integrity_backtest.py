#!/usr/bin/env python3
import argparse
import csv
import io
import json
import math
import os
from dataclasses import dataclass
from datetime import datetime, timedelta, timezone
from pathlib import Path
from typing import Dict, Iterable, Optional
from urllib import request


GNSS_TRUSTED_HDOP_THRESHOLD = 2.5
GNSS_CRITICAL_HDOP_THRESHOLD = 4.0
GNSS_DEGRADED_MAX_AGE = timedelta(seconds=10)
GNSS_CRITICAL_MAX_AGE = timedelta(seconds=30)
GNSS_DEPTH_JUMP_METERS = 1.5
GNSS_DEPTH_JUMP_WINDOW = timedelta(seconds=10)
GNSS_DEGRADED_JUMP_KTS = 6.0
GNSS_CRITICAL_JUMP_KTS = 12.0
GNSS_RECOVERY_SAMPLES = 5
GNSS_RECOVERY_MIN_DURATION = timedelta(seconds=15)
GNSS_SPOOFING_UNIFORM_SNR_STDDEV = 1.5
GNSS_SPOOFING_UNIFORM_SNR_AVG = 30.0
GNSS_JAMMING_MAX_SNR_THRESHOLD = 20.0
METERS_PER_SECOND_TO_KNOTS = 1.943844


@dataclass
class Validation:
    quality_indicator: int
    hdop: float
    snrs: list[float]
    status: str
    reason: str
    trusted: bool
    critical: bool


@dataclass
class ObservedSample:
    latitude: float
    longitude: float
    depth_meters: float
    navigation: str
    observed_at: datetime
    has_observed_at: bool


@dataclass
class HeuristicState:
    last_sample: Optional[ObservedSample] = None
    critical_latched: bool = False
    critical_since: Optional[datetime] = None
    recovery_count: int = 0


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description="Replay GNSS integrity classification with heuristics against InfluxDB data.")
    parser.add_argument("--source", default="WLN10.GP", help="GNSS source tag to replay")
    parser.add_argument("--window", default="-30d", help="Flux range start expression (default: -30d)")
    parser.add_argument("--env-file", default=str(Path(__file__).resolve().parents[1] / ".env"), help="Path to .env file")
    parser.add_argument(
        "--top-reasons",
        type=int,
        default=25,
        help="How many exact reasons to return per status (default: 25). Use 0 for all.",
    )
    return parser.parse_args()


def load_env_file(path: str) -> Dict[str, str]:
    env: Dict[str, str] = {}
    p = Path(path)
    if not p.exists():
        return env
    for line in p.read_text(encoding="utf-8").splitlines():
        line = line.strip()
        if not line or line.startswith("#") or "=" not in line:
            continue
        key, value = line.split("=", 1)
        env[key.strip()] = value.strip().strip('"').strip("'")
    return env


def rfc3339_parse(value: str) -> datetime:
    if value.endswith("Z"):
        value = value[:-1] + "+00:00"
    dt = datetime.fromisoformat(value)
    if dt.tzinfo is None:
        return dt.replace(tzinfo=timezone.utc)
    return dt.astimezone(timezone.utc)


def to_float(value: object, default: float = -1.0) -> float:
    if value is None:
        return default
    if isinstance(value, (int, float)):
        return float(value)
    s = str(value).strip()
    if s == "":
        return default
    try:
        return float(s)
    except ValueError:
        return default


def parse_snrs(value: object) -> list[float]:
    if not value or not isinstance(value, str):
        return []
    try:
        data = json.loads(value)
    except json.JSONDecodeError:
        return []

    snrs = []
    
    if isinstance(data, list):
        for sat in data:
            if not isinstance(sat, dict):
                continue
            snr = to_float(sat.get("snr", sat.get("SNR")))
            if snr > 0:
                snrs.append(snr)
    elif isinstance(data, dict):
        for key, sat in data.items():
            if not isinstance(sat, dict):
                continue
            snr = to_float(sat.get("snr", sat.get("SNR")))
            if snr > 0:
                snrs.append(snr)
    return snrs


def haversine_meters(lat1: float, lon1: float, lat2: float, lon2: float) -> float:
    earth_radius_meters = 6371000.0
    lat1_rad = math.radians(lat1)
    lon1_rad = math.radians(lon1)
    lat2_rad = math.radians(lat2)
    lon2_rad = math.radians(lon2)
    delta_lat = lat2_rad - lat1_rad
    delta_lon = lon2_rad - lon1_rad
    a = (
        math.sin(delta_lat / 2) * math.sin(delta_lat / 2)
        + math.cos(lat1_rad) * math.cos(lat2_rad) * math.sin(delta_lon / 2) * math.sin(delta_lon / 2)
    )
    c = 2 * math.atan2(math.sqrt(a), math.sqrt(1 - a))
    return earth_radius_meters * c


def is_anchored_or_moored_state(value: str) -> bool:
    normalized = value.strip().lower().replace("_", "").replace("-", "").replace(" ", "")
    return normalized in {"anchored", "moored", "atanchor"}


def parse_gnss_quality_label(value: str) -> int:
    normalized = value.strip().lower().replace("-", "").replace("_", "").replace(" ", "").replace(".", "")
    if normalized in {"", "invalid", "nofix", "none", "unavailable"}:
        return 0
    if normalized in {"gnssfix", "gpsfix"}:
        return 1
    if normalized in {"dgnssfix", "dgpsfix"}:
        return 2
    if normalized in {"gps", "standalone", "autonomous", "sps"}:
        return 1
    if normalized in {"dgps", "differential", "rtcm", "sbas", "waas"}:
        return 2
    if normalized in {"rtkfixed", "fixedrtk", "fix"}:
        return 4
    if normalized in {"rtkfloat", "floatrtk", "float"}:
        return 5
    if normalized in {"simulation", "sim", "simulated"}:
        return 8
    if normalized in {"manual", "estimated"}:
        return 1
    return 0


def classify_gnss(quality_indicator: int, hdop: float, snrs: list[float]) -> tuple[str, str]:
    if quality_indicator <= 0:
        return "critical", "quality indicator reports no fix"
    if hdop < 0:
        return "critical", "hdop unavailable"
    if hdop > GNSS_CRITICAL_HDOP_THRESHOLD:
        return "critical", f"hdop {hdop:.1f} exceeds {GNSS_CRITICAL_HDOP_THRESHOLD:.1f}"

    if len(snrs) >= 4:
        avg = sum(snrs) / len(snrs)
        variance = sum((x - avg) ** 2 for x in snrs) / len(snrs)
        stddev = math.sqrt(variance)
        max_snr = max(snrs)

        if stddev < GNSS_SPOOFING_UNIFORM_SNR_STDDEV and avg > GNSS_SPOOFING_UNIFORM_SNR_AVG:
            return "critical", f"suspected spoofing: uniform snr (stddev {stddev:.1f}, avg {avg:.1f})"

        if max_snr < GNSS_JAMMING_MAX_SNR_THRESHOLD:
            return "critical", f"suspected jamming: max snr only {max_snr:.1f}"

    if hdop <= GNSS_TRUSTED_HDOP_THRESHOLD:
        return "trusted", ""
    return "degraded", f"hdop {hdop:.1f} above trusted threshold {GNSS_TRUSTED_HDOP_THRESHOLD:.1f}"


def categorize_reason(reason: str) -> str:
    normalized = (reason or "").strip().lower()
    if normalized.startswith("hdop "):
        return "hdop-threshold"
    if normalized.startswith("depth jump"):
        return "depth-jump"
    if normalized.startswith("position jump"):
        return "position-jump"
    if normalized.startswith("gnss data stale"):
        return "stale-data-critical"
    if normalized.startswith("gnss data aging"):
        return "stale-data-degraded"
    if normalized.startswith("suspected spoofing"):
        return "spoofing"
    if normalized.startswith("suspected jamming"):
        return "jamming"
    if normalized == "gnss timestamp unavailable":
        return "timestamp-missing"
    if normalized == "quality indicator reports no fix":
        return "quality-no-fix"
    if normalized == "hdop unavailable":
        return "hdop-missing"
    if normalized == "awaiting trusted recovery hysteresis":
        return "recovery-hysteresis"
    if normalized == "":
        return "none"
    return "other"


def escalate_validation(current: Validation, target: str, reason: str) -> Validation:
    if target == "critical":
        current.status = "critical"
        current.reason = reason
        current.critical = True
        current.trusted = False
        return current
    if target == "degraded" and current.status == "trusted":
        current.status = "degraded"
        current.reason = reason
        current.critical = False
        current.trusted = False
    return current


def apply_recovery_hysteresis(validation: Validation, now: datetime, state: HeuristicState) -> Validation:
    if validation.status == "critical":
        state.critical_latched = True
        state.critical_since = now
        state.recovery_count = 0
        return validation

    if not state.critical_latched:
        state.recovery_count = 0
        return validation

    if validation.status == "trusted":
        state.recovery_count += 1
        if (
            state.recovery_count >= GNSS_RECOVERY_SAMPLES
            and state.critical_since is not None
            and now - state.critical_since >= GNSS_RECOVERY_MIN_DURATION
        ):
            state.critical_latched = False
            state.recovery_count = 0
            return validation
    else:
        state.recovery_count = 0

    validation.status = "critical"
    validation.reason = "awaiting trusted recovery hysteresis"
    validation.critical = True
    validation.trusted = False
    return validation


def apply_heuristics(validation: Validation, sample: ObservedSample, now: datetime, state: HeuristicState) -> Validation:
    if not sample.has_observed_at:
        validation = escalate_validation(validation, "critical", "gnss timestamp unavailable")
    else:
        age = now - sample.observed_at
        if age > GNSS_CRITICAL_MAX_AGE:
            validation = escalate_validation(validation, "critical", f"gnss data stale ({int(age.total_seconds())}s)")
        elif age > GNSS_DEGRADED_MAX_AGE:
            validation = escalate_validation(validation, "degraded", f"gnss data aging ({int(age.total_seconds())}s)")

    if state.last_sample is not None and sample.has_observed_at and state.last_sample.has_observed_at:
        dt = sample.observed_at - state.last_sample.observed_at
        if dt.total_seconds() > 0:
            distance = haversine_meters(
                state.last_sample.latitude,
                state.last_sample.longitude,
                sample.latitude,
                sample.longitude,
            )
            speed_kts = (distance / dt.total_seconds()) * METERS_PER_SECOND_TO_KNOTS
            if is_anchored_or_moored_state(sample.navigation):
                if speed_kts > GNSS_CRITICAL_JUMP_KTS:
                    validation = escalate_validation(validation, "critical", f"position jump implies {speed_kts:.1f} kts at anchor")
                elif speed_kts > GNSS_DEGRADED_JUMP_KTS:
                    validation = escalate_validation(validation, "degraded", f"position jump implies {speed_kts:.1f} kts at anchor")

                if (
                    dt <= GNSS_DEPTH_JUMP_WINDOW
                    and sample.depth_meters >= 0
                    and state.last_sample.depth_meters >= 0
                ):
                    depth_delta = abs(sample.depth_meters - state.last_sample.depth_meters)
                    if depth_delta > GNSS_DEPTH_JUMP_METERS:
                        if speed_kts > GNSS_DEGRADED_JUMP_KTS:
                            validation = escalate_validation(
                                validation,
                                "critical",
                                f"depth jump {depth_delta:.1f}m with implausible position jump",
                            )
                        else:
                            validation = escalate_validation(
                                validation,
                                "degraded",
                                f"depth jump {depth_delta:.1f}m in {int(dt.total_seconds())}s",
                            )

    if -90 <= sample.latitude <= 90 and -180 <= sample.longitude <= 180:
        state.last_sample = ObservedSample(
            latitude=sample.latitude,
            longitude=sample.longitude,
            depth_meters=sample.depth_meters,
            navigation=sample.navigation,
            observed_at=sample.observed_at,
            has_observed_at=sample.has_observed_at,
        )

    validation = apply_recovery_hysteresis(validation, now, state)
    return validation


def influx_query_csv(
    influx_url: str,
    org: str,
    token: str,
    flux_query: str,
    timeout_seconds: int = 300,
) -> Iterable[Dict[str, str]]:
    query_url = influx_url.rstrip("/") + f"/api/v2/query?org={org}"
    req = request.Request(
        query_url,
        data=flux_query.encode("utf-8"),
        method="POST",
        headers={
            "Authorization": f"Token {token}",
            "Accept": "application/csv",
            "Content-type": "application/vnd.flux",
        },
    )
    with request.urlopen(req, timeout=timeout_seconds) as resp:
        text = io.TextIOWrapper(resp, encoding="utf-8", newline="")
        reader = csv.DictReader(text)
        for row in reader:
            yield row


def ts_epoch_seconds(value: str) -> int:
        return int(rfc3339_parse(value).timestamp())


def fetch_time_value_series(
        influx_url: str,
        org: str,
        token: str,
        flux_query: str,
        parser,
) -> list[tuple[int, object]]:
        series: list[tuple[int, object]] = []
        for row in influx_query_csv(influx_url, org, token, flux_query):
                ts = row.get("_time", "")
                if not ts:
                        continue
                parsed = parser(row)
                if parsed is None:
                        continue
                series.append((ts_epoch_seconds(ts), parsed))
        return series


def advance_series(series: list[tuple[int, object]], idx: int, current_time: int, current_value: object) -> tuple[int, object]:
        while idx < len(series) and series[idx][0] <= current_time:
                current_value = series[idx][1]
                idx += 1
        return idx, current_value


def flux_hdop_driver(bucket: str, source: str, window: str) -> str:
        return f'''
from(bucket: "{bucket}")
    |> range(start: {window})
    |> filter(fn: (r) => r._measurement == "navigation.gnss.horizontalDilution")
    |> filter(fn: (r) => r._field == "value")
    |> filter(fn: (r) => r.source == "{source}")
    |> aggregateWindow(every: 1s, fn: last, createEmpty: false)
    |> keep(columns: ["_time", "_value"])
    |> sort(columns: ["_time"], desc: false)
'''.strip()


def flux_position_dilution(bucket: str, source: str, window: str) -> str:
        return f'''
from(bucket: "{bucket}")
    |> range(start: {window})
    |> filter(fn: (r) => r._measurement == "navigation.gnss.positionDilution")
    |> filter(fn: (r) => r._field == "value")
    |> filter(fn: (r) => r.source == "{source}")
    |> aggregateWindow(every: 1s, fn: last, createEmpty: false)
    |> keep(columns: ["_time", "_value"])
    |> sort(columns: ["_time"], desc: false)
'''.strip()


def flux_method_quality(bucket: str, source: str, window: str) -> str:
        return f'''
from(bucket: "{bucket}")
    |> range(start: {window})
    |> filter(fn: (r) => r._measurement == "navigation.gnss.methodQuality")
    |> filter(fn: (r) => r._field == "value")
    |> filter(fn: (r) => r.source == "{source}")
    |> aggregateWindow(every: 1s, fn: last, createEmpty: false)
    |> keep(columns: ["_time", "_value"])
    |> sort(columns: ["_time"], desc: false)
'''.strip()


def flux_position(bucket: str, source: str, window: str) -> str:
        return f'''
from(bucket: "{bucket}")
    |> range(start: {window})
    |> filter(fn: (r) => r._measurement == "navigation.position")
    |> filter(fn: (r) => r._field == "lat" or r._field == "lon")
    |> filter(fn: (r) => r.source == "{source}")
    |> aggregateWindow(every: 1s, fn: last, createEmpty: false)
    |> pivot(rowKey: ["_time"], columnKey: ["_field"], valueColumn: "_value")
    |> keep(columns: ["_time", "lat", "lon"])
    |> sort(columns: ["_time"], desc: false)
'''.strip()


def flux_navigation_state(bucket: str, window: str) -> str:
        return f'''
from(bucket: "{bucket}")
    |> range(start: {window})
    |> filter(fn: (r) => r._measurement == "navigation.state")
    |> filter(fn: (r) => r._field == "value")
    |> aggregateWindow(every: 1s, fn: last, createEmpty: false)
    |> keep(columns: ["_time", "_value"])
    |> sort(columns: ["_time"], desc: false)
'''.strip()


def flux_depth(bucket: str, depth_measurement: str, window: str) -> str:
        return f'''
from(bucket: "{bucket}")
    |> range(start: {window})
    |> filter(fn: (r) => r._measurement == "{depth_measurement}")
    |> filter(fn: (r) => r._field == "value")
    |> aggregateWindow(every: 1s, fn: last, createEmpty: false)
    |> keep(columns: ["_time", "_value"])
    |> sort(columns: ["_time"], desc: false)
'''.strip()


def flux_satellites(bucket: str, source: str, window: str) -> str:
        return f'''
from(bucket: "{bucket}")
    |> range(start: {window})
    |> filter(fn: (r) => r._measurement == "navigation.gnss.satellites")
    |> filter(fn: (r) => r._field == "value")
    |> filter(fn: (r) => r.source == "{source}")
    |> aggregateWindow(every: 1s, fn: last, createEmpty: false)
    |> keep(columns: ["_time", "_value"])
    |> sort(columns: ["_time"], desc: false)
'''.strip()


def run_backtest(
    influx_url: str,
    org: str,
    bucket: str,
    token: str,
    source: str,
    window: str,
    depth_measurement: str,
    top_reasons: int,
) -> dict:
    heuristic_state = HeuristicState()

    pd_series = fetch_time_value_series(
        influx_url,
        org,
        token,
        flux_position_dilution(bucket=bucket, source=source, window=window),
        lambda row: to_float(row.get("_value"), default=-1.0),
    )
    mq_series = fetch_time_value_series(
        influx_url,
        org,
        token,
        flux_method_quality(bucket=bucket, source=source, window=window),
        lambda row: (row.get("_value") or "").strip(),
    )
    pos_series = fetch_time_value_series(
        influx_url,
        org,
        token,
        flux_position(bucket=bucket, source=source, window=window),
        lambda row: (
            to_float(row.get("lat"), default=-1.0),
            to_float(row.get("lon"), default=-1.0),
        ),
    )
    nav_series = fetch_time_value_series(
        influx_url,
        org,
        token,
        flux_navigation_state(bucket=bucket, window=window),
        lambda row: (row.get("_value") or "").strip(),
    )
    depth_series = fetch_time_value_series(
        influx_url,
        org,
        token,
        flux_depth(bucket=bucket, depth_measurement=depth_measurement, window=window),
        lambda row: to_float(row.get("_value"), default=-1.0),
    )
    sat_series = fetch_time_value_series(
        influx_url,
        org,
        token,
        flux_satellites(bucket=bucket, source=source, window=window),
        lambda row: parse_snrs(row.get("_value")),
    )

    pd_idx = mq_idx = pos_idx = nav_idx = depth_idx = sat_idx = 0
    current_pd: object = None
    current_mq: object = None
    current_pos: object = (-1.0, -1.0)
    current_nav: object = ""
    current_depth: object = -1.0
    current_sat: list[float] = []

    counts = {"trusted": 0, "degraded": 0, "critical": 0}
    reason_counts: dict[str, dict[str, int]] = {"degraded": {}, "critical": {}}
    reason_category_counts: dict[str, dict[str, int]] = {"degraded": {}, "critical": {}}
    total_samples = 0

    for row in influx_query_csv(
        influx_url,
        org,
        token,
        flux_hdop_driver(bucket=bucket, source=source, window=window),
    ):
        ts = row.get("_time", "")
        if not ts:
            continue

        current_time = ts_epoch_seconds(ts)
        pd_idx, current_pd = advance_series(pd_series, pd_idx, current_time, current_pd)
        mq_idx, current_mq = advance_series(mq_series, mq_idx, current_time, current_mq)
        pos_idx, current_pos = advance_series(pos_series, pos_idx, current_time, current_pos)
        nav_idx, current_nav = advance_series(nav_series, nav_idx, current_time, current_nav)
        depth_idx, current_depth = advance_series(depth_series, depth_idx, current_time, current_depth)
        sat_idx, current_sat = advance_series(sat_series, sat_idx, current_time, current_sat)

        observed_at = rfc3339_parse(ts)
        now = observed_at

        quality_label = str(current_mq or "").strip()
        quality_indicator = parse_gnss_quality_label(quality_label) if quality_label else -1

        hdop = to_float(row.get("_value"), default=-1.0)
        if hdop < 0:
            hdop = to_float(current_pd, default=-1.0)

        latitude = to_float(current_pos[0] if isinstance(current_pos, tuple) else -1.0, default=-1.0)
        longitude = to_float(current_pos[1] if isinstance(current_pos, tuple) else -1.0, default=-1.0)
        snrs = current_sat if isinstance(current_sat, list) else []

        status, reason = classify_gnss(quality_indicator, hdop, snrs)
        validation = Validation(
            quality_indicator=quality_indicator,
            hdop=hdop,
            snrs=snrs,
            status=status,
            reason=reason,
            trusted=(status == "trusted"),
            critical=(status == "critical"),
        )

        sample = ObservedSample(
            latitude=latitude,
            longitude=longitude,
            depth_meters=to_float(current_depth, default=-1.0),
            navigation=str(current_nav or "").strip(),
            observed_at=observed_at,
            has_observed_at=True,
        )

        validation = apply_heuristics(validation, sample, now, heuristic_state)
        if validation.status not in counts:
            continue

        counts[validation.status] += 1
        if validation.status in reason_counts:
            reason_key = validation.reason.strip() if validation.reason else "(no reason provided)"
            reason_counts[validation.status][reason_key] = reason_counts[validation.status].get(reason_key, 0) + 1
            category = categorize_reason(validation.reason)
            reason_category_counts[validation.status][category] = reason_category_counts[validation.status].get(category, 0) + 1
        total_samples += 1

    percentages = {
        key: (counts[key] * 100.0 / total_samples if total_samples > 0 else 0.0)
        for key in ("trusted", "degraded", "critical")
    }

    reason_percentages: dict[str, dict[str, float]] = {"degraded": {}, "critical": {}}
    reason_category_percentages: dict[str, dict[str, float]] = {"degraded": {}, "critical": {}}
    for status in ("degraded", "critical"):
        status_total = counts[status]
        if status_total <= 0:
            continue
        for reason, count in reason_counts[status].items():
            reason_percentages[status][reason] = (count * 100.0) / status_total
        for category, count in reason_category_counts[status].items():
            reason_category_percentages[status][category] = (count * 100.0) / status_total

    def top_entries(data: dict[str, int]) -> dict[str, int]:
        items = sorted(data.items(), key=lambda x: x[1], reverse=True)
        if top_reasons > 0:
            items = items[:top_reasons]
        return {k: v for k, v in items}

    exact_reason_counts_output = {
        "degraded": top_entries(reason_counts["degraded"]),
        "critical": top_entries(reason_counts["critical"]),
    }
    exact_reason_percentages_output = {
        "degraded": {k: reason_percentages["degraded"][k] for k in exact_reason_counts_output["degraded"].keys()},
        "critical": {k: reason_percentages["critical"][k] for k in exact_reason_counts_output["critical"].keys()},
    }
    category_counts_output = {
        "degraded": dict(sorted(reason_category_counts["degraded"].items(), key=lambda x: x[1], reverse=True)),
        "critical": dict(sorted(reason_category_counts["critical"].items(), key=lambda x: x[1], reverse=True)),
    }
    category_percentages_output = {
        "degraded": {
            k: reason_category_percentages["degraded"][k]
            for k in category_counts_output["degraded"].keys()
        },
        "critical": {
            k: reason_category_percentages["critical"][k]
            for k in category_counts_output["critical"].keys()
        },
    }

    return {
        "source": source,
        "window": window,
        "total_samples": total_samples,
        "counts": counts,
        "percentages": percentages,
        "reason_categories": {
            "counts": category_counts_output,
            "percentages_within_status": category_percentages_output,
        },
        "reason_details": {
            "top_n": top_reasons,
            "counts": exact_reason_counts_output,
            "percentages_within_status": exact_reason_percentages_output,
        },
    }


def main() -> None:
    args = parse_args()
    file_env = load_env_file(args.env_file)

    influx_url = os.getenv("INFLUXDB_URL", file_env.get("INFLUXDB_URL", "")).strip().strip('"')
    org = os.getenv("INFLUXDB_ORG", file_env.get("INFLUXDB_ORG", "")).strip().strip('"')
    bucket = os.getenv("INFLUXDB_BUCKET", file_env.get("INFLUXDB_BUCKET", "")).strip().strip('"')
    token = os.getenv("INFLUXDB_TOKEN", file_env.get("INFLUXDB_TOKEN", "")).strip().strip('"')
    depth_measurement = os.getenv(
        "INFLUX_DEPTH_MEASUREMENT",
        file_env.get("INFLUX_DEPTH_MEASUREMENT", "environment.depth.belowTransducer"),
    ).strip().strip('"')

    missing = [
        name
        for name, value in {
            "INFLUXDB_URL": influx_url,
            "INFLUXDB_ORG": org,
            "INFLUXDB_BUCKET": bucket,
            "INFLUXDB_TOKEN": token,
        }.items()
        if value == ""
    ]
    if missing:
        raise SystemExit(f"Missing required Influx config: {', '.join(missing)}")

    result = run_backtest(
        influx_url=influx_url,
        org=org,
        bucket=bucket,
        token=token,
        source=args.source,
        window=args.window,
        depth_measurement=depth_measurement,
        top_reasons=args.top_reasons,
    )

    print(json.dumps(result, indent=2))


if __name__ == "__main__":
    main()