// 픽셀 편집기: 상태, 화면 구성, 입력 처리, 파일 열기/저장.
// 문서는 RGBA 픽셀 버퍼 하나(doc.pix, 비곱셈)이고 doc.px32 는 같은 메모리를 uint32 로 본 것입니다.
// 그리기는 모두 "획(stroke)" 단위로 버퍼를 직접 고치면서 바뀐 픽셀의 이전 값을 기록하고,
// 획이 끝나면 History 에 패치 하나로 넣습니다. 선·도형은 드래그하는 동안 되돌렸다 다시 그리며 미리 보여줍니다.
import './editor.css';
import {
  ListPalettes, LoadImageRGBA, LoadPalette, PaletteExists, PickFiles, PickFolder, PickPaletteFile, ProcessPixels, SaveImageAs,
  SaveImageRGBA, SavePaletteColors,
} from '../../wailsjs/go/main/App';
import { css, fromBase64, norm, over, pack, parseHex, toBase64, toHex, unpack } from './color.js';
import { History, HistoryBudget } from './history.js';
import { clearOverlay, drawAnts, drawCursor, drawMain, drawNavigator } from './render.js';
import * as Sel from './selection.js';
import { createStrip, visibleList } from './strip.js';
import { createTabs } from './tabs.js';
import * as T from './tools.js';
import { t as tr, tn } from '../i18n/index.js';
import { el, isModalOpen, localized, modal, relabel } from './ui.js';

const ZOOMS = [0.125, 0.25, 0.5, 1, 2, 3, 4, 5, 6, 8, 10, 12, 16, 20, 24, 32, 40, 48, 64];
const TILES = [0, 8, 16, 32, 64];
const MAX_SIDE = 8192;
const MAX_PIXELS = 4096 * 4096;
const PREF_KEY = 'sprite-tools-editor';
const HISTORY_BUDGET = 256 * 1024 * 1024; // 열려 있는 모든 문서의 실행 취소 기록이 나눠 쓰는 메모리 (문서마다 최대 200단계)

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
  select: '<rect x="2.8" y="3.4" width="10.4" height="9.2" stroke-dasharray="2.2 1.6"/>',
  move: '<path d="M8 1.8v12.4M1.8 8h12.4M6.2 3.6L8 1.8l1.8 1.8M6.2 12.4L8 14.2l1.8-1.8M3.6 6.2L1.8 8l1.8 1.8M12.4 6.2L14.2 8l-1.8 1.8"/>',
  wand: '<path d="M2.6 13.4l6.8-6.8"/><path d="M9.4 6.6l1.4 1.4"/><path d="M11.2 1.8v2.4M10 3h2.4M13.4 5.4v1.8M12.5 6.3h1.8M6.2 2.8v1.6M5.4 3.6H7"/>',
};
function icon(name) {
  const s = document.createElementNS('http://www.w3.org/2000/svg', 'svg');
  s.setAttribute('viewBox', '0 0 16 16');
  s.setAttribute('class', 'ed-icon');
  s.innerHTML = ICONS[name];
  return s;
}

// Tool names and extra tooltip text live in the catalogs: editor.tool.<id> and, when extra is set, editor.tool.<id>.extra.
const TOOLS = [
  { id: 'pencil', key: 'B' },
  { id: 'eraser', key: 'E' },
  { id: 'fill', key: 'G' },
  { id: 'picker', key: 'I', extra: true },
  { id: 'line', key: 'L', extra: true },
  { id: 'rect', key: 'U', extra: true },
  { id: 'ellipse', key: 'Shift+U', extra: true },
  { id: 'select', key: 'M', extra: true, group: 1 },
  { id: 'wand', key: 'W', extra: true, group: 1 },
  { id: 'move', key: 'V', extra: true, group: 1 },
];
const toolTip = (tool) => `${tr(`editor.tool.${tool.id}`)} (${tool.key})${tool.extra ? ` — ${tr(`editor.tool.${tool.id}.extra`)}` : ''}`;
const SELECT_TOOLS = ['select', 'move'];

const defaultPrefs = {
  tool: 'pencil', size: 1, shape: 'square', pixelPerfect: true, fillMode: 'contiguous', tolerance: 0,
  shapeFilled: false, symX: false, symY: false, grid: false, tile: 0,
  primary: pack(0, 0, 0, 255), secondary: pack(255, 255, 255, 255), recent: [],
  adhoc: [], palSource: 'adhoc',
  wandMode: 'contiguous', wand8: false, // 마법봉: 이어진 영역 / 전체 같은 색, 대각선 포함 여부
  autoSave: false, keepZoom: false, stripCollapsed: false, // 파일 띠 옵션
};

function loadPrefs() {
  try {
    const p = { ...defaultPrefs, ...JSON.parse(localStorage.getItem(PREF_KEY) || '{}') };
    if (!TOOLS.some((t) => t.id === p.tool)) p.tool = 'pencil';
    p.recent = Array.isArray(p.recent) ? p.recent.filter((c) => Number.isInteger(c)).slice(0, 16) : [];
    p.adhoc = Array.isArray(p.adhoc) ? p.adhoc.filter((c) => Number.isInteger(c)).slice(0, 256) : [];
    if (typeof p.palSource !== 'string') p.palSource = 'adhoc';
    return p;
  } catch {
    return { ...defaultPrefs };
  }
}

let clip = null; // 내부 클립보드: { w, h, pix: Uint32Array, mask: Uint8Array } — 문서가 바뀌어도 유지됩니다
const clamp = (v, a, b) => Math.max(a, Math.min(b, v));
const sameFile = (a, b) => !!a && !!b && a.toLowerCase().replace(/\\/g, '/') === b.toLowerCase().replace(/\\/g, '/');
const baseName = (p) => p.split(/[\\/]/).pop();

// host: 편집기를 넣을 요소.
// hooks: { toast(text, cls, actions), getBackup(), setBackup(on), onSaved(path, info) -> actions[], onDirtyChange(count),
//   files() -> 일괄 처리 목록(공유), getFilter(), setFilter(text), addPaths(paths) -> 열 수 있는 경로[], removeFile(path),
//   onCurrentChange(path), isBlocked() }
//
// 문서 모델: S.docs 가 열린 문서(탭) 목록이고 S.doc 이 지금 보이는 문서입니다. 문서마다 픽셀·크기·기록(History)·선택(sel)·
// 보기(view/autoFit)·원본 경로/형식을 따로 갖고, 도구·색·도구 옵션·팔레트·격자는 모든 문서가 공유합니다(P).
// 떠 있는 층(S.float)은 문서를 떠날 때 확정합니다. 보고 있는 동안의 확대·이동은 S.view/S.autoFit 에 있다가 문서를 떠날 때 문서에 저장합니다.
export function createEditor(host, hooks) {
  const P = loadPrefs();
  const budget = new HistoryBudget(HISTORY_BUDGET);
  let docSeq = 0;
  const S = {
    doc: null,
    docs: [],
    active: false,
    view: { zoom: 8, ox: 0, oy: 0 },
    autoFit: true,
    hover: null, // {x, y}
    space: false,
    drag: null,
    cur: null, // 진행 중인 획
    saving: false,
    pickerSlot: null,
    float: null, // 떠 있는 층 (이동·붙여넣기 중): { x, y, w, h, pix, off, rec, origSel }
    fx: null, // 진행 중인 효과 미리보기
  };
  const dpr = () => window.devicePixelRatio || 1;
  let savePrefsTimer = 0;
  const savePrefs = () => {
    clearTimeout(savePrefsTimer);
    savePrefsTimer = setTimeout(() => { try { localStorage.setItem(PREF_KEY, JSON.stringify(P)); } catch { /* 무시 */ } }, 200);
  };

  // ---------- 화면 구성 ----------

  const R = {}; // 자주 쓰는 요소
  let saveAllCount = 0; // number of unsaved documents, shown on the "Save all" button
  const saveAllLabel = () => (saveAllCount > 1 ? tr('editor.save_all_n', { n: saveAllCount }) : tr('editor.save_all'));
  // content / title: a string or element as-is, or a function returning text (re-applied on every language change).
  const mkBtn = (cls, content, title, onclick) => {
    const b = el('button', cls);
    b.type = 'button';
    if (content instanceof Node) b.append(content);
    const apply = () => {
      if (typeof content === 'function') b.textContent = content();
      else if (typeof content === 'string') b.textContent = content;
      const tip = typeof title === 'function' ? title() : title;
      if (tip) { b.title = tip; b.setAttribute('aria-label', tip); }
    };
    if (typeof content === 'function' || typeof title === 'function') localized(apply);
    else apply();
    b.onclick = (e) => { onclick(e); b.blur(); };
    return b;
  };
  const sep = () => el('span', 'ed-sep');

  function buildTop() {
    const top = el('div', 'ed-top');
    R.open = mkBtn('btn small', () => tr('editor.open'), () => tr('editor.open.title'), () => openDialog());
    R.new = mkBtn('btn small', () => tr('editor.new'), () => tr('editor.new.title'), () => newDialog());
    R.save = mkBtn('btn small primary', () => tr('editor.save'), () => tr('editor.save.title'), () => save(false));
    R.saveAs = mkBtn('btn small', () => tr('editor.save_as'), () => tr('editor.save_as.title'), () => save(true));
    R.saveAll = mkBtn('btn small', () => saveAllLabel(), () => tr('editor.save_all.title'), () => saveAll());
    R.undo = mkBtn('btn small', '↶', () => tr('editor.undo.title'), () => undo());
    R.redo = mkBtn('btn small', '↷', () => tr('editor.redo.title'), () => redo());
    R.zoomOut = mkBtn('btn small', '−', () => tr('editor.zoom_out.title'), () => zoomStep(-1));
    R.zoomLabel = el('span', 'ed-zoom', '100%');
    localized(() => { R.zoomLabel.title = tr('editor.zoom_label.title'); });
    R.zoomIn = mkBtn('btn small', '+', () => tr('editor.zoom_in.title'), () => zoomStep(1));
    R.fit = mkBtn('btn small', () => tr('editor.fit'), () => tr('editor.fit.title'), () => fit());
    R.one = mkBtn('btn small', '1:1', () => tr('editor.one.title'), () => zoomTo(1));
    R.grid = mkBtn('btn small ed-toggle', icon('grid'), () => tr('editor.grid.title'), () => { P.grid = !P.grid; savePrefs(); syncTop(); render(); });
    R.tile = el('select', 'ed-tile');
    localized(() => {
      R.tile.title = tr('editor.tile.title');
      R.tile.replaceChildren(...TILES.map((n) => new Option(n ? tr('editor.tile.option', { n }) : tr('editor.tile.none'), n)));
      R.tile.value = String(P.tile);
    });
    R.tile.onchange = () => { P.tile = Number(R.tile.value); savePrefs(); render(); R.tile.blur(); };
    R.menuEdit = mkBtn('btn small ed-menubtn', () => tr('editor.menu.edit'), () => tr('editor.menu.edit.title'), (e) => toggleMenu(R.menuEdit, editMenu));
    R.menuImage = mkBtn('btn small ed-menubtn', () => tr('editor.menu.image'), () => tr('editor.menu.image.title'), () => toggleMenu(R.menuImage, imageMenu));
    R.menuFx = mkBtn('btn small ed-menubtn', () => tr('editor.menu.fx'), () => tr('editor.menu.fx.title'), () => toggleMenu(R.menuFx, fxMenu));
    top.append(R.open, R.new, sep(), R.save, R.saveAs, R.saveAll, sep(), R.undo, R.redo, sep(),
      R.menuEdit, R.menuImage, R.menuFx, el('span', 'spacer'), R.zoomOut, R.zoomLabel, R.zoomIn, R.fit, R.one, sep(), R.grid, R.tile);
    return top;
  }

  function buildTools() {
    const bar = el('div', 'ed-tools');
    R.toolBtns = {};
    let group = 0;
    for (const t of TOOLS) {
      if ((t.group || 0) !== group) { group = t.group || 0; bar.append(el('span', 'ed-tools-sep')); }
      const b = mkBtn('ed-tool', icon(t.id), () => toolTip(t), () => setTool(t.id));
      R.toolBtns[t.id] = b;
      bar.append(b);
    }
    bar.append(el('span', 'ed-tools-sep'));
    R.symX = mkBtn('ed-tool', icon('symx'), () => tr('editor.sym_x.title'), () => { P.symX = !P.symX; savePrefs(); syncTools(); updateCursor(); });
    R.symY = mkBtn('ed-tool', icon('symy'), () => tr('editor.sym_y.title'), () => { P.symY = !P.symY; savePrefs(); syncTools(); updateCursor(); });
    bar.append(R.symX, R.symY);
    return bar;
  }

  function buildCenter() {
    const c = el('div', 'ed-center');
    R.tabs = createTabs({
      activate: (id) => { const d = S.docs.find((x) => x.id === id); if (d) switchTo(d); },
      close: (id) => { const d = S.docs.find((x) => x.id === id); if (d) closeDoc(d); },
      pin: (id) => { const d = S.docs.find((x) => x.id === id); if (d) pinDoc(d); },
      closeClean: () => closeClean(),
    });
    R.view = el('div', 'ed-view');
    R.main = el('canvas', 'ed-canvas');
    R.over = el('canvas', 'ed-canvas ed-over');
    R.empty = el('div', 'ed-empty');
    const emptyTitle = el('p');
    const emptyHint = el('p', 'muted small');
    localized(() => {
      emptyTitle.textContent = tr('editor.empty.title');
      emptyHint.textContent = tr('editor.empty.hint');
    });
    R.empty.append(el('div', 'empty-art'), emptyTitle, emptyHint);
    const row = el('div', 'row');
    row.append(mkBtn('btn', () => tr('editor.open'), '', () => openDialog()), mkBtn('btn', () => tr('editor.new'), '', () => newDialog()));
    R.empty.append(row);
    R.view.append(R.main, R.over, R.empty);

    const status = el('div', 'ed-status');
    R.coords = el('span', 'ed-coords', '');
    R.under = el('span', 'ed-under', '');
    R.selInfo = el('span', 'ed-selinfo', '');
    R.hint = el('span', 'muted ellipsis', '');
    status.append(R.coords, R.under, R.selInfo, el('span', 'spacer'), R.hint);
    c.append(R.tabs.el, R.view, status);
    return c;
  }

  // title: a catalog key; the heading is re-translated on every language change.
  function section(titleKey) {
    const s = el('section', 'ed-section');
    const h = el('h3');
    localized(() => { h.textContent = tr(titleKey); });
    s.append(h);
    return s;
  }

  // The swatch tooltip shows the slot's name and the current color: "<name> — click to choose a color [#hex]".
  function swatchTitle(slot) {
    return tr('editor.color.swatch_title', { tip: tr(slot === 'primary' ? 'editor.color.primary' : 'editor.color.secondary'), hex: toHex(P[slot]) });
  }

  function buildSide() {
    const side = el('aside', 'ed-side');
    R.side = side;

    // 색상
    const colors = section('editor.section.colors');
    const sw = el('div', 'ed-swatches');
    R.secSw = el('button', 'ed-sw ed-sw-sec');
    R.priSw = el('button', 'ed-sw ed-sw-pri');
    for (const [b, slot] of [[R.secSw, 'secondary'], [R.priSw, 'primary']]) {
      b.type = 'button';
      b.append(el('i'));
      b.onclick = () => { openPicker(slot); b.blur(); };
    }
    R.swap = mkBtn('ed-swap', icon('swap'), () => tr('editor.color.swap.title'), () => swapColors());
    sw.append(R.secSw, R.priSw, R.swap);
    R.recent = el('div', 'ed-recent');
    const rl = el('div', 'ed-label muted small');
    localized(() => {
      rl.textContent = tr('editor.color.recent');
      rl.title = tr('editor.color.recent.title');
    });
    colors.append(sw, rl, R.recent);

    // 도구 옵션
    R.optSec = section('editor.section.options');
    R.opts = el('div', 'ed-opts');
    R.optSec.append(R.opts);

    // 효과 미리보기 (효과 메뉴를 고르면 맨 위에 나타남; 제목은 효과를 열 때 다시 채워집니다)
    R.fxSec = section('editor.section.fx');
    R.fxSec.classList.add('ed-fx');
    R.fxSec.hidden = true;

    // 팔레트
    const pal = buildPalette();

    // 내비게이터
    const nav = section('editor.section.navigator');
    R.nav = el('canvas', 'ed-nav');
    R.navBox = el('div', 'ed-nav-box');
    R.navBox.append(R.nav);
    nav.append(R.navBox);

    // 이미지 정보
    const info = section('editor.section.info');
    R.info = el('dl', 'ed-info');
    R.backup = el('input');
    R.backup.type = 'checkbox';
    R.backup.onchange = () => hooks.setBackup(R.backup.checked);
    const bl = el('label', 'check small');
    const blText = el('span');
    localized(() => {
      bl.title = tr('editor.backup.title');
      blText.textContent = tr('editor.backup');
    });
    bl.append(R.backup, ' ', blText);
    info.append(R.info, bl);

    side.append(R.fxSec, colors, R.optSec, pal, nav, info);
    return side;
  }

  host.classList.add('editor');
  const top = buildTop();
  const body = el('div', 'ed-body');
  R.strip = createStrip({
    files: () => hooks.files(),
    filter: () => hooks.getFilter(),
    setFilter: (t) => hooks.setFilter(t),
    stateOf: (path) => {
      const d = findDocByPath(path);
      return { open: !!d, dirty: !!d && isDirty(d), current: !!(S.doc && sameFile(S.doc.path, path)) };
    },
    open: (path, pin) => openPath(path, pin ? 'pin' : 'nav'),
    addFiles: () => addFilesDialog(),
    addFolder: () => addFolderDialog(),
    remove: (path) => hooks.removeFile(path),
    get: (k) => P[k === 'collapsed' ? 'stripCollapsed' : k],
    set: (k, v) => { P[k === 'collapsed' ? 'stripCollapsed' : k] = v; savePrefs(); },
  });
  body.append(buildTools(), R.strip.el, buildCenter(), buildSide());
  host.append(top, body);
  buildPicker();

  // ---------- 상태 → 화면 ----------

  function syncTop() {
    const d = S.doc;
    for (const b of [R.save, R.saveAs, R.zoomOut, R.zoomIn, R.fit, R.one, R.grid, R.tile, R.menuEdit, R.menuImage, R.menuFx]) b.disabled = !d;
    const nd = dirtyDocs().length;
    R.saveAll.disabled = nd === 0 || !!S.fx;
    saveAllCount = nd;
    R.saveAll.textContent = saveAllLabel();
    R.undo.disabled = !d || !d.history.canUndo || !!S.fx;
    R.redo.disabled = !d || !d.history.canRedo || !!S.fx;
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
    R.view.dataset.tool = P.tool;
    R.hint.textContent = tr(`editor.hint.${P.tool}`);
    renderOptions();
  }

  function renderSwatch(btn, slot) {
    btn.firstChild.style.setProperty('--c', css(P[slot]));
    btn.title = swatchTitle(slot);
  }

  function syncColors() {
    renderSwatch(R.priSw, 'primary');
    renderSwatch(R.secSw, 'secondary');
    R.recent.replaceChildren(...P.recent.map((c) => {
      const b = el('button', 'ed-recent-sw');
      b.type = 'button';
      b.style.setProperty('--c', css(c));
      b.title = tr('editor.color.click_hint', { hex: toHex(c) });
      b.onclick = () => { setColor('primary', c); b.blur(); };
      b.oncontextmenu = (e) => { e.preventDefault(); setColor('secondary', c); };
      return b;
    }));
    syncPicker();
    markPal();
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

  // 채우기와 마법봉이 함께 쓰는 허용 오차 슬라이더
  function toleranceRow() {
    const tol = el('div', 'ed-range');
    const r = el('input');
    r.type = 'range'; r.min = 0; r.max = 128; r.step = 1; r.value = P.tolerance;
    const v = el('span', 'value', String(P.tolerance));
    r.oninput = () => { P.tolerance = Number(r.value); v.textContent = r.value; savePrefs(); };
    tol.append(r, v);
    return optRow(tr('editor.opt.tolerance'), tol);
  }

  function renderOptions() {
    const o = R.opts;
    o.replaceChildren();
    const t = P.tool;
    const brush = t === 'pencil' || t === 'eraser' || t === 'line' || ((t === 'rect' || t === 'ellipse') && !P.shapeFilled);
    if (brush) {
      o.append(optRow(tr(t === 'rect' || t === 'ellipse' || t === 'line' ? 'editor.opt.line_width' : 'editor.opt.size'), sizeSlider()));
      o.append(optRow(tr('editor.opt.brush_shape'), segment([['square', tr('editor.opt.square')], ['round', tr('editor.opt.round')]], P.shape, (v) => { P.shape = v; savePrefs(); renderOptions(); updateCursor(); })));
    }
    if (t === 'pencil' || t === 'eraser') {
      o.append(checkbox(tr('editor.opt.pixel_perfect'), P.pixelPerfect, (v) => { P.pixelPerfect = v; savePrefs(); },
        tr('editor.opt.pixel_perfect.title')));
    }
    if (t === 'fill') {
      o.append(optRow(tr('editor.opt.fill'), segment([['contiguous', tr('editor.opt.contiguous')], ['global', tr('editor.opt.global')]], P.fillMode, (v) => { P.fillMode = v; savePrefs(); renderOptions(); })));
      o.append(toleranceRow());
    }
    if (t === 'wand') {
      o.append(optRow(tr('editor.opt.wand_range'), segment([['contiguous', tr('editor.opt.contiguous')], ['global', tr('editor.opt.global')]], P.wandMode, (v) => { P.wandMode = v; savePrefs(); renderOptions(); })));
      if (P.wandMode === 'contiguous') {
        o.append(checkbox(tr('common.diagonal'), P.wand8, (v) => { P.wand8 = v; savePrefs(); }, tr('editor.opt.wand_diag_title')));
      }
      o.append(toleranceRow());
      o.append(el('p', 'muted small', tr('editor.opt.wand_note')));
    }
    if (t === 'rect' || t === 'ellipse') {
      o.append(optRow(tr('editor.opt.figure'), segment([[false, tr('editor.opt.outline')], [true, tr('editor.opt.filled')]], P.shapeFilled, (v) => { P.shapeFilled = v; savePrefs(); renderOptions(); })));
    }
    if (t === 'picker') o.append(el('p', 'muted small', tr('editor.opt.picker_note')));
    if (t === 'select' || t === 'wand') o.append(el('p', 'muted small', tr('editor.opt.select_note')));
    if (t === 'move') o.append(el('p', 'muted small', tr('editor.opt.move_note')));
    R.optSec.hidden = false;
  }

  function setTool(id) {
    if (S.cur || S.fx) return;
    if (S.float && !SELECT_TOOLS.includes(id)) commitFloat();
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
    localized(() => { R.hex.placeholder = tr('editor.picker.hex_placeholder'); });
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
    for (const [k, label] of [['r', 'R'], ['g', 'G'], ['b', 'B'], ['a', () => tr('editor.picker.alpha')]]) {
      const row = el('div', 'ed-ch');
      const chLabel = el('span', 'ed-ch-label');
      localized(() => { chLabel.textContent = typeof label === 'function' ? label() : label; });
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
      row.append(chLabel, r, n);
      pop.append(row);
    }
    const foot = el('div', 'row');
    R.sys = el('input');
    R.sys.type = 'color';
    localized(() => { R.sys.title = tr('editor.picker.system.title'); });
    R.sys.oninput = () => {
      const c = parseHex(R.sys.value);
      if (c == null || !S.pickerSlot) return;
      const a = unpack(P[S.pickerSlot]).a;
      P[S.pickerSlot] = (c & 0x00ffffff | (a << 24)) >>> 0;
      savePrefs();
      syncColors();
    };
    const clear = mkBtn('btn small', () => tr('editor.picker.transparent'), () => tr('editor.picker.transparent.title'), () => { if (S.pickerSlot) setColor(S.pickerSlot, 0); });
    const close = mkBtn('btn small', () => tr('common.close'), '', () => closePicker());
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
      id: meta.id ?? ++docSeq,
      w, h, pix, px32: new Uint32Array(pix.buffer), off, octx, imageData, mark: new Uint8Array(w * h),
      path, name, indexed: !!meta.indexed, palSize: meta.colors || 0,
      history: meta.history || new History({ budget }), dirtyRect: null, colorCount: null, sel: null,
      view: meta.view || null, // 문서를 떠날 때 저장해 두는 보기 (없으면 처음 열 때 화면에 맞춤)
      autoFit: meta.autoFit ?? true,
      preview: !!meta.preview, // 탐색으로 연 미리보기 탭 (고치거나 더블클릭하면 고정)
    };
  }

  // 같은 문서(경로·기록·탭 정보 유지)에서 크기나 내용만 통째로 바꾼 새 문서를 만듭니다. 선택은 해제됩니다.
  function rebuildDoc(d, w, h, pix) {
    return makeDoc(w, h, pix, d.path, d.name, {
      id: d.id, indexed: d.indexed, colors: d.palSize, history: d.history, view: d.view, autoFit: d.autoFit, preview: d.preview,
    });
  }

  const findDocByPath = (path) => (path ? S.docs.find((d) => sameFile(d.path, path)) || null : null);
  // 보고 있는 문서의 떠 있는 층도 아직 확정 전인 변경으로 칩니다.
  const isDirty = (d) => d.history.dirty || (S.doc && d.id === S.doc.id && !!S.float);
  const dirtyDocs = () => S.docs.filter(isDirty);

  let lastDirty = 0;
  let docSig = '';
  let lastCurrent = null;
  let lastActiveId = 0;

  // 탭과 파일 띠를 문서 상태에 맞춰 다시 그립니다 (달라진 것이 없으면 건너뜀).
  function syncDocs(force = false) {
    const sig = S.docs.map((d) => `${d.id}:${d.name}:${d.path}:${d.preview ? 1 : 0}:${isDirty(d) ? 1 : 0}`).join('|') + `#${S.doc ? S.doc.id : 0}`;
    if (!force && sig === docSig) return;
    docSig = sig;
    R.tabs.render(S.docs, S.doc ? S.doc.id : 0, isDirty);
    R.strip.render();
    const id = S.doc ? S.doc.id : 0;
    if (id !== lastActiveId) { lastActiveId = id; R.strip.scrollToCurrent(); } // 보던 문서가 바뀔 때만 띠를 맞춰 스크롤
  }

  function updateDirty() {
    const n = dirtyDocs().length;
    if (n !== lastDirty) { lastDirty = n; hooks.onDirtyChange?.(n); }
  }

  function notifyCurrent() {
    const p = S.doc?.path || '';
    if (p !== lastCurrent) { lastCurrent = p; hooks.onCurrentChange?.(p); }
  }

  // 문서를 바꿔 보여 줍니다. 떠 있는 층은 확정하고, 지금 문서의 보기(확대·위치)는 그 문서에 저장해 둡니다.
  // o.view 가 있으면 새 문서의 보기로 쓰고(확대 유지), 없으면 문서가 저장해 둔 보기 또는 화면 맞춤을 씁니다.
  function activateDoc(d, o = {}) {
    cancelStroke();
    cancelFx();
    commitFloat();
    S.drag = null;
    R.view.classList.remove('panning');
    const prev = S.doc;
    if (prev && prev.id !== d.id) { prev.view = { ...S.view }; prev.autoFit = S.autoFit; }
    S.doc = d;
    S.hover = null;
    budget.touch(d.history);
    if (o.view) { S.view = { ...o.view }; S.autoFit = false; }
    else if (d.view) { S.view = { ...d.view }; S.autoFit = d.autoFit; }
    else S.autoFit = true;
    afterChange();
    if (S.autoFit) fit();
    else { measure(); clampPan(); render(); }
    notifyCurrent();
  }

  // 열린 문서가 하나도 없는 상태
  function showNone() {
    cancelStroke();
    cancelFx();
    S.float = null;
    S.drag = null;
    S.doc = null;
    S.hover = null;
    afterChange();
    notifyCurrent();
  }

  function afterChange() {
    syncTop();
    syncSel();
    const d = S.doc;
    if (d && d.preview && isDirty(d)) d.preview = false; // 고치기 시작하면 탭이 고정됩니다
    updateDirty();
    syncDocs();
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
      if (P.palSource === 'image') { computeImageColors(); renderPal(); }
    }
    const rows = [
      [tr('editor.info.size'), `${d.w} × ${d.h}`],
      [tr('editor.info.colors'), d.colorCount ? tn(d.colorCount.trans ? 'editor.info.color_count_trans' : 'editor.info.color_count', d.colorCount.n) : '…'],
      [tr('editor.info.format'), d.path ? (d.indexed ? tn('editor.info.indexed', d.palSize) : 'RGBA PNG') : tr('editor.info.new_image')],
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
    drawMain(R.main.getContext('2d'), R.main.width, R.main.height, r, S.doc, S.view, { grid: P.grid, tile: P.tile, float: floatView() });
    drawNavigator(R.nav, 196, 80, r, S.doc, S.view, viewW, viewH);
    drawOverlay();
  }

  // 떠 있는 층을 캔버스로 만들어 둡니다 (내용이 바뀌면 f.off = null 로 무효화)
  function floatView() {
    const f = S.float;
    if (!f) return null;
    if (!f.off) {
      f.off = document.createElement('canvas');
      f.off.width = f.w;
      f.off.height = f.h;
      f.off.getContext('2d').putImageData(new ImageData(new Uint8ClampedArray(f.pix.buffer), f.w, f.h), 0, 0);
    }
    return { canvas: f.off, x: f.x, y: f.y, w: f.w, h: f.h };
  }

  function drawOverlay() {
    const ctx = R.over.getContext('2d');
    clearOverlay(ctx, R.over.width, R.over.height);
    const d = S.doc;
    if (!d) return;
    drawCursor(ctx, R.over.width, R.over.height, dpr(), S.view, cursorCells());
    const phase = Math.floor(Date.now() / 90) % 8;
    if (d.sel) drawAnts(ctx, dpr(), S.view, Sel.edgesOf(d.sel), d.sel.x, d.sel.y, phase);
    const dr = S.drag;
    if (dr && dr.kind === 'select' && dr.moved) {
      const r = Sel.rectSel(dr.x0, dr.y0, dr.x1, dr.y1);
      drawAnts(ctx, dpr(), S.view, Sel.edgesOf(r), r.x, r.y, phase);
    }
  }
  // 개미 행렬 애니메이션: 선택이 있을 때만 윤곽선을 다시 그립니다.
  setInterval(() => {
    if (S.active && S.doc && (S.doc.sel || (S.drag && S.drag.kind === 'select'))) drawOverlay();
  }, 110);
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
    if (SELECT_TOOLS.includes(t) || S.fx) return null;
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

  // rec(획 또는 떠 있는 층의 기록)에 이전 값을 남기면서 픽셀 하나를 씁니다. 선택 영역은 검사하지 않습니다.
  function put(rec, i, c) {
    const d = S.doc, p = d.px32;
    if (p[i] === c) return;
    if (!d.mark[i]) {
      d.mark[i] = 1;
      rec.idx.push(i);
      rec.before.push(p[i]);
    }
    p[i] = c;
    dirtyAdd(d, i % d.w, (i / d.w) | 0);
  }

  // 그리기 도구용: 선택 영역이 있으면 그 안쪽 픽셀만 바꿉니다.
  function setIdx(i, c) {
    const sel = S.doc.sel;
    if (sel && !Sel.selHas(sel, i % S.doc.w, (i / S.doc.w) | 0)) return;
    put(S.cur, i, c);
  }

  const newRec = () => ({ idx: [], before: [] });

  function revertRec(rec) {
    const d = S.doc;
    for (let k = rec.idx.length - 1; k >= 0; k--) {
      const i = rec.idx[k];
      d.px32[i] = rec.before[k];
      d.mark[i] = 0;
      dirtyAdd(d, i % d.w, (i / d.w) | 0);
    }
    rec.idx.length = 0;
    rec.before.length = 0;
  }

  // 기록을 패치로 만들고 표시를 지웁니다. 실제로 바뀐 픽셀이 없으면 null.
  function finishRec(rec) {
    const d = S.doc;
    const n = rec.idx.length;
    const idx = new Uint32Array(n), before = new Uint32Array(n), after = new Uint32Array(n);
    let m = 0;
    for (let k = 0; k < n; k++) {
      const i = rec.idx[k];
      d.mark[i] = 0;
      const a = d.px32[i];
      if (a === rec.before[k]) continue;
      idx[m] = i; before[m] = rec.before[k]; after[m] = a;
      m++;
    }
    return m ? { idx: idx.slice(0, m), before: before.slice(0, m), after: after.slice(0, m) } : null;
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
    revertRec(S.cur);
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
    const patch = finishRec(s);
    if (patch) {
      d.history.push(patch);
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
    if (S.fx) return;
    if (e.button !== 0 && e.button !== 2) return;
    e.preventDefault();
    if (S.cur) return;
    const { x, y } = toDoc(e);
    S.hover = { x, y };
    const color = e.button === 2 ? P.secondary : P.primary;
    let tool = P.tool;
    if (e.altKey && tool !== 'picker' && tool !== 'select' && tool !== 'wand') tool = 'picker'; // 선택 도구에서 Alt 는 "빼기"
    if (tool === 'picker') {
      S.drag = { kind: 'picker', id: e.pointerId, button: e.button };
      capture(e);
      pick(x, y, e.button);
      return;
    }
    if (tool === 'select' || tool === 'move') {
      if (e.button === 0) selectPointerDown(e, tool, x, y);
      return;
    }
    if (tool === 'wand') {
      if (e.button === 0) wandClick(e, x, y);
      return;
    }
    if (S.float) commitFloat();
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

  // 마법봉: 클릭한 픽셀과 같은 색(허용 오차 이내)인 영역을 선택합니다. Shift: 선택에 더하기, Alt: 선택에서 빼기.
  // 완전히 투명한 픽셀(알파 0)은 RGB 와 상관없이 모두 같은 색입니다.
  function wandClick(e, x, y) {
    const d = S.doc;
    commitFloat();
    const mode = e.shiftKey ? 'add' : e.altKey ? 'sub' : 'replace';
    if (x < 0 || y < 0 || x >= d.w || y >= d.h) { // 이미지 밖 클릭 = 선택 해제 (수정 키가 없을 때)
      if (mode === 'replace') setSelection(null);
      return;
    }
    const list = P.wandMode === 'global'
      ? T.floodGlobal(d.px32, x, y, d.w, P.tolerance)
      : T.floodContiguous(d.px32, d.w, d.h, x, y, P.tolerance, P.wand8);
    setSelection(Sel.combineSels(d.sel, Sel.fromIndices(list, d.w), mode));
  }

  // 선택·이동 도구: 선택 안쪽(또는 이동 도구)에서는 떠 있는 층을 끌고, 그 밖에서는 사각형 선택을 시작합니다.
  function selectPointerDown(e, tool, x, y) {
    const d = S.doc;
    const inside = !!(d.sel && Sel.selHas(d.sel, x, y));
    const mods = e.shiftKey || e.altKey;
    if (tool === 'move' || (inside && !mods)) {
      capture(e);
      const f = S.float;
      S.drag = { kind: 'float', id: e.pointerId, sx: x, sy: y, fx: f ? f.x : 0, fy: f ? f.y : 0, cx: e.clientX, cy: e.clientY, moved: false, inside };
      return;
    }
    if (S.float) commitFloat();
    capture(e);
    const cx = clamp(x, 0, d.w - 1), cy = clamp(y, 0, d.h - 1);
    S.drag = {
      kind: 'select', id: e.pointerId, x0: cx, y0: cy, x1: cx, y1: cy, cx: e.clientX, cy: e.clientY, moved: false,
      mode: e.shiftKey ? 'add' : e.altKey ? 'sub' : 'replace',
    };
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
      else if (dr.kind === 'select') {
        dr.x1 = clamp(x, 0, d.w - 1); dr.y1 = clamp(y, 0, d.h - 1);
        if (!dr.moved && Math.hypot(e.clientX - dr.cx, e.clientY - dr.cy) >= 3) dr.moved = true;
        if (dr.moved) R.selInfo.textContent = tr('editor.sel.size', { w: Math.abs(dr.x1 - dr.x0) + 1, h: Math.abs(dr.y1 - dr.y0) + 1 });
      } else if (dr.kind === 'float') {
        if (!dr.moved && Math.hypot(e.clientX - dr.cx, e.clientY - dr.cy) >= 3) dr.moved = true;
        if (dr.moved || S.float) {
          let f = S.float;
          if (!f) {
            if (x === dr.sx && y === dr.sy) { updateCursor(); return; }
            f = ensureFloat();
            dr.fx = f.x; dr.fy = f.y;
          }
          moveFloatTo(dr.fx + (x - dr.sx), dr.fy + (y - dr.sy));
        }
      }
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
    else if (dr.kind === 'select') {
      const d = S.doc;
      if (!dr.moved) { if (dr.mode === 'replace') setSelection(null); } // 그냥 클릭 = 선택 해제
      else setSelection(Sel.combineRect(d.sel, dr.x0, dr.y0, dr.x1, dr.y1, dr.mode, d.w, d.h));
      syncSel();
    } else if (dr.kind === 'float') {
      // 움직이지 않고 떠 있는 층 밖을 눌렀다면 확정
      if (!dr.moved && S.float && !(S.doc.sel && Sel.selHas(S.doc.sel, dr.sx, dr.sy))) commitFloat();
    }
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

  // ---------- 선택 영역 ----------

  const busy = () => !S.doc || !!S.cur || !!S.fx || !!S.drag;

  function selText() {
    const sel = S.doc?.sel;
    if (!sel) return '';
    const rect = sel.count === sel.w * sel.h;
    const p = { w: sel.w, h: sel.h, x: sel.x, y: sel.y, count: sel.count };
    return `${tr(rect ? 'editor.sel.text' : 'editor.sel.text_cells', p)}${S.float ? ` · ${tr('editor.sel.floating')}` : ''}`;
  }
  function syncSel() {
    R.selInfo.textContent = selText();
    updateCursor();
  }

  function setSelection(sel) {
    if (!S.doc) return;
    S.doc.sel = sel;
    syncSel();
  }

  function selectAll() {
    if (busy()) return;
    commitFloat();
    setSelection(Sel.allSel(S.doc.w, S.doc.h));
  }
  function deselect() {
    if (busy()) return;
    commitFloat();
    setSelection(null);
  }
  function invertSelection() {
    if (busy()) return;
    commitFloat();
    setSelection(Sel.invertSel(S.doc.sel, S.doc.w, S.doc.h));
  }

  // ---------- 떠 있는 층 (이동·붙여넣기) ----------
  // 선택한 픽셀을 문서에서 떼어 내(원래 자리는 투명하게) f.pix 로 들고 있다가, 확정하면 새 자리에 합성합니다.
  // 떼어 내기부터 확정까지의 변경은 f.rec 에 모아 두었다가 실행 취소 한 단계로 넣습니다. 선택 마스크는 층과 함께 움직입니다(d.sel.x/y).

  function ensureFloat() {
    if (S.float) return S.float;
    const d = S.doc;
    const origSel = d.sel ? { ...d.sel } : null;
    if (!d.sel) d.sel = Sel.allSel(d.w, d.h);
    const sel = d.sel;
    const pix = Sel.extractPix(d.px32, d.w, d.h, sel);
    const rec = newRec();
    for (let y = 0; y < sel.h; y++) {
      for (let x = 0; x < sel.w; x++) {
        if (sel.mask[y * sel.w + x]) put(rec, (sel.y + y) * d.w + sel.x + x, 0);
      }
    }
    S.float = { x: sel.x, y: sel.y, w: sel.w, h: sel.h, pix, off: null, rec, origSel };
    afterChange();
    return S.float;
  }

  function moveFloatTo(nx, ny) {
    const f = S.float, sel = S.doc.sel;
    f.x = sel.x = nx;
    f.y = sel.y = ny;
    syncSel();
    render();
  }

  function commitFloat() {
    const f = S.float;
    if (!f) return;
    const d = S.doc;
    const sel = d.sel;
    S.float = null;
    for (let y = 0; y < f.h; y++) {
      const gy = f.y + y;
      if (gy < 0 || gy >= d.h) continue;
      for (let x = 0; x < f.w; x++) {
        const c = f.pix[y * f.w + x];
        const gx = f.x + x;
        if (c >>> 24 === 0 || gx < 0 || gx >= d.w) continue;
        const i = gy * d.w + gx;
        put(f.rec, i, over(c, d.px32[i]));
      }
    }
    const patch = finishRec(f.rec);
    d.sel = Sel.clipSel(sel, d.w, d.h);
    if (patch) d.history.push(patch);
    afterChange();
  }

  function cancelFloat() {
    const f = S.float;
    if (!f) return;
    S.float = null;
    revertRec(f.rec);
    S.doc.sel = f.origSel;
    afterChange();
  }

  function nudge(dx, dy) {
    if (!S.doc || S.cur || S.fx) return;
    if (!S.doc.sel && !S.float) return;
    const f = ensureFloat();
    moveFloatTo(f.x + dx, f.y + dy);
  }

  // 떠 있는 층(없으면 선택 영역을 떼어 낸 뒤)을 뒤집거나 돌립니다. 돌리면 중심을 유지합니다.
  function transformFloat(kind) {
    const f = ensureFloat(), sel = S.doc.sel;
    const r = T.transformPix(f.pix, f.w, f.h, kind);
    const m = T.transformPix(sel.mask, f.w, f.h, kind);
    if (kind === 'cw' || kind === 'ccw') {
      f.x += Math.floor((f.w - r.w) / 2);
      f.y += Math.floor((f.h - r.h) / 2);
    }
    f.pix = r.arr; f.w = r.w; f.h = r.h; f.off = null;
    sel.mask = m.arr; sel.w = r.w; sel.h = r.h; sel.x = f.x; sel.y = f.y; sel.edges = null;
    syncSel();
    render();
  }

  // ---------- 클립보드 ----------

  // 떠 있는 층 > 선택 영역 > 이미지 전체 순으로 복사합니다.
  function copySelection(showToast = true) {
    const d = S.doc;
    if (!d) return;
    const f = S.float, sel = d.sel;
    if (f && sel) clip = { w: f.w, h: f.h, pix: f.pix.slice(), mask: sel.mask.slice() };
    else if (sel) clip = { w: sel.w, h: sel.h, pix: Sel.extractPix(d.px32, d.w, d.h, sel), mask: sel.mask.slice() };
    else clip = { w: d.w, h: d.h, pix: d.px32.slice(), mask: new Uint8Array(d.w * d.h).fill(1) };
    if (showToast) hooks.toast(tr(sel ? 'toast.copied_selection' : 'toast.copied_all', { w: clip.w, h: clip.h }));
  }

  function deleteSelection() {
    if (!S.doc || S.cur || S.fx) return;
    const d = S.doc;
    if (S.float) { S.float.pix.fill(0); S.float.off = null; commitFloat(); return; }
    const sel = d.sel;
    if (!sel) return;
    S.cur = newStroke('delete', 0);
    for (let y = 0; y < sel.h; y++) {
      for (let x = 0; x < sel.w; x++) if (sel.mask[y * sel.w + x]) setIdx((sel.y + y) * d.w + sel.x + x, 0);
    }
    endStroke();
  }

  function cutSelection() {
    if (busy() || !(S.doc.sel || S.float)) return;
    copySelection(false);
    deleteSelection();
    hooks.toast(tr('toast.cut'));
  }

  function pasteClip() {
    if (busy()) return;
    if (!clip) { hooks.toast(tr('toast.paste_empty')); return; }
    commitFloat();
    const d = S.doc, v = S.view;
    // 지금 보이는 영역의 왼쪽 위에 붙입니다 (문서보다 크면 0, 0)
    let px = Math.max(0, Math.ceil(-v.ox / v.zoom)), py = Math.max(0, Math.ceil(-v.oy / v.zoom));
    px = Math.max(0, Math.min(px, d.w - clip.w));
    py = Math.max(0, Math.min(py, d.h - clip.h));
    const origSel = d.sel ? { ...d.sel } : null;
    d.sel = Sel.makeSel(px, py, clip.w, clip.h, clip.mask.slice());
    S.float = { x: px, y: py, w: clip.w, h: clip.h, pix: clip.pix.slice(), off: null, rec: newRec(), origSel };
    if (!SELECT_TOOLS.includes(P.tool)) { P.tool = 'move'; savePrefs(); syncTools(); }
    afterChange();
  }

  // ---------- 변형 · 캔버스 ----------

  // 문서 전체를 새 픽셀 배열(w×h, Uint32Array)로 바꾸는 편집 하나. 크기가 같으면 픽셀 패치, 다르면 스냅샷 패치를 넣습니다.
  function commitContent(w, h, px32) {
    const d = S.doc;
    if (w === d.w && h === d.h) {
      const idx = [], before = [], after = [];
      for (let i = 0; i < px32.length; i++) {
        if (px32[i] !== d.px32[i]) { idx.push(i); before.push(d.px32[i]); after.push(px32[i]); }
      }
      if (!idx.length) return false;
      for (let k = 0; k < idx.length; k++) {
        d.px32[idx[k]] = after[k];
        dirtyAdd(d, idx[k] % d.w, (idx[k] / d.w) | 0);
      }
      d.history.push({ idx: Uint32Array.from(idx), before: Uint32Array.from(before), after: Uint32Array.from(after) });
      afterChange();
      return true;
    }
    const pix1 = new Uint8ClampedArray(px32.buffer, px32.byteOffset, px32.byteLength).slice();
    d.history.push({ kind: 'snap', w0: d.w, h0: d.h, pix0: d.pix.slice(), w1: w, h1: h, pix1 });
    setContent(w, h, pix1.slice());
    return true;
  }

  // 같은 문서를 새 크기·픽셀로 바꿔 끼웁니다 (기록은 유지, 선택은 해제)
  function setContent(w, h, pix) {
    const d = S.doc;
    const sized = d.w !== w || d.h !== h;
    S.float = null;
    S.doc = rebuildDoc(d, w, h, pix);
    const at = S.docs.indexOf(d);
    if (at >= 0) S.docs[at] = S.doc;
    if (sized) {
      if (S.autoFit) fit(); else clampPan();
    }
    afterChange();
  }

  function transform(kind) {
    if (busy()) return;
    const d = S.doc;
    if (S.float) { transformFloat(kind); return; }
    if (d.sel) { transformFloat(kind); commitFloat(); return; }
    const r = T.transformPix(d.px32, d.w, d.h, kind);
    commitContent(r.w, r.h, r.arr);
  }

  async function canvasSizeDialog() {
    if (busy()) return;
    commitFloat();
    const d = S.doc;
    let wIn, hIn;
    const anchor = { x: 1, y: 1 };
    const cells = [];
    const r = await modal({
      title: tr('editor.canvas.title'),
      build: (b) => {
        wIn = el('input'); wIn.type = 'number'; wIn.min = 1; wIn.max = MAX_SIDE; wIn.value = d.w;
        hIn = el('input'); hIn.type = 'number'; hIn.min = 1; hIn.max = MAX_SIDE; hIn.value = d.h;
        const row = el('div', 'ed-newrow');
        const f = (label, input) => { const l = el('label', 'ed-newfield'); l.append(el('span', 'field-label', label), input); return l; };
        row.append(f(tr('editor.field.width'), wIn), el('span', 'muted', '×'), f(tr('editor.field.height'), hIn));
        const grid = el('div', 'ed-anchor');
        const mark = () => cells.forEach((c) => c.classList.toggle('on', c.dataset.ax === String(anchor.x) && c.dataset.ay === String(anchor.y)));
        for (let ay = 0; ay < 3; ay++) {
          for (let ax = 0; ax < 3; ax++) {
            const c = el('button', 'ed-anchor-cell');
            c.type = 'button';
            c.dataset.ax = ax; c.dataset.ay = ay;
            c.title = tr('editor.canvas.anchor_title');
            c.onclick = () => { anchor.x = ax; anchor.y = ay; mark(); };
            cells.push(c);
            grid.append(c);
          }
        }
        mark();
        const wrap = el('div', 'ed-anchor-wrap');
        wrap.append(grid, el('p', 'muted small', tr('editor.canvas.anchor_note')));
        b.append(row, wrap, el('p', 'muted small', tr('editor.canvas.current', { w: d.w, h: d.h })));
      },
      buttons: [
        { label: tr('dialog.cancel'), value: null },
        {
          label: tr('dialog.apply'), value: 'ok', primary: true,
          validate: () => {
            const w = Number(wIn.value), h = Number(hIn.value);
            const ok = Number.isInteger(w) && Number.isInteger(h) && w >= 1 && h >= 1 && w <= MAX_SIDE && h <= MAX_SIDE && w * h <= MAX_PIXELS;
            if (!ok) hooks.toast(tr('common.size_limit', { max: MAX_SIDE, mp: MAX_PIXELS / 1e6 | 0 }), 'failed');
            return ok;
          },
        },
      ],
    });
    if (r !== 'ok' || S.doc !== d) return;
    const nw = Number(wIn.value), nh = Number(hIn.value);
    if (nw === d.w && nh === d.h) return;
    const ox = anchor.x === 0 ? 0 : anchor.x === 1 ? Math.floor((nw - d.w) / 2) : nw - d.w;
    const oy = anchor.y === 0 ? 0 : anchor.y === 1 ? Math.floor((nh - d.h) / 2) : nh - d.h;
    commitContent(nw, nh, T.cropPix(d.px32, d.w, d.h, -ox, -oy, nw, nh));
    hooks.toast(tr('toast.canvas_resized', { w: nw, h: nh }));
  }

  function cropToSelection() {
    if (busy()) return;
    commitFloat();
    const d = S.doc, sel = d.sel;
    if (!sel) { hooks.toast(tr('toast.crop_need_selection')); return; }
    if (sel.x === 0 && sel.y === 0 && sel.w === d.w && sel.h === d.h) { hooks.toast(tr('toast.crop_whole')); return; }
    commitContent(sel.w, sel.h, T.cropPix(d.px32, d.w, d.h, sel.x, sel.y, sel.w, sel.h));
    hooks.toast(tr('toast.cropped', { w: sel.w, h: sel.h }));
  }

  function trimMargins() {
    if (busy()) return;
    commitFloat();
    const d = S.doc;
    const b = T.opaqueBounds(d.px32, d.w, d.h);
    if (!b) { hooks.toast(tr('toast.trim_transparent')); return; }
    const nw = b.x1 - b.x0 + 1, nh = b.y1 - b.y0 + 1;
    if (nw === d.w && nh === d.h) { hooks.toast(tr('toast.trim_none')); return; }
    commitContent(nw, nh, T.cropPix(d.px32, d.w, d.h, b.x0, b.y0, nw, nh));
    hooks.toast(tr('toast.trimmed', { w0: d.w, h0: d.h, w1: nw, h1: nh }));
  }

  // ---------- 메뉴 ----------

  function closeMenu() {
    if (!R.menu) return;
    R.menu.remove();
    R.menu = null;
    R.menuBtn?.classList.remove('on');
    R.menuBtn = null;
  }

  function toggleMenu(btn, itemsFn) {
    if (R.menu && R.menuBtn === btn) { closeMenu(); return; }
    closeMenu();
    const m = el('div', 'ed-menu');
    const dis = busy();
    for (const it of itemsFn()) {
      if (it === '-') { m.append(el('div', 'ed-menu-sep')); continue; }
      const b = el('button', 'ed-menu-item');
      b.type = 'button';
      b.append(el('span', '', it.label), el('span', 'ed-menu-key', it.key || ''));
      b.disabled = dis || !!it.disabled;
      b.onclick = () => { closeMenu(); it.run(); };
      m.append(b);
    }
    host.append(m);
    const br = btn.getBoundingClientRect(), hr = host.getBoundingClientRect();
    m.style.top = `${br.bottom - hr.top + 2}px`;
    m.style.left = `${Math.max(4, Math.min(br.left - hr.left, hr.width - m.offsetWidth - 4))}px`;
    btn.classList.add('on');
    R.menu = m;
    R.menuBtn = btn;
  }
  document.addEventListener('pointerdown', (e) => {
    if (R.menu && !R.menu.contains(e.target) && !e.target.closest('.ed-menubtn')) closeMenu();
  }, true);

  const hasSel = () => !!(S.doc && (S.doc.sel || S.float));
  const editMenu = () => [
    { label: tr('editor.menu.cut'), key: 'Ctrl+X', disabled: !hasSel(), run: cutSelection },
    { label: tr('editor.menu.copy'), key: 'Ctrl+C', run: () => copySelection() },
    { label: tr('editor.menu.paste'), key: 'Ctrl+V', disabled: !clip, run: pasteClip },
    { label: tr('editor.menu.clear'), key: 'Delete', disabled: !hasSel(), run: deleteSelection },
    '-',
    { label: tr('editor.menu.select_all'), key: 'Ctrl+A', run: selectAll },
    { label: tr('editor.menu.deselect'), key: 'Ctrl+D', disabled: !hasSel(), run: deselect },
    { label: tr('editor.menu.invert'), key: 'Ctrl+Shift+I', run: invertSelection },
  ];
  const imageMenu = () => {
    const sel = hasSel();
    const what = sel ? ` ${tr('editor.menu.selection_suffix')}` : '';
    return [
      { label: `${tr('editor.menu.flip_h')}${what}`, key: 'Shift+H', run: () => transform('flipH') },
      { label: `${tr('editor.menu.flip_v')}${what}`, key: 'Shift+V', run: () => transform('flipV') },
      { label: `${tr('editor.menu.rotate_cw')}${what}`, run: () => transform('cw') },
      { label: `${tr('editor.menu.rotate_ccw')}${what}`, run: () => transform('ccw') },
      { label: `${tr('editor.menu.rotate_180')}${what}`, run: () => transform('r180') },
      '-',
      { label: tr('editor.menu.canvas_size'), run: canvasSizeDialog },
      { label: tr('editor.menu.crop'), disabled: !sel, run: cropToSelection },
      { label: tr('editor.menu.trim'), run: trimMargins },
    ];
  };
  const fxMenu = () => [
    { label: tr('editor.menu.fx_reduce'), run: () => openEffect('reduce') },
    { label: tr('editor.menu.fx_recolor'), run: () => openEffect('recolor') },
    { label: tr('editor.menu.fx_downscale'), run: () => openEffect('downscale') },
    { label: tr('editor.menu.fx_upscale'), run: () => openEffect('upscale') },
    '-',
    { label: tr('editor.menu.fx_despeckle'), run: () => openEffect('despeckle') },
  ];

  // ---------- 효과 (일괄 처리 기능을 현재 이미지에 적용) ----------
  // 효과를 고르면 오른쪽 맨 위에 옵션 패널이 열리고, 옵션을 바꿀 때마다 Go(ProcessPixels)로 결과를 계산해 캔버스에 미리 보여 줍니다.
  // 미리 보기는 임시 문서(S.doc 교체)이며 취소하면 원래 문서·보기로 돌아가고, 확인하면 편집 한 단계로 들어갑니다.

  const FX_SCALES = [0, 2, 3, 4, 5, 6, 8, 10, 12, 16];
  const FX_UPSCALES = [2, 3, 4, 5, 6, 8, 10, 12, 16];
  const fxPrefs = { mode: 'threshold', threshold: 0.05, maxColors: 16, scale: 0, upscale: 2, palette: '', minSize: 4, diagonal: true, ...(P.fx || {}) };
  const saveFxPrefs = () => { P.fx = { ...fxPrefs }; savePrefs(); };

  async function openEffect(kind) {
    if (busy()) return;
    commitFloat();
    closePicker();
    const d0 = S.doc;
    const fx = {
      kind, orig: d0, view: { ...S.view }, autoFit: S.autoFit, b64: toBase64(d0.pix), token: 0, timer: 0, result: null, showOrig: false,
      sized: `${d0.w}x${d0.h}`, req: null,
    };
    S.fx = fx;
    buildFxPanel(fx);
  }

  // Builds (or, after a language change, rebuilds) the options panel of the running effect and recomputes the preview.
  function buildFxPanel(fx) {
    const kind = fx.kind, d0 = fx.orig;
    const body = el('div', 'ed-opts');
    const status = el('p', 'small ed-fx-status', '');
    fx.status = status;
    const run = () => { clearTimeout(fx.timer); fx.timer = setTimeout(runEffect, 140); };

    if (kind === 'reduce') {
      const box = el('div', 'ed-opts');
      const draw = () => {
        box.replaceChildren();
        box.append(segment([['threshold', tr('batch.reduce.threshold')], ['count', tr('batch.reduce.count')]], fxPrefs.mode, (v) => { fxPrefs.mode = v; saveFxPrefs(); draw(); run(); }));
        if (fxPrefs.mode === 'threshold') {
          const w = el('div', 'ed-range');
          const r = el('input');
          r.type = 'range'; r.min = 0.005; r.max = 0.3; r.step = 0.005; r.value = fxPrefs.threshold;
          const v = el('span', 'value', Number(fxPrefs.threshold).toFixed(3));
          r.oninput = () => { fxPrefs.threshold = Number(r.value); v.textContent = fxPrefs.threshold.toFixed(3); saveFxPrefs(); run(); };
          w.append(r, v);
          box.append(optRow(tr('editor.fx.reduce_strength'), w));
        } else {
          const n = el('input');
          n.type = 'number'; n.min = 1; n.max = 256; n.value = fxPrefs.maxColors;
          n.oninput = () => {
            const v = Math.round(Number(n.value));
            if (v >= 1 && v <= 256) { fxPrefs.maxColors = v; saveFxPrefs(); run(); }
          };
          box.append(optRow(tr('editor.fx.reduce_count'), n));
        }
      };
      draw();
      body.append(box);
      fx.req = () => (fxPrefs.mode === 'count'
        ? { command: 'reduce', maxColors: fxPrefs.maxColors }
        : { command: 'reduce', threshold: fxPrefs.threshold });
    } else if (kind === 'recolor') {
      const sel = el('select');
      const fill = (want) => {
        sel.replaceChildren();
        if (!PAL.list.length) sel.append(new Option(tr('common.no_palettes'), ''));
        PAL.list.forEach((p) => sel.append(new Option(tn('common.palette_option', p.colors.length, { name: p.label }), p.name)));
        if (want && PAL.list.some((p) => p.name === want)) sel.value = want;
        else if (PAL.list.some((p) => p.name === fxPrefs.palette)) sel.value = fxPrefs.palette;
        fxPrefs.palette = sel.value;
      };
      fill();
      refreshPalList().then(() => { if (S.fx === fx) { fill(); run(); } });
      sel.onchange = () => { fxPrefs.palette = sel.value; saveFxPrefs(); run(); };
      const file = mkBtn('btn small', tr('editor.pal.file'), tr('editor.fx.palette_file.title'), async () => {
        const f = await PickPaletteFile();
        if (!f || S.fx !== fx) return;
        try {
          const p = await LoadPalette(f);
          addFilePalette(p);
          fill(p.name);
          saveFxPrefs();
          run();
        } catch (err) { hooks.toast(String(err), 'failed'); }
      });
      body.append(optRow(tr('editor.fx.palette'), sel), file);
      fx.req = () => ({ command: 'recolor', palette: sel.value });
    } else if (kind === 'despeckle') {
      const n = el('input');
      n.type = 'number'; n.min = 1; n.max = 100000; n.step = 1; n.value = fxPrefs.minSize;
      n.oninput = () => {
        const v = Math.round(Number(n.value));
        if (v >= 1 && v <= 100000) { fxPrefs.minSize = v; saveFxPrefs(); run(); }
      };
      const diag = checkbox(tr('common.diagonal'), fxPrefs.diagonal, (v) => { fxPrefs.diagonal = v; saveFxPrefs(); run(); },
        tr('batch.despeckle.diag_title'));
      body.append(optRow(tr('batch.despeckle.min'), n), diag,
        el('p', 'muted small', tr('editor.fx.despeckle_note')));
      fx.req = () => ({ command: 'despeckle', minSize: fxPrefs.minSize, diagonal: fxPrefs.diagonal });
    } else if (kind === 'downscale') {
      const sel = el('select');
      FX_SCALES.forEach((v) => sel.append(new Option(v === 0 ? tr('common.auto_detect') : tr('common.scale_option', { n: v }), v)));
      sel.value = String(fxPrefs.scale);
      sel.onchange = () => { fxPrefs.scale = Number(sel.value); saveFxPrefs(); run(); };
      body.append(optRow(tr('editor.fx.downscale_scale'), sel), el('p', 'muted small', tr('editor.fx.downscale_note')));
      fx.req = () => ({ command: 'downscale', scale: fxPrefs.scale });
    } else {
      const sel = el('select');
      FX_UPSCALES.forEach((v) => sel.append(new Option(tr('common.scale_option', { n: v }), v)));
      sel.value = String(fxPrefs.upscale);
      sel.onchange = () => { fxPrefs.upscale = Number(sel.value); saveFxPrefs(); run(); };
      body.append(optRow(tr('editor.fx.upscale_scale'), sel), el('p', 'muted small', tr('editor.fx.upscale_note')));
      fx.req = () => ({ command: 'upscale', scale: fxPrefs.upscale });
    }

    const cmp = el('button', 'btn small', tr('editor.fx.compare'));
    cmp.type = 'button';
    cmp.title = tr('editor.fx.compare.title');
    const showOrig = (on) => { fx.showOrig = on; cmp.classList.toggle('on', on); showFxDoc(); };
    cmp.onpointerdown = () => showOrig(true);
    cmp.onpointerup = cmp.onpointerleave = cmp.onpointercancel = () => { if (fx.showOrig) showOrig(false); };
    fx.ok = mkBtn('btn small primary', tr('editor.fx.ok'), tr('editor.fx.ok.title'), () => applyEffect());
    fx.ok.disabled = true;
    const cancel = mkBtn('btn small', tr('editor.fx.cancel'), tr('editor.fx.cancel.title'), () => cancelFx());
    const foot = el('div', 'row');
    foot.append(fx.ok, cancel);
    const scope = kind === 'despeckle'
      ? tr(d0.sel ? 'editor.fx.scope_despeckle_sel' : 'editor.fx.scope_despeckle')
      : tr('editor.fx.scope_all');
    R.fxSec.replaceChildren(el('h3', '', tr('editor.fx.title', { name: tr(`editor.fx.name.${kind}`) })), body, status, el('p', 'muted small', scope), cmp, foot);
    R.fxSec.hidden = false;
    R.side.scrollTop = 0;
    syncTop();
    updateCursor();
    run();
  }

  async function runEffect() {
    const fx = S.fx;
    if (!fx) return;
    const t = ++fx.token;
    const d0 = fx.orig;
    const req = fx.req();
    if (req.command === 'recolor' && !req.palette) {
      fx.result = null;
      fx.status.textContent = tr('editor.fx.choose_palette');
      fx.status.className = 'small warn ed-fx-status';
      fx.ok.disabled = true;
      showFxDoc();
      return;
    }
    fx.status.textContent = tr('editor.fx.calculating');
    fx.status.className = 'small muted ed-fx-status';
    try {
      const res = await ProcessPixels(req, d0.w, d0.h, fx.b64);
      if (t !== fx.token || S.fx !== fx) return;
      if (res.skipped) {
        fx.result = null;
        fx.status.textContent = tr('editor.fx.no_change', { msg: res.message });
        fx.status.className = 'small warn ed-fx-status';
        fx.ok.disabled = true;
      } else {
        let pix = fromBase64(res.pixels, new Uint8ClampedArray(res.width * res.height * 4));
        let msg = res.message;
        // 작은 조각 지우기: 선택 영역이 있으면 그 안쪽 픽셀만 결과를 쓰고 나머지는 원본을 둡니다.
        if (fx.kind === 'despeckle' && d0.sel && res.width === d0.w && res.height === d0.h) {
          const proc = new Uint32Array(pix.buffer);
          const merged = d0.px32.slice();
          const sel = d0.sel;
          let cnt = 0;
          for (let y = 0; y < sel.h; y++) {
            const gy = sel.y + y;
            if (gy < 0 || gy >= d0.h) continue;
            for (let x = 0; x < sel.w; x++) {
              const gx = sel.x + x;
              if (gx < 0 || gx >= d0.w || !sel.mask[y * sel.w + x]) continue;
              const i = gy * d0.w + gx;
              if (merged[i] !== proc[i]) { merged[i] = proc[i]; cnt++; }
            }
          }
          if (!cnt) {
            fx.result = null;
            fx.status.textContent = tr('editor.fx.no_pieces');
            fx.status.className = 'small warn ed-fx-status';
            fx.ok.disabled = true;
            showFxDoc();
            return;
          }
          pix = new Uint8ClampedArray(merged.buffer);
          msg = tn('editor.fx.removed_in_selection', cnt, { msg: res.message });
        }
        fx.result = { doc: rebuildDoc(d0, res.width, res.height, pix), msg, w: res.width, h: res.height };
        fx.status.textContent = `${msg} → ${res.width} × ${res.height}`;
        fx.status.className = 'small ed-fx-status';
        fx.ok.disabled = false;
      }
    } catch (err) {
      if (t !== fx.token || S.fx !== fx) return;
      fx.result = null;
      fx.status.textContent = String(err);
      fx.status.className = 'small warn ed-fx-status';
      fx.ok.disabled = true;
    }
    showFxDoc();
  }

  // 미리 보기 문서(또는 원본)를 화면에 올립니다.
  function showFxDoc() {
    const fx = S.fx;
    if (!fx) return;
    const want = fx.showOrig || !fx.result ? fx.orig : fx.result.doc;
    if (S.doc === want) return;
    S.doc = want;
    const key = `${want.w}x${want.h}`;
    if (key !== fx.sized) {
      fx.sized = key;
      if (fx.autoFit) fit(); else clampPan();
    }
    scheduleInfo();
    render();
  }

  // 효과 패널을 닫고 원래 문서·보기로 되돌립니다.
  function closeFxUi() {
    const fx = S.fx;
    clearTimeout(fx.timer);
    fx.token++;
    S.fx = null;
    R.fxSec.hidden = true;
    R.fxSec.replaceChildren();
    S.doc = fx.orig;
    S.view = { ...fx.view };
    S.autoFit = fx.autoFit;
    return fx;
  }

  function cancelFx() {
    if (!S.fx) return;
    closeFxUi();
    afterChange();
    if (S.autoFit) fit(); else { clampPan(); render(); }
  }

  function applyEffect() {
    const fx = S.fx;
    if (!fx || !fx.result) return;
    const { doc, msg, w, h } = fx.result;
    closeFxUi();
    const changed = commitContent(w, h, new Uint32Array(doc.pix.slice().buffer));
    if (!changed) { afterChange(); hooks.toast(tr('toast.fx_nochange')); return; }
    hooks.toast(tr('toast.fx_applied', { msg }));
  }

  // ---------- 팔레트 패널 ----------
  // 보여 줄 팔레트: 내 팔레트(localStorage) · 이미지에서(빈도순 최대 256색) · 저장된 팔레트(공용 폴더/Lospec) · PNG 파일

  const PAL = { list: [], img: { colors: [], total: 0 }, selIdx: -1 };
  const palLabel = (name) => (name.toLowerCase().endsWith('.png') ? baseName(name) : name);
  const toColors = (hexes) => hexes.map((h) => parseHex(h)).filter((c) => c != null);

  function addFilePalette(p) {
    PAL.list = PAL.list.filter((x) => x.name !== p.name);
    PAL.list.push({ name: p.name, label: palLabel(p.name), colors: toColors(p.colors) });
  }

  async function refreshPalList(want) {
    try {
      const list = await ListPalettes();
      const files = PAL.list.filter((p) => p.name.toLowerCase().endsWith('.png'));
      PAL.list = [...(list || []).map((p) => ({ name: p.name, label: palLabel(p.name), colors: toColors(p.colors) })), ...files];
    } catch (err) {
      hooks.toast(String(err), 'failed');
    }
    if (want) P.palSource = `pal:${want}`;
    if (P.palSource.startsWith('pal:') && !PAL.list.some((p) => `pal:${p.name}` === P.palSource)) P.palSource = 'adhoc';
    savePrefs();
    renderPal();
  }

  function computeImageColors() {
    const d = S.doc;
    if (!d) { PAL.img = { colors: [], total: 0 }; return; }
    const m = new Map();
    const p = d.px32;
    for (let i = 0; i < p.length; i++) {
      const c = p[i];
      if (c >>> 24 === 0) continue;
      const k = (c | 0xff000000) >>> 0;
      m.set(k, (m.get(k) || 0) + 1);
    }
    const arr = [...m].sort((a, b) => b[1] - a[1]); // 안정 정렬 — 동률이면 먼저 나온 색이 앞
    PAL.img = { colors: arr.slice(0, 256).map((a) => a[0]), total: arr.length };
  }

  function curPal() {
    const src = P.palSource;
    if (src === 'image') return { id: 'image', label: tn('editor.pal.image_n', PAL.img.colors.length), colors: PAL.img.colors };
    const p = src.startsWith('pal:') && PAL.list.find((x) => `pal:${x.name}` === src);
    if (p) return { id: src, label: p.label, colors: p.colors, name: p.name };
    return { id: 'adhoc', label: tn('editor.pal.mine_n', P.adhoc.length), colors: P.adhoc };
  }

  function buildPalette() {
    const pal = section('editor.section.palette');
    R.palSel = el('select', 'ed-pal-sel');
    localized(() => { R.palSel.title = tr('editor.pal.title'); });
    R.palSel.onchange = () => {
      P.palSource = R.palSel.value;
      PAL.selIdx = -1;
      if (P.palSource === 'image') computeImageColors();
      savePrefs();
      renderPal();
      R.palSel.blur();
    };
    R.palGrid = el('div', 'ed-pal-grid');
    R.palInfo = el('div', 'muted small ed-pal-info', '');
    R.palAdd = mkBtn('btn small', () => tr('editor.pal.add'), () => tr('editor.pal.add.title'), () => palAdd());
    R.palDel = mkBtn('btn small', () => tr('editor.pal.remove'), () => tr('editor.pal.remove.title'), () => palRemove());
    R.palCopy = mkBtn('btn small', () => tr('editor.pal.copy'), () => tr('editor.pal.copy.title'), () => palCopyToAdhoc());
    R.palSave = mkBtn('btn small', () => tr('editor.pal.save'), () => tr('editor.pal.save.title'), () => palSave());
    R.palName = el('input');
    R.palName.type = 'text';
    localized(() => { R.palName.placeholder = tr('editor.pal.name_placeholder'); });
    R.palName.spellcheck = false;
    R.palName.onkeydown = (e) => { if (e.key === 'Enter') palFetch(); };
    R.palFetch = mkBtn('btn small', () => tr('editor.pal.fetch'), () => tr('editor.pal.fetch.title'), () => palFetch());
    R.palFile = mkBtn('btn small', () => tr('editor.pal.file'), () => tr('editor.pal.file.title'), () => palFile());
    const r1 = el('div', 'ed-palrow'), r2 = el('div', 'ed-palrow'), r3 = el('div', 'ed-palrow');
    r1.append(R.palAdd, R.palDel, R.palCopy);
    r2.append(R.palName, R.palFetch);
    r3.append(R.palFile, R.palSave);
    pal.append(R.palSel, R.palGrid, R.palInfo, r1, r2, r3);
    R.palette = pal;
    return pal;
  }

  function renderPal() {
    const cur = curPal();
    R.palSel.replaceChildren(
      new Option(tn('editor.pal.mine_n', P.adhoc.length), 'adhoc'),
      new Option(tn('editor.pal.image_n', PAL.img.colors.length), 'image'),
      ...PAL.list.map((p) => new Option(tn('common.palette_option', p.colors.length, { name: p.label }), `pal:${p.name}`)),
    );
    R.palSel.value = cur.id;
    R.palGrid.replaceChildren(...cur.colors.map((c, i) => {
      const b = el('button', 'ed-pal-sw');
      b.type = 'button';
      b.style.setProperty('--c', css(c));
      b.dataset.c = String(c);
      b.title = tr('editor.color.click_hint', { hex: toHex(c) });
      b.onclick = () => { PAL.selIdx = i; setColor('primary', c); b.blur(); };
      b.oncontextmenu = (e) => { e.preventDefault(); setColor('secondary', c); };
      return b;
    }));
    if (!cur.colors.length) {
      R.palGrid.append(el('span', 'muted small ed-pal-empty', tr(cur.id === 'adhoc' ? 'editor.pal.empty_mine' : cur.id === 'image' ? 'editor.pal.empty_image' : 'editor.pal.empty')));
    }
    R.palInfo.textContent = cur.id === 'image' && PAL.img.total > 256 ? tr('editor.pal.image_info_many', { total: PAL.img.total }) : cur.id === 'image' ? tr('editor.pal.image_info') : '';
    R.palInfo.hidden = !R.palInfo.textContent;
    const adhoc = cur.id === 'adhoc';
    R.palAdd.disabled = !adhoc;
    R.palDel.disabled = !adhoc || !P.adhoc.length;
    R.palCopy.hidden = adhoc;
    R.palSave.disabled = !cur.colors.length;
    markPal();
  }

  // 팔레트 칸 표시: 주 색과 같으면 굵은 테두리, 보조 색과 같으면 점선 테두리, 마지막으로 클릭한 칸은 살짝 표시
  function markPal() {
    if (!R.palGrid) return;
    let i = 0;
    for (const b of R.palGrid.children) {
      if (!b.dataset.c) continue;
      const c = Number(b.dataset.c);
      b.classList.toggle('cur', c === P.primary);
      b.classList.toggle('cur2', c === P.secondary && c !== P.primary);
      b.classList.toggle('sel', i === PAL.selIdx);
      i++;
    }
  }

  async function palCopyToAdhoc() {
    const cur = curPal();
    if (P.adhoc.length) {
      const r = await modal({
        title: tr('editor.pal.copy_dialog.title'),
        build: (b) => b.append(el('p', '', tr('editor.pal.copy_dialog.body', { n: cur.colors.length, m: P.adhoc.length }))),
        buttons: [{ label: tr('dialog.cancel'), value: null }, { label: tr('dialog.copy'), value: 'ok', primary: true }],
      });
      if (r !== 'ok') return;
    }
    P.adhoc = cur.colors.slice(0, 256);
    P.palSource = 'adhoc';
    PAL.selIdx = -1;
    savePrefs();
    renderPal();
  }

  function palAdd() {
    if (P.palSource !== 'adhoc') return;
    if (P.primary >>> 24 === 0) { hooks.toast(tr('toast.pal_transparent')); return; }
    const c = (P.primary | 0xff000000) >>> 0;
    const at = P.adhoc.indexOf(c);
    if (at >= 0) { PAL.selIdx = at; renderPal(); hooks.toast(tr('toast.pal_exists')); return; }
    if (P.adhoc.length >= 256) { hooks.toast(tr('toast.pal_max')); return; }
    P.adhoc.push(c);
    PAL.selIdx = P.adhoc.length - 1;
    savePrefs();
    renderPal();
  }

  function palRemove() {
    if (P.palSource !== 'adhoc') return;
    let i = PAL.selIdx;
    if (!(i >= 0 && i < P.adhoc.length)) i = P.adhoc.indexOf(P.primary);
    if (i < 0) { hooks.toast(tr('toast.pal_pick_first')); return; }
    P.adhoc.splice(i, 1);
    PAL.selIdx = -1;
    savePrefs();
    renderPal();
  }

  async function palFetch() {
    const name = R.palName.value.trim();
    if (!name) return;
    R.palFetch.disabled = true;
    R.palFetch.textContent = tr('common.loading');
    try {
      const p = await LoadPalette(name);
      R.palName.value = '';
      await refreshPalList(p.name);
    } catch (err) {
      hooks.toast(String(err), 'failed');
    } finally {
      R.palFetch.disabled = false;
      R.palFetch.textContent = tr('editor.pal.fetch');
    }
  }

  async function palFile() {
    const f = await PickPaletteFile();
    if (!f) return;
    try {
      const p = await LoadPalette(f);
      addFilePalette(p);
      await refreshPalList(p.name);
    } catch (err) {
      hooks.toast(String(err), 'failed');
    }
  }

  async function palSave() {
    const cur = curPal();
    const colors = cur.colors.filter((c) => c >>> 24 !== 0);
    if (!colors.length) return;
    let nameIn;
    const r = await modal({
      title: tr('editor.pal.save_dialog.title'),
      build: (b) => {
        nameIn = el('input');
        nameIn.type = 'text';
        nameIn.spellcheck = false;
        nameIn.placeholder = 'my-palette';
        nameIn.value = cur.name && !cur.name.toLowerCase().endsWith('.png') ? cur.name : '';
        b.append(
          el('p', '', tr('editor.pal.save_dialog.body', { n: colors.length })),
          nameIn,
          el('p', 'muted small', tr('editor.pal.save_dialog.note')),
        );
      },
      buttons: [
        { label: tr('dialog.cancel'), value: null },
        {
          label: tr('dialog.save'), value: 'ok', primary: true,
          validate: () => {
            const ok = /^[a-z0-9][a-z0-9-]*$/.test(nameIn.value.trim().toLowerCase());
            if (!ok) hooks.toast(tr('toast.pal_name_invalid'), 'failed');
            return ok;
          },
        },
      ],
    });
    if (r !== 'ok') return;
    const name = nameIn.value.trim().toLowerCase();
    try {
      let force = false;
      if (await PaletteExists(name)) {
        const o = await modal({
          title: tr('editor.pal.exists_dialog.title'),
          build: (b) => b.append(el('p', '', tr('editor.pal.exists_dialog.body', { name }))),
          buttons: [{ label: tr('dialog.cancel'), value: null }, { label: tr('dialog.overwrite'), value: 'ok', primary: true }],
        });
        if (o !== 'ok') return;
        force = true;
      }
      const info = await SavePaletteColors(name, colors.map((c) => toHex((c | 0xff000000) >>> 0)), force);
      await refreshPalList(info.name);
      hooks.toast(tn('toast.pal_saved', info.colors.length, { name: info.name }));
    } catch (err) {
      hooks.toast(String(err), 'failed');
    }
  }

  // ---------- 실행 취소 ----------

  function applyPatch(p, dir) {
    const d = S.doc;
    if (p.kind === 'snap') {
      const undoing = dir === 'undo';
      setContent(undoing ? p.w0 : p.w1, undoing ? p.h0 : p.h1, (undoing ? p.pix0 : p.pix1).slice());
      return;
    }
    const arr = dir === 'undo' ? p.before : p.after;
    let x0 = d.w, y0 = d.h, x1 = 0, y1 = 0;
    for (let k = 0; k < p.idx.length; k++) {
      const i = p.idx[k], x = i % d.w, y = (i / d.w) | 0;
      d.px32[i] = arr[k];
      if (x < x0) x0 = x; if (x > x1) x1 = x; if (y < y0) y0 = y; if (y > y1) y1 = y;
    }
    dirtyAdd(d, x0, y0);
    dirtyAdd(d, x1, y1);
    afterChange();
  }
  function undo() {
    if (!S.doc || S.cur || S.fx || S.drag) return;
    commitFloat();
    const p = S.doc.history.undo();
    if (p) applyPatch(p, 'undo');
  }
  function redo() {
    if (!S.doc || S.cur || S.fx || S.drag) return;
    commitFloat();
    const p = S.doc.history.redo();
    if (p) applyPatch(p, 'redo');
  }

  // ---------- 파일 · 문서(탭) ----------

  const newName = () => {
    let n = 1;
    const taken = new Set(S.docs.map((d) => d.name));
    const name = (k) => (k === 1 ? tr('editor.untitled') : tr('editor.untitled_n', { n: k }));
    while (taken.has(name(n))) n++;
    return name(n);
  };

  // 작업 중(획·효과 미리보기)에는 문서를 바꾸지 않습니다. 효과 미리보기는 안내만 하고 그대로 둡니다.
  function canSwitch() {
    if (S.fx) { hooks.toast(tr('toast.fx_finish_first')); return false; }
    return !S.cur;
  }

  // 탭·Ctrl+Tab 으로 문서를 바꿉니다.
  function switchTo(d) {
    const cur = S.fx ? S.fx.orig : S.doc;
    if (d === cur) return true;
    if (!canSwitch()) return false;
    activateDoc(d);
    return true;
  }

  function cycleDoc(dir) {
    const n = S.docs.length;
    if (n < 2) return;
    const i = S.docs.findIndex((d) => d.id === (S.doc ? S.doc.id : 0));
    switchTo(S.docs[(i + dir + n) % n]);
  }

  function pinDoc(d) {
    if (!d.preview) return;
    d.preview = false;
    syncDocs();
  }

  function removeDoc(d) {
    const i = S.docs.findIndex((x) => x.id === d.id);
    if (i < 0) return;
    S.docs.splice(i, 1);
    d.history.dispose();
  }

  // 이미 열려 있는 파일이면 그 탭으로, 아니면 읽어서 새 탭으로 엽니다.
  //  mode 'nav': 탐색으로 열기 — 수정하지 않은 미리보기 탭이 있으면 그 자리를 대신합니다 (이탤릭 탭).
  //  mode 'pin': 명시적으로 열기 — 항상 새 탭이고 고정됩니다.
  let navSeq = 0;
  async function openPath(path, mode = 'nav') {
    if (S.cur || !path) return false;
    const exist = findDocByPath(path);
    if (exist) {
      if (!switchTo(exist)) return false;
      if (mode === 'pin') pinDoc(exist);
      return true;
    }
    if (S.fx) { canSwitch(); return false; }
    const seq = mode === 'nav' ? ++navSeq : navSeq;
    let r;
    try {
      r = await LoadImageRGBA(path);
    } catch (err) {
      hooks.toast(String(err), 'failed');
      return false;
    }
    if (mode === 'nav' && seq !== navSeq) return false; // 그 사이 다른 파일로 넘어감 (마지막 요청만 반영)
    const again = findDocByPath(r.path);
    if (again) { if (!switchTo(again)) return false; if (mode === 'pin') pinDoc(again); return true; }
    if (S.cur || S.fx) return false;
    const pix = fromBase64(r.pixels, new Uint8ClampedArray(r.width * r.height * 4));
    const doc = makeDoc(r.width, r.height, pix, r.path, baseName(r.path), { indexed: r.indexed, colors: r.colors, preview: mode === 'nav' });
    const view = mode === 'nav' ? keptView(doc) : null;
    const old = insertDoc(doc);
    activateDoc(doc, { view });
    if (old) old.history.dispose();
    return true;
  }

  // 새 문서를 탭 목록에 넣습니다. 미리보기 문서이고 수정 안 한 미리보기 탭이 있으면 그 자리를 대신하고 그 탭을 돌려줍니다.
  function insertDoc(doc) {
    if (doc.preview) {
      const i = S.docs.findIndex((d) => d.preview && !isDirty(d));
      if (i >= 0) {
        const old = S.docs[i];
        S.docs[i] = doc;
        return old;
      }
    }
    const a = S.doc ? S.docs.findIndex((d) => d.id === S.doc.id) : -1;
    S.docs.splice(a >= 0 ? a + 1 : S.docs.length, 0, doc);
    return null;
  }

  // "확대 유지": 새로 연 파일에 지금 보던 확대 배율(같은 크기면 위치까지)을 씁니다. 맞춤 상태였다면 평소처럼 맞춥니다.
  function keptView(doc) {
    if (!P.keepZoom || !S.doc || S.autoFit) return null;
    measure();
    const v = S.view;
    if (S.doc.w === doc.w && S.doc.h === doc.h) return { ...v };
    return { zoom: v.zoom, ox: (viewW - doc.w * v.zoom) / 2, oy: (viewH - doc.h * v.zoom) / 2 };
  }

  // 목록의 다음(1)·이전(-1) 파일을 엽니다. 키를 빠르게 눌러도 한 번에 하나씩 차례로 처리합니다.
  let stepBusy = 0;
  // 앞선 이동이 끝난 뒤에 이어서 실행합니다 (대기는 최대 한 건).
  let stepChain = Promise.resolve();
  function step(dir) {
    if (stepBusy >= 2) return;
    stepBusy++;
    stepChain = stepChain.then(() => stepOnce(dir)).catch(() => {}).finally(() => { stepBusy--; });
  }

  async function stepOnce(dir) {
    if (S.fx) { canSwitch(); return; }
    if (S.cur || S.drag) return;
    const list = visibleList(hooks.files(), hooks.getFilter());
    if (!list.length) { hooks.toast(tr('toast.list_empty')); return; }
    const cur = S.doc?.path;
    const i = list.findIndex((f) => sameFile(f.path, cur));
    const ni = i < 0 ? (dir > 0 ? 0 : list.length - 1) : i + dir;
    if (ni < 0 || ni >= list.length) { hooks.toast(tr(dir > 0 ? 'toast.last_file' : 'toast.first_file')); return; }
    const d = S.doc;
    // 자동 저장: 고친 문서를 저장하고 넘어갑니다. 저장에 실패하면 넘어가지 않습니다.
    if (d && P.autoSave && isDirty(d) && d.path) {
      const r = await saveDoc(d, false);
      if (!r.ok) {
        if (r.error) hooks.toast(tr('toast.autosave_failed', { error: r.error }), 'failed');
        return;
      }
    }
    await openPath(list[ni].path, 'nav');
  }

  // 문서를 닫습니다. 저장하지 않은 변경이 있으면 저장 / 저장 안 함 / 취소를 묻습니다. 닫았으면 true.
  async function closeDoc(d, opt = {}) {
    if (!S.docs.some((x) => x.id === d.id) || S.cur) return false;
    if (S.doc && S.doc.id === d.id) { cancelFx(); commitFloat(); }
    if (isDirty(d) && !opt.discard) {
      const r = await modal({
        title: tr('editor.close.title'),
        build: (b) => b.append(el('p', '', tr('editor.close.body', { name: d.name }))),
        buttons: [{ label: tr('dialog.cancel'), value: 'cancel' }, { label: tr('dialog.discard'), value: 'discard' }, { label: tr('dialog.save'), value: 'save', primary: true }],
      });
      if (r === 'save') {
        const sr = await saveDoc(d, false);
        if (!sr.ok) {
          if (sr.error) hooks.toast(tr('toast.save_failed', { error: sr.error }), 'failed');
          return false;
        }
        reportSaved(sr);
      } else if (r !== 'discard') {
        return false;
      }
    }
    const cur = S.docs.find((x) => x.id === d.id);
    if (!cur) return false; // 대화상자가 떠 있는 동안 이미 닫힘
    if (S.doc && S.doc.id === cur.id) {
      const i = S.docs.indexOf(cur);
      const next = S.docs[i + 1] || S.docs[i - 1];
      if (next) activateDoc(next);
      else showNone();
    }
    removeDoc(cur);
    afterChange();
    return true;
  }

  // 수정하지 않은 탭을 모두 닫습니다 (저장된 탭 정리).
  function closeClean() {
    if (!canSwitch()) return;
    const keep = S.docs.filter(isDirty);
    const clean = S.docs.filter((d) => !isDirty(d));
    if (!clean.length) return;
    if (S.doc && clean.some((d) => d.id === S.doc.id)) {
      const i = S.docs.findIndex((d) => d.id === S.doc.id);
      const near = keep.find((d) => S.docs.indexOf(d) > i) || keep[keep.length - 1];
      if (near) activateDoc(near); else showNone();
    }
    for (const d of clean) removeDoc(d);
    afterChange();
    hooks.toast(tn('toast.closed_clean', clean.length));
  }

  // 목록에 파일을 더하고 첫 파일을 엽니다 (탐색 열기). 열 수 있는 경로 목록을 돌려줍니다.
  async function addAndOpen(paths, mode = 'nav') {
    const list = await hooks.addPaths(paths);
    if (list && list.length) await openPath(list[0], mode);
    return list || [];
  }
  async function addFilesDialog() {
    if (S.cur || S.fx) return;
    const paths = await PickFiles();
    if (paths && paths.length) await addAndOpen(paths);
  }
  async function addFolderDialog() {
    if (S.cur || S.fx) return;
    const dir = await PickFolder(tr('dialog.pick_image_dir'));
    if (dir) await addAndOpen([dir]);
  }

  async function openDialog() {
    if (S.cur || S.fx) return;
    const paths = await PickFiles();
    if (paths && paths.length) await addAndOpen(paths, 'pin');
  }

  async function newDialog() {
    if (S.cur || S.fx) return;
    let wIn, hIn;
    const r = await modal({
      title: tr('editor.new.title_dialog'),
      build: (b) => {
        wIn = el('input'); wIn.type = 'number'; wIn.min = 1; wIn.max = MAX_SIDE; wIn.value = 32;
        hIn = el('input'); hIn.type = 'number'; hIn.min = 1; hIn.max = MAX_SIDE; hIn.value = 32;
        const row = el('div', 'ed-newrow');
        const f = (label, input) => { const l = el('label', 'ed-newfield'); l.append(el('span', 'field-label', label), input); return l; };
        row.append(f(tr('editor.field.width'), wIn), el('span', 'muted', '×'), f(tr('editor.field.height'), hIn));
        const presets = el('div', 'chips');
        for (const n of [16, 32, 64, 128, 256]) {
          const c = el('button', 'chip', `${n}×${n}`);
          c.type = 'button';
          c.onclick = () => { wIn.value = n; hIn.value = n; };
          presets.append(c);
        }
        b.append(row, presets, el('p', 'muted small', tr('editor.new.note')));
      },
      buttons: [
        { label: tr('dialog.cancel'), value: null },
        {
          label: tr('dialog.create'), value: 'ok', primary: true,
          validate: () => {
            const w = Number(wIn.value), h = Number(hIn.value);
            const ok = Number.isInteger(w) && Number.isInteger(h) && w >= 1 && h >= 1 && w <= MAX_SIDE && h <= MAX_SIDE && w * h <= MAX_PIXELS;
            if (!ok) hooks.toast(tr('common.size_limit', { max: MAX_SIDE, mp: MAX_PIXELS / 1e6 | 0 }), 'failed');
            return ok;
          },
        },
      ],
    });
    if (r !== 'ok' || S.cur || S.fx) return;
    const w = Number(wIn.value), h = Number(hIn.value);
    const doc = makeDoc(w, h, new Uint8ClampedArray(w * h * 4), '', newName());
    insertDoc(doc);
    activateDoc(doc);
  }

  // 문서 하나를 저장합니다. as 이거나 경로가 없으면 저장 대화상자를 엽니다.
  // 반환: { ok, canceled, error, res, isNewPath, backup, name } — 토스트는 부르는 쪽이 정합니다.
  async function saveDoc(d, as = false) {
    if (S.saving) return { ok: false, error: tr('toast.save_busy') };
    S.saving = true;
    try {
      if (S.doc && S.doc.id === d.id && !S.fx) commitFloat();
      const pixels = toBase64(d.pix);
      const backup = !!hooks.getBackup();
      const h0 = d.history;
      const mark = { len: h0.undoStack.length, last: h0.undoStack[h0.undoStack.length - 1] };
      let res;
      if (!as && d.path) res = await SaveImageRGBA(d.path, d.w, d.h, pixels, backup);
      else res = await SaveImageAs(d.path, d.path ? baseName(d.path) : d.name, d.w, d.h, pixels, backup);
      if (res.canceled) return { ok: false, canceled: true };
      const cur = S.docs.find((x) => x.id === d.id) || d;
      const isNewPath = !sameFile(cur.path, res.path);
      cur.path = res.path;
      cur.name = baseName(res.path);
      cur.indexed = res.format === 'indexed';
      cur.palSize = res.colors || 0;
      // 저장하는 동안 더 고쳤다면 저장된 시점이 아니므로 계속 "변경됨"으로 둡니다.
      const h = cur.history;
      if (h.undoStack.length === mark.len && h.undoStack[mark.len - 1] === mark.last) h.markSaved();
      afterChange();
      const actions = hooks.onSaved(res.path, { isNewPath, backup: res.backup }) || [];
      return { ok: true, res, isNewPath, backup: !!res.backup, name: cur.name, actions };
    } catch (err) {
      return { ok: false, error: String(err) };
    } finally {
      S.saving = false;
    }
  }

  function reportSaved(r) {
    const format = r.res.format === 'indexed' ? tn('toast.format_indexed', r.res.colors) : 'RGBA PNG';
    hooks.toast(tr(r.backup ? 'toast.saved_backup' : 'toast.saved', { name: r.name, format }), '', r.actions);
  }

  // 현재 문서를 저장합니다. 성공하면 true.
  async function save(as) {
    if (S.fx || S.cur) return false;
    const d = S.doc;
    if (!d) return false;
    const r = await saveDoc(d, as);
    if (r.ok) { reportSaved(r); return true; }
    if (r.error) hooks.toast(tr('toast.save_failed', { error: r.error }), 'failed');
    return false;
  }

  // 저장하지 않은 문서를 모두 저장합니다. 경로가 없는 새 문서(제목 없음)는 저장 대화상자를 연달아 띄우지 않고 건너뛰며,
  // 건너뛴 문서는 알림의 버튼으로 바로 저장할 수 있습니다. 하나가 실패해도 나머지는 계속 저장합니다.
  async function saveAll() {
    if (S.fx) { canSwitch(); return; }
    if (S.cur || S.saving) return;
    commitFloat();
    const targets = dirtyDocs();
    if (!targets.length) { hooks.toast(tr('toast.nothing_to_save')); return; }
    let saved = 0, backups = 0;
    const skipped = [], failed = [];
    for (const d of targets) {
      if (!d.path) { skipped.push(d); continue; }
      const r = await saveDoc(d, false);
      if (r.ok) { saved++; if (r.backup) backups++; }
      else failed.push(`${d.name}: ${r.error || tr('status.canceled')}`);
    }
    const parts = saved ? [backups ? tr('toast.saved_all_backup', { n: saved, b: backups }) : tr('toast.saved_all', { n: saved })] : [];
    if (skipped.length) parts.push(tn('toast.saved_all_skipped', skipped.length));
    if (failed.length) parts.push(tr('toast.saved_all_failed', { n: failed.length, first: failed[0] }));
    const actions = [];
    if (skipped.length) {
      actions.push({ label: tr('toast.action.save_skipped'), run: async () => { if (switchTo(skipped[0])) await save(true); } });
    }
    hooks.toast(parts.join(' · '), failed.length ? 'failed' : '', actions);
  }

  // 편집기에 파일·폴더를 끌어다 놓았을 때: 공유 목록에 더하고 첫 파일을 엽니다.
  async function dropPaths(paths) {
    if (S.cur || S.fx) return;
    const list = await addAndOpen(paths);
    if (!list.length) hooks.toast(tr('toast.no_png'), 'failed');
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

    // 파일·문서 이동은 어느 도구에서나 됩니다 (글자를 입력하는 칸에서는 제외)
    if (!entering && !ctrl && !e.shiftKey) {
      if (!e.altKey && (e.key === 'PageDown' || e.key === 'PageUp')) { e.preventDefault(); step(e.key === 'PageDown' ? 1 : -1); return; }
      if (e.altKey && (e.key === 'ArrowRight' || e.key === 'ArrowLeft')) { e.preventDefault(); step(e.key === 'ArrowRight' ? 1 : -1); return; }
    }
    if (ctrl && e.key === 'Tab') { e.preventDefault(); cycleDoc(e.shiftKey ? -1 : 1); return; }
    if (ctrl && !e.altKey && !e.shiftKey && k === 'w') {
      e.preventDefault();
      const cur = S.doc && S.docs.find((d) => d.id === S.doc.id);
      if (cur) closeDoc(cur);
      return;
    }
    if (ctrl && e.altKey && !e.shiftKey && k === 's') { e.preventDefault(); saveAll(); return; }

    // 효과 미리보기 중에는 확인/취소만 받습니다.
    if (S.fx) {
      if (e.key === 'Escape') { e.preventDefault(); cancelFx(); }
      else if (e.key === 'Enter' && t.tagName !== 'BUTTON' && t.tagName !== 'SELECT') { e.preventDefault(); applyEffect(); }
      return;
    }

    if (ctrl && !e.altKey) {
      if (k === 'z' || k === 'y') {
        if (entering) return;
        e.preventDefault();
        if (k === 'y' || e.shiftKey) redo(); else undo();
      } else if (k === 's') { e.preventDefault(); save(e.shiftKey); }
      else if (k === 'o') { e.preventDefault(); openDialog(); }
      else if (k === 'n') { e.preventDefault(); newDialog(); }
      else if (e.key === "'") { e.preventDefault(); P.grid = !P.grid; savePrefs(); syncTop(); render(); }
      else if (!entering && S.doc && k === 'a') { e.preventDefault(); selectAll(); }
      else if (!entering && S.doc && k === 'd') { e.preventDefault(); deselect(); }
      else if (!entering && S.doc && k === 'i' && e.shiftKey) { e.preventDefault(); invertSelection(); }
      else if (!entering && S.doc && k === 'c') { e.preventDefault(); if (!busy()) copySelection(); }
      else if (!entering && S.doc && k === 'x') { e.preventDefault(); cutSelection(); }
      else if (!entering && S.doc && k === 'v') { e.preventDefault(); pasteClip(); }
      return;
    }
    if (entering) return;
    if (e.altKey || e.metaKey) return;

    if (e.key === 'Escape') {
      if (R.menu) closeMenu();
      else if (S.pickerSlot) closePicker();
      else if (S.cur) { cancelStroke(); S.drag = null; render(); }
      else if (S.drag && S.drag.kind === 'select') { S.drag = null; syncSel(); updateCursor(); }
      else if (S.float) { S.drag = null; cancelFloat(); }
      return;
    }
    if (e.key === 'Enter') {
      if (S.float && t.tagName !== 'BUTTON') { e.preventDefault(); S.drag = null; commitFloat(); }
      return;
    }
    if (S.doc && (e.key === 'Delete' || e.key === 'Backspace')) {
      e.preventDefault();
      if (!busy()) deleteSelection();
      return;
    }
    if (S.doc && e.key.startsWith('Arrow')) {
      e.preventDefault();
      const n = e.shiftKey ? 10 : 1;
      nudge(e.key === 'ArrowLeft' ? -n : e.key === 'ArrowRight' ? n : 0, e.key === 'ArrowUp' ? -n : e.key === 'ArrowDown' ? n : 0);
      return;
    }
    if (e.code === 'Space') {
      e.preventDefault();
      if (!S.space) { S.space = true; R.view.classList.add('pan-ready'); updateCursor(); }
      return;
    }
    if (e.repeat && !'[]'.includes(e.key)) return;
    if (S.doc && e.shiftKey && (k === 'h' || k === 'v')) { e.preventDefault(); transform(k === 'h' ? 'flipH' : 'flipV'); return; }
    let handled = true;
    switch (e.key) {
      case 'b': case 'B': setTool('pencil'); break;
      case 'e': case 'E': setTool('eraser'); break;
      case 'g': case 'G': setTool('fill'); break;
      case 'i': case 'I': setTool('picker'); break;
      case 'l': case 'L': setTool('line'); break;
      case 'm': case 'M': setTool('select'); break;
      case 'v': setTool('move'); break;
      case 'w': case 'W': setTool('wand'); break;
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
  syncDocs(true);
  renderPal();
  refreshPalList();

  return {
    // 파일을 엽니다. 기본은 탐색 열기(미리보기 탭), opt.pin 이면 고정된 새 탭
    open: (path, opt = {}) => openPath(path, opt.pin ? 'pin' : 'nav'),
    openDialog,
    newDialog,
    saveAll,
    dropPaths,
    isDirty: () => dirtyDocs().length > 0,
    dirtyCount: () => dirtyDocs().length,
    currentPath: () => S.doc?.path || '',
    // 일괄 처리 목록이 바뀌었을 때(추가·제거·필터·썸네일 갱신) 파일 띠를 다시 그립니다.
    filesChanged: () => R.strip.render(),
    // Called by main.js after the language changed and the Go language has been updated:
    // re-applies every label and re-renders the dynamic parts (the effect preview is recomputed in the new language).
    relocalize() {
      relabel();
      closeMenu();
      syncTools();
      syncColors();
      syncTop();
      syncSel();
      renderInfo(false);
      renderPal();
      syncDocs(true);
      if (S.fx) {
        S.fx.showOrig = false;
        buildFxPanel(S.fx);
      }
    },
    activate(on) {
      S.active = on;
      if (on) {
        measure();
        syncTop();
        syncDocs(true);
        if (S.doc && S.autoFit) fit();
        else render();
      } else {
        closePicker();
        closeMenu();
        cancelFx();
      }
    },
    // 개발·검증용: 현재 문서 상태 확인
    debug: () => ({ doc: S.doc, P, view: S.view }),
  };
}
