"use client";

import { useMemo } from "react";

/**
 * WindCompass — a wind-direction compass with four quadrant gauges.
 *
 * The center dial shows a heading arrow that smoothly rotates when `direction`
 * changes. Each gauge sits in a quadrant; the corner nearest the dial is cut
 * with a concave arc (CSS radial-gradient mask) so it wraps the circle with an
 * even gap. Card colors are themeable via `colorA` / `colorB`.
 *
 * Geometry is authored against a fixed 1024×560 coordinate space; wrap in a
 * container and `transform: scale()` (or zoom) to make it responsive.
 */

export interface WindCompassProps {
  /** Heading the arrow points to, in degrees (0 = N, 90 = E). */
  direction?: number;
  /** Base color for the "A" cards (top-left, bottom-left). A gradient is derived. */
  colorA?: string;
  /** Base color for the "B" cards (top-right, bottom-right). */
  colorB?: string;

  setLabel?: string;
  setValue?: string;
  setUnit?: string;

  driftLabel?: string;
  driftValue?: string;
  driftUnit?: string;

  gust10Label?: string;
  gust10Speed?: string;
  gust10Unit?: string;

  gustHourLabel?: string;
  gustHourSpeed?: string;
  gustHourUnit?: string;

  className?: string;
}

/** Lighten (pct > 0) or darken (pct < 0) a hex color by a fraction [-1, 1]. */
function shade(hex: string, pct: number): string {
  const h = hex.replace("#", "");
  const full = h.length === 3 ? h.split("").map((c) => c + c).join("") : h;
  const n = parseInt(full, 16);
  let r = (n >> 16) & 255;
  let g = (n >> 8) & 255;
  let b = n & 255;
  const t = pct < 0 ? 0 : 255;
  const p = Math.abs(pct);
  r = Math.round((t - r) * p + r);
  g = Math.round((t - g) * p + g);
  b = Math.round((t - b) * p + b);
  return "#" + [r, g, b].map((x) => x.toString(16).padStart(2, "0")).join("");
}

const POINTS = ["N","NNE","NE","ENE","E","ESE","SE","SSE","S","SSW","SW","WSW","W","WNW","NW","NNW"];
function compassPoint(deg: number): string {
  return POINTS[Math.round(((((deg % 360) + 360) % 360) / 22.5)) % 16];
}

export default function WindCompass({
  direction = 315,
  colorA = "#16263f",
  colorB = "#34383d",
  setLabel = "Set",
  setValue = "134",
  setUnit = "°",
  driftLabel = "Drift",
  driftValue = "3.7",
  driftUnit = "kn",
  gust10Label = "Max Gust 10 m",
  gust10Speed = "9.1",
  gust10Unit = "kts",
  gustHourLabel = "Max Gust 1 hr",
  gustHourSpeed = "12.2",
  gustHourUnit = "kts",
  className = "",
}: WindCompassProps) {
  const cA1 = shade(colorA, 0.12);
  const cA2 = shade(colorA, -0.45);
  const cB1 = shade(colorB, 0.12);
  const cB2 = shade(colorB, -0.4);

  const ticks = useMemo(() => {
    const out: React.CSSProperties[] = [];
    for (let a = 0; a < 360; a += 2) {
      const major = a % 10 === 0;
      out.push({
        position: "absolute",
        left: "50%",
        top: "50%",
        width: major ? 2 : 1,
        height: major ? 13 : 7,
        background: major ? "#d3d7db" : "#6c7176",
        transformOrigin: "50% 50%",
        transform: `translate(-50%,-50%) rotate(${a}deg) translateY(-150px)`,
        borderRadius: 1,
      });
    }
    return out;
  }, []);

  const degLabels = useMemo(() => {
    const out: { text: string; style: React.CSSProperties }[] = [];
    for (let a = 0; a < 360; a += 10) {
      out.push({
        text: String(a === 0 ? 360 : a),
        style: {
          position: "absolute",
          left: "50%",
          top: "50%",
          fontSize: 11,
          fontWeight: 600,
          color: "#9aa0a7",
          fontFamily: "'Barlow Semi Condensed', sans-serif",
          transformOrigin: "50% 50%",
          transform: `translate(-50%,-50%) rotate(${a}deg) translateY(-133px)`,
          whiteSpace: "nowrap",
        },
      });
    }
    return out;
  }, []);

  const cardinals = useMemo(() => {
    const defs = [
      { t: "N", a: 0, m: true }, { t: "NE", a: 45, m: false },
      { t: "E", a: 90, m: true }, { t: "SE", a: 135, m: false },
      { t: "S", a: 180, m: true }, { t: "SW", a: 225, m: false },
      { t: "W", a: 270, m: true }, { t: "NW", a: 315, m: false },
    ];
    return defs.map((c) => ({
      text: c.t,
      style: {
        position: "absolute",
        left: "50%",
        top: "50%",
        fontSize: c.m ? 26 : 15,
        fontWeight: c.m ? 700 : 600,
        color: c.m ? "#f0f3f6" : "#8b929b",
        letterSpacing: "0.5px",
        transformOrigin: "50% 50%",
        transform: `translate(-50%,-50%) rotate(${c.a}deg) translateY(-92px) rotate(${-c.a}deg)`,
      } as React.CSSProperties,
    }));
  }, []);

  const dirText = `${Math.round(direction)}° ${compassPoint(direction)}`;

  // concave-corner masks: a circle centered on the dial center, expressed in
  // each card's local coordinates. radius 198 = dial radius 180 + 18px gap.
  const maskTL = "radial-gradient(circle at 408px 230px, transparent 197px, #000 199px)";
  const maskTR = "radial-gradient(circle at -104px 230px, transparent 197px, #000 199px)";
  const maskBL = "radial-gradient(circle at 408px -98px, transparent 197px, #000 199px)";
  const maskBR = "radial-gradient(circle at -104px -98px, transparent 197px, #000 199px)";

  return (
    <div
      className={`relative font-[Barlow,system-ui,sans-serif] ${className}`}
      style={{ width: 1024, height: 560 }}
    >
      {/* ===== COMPASS ===== */}
      <div className="absolute left-[512px] top-[300px] -ml-[180px] -mt-[180px] h-[360px] w-[360px]">
        {/* metallic bezel */}
        <div
          className="absolute inset-0 rounded-full"
          style={{
            background:
              "conic-gradient(from 135deg,#e6e9ec,#9aa0a6 12%,#5c6168 25%,#c9ced3 38%,#7d828a 50%,#eef1f3 62%,#80858d 74%,#565b62 86%,#d7dbdf 100%)",
            boxShadow:
              "0 18px 40px rgba(0,0,0,0.45), 0 3px 8px rgba(0,0,0,0.35), inset 0 2px 3px rgba(255,255,255,0.6), inset 0 -3px 6px rgba(0,0,0,0.4)",
          }}
        />
        {/* inner metal step */}
        <div
          className="absolute inset-[9px] rounded-full"
          style={{
            background: "linear-gradient(135deg,#3a3f45,#1c1f23 55%,#2c3036)",
            boxShadow:
              "inset 0 2px 5px rgba(0,0,0,0.7), inset 0 -1px 2px rgba(255,255,255,0.12)",
          }}
        />
        {/* scale band */}
        <div
          className="absolute inset-[16px] overflow-hidden rounded-full"
          style={{ background: "radial-gradient(circle at 50% 38%,#202327,#141619 70%,#0c0d0f)" }}
        >
          {ticks.map((s, i) => (
            <div key={i} style={s} />
          ))}
          {degLabels.map((d, i) => (
            <div key={i} style={d.style}>{d.text}</div>
          ))}
        </div>
        {/* dark dial face */}
        <div
          className="absolute left-1/2 top-1/2 -ml-[125px] -mt-[125px] h-[250px] w-[250px] rounded-full"
          style={{
            background: "radial-gradient(circle at 50% 34%,#2b2f34,#16181b 66%,#0d0e10)",
            boxShadow:
              "inset 0 4px 14px rgba(0,0,0,0.8), inset 0 -2px 6px rgba(255,255,255,0.05), 0 0 0 1px rgba(0,0,0,0.6)",
          }}
        >
          {cardinals.map((c, i) => (
            <div key={i} style={c.style}>{c.text}</div>
          ))}

          {/* heading arrow */}
          <div
            className="absolute left-1/2 top-1/2 -ml-[120px] -mt-[120px] h-[240px] w-[240px]"
            style={{
              transformOrigin: "50% 50%",
              transition: "transform 0.9s cubic-bezier(.34,1.2,.44,1)",
              transform: `rotate(${direction}deg)`,
              filter: "drop-shadow(0 4px 6px rgba(0,0,0,0.6))",
            }}
          >
            <div
              className="absolute inset-0 rounded-[2px]"
              style={{
                background: "linear-gradient(135deg,#9aa1ab,#6c7480 55%,#4c525c)",
                clipPath:
                  "polygon(50% 7.5%, 62.5% 58%, 51.5% 50%, 50% 87.5%, 48.5% 50%, 37.5% 58%)",
              }}
            />
          </div>

          {/* center readout */}
          <div className="absolute left-1/2 top-[62%] -translate-x-1/2 whitespace-nowrap text-center">
            <div
              className="text-[30px] font-bold tracking-[0.5px] text-[#eef1f4]"
              style={{ fontFamily: "'Barlow Semi Condensed', sans-serif", textShadow: "0 1px 6px rgba(0,0,0,0.8)" }}
            >
              {dirText}
            </div>
          </div>

          {/* center hub */}
          <div
            className="absolute left-1/2 top-1/2 -ml-[6.5px] -mt-[6.5px] h-[13px] w-[13px] rounded-full"
            style={{
              background: "radial-gradient(circle at 40% 35%,#cfd4da,#5a6068 70%,#34383d)",
              boxShadow: "0 1px 2px rgba(0,0,0,0.7)",
            }}
          />
        </div>
      </div>

      {/* ===== GAUGES ===== */}
      {/* Top-left */}
      <div
        className="absolute left-[104px] top-[70px] h-[132px] w-[304px] rounded-[22px]"
        style={{
          background: `linear-gradient(150deg, ${cA1}, ${cA2})`,
          boxShadow: "0 16px 30px rgba(0,0,0,0.18), inset 0 1px 0 rgba(255,255,255,0.07)",
          WebkitMaskImage: maskTL,
          maskImage: maskTL,
        }}
      >
        <div className="px-[26px] py-[22px]">
          <div className="mb-[16px] border-b border-white/10 pb-[9px] text-[14px] font-bold tracking-[1.6px] text-[#aeb9cb]">
            {setLabel}
          </div>
          <div
            className="text-[44px] font-bold leading-none text-[#f2f5f9]"
            style={{ fontFamily: "'Barlow Semi Condensed', sans-serif", textShadow: "0 0 18px rgba(45,108,255,0.28)" }}
          >
            {setValue}
            <span className="text-[26px] font-medium text-[#c3ccd9]">{setUnit}</span>
          </div>
        </div>
      </div>

      {/* Top-right */}
      <div
        className="absolute left-[616px] top-[70px] h-[132px] w-[304px] rounded-[22px]"
        style={{
          background: `linear-gradient(210deg, ${cB1}, ${cB2})`,
          boxShadow: "0 16px 30px rgba(0,0,0,0.18), inset 0 1px 0 rgba(255,255,255,0.07)",
          WebkitMaskImage: maskTR,
          maskImage: maskTR,
        }}
      >
        <div className="px-[26px] py-[22px] text-right">
          <div className="mb-[16px] border-b border-white/10 pb-[9px] text-[14px] font-bold tracking-[1.6px] text-[#c2c7cd]">
            {driftLabel}
          </div>
          <div
            className="text-[44px] font-bold leading-none text-[#f2f5f9]"
            style={{ fontFamily: "'Barlow Semi Condensed', sans-serif", textShadow: "0 0 18px rgba(159,182,214,0.22)" }}
          >
            {driftValue}
            <span className="pl-[6px] text-[26px] font-medium text-[#c3ccd9]">{driftUnit}</span>
          </div>
        </div>
      </div>

      {/* Bottom-left */}
      <div
        className="absolute left-[104px] top-[398px] h-[132px] w-[304px] rounded-[22px]"
        style={{
          background: `linear-gradient(30deg, ${cA1}, ${cA2})`,
          boxShadow: "0 16px 30px rgba(0,0,0,0.18), inset 0 1px 0 rgba(255,255,255,0.07)",
          WebkitMaskImage: maskBL,
          maskImage: maskBL,
        }}
      >
        <div className="px-[26px] py-[22px]">
          <div className="mb-[16px] border-b border-white/10 pb-[9px] text-[14px] font-bold tracking-[1.6px] text-[#aeb9cb]">
            {gust10Label}
          </div>
          <div
            className="text-[44px] font-bold leading-none text-[#f2f5f9]"
            style={{ fontFamily: "'Barlow Semi Condensed', sans-serif", textShadow: "0 0 18px rgba(45,108,255,0.30)" }}
          >
            {gust10Speed}
            <span className="text-[26px] font-medium text-[#c3ccd9]"> {gust10Unit}</span>
          </div>
        </div>
      </div>

      {/* Bottom-right */}
      <div
        className="absolute left-[616px] top-[398px] h-[132px] w-[304px] rounded-[22px]"
        style={{
          background: `linear-gradient(330deg, ${cB1}, ${cB2})`,
          boxShadow: "0 16px 30px rgba(0,0,0,0.18), inset 0 1px 0 rgba(255,255,255,0.07)",
          WebkitMaskImage: maskBR,
          maskImage: maskBR,
        }}
      >
        <div className="px-[26px] py-[22px] text-right">
          <div className="mb-[16px] border-b border-white/10 pb-[9px] text-[14px] font-bold tracking-[1.6px] text-[#c2c7cd]">
            {gustHourLabel}
          </div>
          <div
            className="text-[44px] font-bold leading-none text-[#f2f5f9]"
            style={{ fontFamily: "'Barlow Semi Condensed', sans-serif", textShadow: "0 0 20px rgba(120,170,255,0.40)" }}
          >
            {gustHourSpeed}
            <span className="pl-[6px] text-[26px] font-medium text-[#c3ccd9]">{gustHourUnit}</span>
          </div>
        </div>
      </div>
    </div>
  );
}
