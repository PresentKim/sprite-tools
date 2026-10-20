// Selection (pure functions, independent of the DOM).
// A selection is represented as "a mask inside a bounding box": { x, y, w, h, mask: Uint8Array(w*h), count, edges }.
// Cells outside the box are unselected, and moving just the box moves the whole selection, so the mask need not be rebuilt when dragging a floating layer.

export function makeSel(x, y, w, h, mask) {
  let count = 0;
  for (let i = 0; i < mask.length; i++) if (mask[i]) count++;
  return { x, y, w, h, mask, count, edges: null };
}

// Rectangle (both end cells inclusive, order does not matter)
export function rectSel(x0, y0, x1, y1) {
  const l = Math.min(x0, x1), t = Math.min(y0, y1), r = Math.max(x0, x1), b = Math.max(y0, y1);
  const w = r - l + 1, h = b - t + 1;
  const s = makeSel(l, t, w, h, new Uint8Array(w * h).fill(1));
  return s;
}

export function selHas(s, x, y) {
  x -= s.x; y -= s.y;
  return x >= 0 && y >= 0 && x < s.w && y < s.h && s.mask[y * s.w + x] === 1;
}

// null if no cell is selected; otherwise the selection with its bounding box shrunk to fit
export function tighten(s) {
  if (!s) return null;
  const { w, h, mask } = s;
  let x0 = w, y0 = h, x1 = -1, y1 = -1;
  for (let y = 0; y < h; y++) {
    const row = y * w;
    for (let x = 0; x < w; x++) {
      if (!mask[row + x]) continue;
      if (x < x0) x0 = x;
      if (x > x1) x1 = x;
      if (y < y0) y0 = y;
      y1 = y;
    }
  }
  if (x1 < 0) return null;
  if (x0 === 0 && y0 === 0 && x1 === w - 1 && y1 === h - 1) return s;
  const nw = x1 - x0 + 1, nh = y1 - y0 + 1;
  const out = new Uint8Array(nw * nh);
  for (let y = 0; y < nh; y++) out.set(mask.subarray((y + y0) * w + x0, (y + y0) * w + x0 + nw), y * nw);
  return makeSel(s.x + x0, s.y + y0, nw, nh, out);
}

// Keep only the part inside the document [0,W)×[0,H).
export function clipSel(s, W, H) {
  if (!s) return null;
  if (s.x >= 0 && s.y >= 0 && s.x + s.w <= W && s.y + s.h <= H) return s;
  const l = Math.max(0, s.x), t = Math.max(0, s.y), r = Math.min(W, s.x + s.w), b = Math.min(H, s.y + s.h);
  if (r <= l || b <= t) return null;
  const nw = r - l, nh = b - t;
  const out = new Uint8Array(nw * nh);
  for (let y = 0; y < nh; y++) {
    const so = (t + y - s.y) * s.w + (l - s.x);
    out.set(s.mask.subarray(so, so + nw), y * nw);
  }
  return tighten(makeSel(l, t, nw, nh, out));
}

export function allSel(W, H) {
  return rectSel(0, 0, W - 1, H - 1);
}

// Combine a rectangle (ends inclusive) using mode (replace | add | sub). It is clipped to the document size W×H first.
export function combineRect(s, x0, y0, x1, y1, mode, W, H) {
  const l = Math.max(0, Math.min(x0, x1)), t = Math.max(0, Math.min(y0, y1));
  const r = Math.min(W - 1, Math.max(x0, x1)), b = Math.min(H - 1, Math.max(y0, y1));
  if (r < l || b < t) return mode === 'replace' ? null : s;
  if (mode === 'replace' || !s) return mode === 'sub' ? null : rectSel(l, t, r, b);
  if (mode === 'add') {
    const nl = Math.min(l, s.x), nt = Math.min(t, s.y), nr = Math.max(r, s.x + s.w - 1), nb = Math.max(b, s.y + s.h - 1);
    const nw = nr - nl + 1, nh = nb - nt + 1;
    const out = new Uint8Array(nw * nh);
    for (let y = 0; y < s.h; y++) out.set(s.mask.subarray(y * s.w, (y + 1) * s.w), (s.y - nt + y) * nw + (s.x - nl));
    for (let y = t; y <= b; y++) out.fill(1, (y - nt) * nw + (l - nl), (y - nt) * nw + (r - nl) + 1);
    return makeSel(nl, nt, nw, nh, out);
  }
  // sub
  const out = s.mask.slice();
  for (let y = Math.max(t, s.y); y <= Math.min(b, s.y + s.h - 1); y++) {
    const a = Math.max(l, s.x), z = Math.min(r, s.x + s.w - 1);
    if (z >= a) out.fill(0, (y - s.y) * s.w + (a - s.x), (y - s.y) * s.w + (z - s.x) + 1);
  }
  return tighten(makeSel(s.x, s.y, s.w, s.h, out));
}

// Invert the selection (relative to the whole document). null if everything was selected
export function invertSel(s, W, H) {
  if (!s) return allSel(W, H);
  const out = new Uint8Array(W * H);
  for (let y = 0; y < H; y++) {
    for (let x = 0; x < W; x++) out[y * W + x] = selHas(s, x, y) ? 0 : 1;
  }
  return tighten(makeSel(0, 0, W, H, out));
}

// Selection boundary segments [x0,y0,x1,y1,...] (coordinates relative to the selection box, connected runs merged into one). The result is cached.
export function edgesOf(s) {
  if (s.edges) return s.edges;
  const { w, h, mask } = s;
  const out = [];
  const at = (x, y) => (x >= 0 && y >= 0 && x < w && y < h && mask[y * w + x] ? 1 : 0);
  for (let y = 0; y <= h; y++) {
    let start = -1;
    for (let x = 0; x <= w; x++) {
      const on = x < w && at(x, y - 1) !== at(x, y);
      if (on && start < 0) start = x;
      else if (!on && start >= 0) { out.push(start, y, x, y); start = -1; }
    }
  }
  for (let x = 0; x <= w; x++) {
    let start = -1;
    for (let y = 0; y <= h; y++) {
      const on = y < h && at(x - 1, y) !== at(x, y);
      if (on && start < 0) start = y;
      else if (!on && start >= 0) { out.push(x, start, x, y); start = -1; }
    }
  }
  s.edges = out;
  return out;
}

// Get the pixels under the selection (cells outside it are 0). Returns: Uint32Array(w*h)
export function extractPix(px32, W, H, s) {
  const out = new Uint32Array(s.w * s.h);
  for (let y = 0; y < s.h; y++) {
    const gy = s.y + y;
    if (gy < 0 || gy >= H) continue;
    for (let x = 0; x < s.w; x++) {
      const gx = s.x + x;
      if (gx < 0 || gx >= W || !s.mask[y * s.w + x]) continue;
      out[y * s.w + x] = px32[gy * W + gx];
    }
  }
  return out;
}

// Build a selection from a list of pixel indices (y*W+x). null if empty.
export function fromIndices(list, W) {
  if (!list.length) return null;
  let x0 = Infinity, y0 = Infinity, x1 = -1, y1 = -1;
  for (const i of list) {
    const x = i % W, y = (i / W) | 0;
    if (x < x0) x0 = x;
    if (x > x1) x1 = x;
    if (y < y0) y0 = y;
    if (y > y1) y1 = y;
  }
  const w = x1 - x0 + 1, h = y1 - y0 + 1;
  const mask = new Uint8Array(w * h);
  for (const i of list) mask[(((i / W) | 0) - y0) * w + (i % W) - x0] = 1;
  return makeSel(x0, y0, w, h, mask);
}

// Combine selection b into selection a. mode: replace (use b) | add (union) | sub (subtract b from a)
export function combineSels(a, b, mode) {
  if (mode === 'replace' || !a) return mode === 'sub' ? null : b;
  if (!b) return a;
  if (mode === 'add') {
    const nl = Math.min(a.x, b.x), nt = Math.min(a.y, b.y);
    const nr = Math.max(a.x + a.w, b.x + b.w), nb = Math.max(a.y + a.h, b.y + b.h);
    const nw = nr - nl, nh = nb - nt;
    const out = new Uint8Array(nw * nh);
    for (const s of [a, b]) {
      for (let y = 0; y < s.h; y++) {
        for (let x = 0; x < s.w; x++) if (s.mask[y * s.w + x]) out[(s.y - nt + y) * nw + (s.x - nl + x)] = 1;
      }
    }
    return makeSel(nl, nt, nw, nh, out);
  }
  const out = a.mask.slice();
  for (let y = 0; y < a.h; y++) {
    for (let x = 0; x < a.w; x++) if (out[y * a.w + x] && selHas(b, a.x + x, a.y + y)) out[y * a.w + x] = 0;
  }
  return tighten(makeSel(a.x, a.y, a.w, a.h, out));
}
