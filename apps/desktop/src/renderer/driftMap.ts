/* Continental-drift map. Geometry ported from the marketing site's DriftMap so
   the two stay visually identical; the plates' position is bound to CSS state. */

type Pt = [number, number];

// Deterministic PRNG — the coastline must be identical on every render.
const mulberry32 = (seed: number) => (): number => {
  seed |= 0;
  seed = (seed + 0x6d2b79f5) | 0;
  let t = Math.imul(seed ^ (seed >>> 15), 1 | seed);
  t = (t + Math.imul(t ^ (t >>> 7), 61 | t)) ^ t;
  return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
};

const SKELETON: Pt[] = [
  [285, 165], [370, 120], [470, 105], [560, 145], [610, 215],
  [625, 280], [505, 300], [450, 360], [505, 405], [595, 420],
  [640, 475], [620, 545], [585, 635], [470, 655], [380, 665],
  [285, 610], [225, 520], [230, 435], [205, 360], [235, 285],
  [250, 220]
];

// Endpoints stay fixed so the shared joints between fragments keep meeting.
function fractalLine(points: Pt[], iterations: number, amplitude: number, rng: () => number): Pt[] {
  if (iterations === 0) return points;
  const next: Pt[] = [];
  for (let i = 0; i < points.length - 1; i++) {
    const [ax, ay] = points[i];
    const [bx, by] = points[i + 1];
    const dx = bx - ax;
    const dy = by - ay;
    const len = Math.hypot(dx, dy) || 1;
    const offset = (rng() - 0.5) * 2 * len * amplitude;
    next.push([ax, ay], [(ax + bx) / 2 - (dy / len) * offset, (ay + by) / 2 + (dx / len) * offset]);
  }
  next.push(points[points.length - 1]);
  return fractalLine(next, iterations - 1, amplitude * 0.8, rng);
}

const S = SKELETON;
const BOTTOM_SPLIT: Pt = [432, 658];

// Each rift is fractalised once and shared by the fragments either side, so the
// facing coastlines still fit when the plates pull apart.
const RIFT_A = fractalLine([S[7], [385, 342], [318, 368], [258, 348], S[18]], 3, 0.09, mulberry32(11));
const RIFT_B = fractalLine([S[8], [488, 470], [502, 540], [462, 600], BOTTOM_SPLIT], 3, 0.09, mulberry32(12));

const coast = (pts: Pt[], seed: number): Pt[] => fractalLine(pts, 3, 0.14, mulberry32(seed));
const reversed = (pts: Pt[]): Pt[] => pts.slice().reverse();
const toPath = (pts: Pt[]): string => `M ${pts.map(([x, y]) => `${x.toFixed(1)} ${y.toFixed(1)}`).join(" L ")} Z`;

const NORTH_PATH = toPath([
  ...coast([S[18], S[19], S[20], S[0], S[1], S[2], S[3], S[4], S[5], S[6], S[7]], 21),
  ...RIFT_A.slice(1, -1)
]);

const SOUTH_EAST_PATH = toPath([
  ...coast([S[8], S[9], S[10], S[11], S[12], S[13], BOTTOM_SPLIT], 22),
  ...reversed(RIFT_B).slice(1, -1)
]);

const SOUTH_WEST_PATH = toPath([
  ...coast([S[7], S[8]], 23),
  ...RIFT_B.slice(1),
  ...coast([BOTTOM_SPLIT, S[14], S[15], S[16], S[17], S[18]], 24).slice(1),
  ...reversed(RIFT_A).slice(1, -1)
]);

interface Fragment {
  readonly id: string;
  readonly d: string;
  readonly ridge?: string;
  readonly stipple: readonly Pt[];
}

// How far each plate drifts lives in styles.css, keyed on [data-frag] — the
// page CSP forbids inline styles, and the distance is a design constant.
const FRAGMENTS: readonly Fragment[] = [
  {
    id: "north",
    d: NORTH_PATH,
    ridge: "M 296 220 C 330 208, 366 224, 400 212 C 424 204, 448 214, 470 226",
    stipple: [[330, 180], [392, 158], [450, 180], [520, 230], [280, 260], [300, 310], [560, 200]]
  },
  {
    id: "southWest",
    d: SOUTH_WEST_PATH,
    ridge: "M 268 440 C 296 464, 306 500, 334 524 C 356 542, 382 556, 400 574",
    stipple: [[300, 470], [260, 520], [340, 560], [310, 610], [270, 410]]
  },
  {
    id: "southEast",
    d: SOUTH_EAST_PATH,
    stipple: [[560, 470], [540, 560], [530, 610], [590, 505]]
  }
];

const NS = "http://www.w3.org/2000/svg";

function el(tag: string, attrs: Record<string, string | number>): SVGElement {
  const node = document.createElementNS(NS, tag);
  for (const [key, value] of Object.entries(attrs)) node.setAttribute(key, String(value));
  return node;
}

export function buildDriftMap(): SVGSVGElement {
  const svg = el("svg", {
    viewBox: "0 0 800 800",
    fill: "none",
    "aria-hidden": "true",
    class: "drift-map"
  }) as SVGSVGElement;

  const graticule = el("g", { stroke: "currentColor", "stroke-width": "0.75", opacity: "0.16" });
  graticule.append(
    el("circle", { cx: 400, cy: 400, r: 340 }),
    el("ellipse", { cx: 400, cy: 400, rx: 240, ry: 340 }),
    el("ellipse", { cx: 400, cy: 400, rx: 120, ry: 340 }),
    el("ellipse", { cx: 400, cy: 400, rx: 340, ry: 240 }),
    el("ellipse", { cx: 400, cy: 400, rx: 340, ry: 120 }),
    el("line", { x1: 60, y1: 400, x2: 740, y2: 400 }),
    el("line", { x1: 400, y1: 60, x2: 400, y2: 740 })
  );
  svg.append(graticule);

  svg.append(el("circle", {
    class: "drift-orbit",
    cx: 400, cy: 400, r: 356,
    stroke: "currentColor", "stroke-width": "0.75", "stroke-dasharray": "2 10", opacity: "0.14"
  }));

  for (const { id, d, ridge, stipple } of FRAGMENTS) {
    const group = el("g", { class: "drift-frag", "data-frag": id }) as SVGGElement;

    group.append(el("path", {
      d,
      stroke: "currentColor",
      "stroke-width": "2",
      "stroke-linejoin": "round",
      "stroke-linecap": "round",
      fill: "currentColor",
      "fill-opacity": "0.05"
    }));

    if (ridge) {
      group.append(el("path", {
        d: ridge, stroke: "currentColor", "stroke-width": "1", opacity: "0.4", "stroke-linecap": "round"
      }));
    }

    const dots = el("g", { fill: "currentColor", opacity: "0.3" });
    for (const [x, y] of stipple) dots.append(el("circle", { cx: x, cy: y, r: 1.6 }));
    group.append(dots);

    svg.append(group);
  }

  const rose = el("g", { stroke: "currentColor", "stroke-width": "0.9", opacity: "0.5", "stroke-linecap": "round" });
  rose.append(
    el("line", { x1: 676, y1: 138, x2: 676, y2: 198 }),
    el("line", { x1: 646, y1: 168, x2: 706, y2: 168 }),
    el("line", { x1: 657, y1: 149, x2: 695, y2: 187 }),
    el("line", { x1: 695, y1: 149, x2: 657, y2: 187 }),
    el("circle", { cx: 676, cy: 168, r: 6 }),
    el("circle", { cx: 676, cy: 168, r: 1.5, fill: "currentColor", stroke: "none" })
  );
  svg.append(rose);

  return svg;
}
