// 픽셀 편집기: 상태, 화면 구성, 입력 처리, 파일 열기/저장.
// 문서는 RGBA 픽셀 버퍼 하나(doc.pix, 비곱셈)이고 doc.px32 는 같은 메모리를 uint32 로 본 것입니다.
// 그리기는 모두 "획(stroke)" 단위로 버퍼를 직접 고치면서 바뀐 픽셀의 이전 값을 기록하고,
// 획이 끝나면 History 에 패치 하나로 넣습니다. 선·도형은 드래그하는 동안 되돌렸다 다시 그리며 미리 보여줍니다.
import './editor.css';
import { LoadImageRGBA, PickFiles, SaveImageAs, SaveImageRGBA } from '../../wailsjs/go/main/App';
import { css, fromBase64, norm, pack, parseHex, toBase64, toHex, unpack } from './color.js';
import { History } from './history.js';
import { drawCursor, drawMain, drawNavigator } from './render.js';
import * as T from './tools.js';
import { el, isModalOpen, modal } from './ui.js';

const ZOOMS = [0.125, 0.25, 0.5, 1, 2, 3, 4, 5, 6, 8, 10, 12, 16, 20, 24, 32, 40, 48, 64];
const TILES = [0, 8, 16, 32, 64];
const MAX_SIDE = 8192;
const MAX_PIXELS = 4096 * 4096;
const PREF_KEY = 'sprite-tools-editor';

const ICONS = {
  pencil: '<path d="M3 13l.8-3.2L11 2.6 13.4 5 6.2 12.2z"/><path d="M9.6 4l2.4 2.4"/>',
  eraser: '<path d="M2.6 10.4l6.2-6.6 4.8 4.8-4.4 4.6H5.4z"/><path d="M6 7.6l4.6 4.6"/>',
  fill: '<path d="M2.8 8.6l5-5 4.8 4.8-5 5z"/><path d="M5.4 6l4.8 4.8"/><path d="M13 11c.9 1.3 1.3 2.1.7 2.8-.7.6-1.7.1-1.3-.9z"/>',
  picker: '<path d="M10.6 2.4l3 3-1.3 1.3-.7-.7-4.3 4.3-2.3.8.8-2.3L9.8 4l-.7-.7z"/><path d="M3.6 12.4l-.6 1.6 1.6-.6"/>',
  line: '<path d="M3.2 12.8L12.8 3.2"/><rect x="2" y="11.6" width="2.4" height="2.4"/><rect x="11.6" y="2" width="2.4" height="2.4"/>',
  rect: '<rect x="3" y="4" width="10" height="8"/>',
  ellipse: '<ellipse cx="8" cy="8" rx="5.6" ry="4.2"/>',
  symx: '<path d="M8 1.8v12.4" stroke-dasharray="1.6 1.4"/><path d="M5.4 5L2.8 8l2.6 3M10.6 5l2.6 3-2.6 3"/>',
  symy: '<path d="M1.8 8h12.4" stroke-dasharray="1.6 1.4"/><path d="M5 5.4L8 2.8l3 2.6M5 10.6l3 2.6 3-2.6"/>',
  grid: '<path d="M2.8 2.8h10.4v10.4H2.8zM2.8 8h10.4M8 2.8v10.4"/>',
  swap: '<path d="M3 6h9l-2.4-2.4M13 10H4l2.4 2.4"/>',
};
function icon(name) {
  const s = document.createElementNS('http://www.w3.org/2000/svg', 'svg');
  s.setAttribute('viewBox', '0 0 16 16');
  s.setAttribute('class', 'ed-icon');
  s.innerHTML = ICONS[name];
  return s;
}

const TOOLS = [
  { id: 'pencil', label: '연필', key: 'B' },
  { id: 'eraser', label: '지우개', key: 'E' },
  { id: 'fill', label: '채우기', key: 'G' },
  { id: 'picker', label: '스포이트', key: 'I', extra: 'Alt+클릭으로 어느 도구에서나' },
  { id: 'line', label: '선', key: 'L', extra: 'Shift: 0/45/90° 고정' },
  { id: 'rect', label: '사각형', key: 'U', extra: 'Shift: 정사각형' },
  { id: 'ellipse', label: '타원', key: 'Shift+U', extra: 'Shift: 원' },
];

const defaultPrefs = {
  tool: 'pencil', size: 1, shape: 'square', pixelPerfect: true, fillMode: 'contiguous', tolerance: 0,
  shapeFilled: false, symX: false, symY: false, grid: false, tile: 0,
  primary: pack(0, 0, 0, 255), secondary: pack(255, 255, 255, 255), recent: [],
};

function loadPrefs() {
  try {
    const p = { ...defaultPrefs, ...JSON.parse(localStorage.getItem(PREF_KEY) || '{}') };
    if (!TOOLS.some((t) => t.id === p.tool)) p.tool = 'pencil';
    p.recent = Array.isArray(p.recent) ? p.recent.filter((c) => Number.isInteger(c)).slice(0, 16) : [];
    return p;
  } catch {
    return { ...defaultPrefs };
  }
}

const clamp = (v, a, b) => Math.max(a, Math.min(b, v));
const sameFile = (a, b) => !!a && !!b && a.toLowerCase().replace(/\\/g, '/') === b.toLowerCase().replace(/\\/g, '/');
const baseName = (p) => p.split(/[\\/]/).pop();

// host: 편집기를 넣을 요소. hooks: { toast(text, cls, actions), getBackup(), setBackup(on), onSaved(path, info) -> actions[], onDirtyChange(dirty) }
export function createEditor(host, hooks) {
  const P = loadPrefs();
  const S = {
    doc: null,
    active: false,
    view: { zoom: 8, ox: 0, oy: 0 },
    autoFit: true,
    hover: null, // {x, y}
    space: false,
    drag: null,
    cur: null, // 진행 중인 획
    saving: false,
    pickerSlot: null,
  };
  const dpr = () => window.devicePixelRatio || 1;
  let savePrefsTimer = 0;
  const savePrefs = () => {
    clearTimeout(savePrefsTimer);
    savePrefsTimer = setTimeout(() => { try { localStorage.setItem(PREF_KEY, JSON.stringify(P)); } catch { /* 무시 */ } }, 200);
  };

  // ---------- 화면 구성 ----------

  const R = {}; // 자주 쓰는 요소
  const mkBtn = (cls, content, title, onclick) => {
    const b = el('button', cls);
    if (typeof content === 'string') b.textContent = content;
    else b.append(content);
    b.type = 'button';
    if (title) { b.title = title; b.setAttribute('aria-label', title); }
    b.onclick = (e) => { onclick(e); b.blur(); };
    return b;
  };
  const sep = () => el('span', 'ed-sep');

  function buildTop() {
    const top = el('div', 'ed-top');
    R.name = el('span', 'ed-name ellipsis', '');
    R.dirty = el('span', 'ed-dirty', '●');
    R.dirty.title = '저장하지 않은 변경 사항';
    R.dirty.hidden = true;
    R.open = mkBtn('btn small', '열기…', '파일 열기 (Ctrl+O)', () => openDialog());
    R.new = mkBtn('btn small', '새로 만들기', '새 이미지 (Ctrl+N)', () => newDialog());
    R.save = mkBtn('btn small primary', '저장', '저장 (Ctrl+S)', () => save(false));
    R.saveAs = mkBtn('btn small', '다른 이름으로', '다른 이름으로 저장 (Ctrl+Shift+S)', () => save(true));
    R.undo = mkBtn('btn small', '↶', '실행 취소 (Ctrl+Z)', () => undo());
    R.redo = mkBtn('btn small', '↷', '다시 실행 (Ctrl+Y / Ctrl+Shift+Z)', () => redo());
    R.zoomOut = mkBtn('btn small', '−', '축소 (-)', () => zoomStep(-1));
    R.zoomLabel = el('span', 'ed-zoom', '100%');
    R.zoomLabel.title = 'Ctrl+휠로 확대/축소';
    R.zoomIn = mkBtn('btn small', '+', '확대 (+)', () => zoomStep(1));
    R.fit = mkBtn('btn small', '맞춤', '화면에 맞춤 (0)', () => fit());
    R.one = mkBtn('btn small', '1:1', '100% (1)', () => zoomTo(1));
    R.grid = mkBtn('btn small ed-toggle', icon('grid'), '픽셀 격자 (Ctrl+\' 또는 #) — 4배 이상 확대에서 보임', () => { P.grid = !P.grid; savePrefs(); syncTop(); render(); });
    R.tile = el('select', 'ed-tile');
    R.tile.title = '타일 격자 간격 (px)';
    TILES.forEach((t) => R.tile.append(new Option(t ? `타일 ${t}` : '타일 없음', t)));
    R.tile.onchange = () => { P.tile = Number(R.tile.value); savePrefs(); render(); R.tile.blur(); };
    top.append(R.name, R.dirty, el('span', 'spacer'), R.open, R.new, sep(), R.save, R.saveAs, sep(), R.undo, R.redo, sep(),
      R.zoomOut, R.zoomLabel, R.zoomIn, R.fit, R.one, sep(), R.grid, R.tile);
    return top;
  }

  function buildTools() {
    const bar = el('div', 'ed-tools');
    R.toolBtns = {};
    for (const t of TOOLS) {
      const b = mkBtn('ed-tool', icon(t.id), `${t.label} (${t.key})${t.extra ? ` — ${t.extra}` : ''}`, () => setTool(t.id));
      R.toolBtns[t.id] = b;
      bar.append(b);
    }
    bar.append(el('span', 'ed-tools-sep'));
    R.symX = mkBtn('ed-tool', icon('symx'), '가로 대칭 그리기 — 가운데 세로축 기준 좌우 반사 (연필·지우개·선·도형)', () => { P.symX = !P.symX; savePrefs(); syncTools(); updateCursor(); });
    R.symY = mkBtn('ed-tool', icon('symy'), '세로 대칭 그리기 — 가운데 가로축 기준 위아래 반사 (연필·지우개·선·도형)', () => { P.symY = !P.symY; savePrefs(); syncTools(); updateCursor(); });
    bar.append(R.symX, R.symY);
    return bar;
  }

  function buildCenter() {
    const c = el('div', 'ed-center');
    R.view = el('div', 'ed-view');
    R.main = el('canvas', 'ed-canvas');
    R.over = el('canvas', 'ed-canvas ed-over');
    R.empty = el('div', 'ed-empty');
    R.empty.append(
      el('div', 'empty-art'),
      el('p', '', 'PNG 파일을 열거나 새 이미지를 만드세요'),
      el('p', 'muted small', '일괄 처리 목록에서 파일을 더블클릭해도 편집기에서 열립니다'),
    );
    const row = el('div', 'row');
    row.append(mkBtn('btn', '열기…', '', () => openDialog()), mkBtn('btn', '새로 만들기', '', () => newDialog()));
    R.empty.append(row);
    R.view.append(R.main, R.over, R.empty);

    const status = el('div', 'ed-status');
    R.coords = el('span', 'ed-coords', '');
    R.under = el('span', 'ed-under', '');
    R.hint = el('span', 'muted ellipsis', '');
    status.append(R.coords, R.under, el('span', 'spacer'), R.hint);
    c.append(R.view, status);
    return c;
  }

  function section(title) {
    const s = el('section', 'ed-section');
    s.append(el('h3', '', title));
    return s;
  }

  function buildSide() {
    const side = el('aside', 'ed-side');

    // 색상
    const colors = section('색상');
    const sw = el('div', 'ed-swatches');
    R.secSw = el('button', 'ed-sw ed-sw-sec');
    R.priSw = el('button', 'ed-sw ed-sw-pri');
    for (const [b, slot, tip] of [[R.secSw, 'secondary', '보조 색 (오른쪽 버튼)'], [R.priSw, 'primary', '주 색 (왼쪽 버튼)']]) {
      b.type = 'button';
      b.title = `${tip} — 눌러서 색 고르기`;
      b.append(el('i'));
      b.onclick = () => { openPicker(slot); b.blur(); };
    }
    R.swap = mkBtn('ed-swap', icon('swap'), '주 색 ↔ 보조 색 (X)', () => swapColors());
    sw.append(R.secSw, R.priSw, R.swap);
    R.recent = el('div', 'ed-recent');
    const rl = el('div', 'ed-label muted small', '최근 색');
    rl.title = '클릭: 주 색 · 오른쪽 클릭: 보조 색';
    colors.append(sw, rl, R.recent);

    // 도구 옵션
    R.optSec = section('도구 옵션');
    R.opts = el('div', 'ed-opts');
    R.optSec.append(R.opts);

    // 팔레트 자리 (A2)
    const pal = section('팔레트');
    pal.append(el('p', 'muted small', '다음 단계에서 추가됩니다'));
    R.palette = pal;

    // 내비게이터
    const nav = section('내비게이터');
    R.nav = el('canvas', 'ed-nav');
    R.navBox = el('div', 'ed-nav-box');
    R.navBox.append(R.nav);
    nav.append(R.navBox);

    // 이미지 정보
    const info = section('이미지 정보');
    R.info = el('dl', 'ed-info');
    R.backup = el('input');
    R.backup.type = 'checkbox';
    R.backup.onchange = () => hooks.setBackup(R.backup.checked);
    const bl = el('label', 'check small');
    bl.title = '기존 파일을 덮어쓰기 전에 원본을 같은 폴더의 .sprite-tools-backup 에 복사해 둡니다';
    bl.append(R.backup, ' 덮어쓰기 전에 원본 백업');
    info.append(R.info, bl);

    side.append(colors, R.optSec, pal, nav, info);
    return side;
  }

  host.classList.add('editor');
  const top = buildTop();
  const body = el('div', 'ed-body');
  body.append(buildTools(), buildCenter(), buildSide());
  host.append(top, body);
  buildPicker();

  // ---------- 상태 → 화면 ----------

  function syncTop() {
    const d = S.doc;
    R.name.textContent = d ? d.name : '열린 파일 없음';
    R.name.title = d?.path || '';
    R.dirty.hidden = !(d && d.history.dirty);
    for (const b of [R.save, R.saveAs, R.zoomOut, R.zoomIn, R.fit, R.one, R.grid, R.tile]) b.disabled = !d;
    R.undo.disabled = !d || !d.history.canUndo;
    R.redo.disabled = !d || !d.history.canRedo;
    R.grid.classList.toggle('on', P.grid);
    R.tile.value = String(P.tile);
    R.zoomLabel.textContent = `${Math.round(S.view.zoom * 100)}%`;
    R.empty.hidden = !!d;
    R.backup.checked = !!hooks.getBackup();
  }

  function syncTools() {
    for (const t of TOOLS) R.toolBtns[t.id].classList.toggle('on', P.tool === t.id);
    R.symX.classList.toggle('on', P.symX);
    R.symY.classList.toggle('on', P.symY);
    const hints = {
      pencil: '왼쪽: 주 색 · 오른쪽: 보조 색 · Alt+클릭: 스포이트',
      eraser: '왼쪽·오른쪽 버튼 모두 지웁니다',
      fill: '클릭한 곳의 같은 색을 주 색(오른쪽: 보조 색)으로 채웁니다 · Alt+클릭: 스포이트',
      picker: '왼쪽: 주 색으로 · 오른쪽: 보조 색으로',
      line: '드래그 · Shift: 0/45/90° 고정 · Esc: 취소',
      rect: '드래그 · Shift: 정사각형 · Esc: 취소',
      ellipse: '드래그 · Shift: 원 · Esc: 취소',
    };
    R.hint.textContent = hints[P.tool] || '';
    renderOptions();
  }

  function renderSwatch(btn, c) {
    btn.firstChild.style.setProperty('--c', css(c));
    btn.title = btn.title.replace(/ \[.*\]$/, '') + ` [${toHex(c)}]`;
  }

  function syncColors() {
    renderSwatch(R.priSw, P.primary);
    renderSwatch(R.secSw, P.secondary);
    R.recent.replaceChildren(...P.recent.map((c) => {
      const b = el('button', 'ed-recent-sw');
      b.type = 'button';
      b.style.setProperty('--c', css(c));
      b.title = `${toHex(c)} — 클릭: 주 색, 오른쪽 클릭: 보조 색`;
      b.onclick = () => { setColor('primary', c); b.blur(); };
      b.oncontextmenu = (e) => { e.preventDefault(); setColor('secondary', c); };
      return b;
    }));
    syncPicker();
  }

  function setColor(slot, c) {
    P[slot] = c >>> 0;
    savePrefs();
    syncColors();
  }
  function swapColors() {
    [P.primary, P.secondary] = [P.secondary, P.primary];
    savePrefs();
    syncColors();
  }
  function pushRecent(c) {
    if (c >>> 24 === 0) return;
    P.recent = [c, ...P.recent.filter((x) => x !== c)].slice(0, 16);
    savePrefs();
    syncColors();
  }

  function optRow(label, content) {
    const r = el('div', 'ed-opt');
    r.append(el('span', 'ed-opt-label', label), content);
    return r;
  }
  function segment(items, current, onpick) {
    const seg = el('div', 'seg');
    for (const [v, label, tip] of items) {
      const b = el('button', current === v ? 'on' : '', label);
      b.type = 'button';
      if (tip) b.title = tip;
      b.onclick = () => { onpick(v); b.blur(); };
      seg.append(b);
    }
    return seg;
  }
  function checkbox(label, checked, onchange, tip) {
    const l = el('label', 'check small');
    const i = el('input');
    i.type = 'checkbox';
    i.checked = checked;
    i.onchange = () => { onchange(i.checked); i.blur(); };
    if (tip) l.title = tip;
    l.append(i, ` ${label}`);
    return l;
  }
  function sizeSlider() {
    const wrap = el('div', 'ed-range');
    const r = el('input');
    r.type = 'range'; r.min = 1; r.max = 16; r.step = 1; r.value = P.size;
    const v = el('span', 'value', `${P.size}px`);
    r.oninput = () => { setSize(Number(r.value)); v.textContent = `${P.size}px`; };
    wrap.append(r, v);
    return wrap;
  }

  function renderOptions() {
    const o = R.opts;
    o.replaceChildren();
    const t = P.tool;
    const brush = t === 'pencil' || t === 'eraser' || t === 'line' || ((t === 'rect' || t === 'ellipse') && !P.shapeFilled);
    if (brush) {
      o.append(optRow(t === 'rect' || t === 'ellipse' || t === 'line' ? '선 굵기 ([ ])' : '크기 ([ ])', sizeSlider()));
      o.append(optRow('모양', segment([['square', '사각'], ['round', '원형']], P.shape, (v) => { P.shape = v; savePrefs(); renderOptions(); updateCursor(); })));
    }
    if (t === 'pencil' || t === 'eraser') {
      o.append(checkbox('픽셀 퍼펙트 (크기 1)', P.pixelPerfect, (v) => { P.pixelPerfect = v; savePrefs(); },
        '1px 선에서 L 자 모서리 픽셀을 없애 깔끔한 선으로 만듭니다'));
    }
    if (t === 'fill') {
      o.append(optRow('채우기', segment([['contiguous', '이어진 영역'], ['global', '전체 같은 색']], P.fillMode, (v) => { P.fillMode = v; savePrefs(); renderOptions(); })));
      const tol = el('div', 'ed-range');
      const r = el('input');
      r.type = 'range'; r.min = 0; r.max = 128; r.step = 1; r.value = P.tolerance;
      const v = el('span', 'value', String(P.tolerance));
      r.oninput = () => { P.tolerance = Number(r.value); v.textContent = r.value; savePrefs(); };
      tol.append(r, v);
      o.append(optRow('허용 오차 (0 = 정확히 같은 색)', tol));
    }
    if (t === 'rect' || t === 'ellipse') {
      o.append(optRow('도형', segment([[false, '윤곽선'], [true, '채움']], P.shapeFilled, (v) => { P.shapeFilled = v; savePrefs(); renderOptions(); })));
    }
    if (t === 'picker') o.append(el('p', 'muted small', '클릭한 픽셀의 색(알파 포함)을 가져옵니다.'));
    R.optSec.hidden = false;
  }

  function setTool(id) {
    if (S.cur) return;
    P.tool = id;
    savePrefs();
    syncTools();
    updateCursor();
  }
  function setSize(n) {
    P.size = clamp(Math.round(n), 1, 16);
    savePrefs();
    updateCursor();
  }

  // ---------- 색 선택 팝오버 ----------

  function buildPicker() {
    const pop = el('div', 'ed-picker');
    pop.hidden = true;
    R.pop = pop;
    R.popPrev = el('div', 'ed-picker-prev');
    R.popPrev.append(el('i'));
    R.hex = el('input');
    R.hex.type = 'text';
    R.hex.spellcheck = false;
    R.hex.placeholder = '#rrggbb 또는 #rrggbbaa';
    R.hex.oninput = () => {
      const c = parseHex(R.hex.value);
      R.hex.classList.toggle('bad', c == null);
      if (c != null && S.pickerSlot) { P[S.pickerSlot] = c; savePrefs(); syncColors(true); }
    };
    R.hex.onblur = () => { R.hex.classList.remove('bad'); syncPicker(); };
    R.hex.onkeydown = (e) => { if (e.key === 'Enter') { R.hex.blur(); closePicker(); } };
    const head = el('div', 'row');
    head.append(R.popPrev, R.hex);
    pop.append(head);
    R.sl = {};
    for (const [k, label] of [['r', 'R'], ['g', 'G'], ['b', 'B'], ['a', '알파']]) {
      const row = el('div', 'ed-ch');
      const r = el('input'); r.type = 'range'; r.min = 0; r.max = 255; r.step = 1;
      const n = el('input'); n.type = 'number'; n.min = 0; n.max = 255;
      const upd = (v) => {
        if (!S.pickerSlot || !Number.isFinite(v)) return;
        const u = unpack(P[S.pickerSlot]);
        u[k] = clamp(Math.round(v), 0, 255);
        P[S.pickerSlot] = pack(u.r, u.g, u.b, u.a);
        savePrefs();
        syncColors(true);
      };
      r.oninput = () => upd(Number(r.value));
      n.oninput = () => { if (n.value !== '') upd(Number(n.value)); };
      n.onblur = () => syncPicker();
      R.sl[k] = [r, n];
      row.append(el('span', 'ed-ch-label', label), r, n);
      pop.append(row);
    }
    const foot = el('div', 'row');
    R.sys = el('input');
    R.sys.type = 'color';
    R.sys.title = '시스템 색 선택기';
    R.sys.oninput = () => {
      const c = parseHex(R.sys.value);
      if (c == null || !S.pickerSlot) return;
      const a = unpack(P[S.pickerSlot]).a;
      P[S.pickerSlot] = (c & 0x00ffffff | (a << 24)) >>> 0;
      savePrefs();
      syncColors();
    };
    const clear = mkBtn('btn small', '투명', '완전 투명한 색', () => { if (S.pickerSlot) setColor(S.pickerSlot, 0); });
    const close = mkBtn('btn small', '닫기', '', () => closePicker());
    foot.append(R.sys, clear, el('span', 'spacer'), close);
    pop.append(foot);
    host.append(pop);
    document.addEventListener('pointerdown', (e) => {
      if (!pop.hidden && !pop.contains(e.target) && !e.target.closest('.ed-sw')) closePicker();
    }, true);
  }

  function openPicker(slot) {
    S.pickerSlot = slot;
    R.pop.hidden = false;
    const btn = slot === 'primary' ? R.priSw : R.secSw;
    const b = btn.getBoundingClientRect(), h = host.getBoundingClientRect();
    const w = R.pop.offsetWidth || 230;
    R.pop.style.left = `${Math.max(8, b.left - h.left - w - 10)}px`;
    R.pop.style.top = `${clamp(b.top - h.top, 4, Math.max(4, h.height - R.pop.offsetHeight - 4))}px`;
    R.priSw.classList.toggle('sel', slot === 'primary');
    R.secSw.classList.toggle('sel', slot === 'secondary');
    syncPicker();
    R.hex.focus();
    R.hex.select();
  }
  function closePicker() {
    S.pickerSlot = null;
    R.pop.hidden = true;
    R.priSw.classList.remove('sel');
    R.secSw.classList.remove('sel');
  }
  function syncPicker() {
    if (!S.pickerSlot) return;
    const c = P[S.pickerSlot];
    const u = unpack(c);
    R.popPrev.firstChild.style.setProperty('--c', css(c));
    if (document.activeElement !== R.hex) R.hex.value = toHex(c);
    for (const k of ['r', 'g', 'b', 'a']) {
      const [r, n] = R.sl[k];
      r.value = u[k];
      if (document.activeElement !== n) n.value = u[k];
    }
    R.sys.value = `#${[u.r, u.g, u.b].map((v) => v.toString(16).padStart(2, '0')).join('')}`;
  }

  // ---------- 문서 ----------

  function makeDoc(w, h, pix, path, name, meta = {}) {
    const off = document.createElement('canvas');
    off.width = w;
    off.height = h;
    const octx = off.getContext('2d');
    const imageData = new ImageData(pix, w, h);
    octx.putImageData(imageData, 0, 0);
    return {
      w, h, pix, px32: new Uint32Array(pix.buffer), off, octx, imageData, mark: new Uint8Array(w * h),
      path, name, indexed: !!meta.indexed, palSize: meta.colors || 0,
      history: new History(), dirtyRect: null, colorCount: null,
    };
  }

  function setDoc(doc) {
    cancelStroke();
    S.doc = doc;
    S.autoFit = true;
    S.hover = null;
    afterChange();
    fit();
  }

  let lastDirty = false;
  function afterChange() {
    syncTop();
    const dirty = !!(S.doc && S.doc.history.dirty);
    if (dirty !== lastDirty) { lastDirty = dirty; hooks.onDirtyChange?.(dirty); }
    scheduleInfo();
    render();
  }

  let infoTimer = 0;
  function scheduleInfo() {
    renderInfo(false);
    clearTimeout(infoTimer);
    infoTimer = setTimeout(() => renderInfo(true), 250);
  }
  function renderInfo(count) {
    const d = S.doc;
    R.info.replaceChildren();
    if (!d) return;
    if (count) {
      const set = new Set();
      let trans = false;
      for (let i = 0; i < d.px32.length; i++) {
        const c = d.px32[i];
        if (c >>> 24 === 0) trans = true;
        else set.add(c);
      }
      d.colorCount = { n: set.size, trans };
    }
    const rows = [
      ['크기', `${d.w} × ${d.h}`],
      ['색상', d.colorCount ? `${d.colorCount.n}색${d.colorCount.trans ? ' + 투명' : ''}` : '…'],
      ['원본 형식', d.path ? (d.indexed ? `인덱스 PNG (${d.palSize}색)` : 'RGBA PNG') : '새 이미지'],
    ];
    for (const [k, v] of rows) R.info.append(el('dt', '', k), el('dd', '', v));
  }

  // ---------- 보기 (확대·이동) ----------

  let viewW = 0, viewH = 0;
  function measure() {
    viewW = R.view.clientWidth;
    viewH = R.view.clientHeight;
    const r = dpr();
    for (const c of [R.main, R.over]) {
      const pw = Math.max(1, Math.round(viewW * r)), ph = Math.max(1, Math.round(viewH * r));
      if (c.width !== pw) c.width = pw;
      if (c.height !== ph) c.height = ph;
    }
  }

  function clampPan() {
    const d = S.doc;
    if (!d) return;
    const v = S.view, m = 48;
    v.ox = clamp(v.ox, m - d.w * v.zoom, viewW - m);
    v.oy = clamp(v.oy, m - d.h * v.zoom, viewH - m);
  }

  function zoomTo(z, cx = viewW / 2, cy = viewH / 2) {
    const d = S.doc;
    if (!d) return;
    S.autoFit = false;
    z = clamp(z, ZOOMS[0], ZOOMS[ZOOMS.length - 1]);
    const v = S.view;
    const k = z / v.zoom;
    v.ox = cx - (cx - v.ox) * k;
    v.oy = cy - (cy - v.oy) * k;
    v.zoom = z;
    clampPan();
    syncTop();
    render();
  }

  function zoomStep(dir, cx, cy) {
    const z = S.view.zoom;
    let i;
    if (dir > 0) { i = ZOOMS.findIndex((v) => v > z + 1e-9); if (i < 0) i = ZOOMS.length - 1; }
    else { i = ZOOMS.findLastIndex((v) => v < z - 1e-9); if (i < 0) i = 0; }
    zoomTo(ZOOMS[i], cx, cy);
  }

  function fit() {
    const d = S.doc;
    if (!d) return;
    measure();
    const want = Math.min((viewW - 48) / d.w, (viewH - 48) / d.h);
    let z = ZOOMS[0];
    for (const v of ZOOMS) if (v <= want + 1e-9) z = v;
    const v = S.view;
    v.zoom = z;
    v.ox = (viewW - d.w * z) / 2;
    v.oy = (viewH - d.h * z) / 2;
    S.autoFit = true;
    syncTop();
    render();
  }

  // ---------- 그리기 ----------

  let rafId = 0;
  function render() {
    if (rafId) return;
    rafId = requestAnimationFrame(() => {
      rafId = 0;
      doRender();
    });
  }

  function flushDoc() {
    const d = S.doc;
    if (!d || !d.dirtyRect) return;
    const { x0, y0, x1, y1 } = d.dirtyRect;
    d.octx.putImageData(d.imageData, 0, 0, x0, y0, x1 - x0 + 1, y1 - y0 + 1);
    d.dirtyRect = null;
  }

  function doRender() {
    if (!S.active) return;
    if (!viewW) measure();
    flushDoc();
    const r = dpr();
    drawMain(R.main.getContext('2d'), R.main.width, R.main.height, r, S.doc, S.view, { grid: P.grid, tile: P.tile });
    drawNavigator(R.nav, 196, 80, r, S.doc, S.view, viewW, viewH);
    drawOverlay();
  }

  function drawOverlay() {
    drawCursor(R.over.getContext('2d'), R.over.width, R.over.height, dpr(), S.view, cursorCells());
  }
  function updateCursor() {
    if (S.active) drawOverlay();
  }

  const symmetric = () => P.symX || P.symY;

  // 커서가 있는 곳에서 칠해질 칸들 (대칭 포함, 문서 안쪽만)
  function cursorCells() {
    const d = S.doc, h = S.hover;
    if (!d || !h || S.space || (S.drag && S.drag.kind === 'pan')) return null;
    let base;
    const t = P.tool;
    if (t === 'pencil' || t === 'eraser' || t === 'line' || ((t === 'rect' || t === 'ellipse') && !P.shapeFilled)) {
      const fp = T.footprint(P.size, P.shape);
      base = [];
      for (let i = 0; i < fp.length; i += 2) base.push(h.x + fp[i], h.y + fp[i + 1]);
    } else {
      base = [h.x, h.y];
    }
    const out = [];
    const add = (x, y) => { if (x >= 0 && y >= 0 && x < d.w && y < d.h) out.push(x, y); };
    const sym = t === 'fill' || t === 'picker' ? false : symmetric();
    for (let i = 0; i < base.length; i += 2) {
      const x = base[i], y = base[i + 1];
      add(x, y);
      if (sym && P.symX) add(d.w - 1 - x, y);
      if (sym && P.symY) add(x, d.h - 1 - y);
      if (sym && P.symX && P.symY) add(d.w - 1 - x, d.h - 1 - y);
    }
    return out;
  }

  function toDoc(e) {
    const r = R.view.getBoundingClientRect();
    const v = S.view;
    return {
      x: Math.floor((e.clientX - r.left - v.ox) / v.zoom),
      y: Math.floor((e.clientY - r.top - v.oy) / v.zoom),
    };
  }

  // --- 획: 픽셀을 직접 쓰면서 이전 값을 기록 ---

  function newStroke(tool, color) {
    return { tool, color, idx: [], before: [], kept: [], last: null, sx: 0, sy: 0, ex: 0, ey: 0 };
  }

  function dirtyAdd(d, x, y) {
    const r = d.dirtyRect;
    if (!r) d.dirtyRect = { x0: x, y0: y, x1: x, y1: y };
    else {
      if (x < r.x0) r.x0 = x; else if (x > r.x1) r.x1 = x;
      if (y < r.y0) r.y0 = y; else if (y > r.y1) r.y1 = y;
    }
  }

  function setIdx(i, c) {
    const d = S.doc, p = d.px32;
    if (p[i] === c) return;
    if (!d.mark[i]) {
      d.mark[i] = 1;
      S.cur.idx.push(i);
      S.cur.before.push(p[i]);
    }
    p[i] = c;
    dirtyAdd(d, i % d.w, (i / d.w) | 0);
  }

  function setPx(x, y, c) {
    const d = S.doc;
    if (x < 0 || y < 0 || x >= d.w || y >= d.h) return;
    setIdx(y * d.w + x, c);
  }

  // 대칭을 적용해 한 칸을 칠합니다.
  function putCell(x, y, c) {
    const d = S.doc;
    setPx(x, y, c);
    if (P.symX) setPx(d.w - 1 - x, y, c);
    if (P.symY) setPx(x, d.h - 1 - y, c);
    if (P.symX && P.symY) setPx(d.w - 1 - x, d.h - 1 - y, c);
  }

  function stamp(x, y, c) {
    const fp = T.footprint(P.size, P.shape);
    for (let i = 0; i < fp.length; i += 2) putCell(x + fp[i], y + fp[i + 1], c);
  }

  function revertStroke() {
    const d = S.doc, s = S.cur;
    for (let k = s.idx.length - 1; k >= 0; k--) {
      const i = s.idx[k];
      d.px32[i] = s.before[k];
      d.mark[i] = 0;
      dirtyAdd(d, i % d.w, (i / d.w) | 0);
    }
    s.idx.length = 0;
    s.before.length = 0;
  }

  function cancelStroke() {
    if (S.cur && S.doc) { revertStroke(); render(); }
    S.cur = null;
    if (S.drag && S.drag.kind !== 'pan') S.drag = null;
  }

  function endStroke() {
    const d = S.doc, s = S.cur;
    S.cur = null;
    if (!d || !s) return;
    const n = s.idx.length;
    const idx = new Uint32Array(n), before = new Uint32Array(n), after = new Uint32Array(n);
    let m = 0;
    for (let k = 0; k < n; k++) {
      const i = s.idx[k];
      d.mark[i] = 0;
      const a = d.px32[i];
      if (a === s.before[k]) continue;
      idx[m] = i; before[m] = s.before[k]; after[m] = a;
      m++;
    }
    if (m) {
      d.history.push({ idx: idx.slice(0, m), before: before.slice(0, m), after: after.slice(0, m) });
      if (s.tool !== 'eraser') pushRecent(s.color);
    }
    afterChange();
  }

  function repaintFreehand() {
    revertStroke();
    const s = S.cur;
    for (let i = 0; i < s.kept.length; i += 2) stamp(s.kept[i], s.kept[i + 1], s.color);
  }

  function freehandPoint(x, y) {
    const s = S.cur;
    if (P.pixelPerfect && P.size === 1) {
      if (T.pushPixelPerfect(s.kept, x, y)) repaintFreehand();
      else if (s.kept.length && s.kept[s.kept.length - 2] === x && s.kept[s.kept.length - 1] === y) stamp(x, y, s.color);
    } else {
      stamp(x, y, s.color);
    }
  }

  function freehandTo(x, y) {
    const s = S.cur;
    const lim = (v, hi) => clamp(v, -64, hi + 64);
    x = lim(x, S.doc.w); y = lim(y, S.doc.h);
    if (!s.last) { freehandPoint(x, y); }
    else {
      const pts = T.linePoints(s.last[0], s.last[1], x, y);
      for (let i = 2; i < pts.length; i += 2) freehandPoint(pts[i], pts[i + 1]);
      if (pts.length === 2) freehandPoint(x, y);
    }
    s.last = [x, y];
  }

  function drawShape(x, y, shift) {
    const s = S.cur, d = S.doc;
    revertStroke();
    const lim = (v, hi) => clamp(v, -64, hi + 64);
    let ex = lim(x, d.w), ey = lim(y, d.h);
    const { sx, sy } = s;
    if (s.tool === 'line') {
      if (shift) [ex, ey] = T.snapLine(sx, sy, ex, ey);
      const pts = T.linePoints(sx, sy, ex, ey);
      for (let i = 0; i < pts.length; i += 2) stamp(pts[i], pts[i + 1], s.color);
    } else {
      if (shift) [ex, ey] = T.squareEnd(sx, sy, ex, ey);
      const fn = s.tool === 'rect' ? (P.shapeFilled ? T.rectFill : T.rectOutline) : (P.shapeFilled ? T.ellipseFill : T.ellipseOutline);
      const emit = P.shapeFilled ? (px, py) => putCell(px, py, s.color) : (px, py) => stamp(px, py, s.color);
      fn(sx, sy, ex, ey, emit);
    }
    s.ex = ex; s.ey = ey;
    render();
    updateCursor();
  }

  function doFill(x, y, c) {
    const d = S.doc;
    if (x < 0 || y < 0 || x >= d.w || y >= d.h) return;
    if (norm(d.px32[y * d.w + x]) === norm(c) && P.tolerance === 0) return;
    const list = P.fillMode === 'global'
      ? T.floodGlobal(d.px32, x, y, d.w, P.tolerance)
      : T.floodContiguous(d.px32, d.w, d.h, x, y, P.tolerance);
    S.cur = newStroke('fill', c);
    for (const i of list) setIdx(i, c);
    endStroke();
  }

  function pick(x, y, button) {
    const d = S.doc;
    if (x < 0 || y < 0 || x >= d.w || y >= d.h) return;
    setColor(button === 2 ? 'secondary' : 'primary', norm(d.px32[y * d.w + x]));
  }

  // ---------- 포인터 입력 ----------

  function onPointerDown(e) {
    if (!S.doc) return;
    closeOnCanvas();
    const pan = e.button === 1 || (e.button === 0 && S.space);
    if (pan) {
      e.preventDefault();
      S.drag = { kind: 'pan', id: e.pointerId, sx: e.clientX, sy: e.clientY, ox: S.view.ox, oy: S.view.oy };
      capture(e);
      R.view.classList.add('panning');
      updateCursor();
      return;
    }
    if (e.button !== 0 && e.button !== 2) return;
    e.preventDefault();
    if (S.cur) return;
    const { x, y } = toDoc(e);
    S.hover = { x, y };
    const color = e.button === 2 ? P.secondary : P.primary;
    let tool = P.tool;
    if (e.altKey && tool !== 'picker') tool = 'picker';
    if (tool === 'picker') {
      S.drag = { kind: 'picker', id: e.pointerId, button: e.button };
      capture(e);
      pick(x, y, e.button);
      return;
    }
    if (tool === 'fill') { doFill(x, y, color); return; }
    capture(e);
    if (tool === 'pencil' || tool === 'eraser') {
      S.cur = newStroke(tool, tool === 'eraser' ? 0 : color);
      S.drag = { kind: 'free', id: e.pointerId };
      freehandTo(x, y);
    } else {
      S.cur = newStroke(tool, color);
      S.cur.sx = x; S.cur.sy = y;
      S.drag = { kind: 'shape', id: e.pointerId, x, y };
      drawShape(x, y, e.shiftKey);
    }
    render();
  }

  function capture(e) {
    try { R.view.setPointerCapture(e.pointerId); } catch { /* 합성 이벤트 등은 무시 */ }
  }
  function closeOnCanvas() {
    if (document.activeElement && document.activeElement !== document.body && document.activeElement.tagName !== 'CANVAS') document.activeElement.blur();
  }

  function onPointerMove(e) {
    const d = S.doc;
    if (!d) return;
    const dr = S.drag;
    if (dr && dr.kind === 'pan') {
      S.autoFit = false;
      S.view.ox = dr.ox + (e.clientX - dr.sx);
      S.view.oy = dr.oy + (e.clientY - dr.sy);
      clampPan();
      render();
      return;
    }
    const { x, y } = toDoc(e);
    S.hover = { x, y };
    showProbe(x, y);
    if (dr) {
      if (dr.kind === 'free') { freehandTo(x, y); render(); }
      else if (dr.kind === 'shape') { dr.x = x; dr.y = y; drawShape(x, y, e.shiftKey); }
      else if (dr.kind === 'picker') pick(x, y, dr.button);
    }
    updateCursor();
  }

  function onPointerUp(e) {
    const dr = S.drag;
    if (!dr) return;
    S.drag = null;
    R.view.classList.remove('panning');
    try { R.view.releasePointerCapture(e.pointerId); } catch { /* 무시 */ }
    if (dr.kind === 'free' || dr.kind === 'shape') endStroke();
    updateCursor();
  }

  function showProbe(x, y) {
    const d = S.doc;
    R.coords.textContent = `${x}, ${y}`;
    R.under.replaceChildren();
    if (!d || x < 0 || y < 0 || x >= d.w || y >= d.h) return;
    const c = d.px32[y * d.w + x];
    const sw = el('i', 'probe-sw');
    sw.style.setProperty('--c', css(c));
    R.under.append(sw, `${toHex(norm(c) === 0 ? 0 : c)}${(c >>> 24) === 255 ? '  α255' : `  α${c >>> 24}`}`);
  }

  let wheelAcc = 0;
  function onWheel(e) {
    if (!S.doc) return;
    e.preventDefault();
    const r = R.view.getBoundingClientRect();
    if (e.ctrlKey) {
      wheelAcc += e.deltaY;
      if (Math.abs(wheelAcc) < 40) return;
      const dir = wheelAcc < 0 ? 1 : -1;
      wheelAcc = 0;
      zoomStep(dir, e.clientX - r.left, e.clientY - r.top);
      return;
    }
    S.autoFit = false;
    const scale = e.deltaMode === 1 ? 32 : 1;
    const dx = (e.shiftKey ? e.deltaY : e.deltaX) * scale, dy = e.shiftKey ? 0 : e.deltaY * scale;
    S.view.ox -= dx;
    S.view.oy -= dy;
    clampPan();
    render();
  }

  // 내비게이터: 누르거나 끌면 그 위치가 화면 가운데로 오도록 이동
  function navMove(e) {
    const d = S.doc;
    if (!d) return;
    const r = R.nav.getBoundingClientRect();
    const px = ((e.clientX - r.left) / r.width) * d.w, py = ((e.clientY - r.top) / r.height) * d.h;
    S.autoFit = false;
    S.view.ox = viewW / 2 - px * S.view.zoom;
    S.view.oy = viewH / 2 - py * S.view.zoom;
    clampPan();
    render();
  }

  R.view.addEventListener('pointerdown', onPointerDown);
  R.view.addEventListener('pointermove', onPointerMove);
  R.view.addEventListener('pointerup', onPointerUp);
  R.view.addEventListener('pointercancel', onPointerUp);
  R.view.addEventListener('pointerleave', () => {
    if (S.drag) return;
    S.hover = null;
    R.coords.textContent = '';
    R.under.replaceChildren();
    updateCursor();
  });
  R.view.addEventListener('contextmenu', (e) => e.preventDefault());
  R.view.addEventListener('wheel', onWheel, { passive: false });
  R.view.addEventListener('auxclick', (e) => e.preventDefault());
  let navDrag = false;
  R.navBox.addEventListener('pointerdown', (e) => { navDrag = true; try { R.navBox.setPointerCapture(e.pointerId); } catch { /* 무시 */ } navMove(e); });
  R.navBox.addEventListener('pointermove', (e) => { if (navDrag) navMove(e); });
  R.navBox.addEventListener('pointerup', () => { navDrag = false; });

  new ResizeObserver(() => {
    measure();
    if (S.doc && S.autoFit) fit();
    else { clampPan(); render(); }
  }).observe(R.view);
  window.matchMedia('(prefers-color-scheme: dark)').addEventListener('change', render);

  // ---------- 실행 취소 ----------

  function applyPatch(p) {
    if (!p) return;
    const d = S.doc;
    let x0 = d.w, y0 = d.h, x1 = 0, y1 = 0;
    for (let k = 0; k < p.idx.length; k++) {
      const i = p.idx[k], x = i % d.w, y = (i / d.w) | 0;
      if (x < x0) x0 = x; if (x > x1) x1 = x; if (y < y0) y0 = y; if (y > y1) y1 = y;
    }
    dirtyAdd(d, x0, y0);
    dirtyAdd(d, x1, y1);
    afterChange();
  }
  function undo() { if (S.doc && !S.cur) applyPatch(S.doc.history.undo(S.doc.px32)); }
  function redo() { if (S.doc && !S.cur) applyPatch(S.doc.history.redo(S.doc.px32)); }

  // ---------- 파일 ----------

  async function confirmDiscard() {
    const d = S.doc;
    if (!d || !d.history.dirty) return true;
    const r = await modal({
      title: '저장하지 않은 변경 사항이 있습니다',
      build: (b) => b.append(el('p', '', `'${d.name}' 의 변경 사항을 저장할까요?`)),
      buttons: [{ label: '취소', value: 'cancel' }, { label: '저장 안 함', value: 'discard' }, { label: '저장', value: 'save', primary: true }],
    });
    if (r === 'discard') return true;
    if (r === 'save') return save(false);
    return false;
  }

  async function open(path) {
    if (S.cur) return false;
    const d = S.doc;
    if (d && sameFile(d.path, path) && d.history.dirty) return true; // 이미 열려 있고 고친 상태 — 그대로 둠
    if (!(d && sameFile(d.path, path)) && !(await confirmDiscard())) return false;
    try {
      const r = await LoadImageRGBA(path);
      const pix = fromBase64(r.pixels, new Uint8ClampedArray(r.width * r.height * 4));
      setDoc(makeDoc(r.width, r.height, pix, r.path, baseName(r.path), { indexed: r.indexed, colors: r.colors }));
      return true;
    } catch (err) {
      hooks.toast(String(err), 'failed');
      return false;
    }
  }

  async function openDialog() {
    if (S.cur) return;
    const paths = await PickFiles();
    if (paths && paths.length) await open(paths[0]);
  }

  async function newDialog() {
    if (S.cur) return;
    let wIn, hIn;
    const r = await modal({
      title: '새 이미지',
      build: (b) => {
        wIn = el('input'); wIn.type = 'number'; wIn.min = 1; wIn.max = MAX_SIDE; wIn.value = 32;
        hIn = el('input'); hIn.type = 'number'; hIn.min = 1; hIn.max = MAX_SIDE; hIn.value = 32;
        const row = el('div', 'ed-newrow');
        const f = (label, input) => { const l = el('label', 'ed-newfield'); l.append(el('span', 'field-label', label), input); return l; };
        row.append(f('너비 (px)', wIn), el('span', 'muted', '×'), f('높이 (px)', hIn));
        const presets = el('div', 'chips');
        for (const n of [16, 32, 64, 128, 256]) {
          const c = el('button', 'chip', `${n}×${n}`);
          c.type = 'button';
          c.onclick = () => { wIn.value = n; hIn.value = n; };
          presets.append(c);
        }
        b.append(row, presets, el('p', 'muted small', '배경은 투명으로 시작합니다.'));
      },
      buttons: [
        { label: '취소', value: null },
        {
          label: '만들기', value: 'ok', primary: true,
          validate: () => {
            const w = Number(wIn.value), h = Number(hIn.value);
            const ok = Number.isInteger(w) && Number.isInteger(h) && w >= 1 && h >= 1 && w <= MAX_SIDE && h <= MAX_SIDE && w * h <= MAX_PIXELS;
            if (!ok) hooks.toast(`크기는 1~${MAX_SIDE}px, 전체 ${MAX_PIXELS / 1e6 | 0}백만 픽셀 이하여야 합니다`, 'failed');
            return ok;
          },
        },
      ],
    });
    if (r !== 'ok') return;
    if (!(await confirmDiscard())) return;
    const w = Number(wIn.value), h = Number(hIn.value);
    setDoc(makeDoc(w, h, new Uint8ClampedArray(w * h * 4), '', '제목 없음.png'));
  }

  // 저장에 성공하면 true
  async function save(as) {
    const d = S.doc;
    if (!d || S.saving || S.cur) return false;
    S.saving = true;
    try {
      const pixels = toBase64(d.pix);
      const backup = !!hooks.getBackup();
      let res;
      if (!as && d.path) res = await SaveImageRGBA(d.path, d.w, d.h, pixels, backup);
      else res = await SaveImageAs(d.path, d.path ? baseName(d.path) : '제목 없음.png', d.w, d.h, pixels, backup);
      if (res.canceled) return false;
      const isNewPath = !sameFile(d.path, res.path);
      d.path = res.path;
      d.name = baseName(res.path);
      d.indexed = res.format === 'indexed';
      d.palSize = res.colors || 0;
      d.history.markSaved();
      afterChange();
      const fmt = res.format === 'indexed' ? `인덱스 PNG ${res.colors}색` : 'RGBA PNG';
      const actions = hooks.onSaved(res.path, { isNewPath, backup: res.backup }) || [];
      hooks.toast(`${d.name} 저장됨 · ${fmt}${res.backup ? ' · 원본 백업됨' : ''}`, '', actions);
      return true;
    } catch (err) {
      hooks.toast(`저장 실패: ${err}`, 'failed');
      return false;
    } finally {
      S.saving = false;
    }
  }

  // ---------- 키보드 ----------

  function onKeyDown(e) {
    if (!S.active || isModalOpen() || hooks.isBlocked?.()) return;
    const t = e.target;
    const entering = t.tagName === 'TEXTAREA' || t.tagName === 'SELECT' ||
      (t.tagName === 'INPUT' && !['checkbox', 'radio', 'range', 'button', 'color'].includes(t.type));
    const k = e.key.toLowerCase();
    const ctrl = e.ctrlKey || e.metaKey;

    if (e.key === 'Alt') { e.preventDefault(); return; }
    if (e.key === 'Shift' && S.drag && S.drag.kind === 'shape' && !e.repeat) { drawShape(S.drag.x, S.drag.y, true); return; }

    if (ctrl && !e.altKey) {
      if (k === 'z' || k === 'y') {
        if (entering) return;
        e.preventDefault();
        if (k === 'y' || e.shiftKey) redo(); else undo();
      } else if (k === 's') { e.preventDefault(); save(e.shiftKey); }
      else if (k === 'o') { e.preventDefault(); openDialog(); }
      else if (k === 'n') { e.preventDefault(); newDialog(); }
      else if (e.key === "'") { e.preventDefault(); P.grid = !P.grid; savePrefs(); syncTop(); render(); }
      return;
    }
    if (entering) return;
    if (e.altKey || e.metaKey) return;

    if (e.key === 'Escape') {
      if (S.pickerSlot) closePicker();
      else if (S.cur) { cancelStroke(); S.drag = null; render(); }
      return;
    }
    if (e.code === 'Space') {
      e.preventDefault();
      if (!S.space) { S.space = true; R.view.classList.add('pan-ready'); updateCursor(); }
      return;
    }
    if (e.repeat && !'[]'.includes(e.key)) return;
    let handled = true;
    switch (e.key) {
      case 'b': case 'B': setTool('pencil'); break;
      case 'e': case 'E': setTool('eraser'); break;
      case 'g': case 'G': setTool('fill'); break;
      case 'i': case 'I': setTool('picker'); break;
      case 'l': case 'L': setTool('line'); break;
      case 'u': setTool('rect'); break;
      case 'U': setTool('ellipse'); break;
      case 'x': case 'X': swapColors(); break;
      case '[': setSize(P.size - 1); renderOptions(); break;
      case ']': setSize(P.size + 1); renderOptions(); break;
      case '0': fit(); break;
      case '1': zoomTo(1); break;
      case '+': case '=': zoomStep(1); break;
      case '-': case '_': zoomStep(-1); break;
      case '#': P.grid = !P.grid; savePrefs(); syncTop(); render(); break;
      default: handled = false;
    }
    if (handled) e.preventDefault();
  }

  function onKeyUp(e) {
    if (!S.active) return;
    if (e.code === 'Space' && S.space) { S.space = false; R.view.classList.remove('pan-ready'); updateCursor(); }
    if (e.key === 'Shift' && S.drag && S.drag.kind === 'shape') drawShape(S.drag.x, S.drag.y, false);
  }

  window.addEventListener('keydown', onKeyDown);
  window.addEventListener('keyup', onKeyUp);
  window.addEventListener('blur', () => { S.space = false; R.view.classList.remove('pan-ready'); });

  // ---------- 시작 ----------

  syncTools();
  syncColors();
  syncTop();

  return {
    open,
    openDialog,
    newDialog,
    confirmDiscard,
    isDirty: () => !!(S.doc && S.doc.history.dirty),
    currentPath: () => S.doc?.path || '',
    activate(on) {
      S.active = on;
      if (on) {
        measure();
        syncTop();
        if (S.doc && S.autoFit) fit();
        else render();
      } else {
        closePicker();
      }
    },
    // 개발·검증용: 현재 문서 상태 확인
    debug: () => ({ doc: S.doc, P, view: S.view }),
  };
}
