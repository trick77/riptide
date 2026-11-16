// The Sankey's geometry: band heights, node stacks and label positions in
// viewBox units. Rendering only; every value comes from the backend.

export const W = 680;
export const H = 300;
export const X0 = 170;
export const X1 = 500;
export const NW = 12;
const PAD = 12;
const TOP = 8;
// Two label lines need this much height, or a thin node's label prints
// over its neighbour's.
const PITCH = 30;
// The first label line's baseline bounds: its cap height below the top,
// the second line above the bottom.
const LABEL_MIN = 12;
const LABEL_MAX = H - 22;
// A band never thinner than this, however large the rest: a few failed
// hours next to a long outage stay visible.
export const MIN_BAND = 2;

export type FlowIn = { from: string; to: string; seconds: number };

export type Band = {
  index: number;
  from: string;
  to: string;
  h: number;
  sy: number;
  ty: number;
};

// off: where the node's next band starts while the bands are laid out.
export type Slot = {
  key: string;
  y: number;
  h: number;
  labelY: number;
  off: number;
};

// Band heights that fit avail, each at least a minimum: the thin ones take
// the minimum, the rest share what is left by their value. The minimum
// shrinks when the thin ones would take more than half the room, so the
// rest keep a positive scale and their proportions.
export function bandHeights(values: number[], avail: number): number[] {
  if (!values.length || avail <= 0) return values.map(() => 0);
  const floor = Math.min(MIN_BAND, avail / (2 * values.length));
  const total = values.reduce((a, v) => a + v, 0);
  let k = avail / total;
  let thin = -1;
  for (let pass = 0; pass <= values.length; pass++) {
    const small = values.filter((v) => v * k < floor);
    const rest = total - small.reduce((a, v) => a + v, 0);
    if (rest <= 0 || small.length === thin) break;
    thin = small.length;
    k = (avail - small.length * floor) / rest;
  }
  return values.map((v) => Math.max(floor, v * k));
}

// The first label line's baseline per node, centred on it, at least pitch
// apart and inside [LABEL_MIN, LABEL_MAX]; with more labels than fit at
// PITCH, the pitch narrows.
export function labelYs(centres: number[]): number[] {
  const n = centres.length;
  const pitch =
    n > 1 ? Math.min(PITCH, (LABEL_MAX - LABEL_MIN) / (n - 1)) : PITCH;
  const ys = centres.map((c) =>
    Math.min(LABEL_MAX, Math.max(LABEL_MIN, c - 3)),
  );
  for (let i = 1; i < n; i++) ys[i] = Math.max(ys[i], ys[i - 1] + pitch);
  if (n) ys[n - 1] = Math.min(ys[n - 1], LABEL_MAX);
  // The floor again only against rounding: pitch × (n − 1) fits.
  for (let i = n - 2; i >= 0; i--)
    ys[i] = Math.max(LABEL_MIN, Math.min(ys[i], ys[i + 1] - pitch));
  return ys;
}

// One side's nodes in order, each as high as its bands, PAD between,
// centred in the chart.
function stack(keys: string[], heights: Map<string, number>): Slot[] {
  const used =
    keys.reduce((a, k) => a + (heights.get(k) ?? 0), 0) +
    PAD * (keys.length - 1);
  let y = TOP + (H - 2 * TOP - used) / 2;
  const slots = keys.map((key) => {
    const h = heights.get(key) ?? 0;
    const s = { key, y, h, labelY: 0, off: y };
    y += h + PAD;
    return s;
  });
  labelYs(slots.map((s) => s.y + s.h / 2)).forEach(
    (ly, i) => (slots[i].labelY = ly),
  );
  return slots;
}

export type Layout = {
  left: Map<string, Slot>;
  right: Map<string, Slot>;
  bands: Band[];
};

// Lays the flows out between the left and right node keys, in order. A
// flow to a node not given is left out.
export function layout(
  leftKeys: string[],
  rightKeys: string[],
  flows: FlowIn[],
): Layout {
  const lk = new Set(leftKeys);
  const rk = new Set(rightKeys);
  const drawn = flows
    .map((f, index) => ({ ...f, index }))
    .filter((f) => f.seconds > 0 && lk.has(f.from) && rk.has(f.to));
  const n = Math.max(leftKeys.length, rightKeys.length);
  const hs = bandHeights(
    drawn.map((f) => f.seconds),
    H - 2 * TOP - PAD * (n - 1),
  );
  const sums = (side: "from" | "to") => {
    const m = new Map<string, number>();
    drawn.forEach((f, i) => m.set(f[side], (m.get(f[side]) ?? 0) + hs[i]));
    return m;
  };
  const left = new Map(stack(leftKeys, sums("from")).map((s) => [s.key, s]));
  const right = new Map(stack(rightKeys, sums("to")).map((s) => [s.key, s]));
  // Each slot's next free y, as its bands stack up.
  const at = (s: Slot, h: number) => {
    const y = s.off;
    s.off += h;
    return y;
  };
  const bands = drawn.map((f, i) => ({
    index: f.index,
    from: f.from,
    to: f.to,
    h: hs[i],
    sy: at(left.get(f.from)!, hs[i]),
    ty: at(right.get(f.to)!, hs[i]),
  }));
  return { left, right, bands };
}
