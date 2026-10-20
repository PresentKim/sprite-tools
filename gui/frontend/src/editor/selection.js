// 선택 영역 (DOM 과 무관한 순수 함수).
// 선택은 "경계 상자 안의 마스크"로 표현합니다: { x, y, w, h, mask: Uint8Array(w*h), count, edges }.
// 상자 밖은 선택되지 않은 칸이고, 상자만 옮기면 선택이 통째로 이동하므로 떠 있는 층을 끌 때 마스크를 다시 만들지 않아도 됩니다.

export function makeSel(x, y, w, h, mask) {
  let count = 0;
  for (let i = 0; i < mask.length; i++) if (mask[i]) count++;
  return { x, y, w, h, mask, count, edges: null };
}

// 사각형 (양 끝 칸 포함, 순서 무관)
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

// 선택된 칸이 하나도 없으면 null, 있으면 경계 상자를 꼭 맞게 줄인 선택
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

// 문서 [0,W)×[0,H) 안쪽만 남깁니다.
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

// 사각형(양 끝 포함)을 mode(replace | add | sub)로 합칩니다. 문서 크기 W×H 안으로 줄여서 합니다.
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

// 선택 반전 (문서 전체 기준). 전부 선택돼 있었으면 null
export function invertSel(s, W, H) {
  if (!s) return allSel(W, H);
  const out = new Uint8Array(W * H);
  for (let y = 0; y < H; y++) {
    for (let x = 0; x < W; x++) out[y * W + x] = selHas(s, x, y) ? 0 : 1;
  }
  return tighten(makeSel(0, 0, W, H, out));
}

// 선택 경계선 조각들 [x0,y0,x1,y1,...] (선택 상자 기준 좌표, 이어진 구간은 하나로 합침). 결과를 캐시합니다.
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

// 선택에 해당하는 픽셀을 가져옵니다 (선택 밖 칸은 0). 반환: Uint32Array(w*h)
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
