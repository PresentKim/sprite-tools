// Drawing algorithms (pure functions, independent of the DOM). All coordinates are integer pixels.
import { diff, norm } from './color.js';

// ---------- Brush ----------

const fpCache = new Map();

// Brush footprint: [dx0, dy0, dx1, dy1, ...] (coordinates relative to the center pixel). For size×size the center is (size>>1).
export function footprint(size, shape) {
  const key = `${size}|${shape}`;
  let fp = fpCache.get(key);
  if (fp) return fp;
  const cells = [];
  const c = (size - 1) / 2;
  const r2 = (size / 2) * (size / 2) - 0.5;
  const off = size >> 1;
  for (let y = 0; y < size; y++) {
    for (let x = 0; x < size; x++) {
      if (shape === 'round' && size > 2 && (x - c) ** 2 + (y - c) ** 2 > r2) continue;
      cells.push(x - off, y - off);
    }
  }
  fp = Int16Array.from(cells);
  fpCache.set(key, fp);
  return fp;
}

// ---------- Line ----------

// Bresenham. Returns [x0,y0,x1,y1,...] including the end points.
export function linePoints(x0, y0, x1, y1) {
  const out = [];
  const dx = Math.abs(x1 - x0), sx = x0 < x1 ? 1 : -1;
  const dy = -Math.abs(y1 - y0), sy = y0 < y1 ? 1 : -1;
  let err = dx + dy;
  for (;;) {
    out.push(x0, y0);
    if (x0 === x1 && y0 === y1) break;
    const e2 = 2 * err;
    if (e2 >= dy) { err += dy; x0 += sx; }
    if (e2 <= dx) { err += dx; y0 += sy; }
  }
  return out;
}

const DIRS = [[1, 0], [1, 1], [0, 1], [-1, 1], [-1, 0], [-1, -1], [0, -1], [1, -1]];

// Snap the end point to a 0/45/90... degree direction from the start point.
export function snapLine(x0, y0, x1, y1) {
  const dx = x1 - x0, dy = y1 - y0;
  if (!dx && !dy) return [x1, y1];
  const oct = ((Math.round(Math.atan2(dy, dx) / (Math.PI / 4)) % 8) + 8) % 8;
  const [ux, uy] = DIRS[oct];
  const n = ux && uy ? Math.round((Math.abs(dx) + Math.abs(dy)) / 2) : ux ? Math.abs(dx) : Math.abs(dy);
  return [x0 + ux * n, y0 + uy * n];
}

// Shift: make rectangles and ellipses squares/circles (fit to the longer dragged side)
export function squareEnd(x0, y0, x1, y1) {
  const d = Math.max(Math.abs(x1 - x0), Math.abs(y1 - y0));
  return [x0 + (x1 < x0 ? -d : d), y0 + (y1 < y0 ? -d : d)];
}

// ---------- Pixel perfect ----------

// Add one point to the end of the path while removing the middle point that forms an L-shaped corner.
// Returns the removed point as [x, y], or null if nothing was removed. path is a flat array [x,y,x,y,...].
export function pushPixelPerfect(path, x, y) {
  const n = path.length;
  if (n >= 2 && path[n - 2] === x && path[n - 1] === y) return null;
  path.push(x, y);
  if (path.length < 6) return null;
  const m = path.length;
  const ax = path[m - 6], ay = path[m - 5], bx = path[m - 4], by = path[m - 3];
  if (Math.abs(ax - x) === 1 && Math.abs(ay - y) === 1 && ((ax === bx && by === y) || (ay === by && bx === x))) {
    path.splice(m - 4, 2);
    return [bx, by];
  }
  return null;
}

// ---------- Shapes ----------

// Centerline coordinates of a rectangle outline (emit(x,y))
export function rectOutline(x0, y0, x1, y1, emit) {
  const l = Math.min(x0, x1), r = Math.max(x0, x1), t = Math.min(y0, y1), b = Math.max(y0, y1);
  for (let x = l; x <= r; x++) { emit(x, t); if (b !== t) emit(x, b); }
  for (let y = t + 1; y < b; y++) { emit(l, y); if (r !== l) emit(r, y); }
}

export function rectFill(x0, y0, x1, y1, emit) {
  const l = Math.min(x0, x1), r = Math.max(x0, x1), t = Math.min(y0, y1), b = Math.max(y0, y1);
  for (let y = t; y <= b; y++) for (let x = l; x <= r; x++) emit(x, y);
}

// Per-row [left, right] ranges of the ellipse that exactly fits the bounding box
function ellipseRows(x0, y0, x1, y1) {
  const l = Math.min(x0, x1), r = Math.max(x0, x1), t = Math.min(y0, y1), b = Math.max(y0, y1);
  const w = r - l + 1, h = b - t + 1;
  const cx2 = l + r, cy2 = t + b;
  const rows = [];
  for (let y = t; y <= b; y++) {
    const ty = (2 * y - cy2) / h;
    const s = Math.sqrt(Math.max(0, 1 - ty * ty));
    let xl = Math.ceil((cx2 - w * s) / 2 - 1e-9);
    let xr = Math.floor((cx2 + w * s) / 2 + 1e-9);
    if (xl > xr) { xl = cx2 >> 1; xr = (cx2 + 1) >> 1; }
    rows.push([Math.max(l, xl), Math.min(r, xr)]);
  }
  return { rows, t };
}

export function ellipseFill(x0, y0, x1, y1, emit) {
  const { rows, t } = ellipseRows(x0, y0, x1, y1);
  rows.forEach(([a, b], i) => { for (let x = a; x <= b; x++) emit(x, t + i); });
}

// Inner pixels with an outside neighbor up/down/left/right = thin outline
export function ellipseOutline(x0, y0, x1, y1, emit) {
  const { rows, t } = ellipseRows(x0, y0, x1, y1);
  rows.forEach(([L, R], i) => {
    const up = rows[i - 1], dn = rows[i + 1];
    const lu = up ? up[0] : Infinity, ld = dn ? dn[0] : Infinity;
    const ru = up ? up[1] : -Infinity, rd = dn ? dn[1] : -Infinity;
    const a = Math.min(R, Math.max(L, Math.max(lu, ld) - 1));
    const b = Math.max(L, Math.min(R, Math.min(ru, rd) + 1)); // the outermost pixel R is always part of the outline
    const y = t + i;
    if (b <= a + 1) {
      for (let x = L; x <= R; x++) emit(x, y);
    } else {
      for (let x = L; x <= a; x++) emit(x, y);
      for (let x = b; x <= R; x++) emit(x, y);
    }
  });
}

// ---------- Fill ----------

// Pixel indices of the connected region equal to the start pixel (within tolerance). Connectivity is 4-way (up/down/left/right) by default, and 8-way including diagonals if diag is set.
export function floodContiguous(px32, w, h, sx, sy, tol, diag = false) {
  const seed = norm(px32[sy * w + sx]);
  const match = tol > 0 ? (c) => diff(c, seed) <= tol : (c) => norm(c) === seed;
  const seen = new Uint8Array(w * h);
  const out = [];
  const stack = [sy * w + sx];
  seen[stack[0]] = 1;
  while (stack.length) {
    const i = stack.pop();
    if (!match(px32[i])) continue;
    out.push(i);
    const x = i % w, y = (i / w) | 0;
    if (x > 0 && !seen[i - 1]) { seen[i - 1] = 1; stack.push(i - 1); }
    if (x < w - 1 && !seen[i + 1]) { seen[i + 1] = 1; stack.push(i + 1); }
    if (y > 0 && !seen[i - w]) { seen[i - w] = 1; stack.push(i - w); }
    if (y < h - 1 && !seen[i + w]) { seen[i + w] = 1; stack.push(i + w); }
    if (diag) {
      const l = x > 0, r = x < w - 1;
      if (y > 0 && l && !seen[i - w - 1]) { seen[i - w - 1] = 1; stack.push(i - w - 1); }
      if (y > 0 && r && !seen[i - w + 1]) { seen[i - w + 1] = 1; stack.push(i - w + 1); }
      if (y < h - 1 && l && !seen[i + w - 1]) { seen[i + w - 1] = 1; stack.push(i + w - 1); }
      if (y < h - 1 && r && !seen[i + w + 1]) { seen[i + w + 1] = 1; stack.push(i + w + 1); }
    }
  }
  return out;
}

// Pixel indices of all pixels in the whole image with the same color as the start pixel
export function floodGlobal(px32, sx, sy, w, tol) {
  const seed = norm(px32[sy * w + sx]);
  const match = tol > 0 ? (c) => diff(c, seed) <= tol : (c) => norm(c) === seed;
  const out = [];
  for (let i = 0; i < px32.length; i++) if (match(px32[i])) out.push(i);
  return out;
}

// ---------- Transform ----------

// Flip or rotate an array (w×h, any type). kind: flipH | flipV | cw | ccw | r180. Returns: { arr, w, h }
export function transformPix(src, w, h, kind) {
  const rot = kind === 'cw' || kind === 'ccw';
  const nw = rot ? h : w, nh = rot ? w : h;
  const dst = new src.constructor(w * h);
  for (let y = 0; y < h; y++) {
    for (let x = 0; x < w; x++) {
      let nx, ny;
      switch (kind) {
        case 'flipH': nx = w - 1 - x; ny = y; break;
        case 'flipV': nx = x; ny = h - 1 - y; break;
        case 'cw': nx = h - 1 - y; ny = x; break;
        case 'ccw': nx = y; ny = w - 1 - x; break;
        default: nx = w - 1 - x; ny = h - 1 - y; // r180
      }
      dst[ny * nw + nx] = src[y * w + x];
    }
  }
  return { arr: dst, w: nw, h: nh };
}

// Bounding box { x0, y0, x1, y1 } of pixels with non-zero alpha (end cells inclusive). null if none
export function opaqueBounds(px32, w, h) {
  let x0 = w, y0 = h, x1 = -1, y1 = -1;
  for (let y = 0; y < h; y++) {
    for (let x = 0; x < w; x++) {
      if (px32[y * w + x] >>> 24 === 0) continue;
      if (x < x0) x0 = x;
      if (x > x1) x1 = x;
      if (y < y0) y0 = y;
      y1 = y;
    }
  }
  return x1 < 0 ? null : { x0, y0, x1, y1 };
}

// Copy nw×nh starting at (sx,sy) of src (sw×sh) into a new array. Outside the range is 0 (transparent)
export function cropPix(src, sw, sh, sx, sy, nw, nh) {
  const dst = new Uint32Array(nw * nh);
  for (let y = 0; y < nh; y++) {
    const gy = sy + y;
    if (gy < 0 || gy >= sh) continue;
    const a = Math.max(0, sx), b = Math.min(sw, sx + nw);
    if (b > a) dst.set(src.subarray(gy * sw + a, gy * sw + b), y * nw + (a - sx));
  }
  return dst;
}
