// 캔버스 그리기. 문서는 오프스크린 캔버스(doc.off)에 1:1 로 들어 있고, 여기서 보이는 캔버스로 정수 배율 확대해 그립니다.

const cssVar = (name, fallback) => getComputedStyle(document.documentElement).getPropertyValue(name).trim() || fallback;

let checkCanvas = null;
let checkKey = '';
function checkPattern(ctx, cell) {
  const a = cssVar('--check-a', '#2a2b33'), b = cssVar('--check-b', '#33343d');
  const key = `${cell}|${a}|${b}`;
  if (!checkCanvas || key !== checkKey) {
    checkCanvas = document.createElement('canvas');
    checkCanvas.width = checkCanvas.height = cell * 2;
    const g = checkCanvas.getContext('2d');
    g.fillStyle = a;
    g.fillRect(0, 0, cell * 2, cell * 2);
    g.fillStyle = b;
    g.fillRect(cell, 0, cell, cell);
    g.fillRect(0, cell, cell, cell);
    checkKey = key;
  }
  return ctx.createPattern(checkCanvas, 'repeat');
}

// view: {zoom, ox, oy} — ox,oy 는 뷰포트 안에서 문서 왼쪽 위의 CSS 픽셀 위치
export function drawMain(ctx, cw, ch, dpr, doc, view, o) {
  ctx.setTransform(1, 0, 0, 1, 0, 0);
  ctx.clearRect(0, 0, cw, ch);
  if (!doc) return;
  const z = view.zoom * dpr;
  const dx = Math.round(view.ox * dpr), dy = Math.round(view.oy * dpr);
  const dw = Math.round(doc.w * z), dh = Math.round(doc.h * z);
  const x0 = Math.max(0, dx), y0 = Math.max(0, dy), x1 = Math.min(cw, dx + dw), y1 = Math.min(ch, dy + dh);
  if (x1 <= x0 || y1 <= y0) return;

  // 투명 배경 체크무늬
  ctx.save();
  ctx.beginPath();
  ctx.rect(x0, y0, x1 - x0, y1 - y0);
  ctx.clip();
  ctx.translate(dx, dy);
  ctx.fillStyle = checkPattern(ctx, Math.max(4, Math.round(8 * dpr)));
  ctx.fillRect(x0 - dx, y0 - dy, x1 - x0, y1 - y0);
  ctx.restore();

  // 이미지 (보이는 부분만)
  const sx0 = Math.max(0, Math.floor((x0 - dx) / z)), sy0 = Math.max(0, Math.floor((y0 - dy) / z));
  const sx1 = Math.min(doc.w, Math.ceil((x1 - dx) / z)), sy1 = Math.min(doc.h, Math.ceil((y1 - dy) / z));
  ctx.imageSmoothingEnabled = false;
  const px = (i) => Math.round(i * z);
  ctx.drawImage(doc.off, sx0, sy0, sx1 - sx0, sy1 - sy0, dx + px(sx0), dy + px(sy0), px(sx1) - px(sx0), px(sy1) - px(sy0));

  // 픽셀 격자
  if (o.grid && view.zoom >= 4) {
    ctx.fillStyle = 'rgba(128,128,128,0.38)';
    for (let i = sx0; i <= sx1; i++) { const x = dx + px(i); if (x >= x0 && x < x1) ctx.fillRect(x, y0, 1, y1 - y0); }
    for (let j = sy0; j <= sy1; j++) { const y = dy + px(j); if (y >= y0 && y < y1) ctx.fillRect(x0, y, x1 - x0, 1); }
  }
  // 타일 격자
  const t = o.tile;
  if (t > 0 && view.zoom * t >= 6) {
    ctx.fillStyle = cssVar('--accent', '#6aa7ff');
    ctx.globalAlpha = 0.75;
    const lw = Math.max(1, Math.round(dpr));
    for (let i = Math.ceil(sx0 / t) * t; i <= sx1; i += t) { const x = dx + px(i); if (x >= x0 - 1 && x < x1) ctx.fillRect(x, y0, lw, y1 - y0); }
    for (let j = Math.ceil(sy0 / t) * t; j <= sy1; j += t) { const y = dy + px(j); if (y >= y0 - 1 && y < y1) ctx.fillRect(x0, y, x1 - x0, lw); }
    ctx.globalAlpha = 1;
  }
  // 테두리
  ctx.strokeStyle = 'rgba(128,128,128,0.7)';
  ctx.lineWidth = 1;
  ctx.strokeRect(dx - 0.5, dy - 0.5, dw + 1, dh + 1);
}

// 커서 위치의 브러시 발자국 윤곽. cells: [x,y,x,y,...] (문서 좌표)
export function drawCursor(ctx, cw, ch, dpr, view, cells) {
  ctx.setTransform(1, 0, 0, 1, 0, 0);
  ctx.clearRect(0, 0, cw, ch);
  if (!cells || !cells.length) return;
  const z = view.zoom * dpr;
  const dx = Math.round(view.ox * dpr), dy = Math.round(view.oy * dpr);
  const set = new Set();
  for (let i = 0; i < cells.length; i += 2) set.add(cells[i] * 65536 + cells[i + 1]);
  const has = (x, y) => set.has(x * 65536 + y);
  ctx.beginPath();
  for (let i = 0; i < cells.length; i += 2) {
    const x = cells[i], y = cells[i + 1];
    const l = dx + Math.round(x * z) + 0.5, r = dx + Math.round((x + 1) * z) - 0.5;
    const t = dy + Math.round(y * z) + 0.5, b = dy + Math.round((y + 1) * z) - 0.5;
    if (!has(x, y - 1)) { ctx.moveTo(l, t); ctx.lineTo(r + 1, t); }
    if (!has(x, y + 1)) { ctx.moveTo(l, b + 1); ctx.lineTo(r + 1, b + 1); }
    if (!has(x - 1, y)) { ctx.moveTo(l, t); ctx.lineTo(l, b + 1); }
    if (!has(x + 1, y)) { ctx.moveTo(r + 1, t); ctx.lineTo(r + 1, b + 1); }
  }
  ctx.strokeStyle = 'rgba(0,0,0,0.85)';
  ctx.lineWidth = 3;
  ctx.stroke();
  ctx.strokeStyle = 'rgba(255,255,255,0.95)';
  ctx.lineWidth = 1;
  ctx.stroke();
}

// 작은 내비게이터: 가능하면 1:1, 크면 칸에 맞춰 줄이고 현재 보이는 영역을 사각형으로 표시합니다.
export function drawNavigator(canvas, maxW, maxH, dpr, doc, view, vw, vh) {
  if (!doc) { canvas.width = canvas.height = 0; return null; }
  const s = Math.min(1, maxW / doc.w, maxH / doc.h);
  const cssW = Math.max(1, Math.round(doc.w * s)), cssH = Math.max(1, Math.round(doc.h * s));
  canvas.style.width = `${cssW}px`;
  canvas.style.height = `${cssH}px`;
  const pw = Math.round(cssW * dpr), ph = Math.round(cssH * dpr);
  if (canvas.width !== pw) canvas.width = pw;
  if (canvas.height !== ph) canvas.height = ph;
  const ctx = canvas.getContext('2d');
  ctx.setTransform(1, 0, 0, 1, 0, 0);
  ctx.clearRect(0, 0, pw, ph);
  ctx.fillStyle = checkPattern(ctx, Math.max(3, Math.round(4 * dpr)));
  ctx.fillRect(0, 0, pw, ph);
  ctx.imageSmoothingEnabled = false;
  ctx.drawImage(doc.off, 0, 0, doc.w, doc.h, 0, 0, pw, ph);
  const k = pw / doc.w;
  const vx0 = Math.max(0, -view.ox / view.zoom), vy0 = Math.max(0, -view.oy / view.zoom);
  const vx1 = Math.min(doc.w, (vw - view.ox) / view.zoom), vy1 = Math.min(doc.h, (vh - view.oy) / view.zoom);
  if (vx1 > vx0 && vy1 > vy0) {
    ctx.strokeStyle = cssVar('--accent', '#6aa7ff');
    ctx.lineWidth = Math.max(1, Math.round(dpr));
    ctx.strokeRect(vx0 * k + 0.5, vy0 * k + 0.5, Math.max(1, (vx1 - vx0) * k - 1), Math.max(1, (vy1 - vy0) * (ph / doc.h) - 1));
  }
  return { scale: pw / dpr / doc.w };
}
