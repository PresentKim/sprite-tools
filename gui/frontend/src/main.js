import './style.css';
import {
  Cancel, DefaultSuffix, Describe, ExpandPaths, ListPalettes, LoadPalette,
  PickFiles, PickFolder, PickPaletteFile, Preview, PreviewPack, Reveal, Run, RunPack,
} from '../wailsjs/go/main/App';
import { BrowserOpenURL, EventsOn, OnFileDrop } from '../wailsjs/runtime/runtime';

// out: 결과를 저장하는 방식 — file(접미사/다른 폴더/덮어쓰기), extract(덮어쓰기 제외), slice(프레임 폴더), pack(저장 시 파일 선택)
// 순서가 Alt+1..9 단축키 순서이고, group 이 바뀌는 곳에 탭 구분선이 들어갑니다.
const COMMANDS = [
  { id: 'reduce', group: 0, label: '색상 줄이기', out: 'file', desc: '비슷한 색상을 합쳐 색상 수를 줄입니다. 많이 쓰인 색이 대표 색으로 남습니다.' },
  { id: 'recolor', group: 0, label: '팔레트 적용', out: 'file', desc: '모든 색을 선택한 팔레트에서 가장 가까운 색으로 바꿉니다.' },
  { id: 'index', group: 0, label: '인덱스 PNG', out: 'file', desc: '팔레트(인덱스) PNG로 바꾸고 0번 색을 투명(#ffffff00)으로 맞춥니다.' },
  { id: 'extract', group: 0, label: '팔레트 추출', out: 'extract', desc: '이미지에 쓰인 색을 팔레트 PNG로 뽑습니다. 이름을 주면 팔레트 목록에도 저장됩니다.' },
  { id: 'downscale', group: 1, label: '1:1 축소', out: 'file', desc: '확대해서 저장된 픽셀 아트를 원래 크기로 되돌립니다.' },
  { id: 'upscale', group: 1, label: '확대', out: 'file', desc: '정수 배율로 픽셀을 그대로 키웁니다(최근접 이웃).' },
  { id: 'trim', group: 1, label: '여백 자르기', out: 'file', desc: '투명한 바깥 여백을 잘라냅니다. 필요하면 일정한 여백을 남길 수 있습니다.' },
  { id: 'slice', group: 2, label: '프레임 자르기', out: 'slice', desc: '스프라이트 시트를 같은 크기의 프레임으로 잘라 각각 저장합니다.' },
  { id: 'pack', group: 2, label: '시트 합치기', out: 'pack', desc: '목록의 모든 이미지를 한 장의 스프라이트 시트로 합칩니다(프레임 자르기의 반대).' },
];
const FRAME_PRESETS = ['8', '16', '24', '32', '48', '64', '16x32'];
const SCALES = [0, 2, 3, 4, 5, 6, 8, 10, 12, 16];
const UPSCALES = [2, 3, 4, 5, 6, 8, 10, 12, 16];
const ZOOMS = [['fit', '맞춤'], [1, '1x'], [2, '2x'], [4, '4x'], [8, '8x']];
const STATUS_LABEL = { done: '완료', skipped: '건너뜀', failed: '실패', running: '처리 중' };

const $ = (id) => document.getElementById(id);
const el = (tag, cls, text) => {
  const e = document.createElement(tag);
  if (cls) e.className = cls;
  if (text != null) e.textContent = text;
  return e;
};

// ---------- 상태 ----------

const defaults = {
  command: 'reduce',
  threshold: 0.05,
  reduceMode: 'threshold', // threshold | count
  maxColors: 16,
  frameSize: '16',
  keepEmpty: false,
  outDir: '', // 프레임 자르기의 프레임 저장 폴더
  scale: 0,
  upscale: 2,
  pad: 0,
  packCols: 0,
  packGap: 0,
  packAlign: 'topleft', // topleft | center
  palette: '',
  outMode: 'suffix', // suffix | dir | inplace
  exportDir: '', // 다른 폴더에 저장할 때의 폴더
  suffixes: {},
  threads: 0,
  recursive: true,
  zoom: 'fit',
};
const settings = loadSettings();
const state = {
  files: [], // {path,name,dir,width,height,bytes,error,status,message,v}
  selected: null,
  running: false,
  palettes: [],
};

function loadSettings() {
  try {
    const stored = JSON.parse(localStorage.getItem('sprite-tools') || '{}');
    const s = { ...defaults, ...stored };
    // 예전 설정(inPlace 불리언)을 저장 방식으로 옮깁니다.
    if (!stored.outMode && stored.inPlace) s.outMode = 'inplace';
    return s;
  } catch {
    return { ...defaults };
  }
}
function saveSettings() {
  try { localStorage.setItem('sprite-tools', JSON.stringify(settings)); } catch { /* 저장 실패는 무시 */ }
}

async function suffixFor(cmd) {
  if (settings.suffixes[cmd] == null) settings.suffixes[cmd] = await DefaultSuffix(cmd);
  return settings.suffixes[cmd];
}

function cmdInfo() {
  return COMMANDS.find((c) => c.id === settings.command) || COMMANDS[0];
}

// 현재 명령에 실제로 적용되는 저장 방식입니다. 슬라이스/합치기는 따로 저장 위치를 정하고, 팔레트 추출은 덮어쓰기를 지원하지 않습니다.
function effMode() {
  const out = cmdInfo().out;
  if (out === 'slice' || out === 'pack') return 'none';
  if (out === 'extract' && settings.outMode === 'inplace') return 'suffix';
  return settings.outMode;
}

function request() {
  const cmd = settings.command;
  const mode = effMode();
  return {
    command: cmd,
    threshold: Number(settings.threshold),
    maxColors: settings.reduceMode === 'count' ? Math.round(Number(settings.maxColors)) || 16 : 0,
    scale: cmd === 'upscale' ? Number(settings.upscale) : Number(settings.scale),
    frameSize: settings.frameSize,
    outDir: cmd === 'slice' ? settings.outDir : mode === 'dir' ? settings.exportDir : '',
    keepEmpty: settings.keepEmpty,
    palette: settings.palette,
    suffix: settings.suffixes[cmd] ?? '',
    inPlace: mode === 'inplace',
    threads: Number(settings.threads) || 0,
    pad: Math.max(0, Math.round(Number(settings.pad)) || 0),
    saveName: cmd === 'extract' ? $('save-name').value.trim() : '',
    force: cmd === 'extract' && $('save-force').checked,
    cols: Math.max(0, Math.round(Number(settings.packCols)) || 0),
    gap: Math.max(0, Math.round(Number(settings.packGap)) || 0),
    align: settings.packAlign,
  };
}

const localUrl = (f) => `/local?path=${encodeURIComponent(f.path)}&v=${f.v || 0}`;
const fmtBytes = (n) => (n < 1024 ? `${n} B` : n < 1048576 ? `${(n / 1024).toFixed(1)} KB` : `${(n / 1048576).toFixed(1)} MB`);

// ---------- 탭·옵션 ----------

function renderTabs() {
  const tabs = $('tabs');
  tabs.replaceChildren();
  COMMANDS.forEach((c, i) => {
    if (i > 0 && c.group !== COMMANDS[i - 1].group) tabs.append(el('span', 'tab-sep'));
    const b = el('button', 'tab');
    b.setAttribute('role', 'tab');
    b.setAttribute('aria-selected', String(c.id === settings.command));
    b.title = `${c.label} (Alt+${i + 1})`;
    b.append(c.label);
    b.onclick = () => setCommand(c.id);
    tabs.append(b);
  });
}

async function setCommand(id) {
  settings.command = id;
  saveSettings();
  renderTabs();
  const c = COMMANDS.find((x) => x.id === id);
  $('cmd-title').textContent = c.label;
  $('cmd-desc').textContent = c.desc;
  document.querySelectorAll('[data-cmd]').forEach((g) => { g.hidden = g.dataset.cmd !== id; });
  $('output-group').hidden = c.out === 'pack';
  $('output-file').hidden = c.out === 'slice' || c.out === 'pack';
  $('output-dir').hidden = c.out !== 'slice';
  $('inplace-radio').hidden = c.out === 'extract';
  $('suffix').value = await suffixFor(id);
  if (id === 'recolor') refreshPalettes(); // 팔레트 추출에서 저장한 팔레트가 바로 보이도록 매번 갱신
  renderOutput();
  renderRunButton();
  schedulePreview();
}

function renderOutput() {
  const mode = effMode();
  document.querySelectorAll('input[name="out-mode"]').forEach((r) => { r.checked = r.value === mode; });
  $('suffix-box').hidden = mode === 'inplace';
  $('export-dir-box').hidden = mode !== 'dir';
  $('inplace-warn').hidden = mode !== 'inplace';
  const sfx = settings.suffixes[settings.command] ?? '';
  if (mode === 'dir') {
    $('suffix-example').textContent = `hero.png → (폴더)/hero${sfx}.png` + (sfx ? '' : ' · 접미사는 비워도 됩니다');
  } else {
    $('suffix-example').textContent = sfx ? `hero.png → hero${sfx}.png` : '접미사를 입력하세요';
  }
  $('export-dir-label').textContent = settings.exportDir || '폴더를 선택하세요';
  $('export-dir-label').title = settings.exportDir;
  $('out-dir-label').textContent = settings.outDir || '원본과 같은 폴더';
  $('out-dir-label').title = settings.outDir;
}

function initOptions() {
  const th = $('threshold');
  th.value = settings.threshold;
  const showTh = () => { $('threshold-value').textContent = Number(settings.threshold).toFixed(3); };
  showTh();
  th.oninput = () => { settings.threshold = Number(th.value); showTh(); saveSettings(); schedulePreview(); };

  const mc = $('max-colors');
  mc.value = settings.maxColors;
  const showMode = () => {
    $('reduce-threshold').hidden = settings.reduceMode !== 'threshold';
    $('reduce-count').hidden = settings.reduceMode !== 'count';
    $('reduce-mode').querySelectorAll('button').forEach((b) => b.classList.toggle('on', b.dataset.mode === settings.reduceMode));
  };
  showMode();
  $('reduce-mode').querySelectorAll('button').forEach((b) => {
    b.onclick = () => { settings.reduceMode = b.dataset.mode; showMode(); saveSettings(); schedulePreview(); };
  });
  mc.oninput = () => {
    const v = Math.round(Number(mc.value));
    if (!(v >= 1)) return; // 입력 중인 빈 값은 무시
    settings.maxColors = Math.min(256, v);
    saveSettings();
    schedulePreview();
  };

  const fs = $('frame-size');
  fs.value = settings.frameSize;
  const presets = $('frame-presets');
  const markPreset = () => presets.querySelectorAll('.chip').forEach((c) => c.classList.toggle('on', c.textContent === settings.frameSize));
  FRAME_PRESETS.forEach((p) => {
    const c = el('button', 'chip', p);
    c.onclick = () => { settings.frameSize = p; fs.value = p; markPreset(); saveSettings(); schedulePreview(); };
    presets.append(c);
  });
  markPreset();
  fs.oninput = () => { settings.frameSize = fs.value.trim(); markPreset(); saveSettings(); schedulePreview(); };

  $('keep-empty').checked = settings.keepEmpty;
  $('keep-empty').onchange = (e) => { settings.keepEmpty = e.target.checked; saveSettings(); schedulePreview(); };

  const sc = $('scale');
  SCALES.forEach((s) => sc.append(new Option(s === 0 ? '자동 감지' : `${s}배`, s)));
  sc.value = settings.scale;
  sc.onchange = () => { settings.scale = Number(sc.value); saveSettings(); schedulePreview(); };

  const up = $('upscale-scale');
  UPSCALES.forEach((s) => up.append(new Option(`${s}배`, s)));
  up.value = settings.upscale;
  up.onchange = () => { settings.upscale = Number(up.value); saveSettings(); schedulePreview(); };

  const numberSetting = (id, key, max) => {
    const input = $(id);
    input.value = settings[key];
    input.oninput = () => {
      const v = Math.round(Number(input.value));
      if (!(v >= 0)) return; // 입력 중인 빈 값은 무시
      settings[key] = Math.min(max, v);
      saveSettings();
      schedulePreview();
    };
  };
  numberSetting('trim-pad', 'pad', 256);
  numberSetting('pack-cols', 'packCols', 256);
  numberSetting('pack-gap', 'packGap', 64);
  const showAlign = () => $('pack-align').querySelectorAll('button').forEach((b) => b.classList.toggle('on', b.dataset.align === settings.packAlign));
  showAlign();
  $('pack-align').querySelectorAll('button').forEach((b) => {
    b.onclick = () => { settings.packAlign = b.dataset.align; showAlign(); saveSettings(); schedulePreview(); };
  });

  // 팔레트 저장 이름/덮어쓰기는 저장하지 않고 실행할 때만 읽습니다.
  $('save-name').onkeydown = (e) => { if (e.key === 'Enter') startRun(); };

  document.querySelectorAll('input[name="out-mode"]').forEach((r) => {
    r.onchange = () => { if (r.checked) { settings.outMode = r.value; saveSettings(); renderOutput(); } };
  });
  $('export-dir-pick').onclick = async () => {
    const dir = await PickFolder('저장할 폴더 선택');
    if (dir) { settings.exportDir = dir; saveSettings(); renderOutput(); }
  };
  $('suffix').oninput = (e) => { settings.suffixes[settings.command] = e.target.value; saveSettings(); renderOutput(); };

  $('out-dir-pick').onclick = async () => {
    const dir = await PickFolder('프레임 저장 폴더 선택');
    if (dir) { settings.outDir = dir; saveSettings(); renderOutput(); }
  };
  $('out-dir-clear').onclick = () => { settings.outDir = ''; saveSettings(); renderOutput(); };

  $('threads').value = settings.threads;
  $('threads').oninput = (e) => { settings.threads = Math.max(0, Number(e.target.value) || 0); saveSettings(); };

  $('recursive').checked = settings.recursive;
  $('recursive').onchange = (e) => { settings.recursive = e.target.checked; saveSettings(); };

  // 팔레트
  $('palette-select').onchange = (e) => selectPalette(e.target.value);
  const fetchPalette = async () => {
    const name = $('palette-name').value.trim();
    if (!name) return;
    $('palette-fetch').disabled = true;
    $('palette-info').textContent = '불러오는 중…';
    try {
      const p = await LoadPalette(name);
      $('palette-name').value = '';
      await refreshPalettes(p.name);
    } catch (err) {
      $('palette-info').textContent = String(err);
      $('palette-info').className = 'small warn';
    } finally {
      $('palette-fetch').disabled = false;
    }
  };
  $('palette-fetch').onclick = fetchPalette;
  $('palette-name').onkeydown = (e) => { if (e.key === 'Enter') fetchPalette(); };
  $('palette-file').onclick = async () => {
    const f = await PickPaletteFile();
    if (!f) return;
    try {
      const p = await LoadPalette(f);
      state.palettes = state.palettes.filter((x) => x.name !== p.name).concat(p);
      renderPaletteSelect();
      selectPalette(p.name);
    } catch (err) {
      toast(String(err), 'failed');
    }
  };
  $('palette-browse').onclick = () => BrowserOpenURL('https://lospec.com/palette-list');

  // 확대
  const seg = $('zoom-seg');
  ZOOMS.forEach(([z, label]) => {
    const b = el('button', '', label);
    b.onclick = () => { settings.zoom = z; saveSettings(); renderZoom(); layoutImages(); };
    b.dataset.zoom = z;
    seg.append(b);
  });
  renderZoom();

  $('reveal-btn').onclick = () => { const f = selectedFile(); if (f) Reveal(f.path); };
}

function renderZoom() {
  $('zoom-seg').querySelectorAll('button').forEach((b) => b.classList.toggle('on', String(settings.zoom) === b.dataset.zoom));
}

// ---------- 팔레트 ----------

async function refreshPalettes(select) {
  try {
    const list = await ListPalettes();
    const custom = state.palettes.filter((p) => p.name.toLowerCase().endsWith('.png'));
    // 지난번에 PNG 파일로 고른 팔레트도 다시 불러옵니다.
    if (settings.palette.toLowerCase().endsWith('.png') && !custom.some((p) => p.name === settings.palette)) {
      try { custom.push(await LoadPalette(settings.palette)); } catch { /* 파일이 사라졌으면 무시 */ }
    }
    state.palettes = [...(list || []), ...custom];
  } catch (err) {
    toast(String(err), 'failed');
  }
  renderPaletteSelect();
  const want = select || settings.palette;
  if (state.palettes.some((p) => p.name === want)) selectPalette(want);
  else if (state.palettes.length) selectPalette(state.palettes[0].name);
  else renderSwatches(null);
}

function renderPaletteSelect() {
  const s = $('palette-select');
  s.replaceChildren();
  if (!state.palettes.length) s.append(new Option('저장된 팔레트 없음', ''));
  state.palettes.forEach((p) => {
    const label = p.name.toLowerCase().endsWith('.png') ? p.name.split(/[\\/]/).pop() : p.name;
    s.append(new Option(`${label} (${p.colors.length}색)`, p.name));
  });
  s.value = settings.palette;
}

function selectPalette(name) {
  settings.palette = name;
  saveSettings();
  $('palette-select').value = name;
  renderSwatches(state.palettes.find((p) => p.name === name));
  schedulePreview();
}

function renderSwatches(p) {
  const box = $('palette-swatches');
  box.replaceChildren();
  const info = $('palette-info');
  info.className = 'muted small';
  if (!p) {
    info.textContent = 'Lospec 팔레트 이름을 입력해 불러오세요. 한 번 받은 팔레트는 오프라인에서도 쓸 수 있습니다.';
    return;
  }
  p.colors.forEach((c) => {
    const s = el('span');
    s.style.background = c;
    s.title = c;
    box.append(s);
  });
  info.textContent = `${p.colors.length}색 · ${p.path}`;
}

// ---------- 파일 목록 ----------

async function addPaths(paths) {
  if (!paths || !paths.length) return;
  try {
    const entries = await ExpandPaths(paths, settings.recursive);
    const known = new Set(state.files.map((f) => f.path.toLowerCase()));
    const fresh = (entries || []).filter((e) => !known.has(e.path.toLowerCase()));
    state.files.push(...fresh.map((e) => ({ ...e, status: e.error ? 'failed' : '', message: e.error || '', v: Date.now() })));
    if (!fresh.length) toast(entries && entries.length ? '이미 목록에 있는 파일입니다' : 'PNG 파일을 찾지 못했습니다');
    else if (!state.selected) state.selected = (fresh.find((f) => !f.error) || fresh[0]).path;
    renderFiles();
    schedulePreview();
  } catch (err) {
    toast(String(err), 'failed');
  }
}

function selectedFile() {
  return state.files.find((f) => f.path === state.selected) || null;
}

function select(path, scroll) {
  if (state.selected === path) return;
  state.selected = path;
  document.querySelectorAll('.file').forEach((r) => r.classList.toggle('selected', r.dataset.path === path));
  if (scroll) document.querySelector('.file.selected')?.scrollIntoView({ block: 'nearest' });
  schedulePreview();
}

function removeFile(path) {
  const i = state.files.findIndex((f) => f.path === path);
  if (i < 0) return;
  state.files.splice(i, 1);
  if (state.selected === path) state.selected = state.files[Math.min(i, state.files.length - 1)]?.path ?? null;
  renderFiles();
  schedulePreview();
}

function fileRow(f) {
  const row = el('div', 'file');
  row.dataset.path = f.path;
  if (f.path === state.selected) row.classList.add('selected');

  const thumb = el('div', 'thumb');
  const img = el('img');
  img.loading = 'lazy';
  img.draggable = false;
  img.onerror = () => img.remove();
  if (!f.error) {
    img.src = localUrl(f);
    thumb.append(img);
  } else {
    thumb.append(el('span', 'thumb-bad', '!'));
  }

  const main = el('div', 'file-main');
  main.append(el('div', 'file-name', f.name));
  const dims = f.width ? `${f.width}×${f.height} · ${fmtBytes(f.bytes)}` : '';
  const sub = el('div', 'file-sub', dims ? `${dims} · ${f.dir}` : f.dir);
  sub.title = f.path;
  main.append(sub);
  if (f.message) main.append(el('div', `file-msg ${f.status}`, f.message));

  const badge = f.status ? el('span', `badge ${f.status}`, STATUS_LABEL[f.status]) : el('span');
  const rm = el('button', 'remove', '×');
  rm.title = '목록에서 제거 (Delete)';
  rm.onclick = (e) => { e.stopPropagation(); removeFile(f.path); };

  row.append(thumb, main, badge, rm);
  row.onclick = () => select(f.path);
  row.ondblclick = () => Reveal(f.path);
  return row;
}

function renderFiles() {
  const list = $('file-list');
  list.replaceChildren(...state.files.map(fileRow));
  $('empty').hidden = state.files.length > 0;
  $('file-count').textContent = state.files.length ? `${state.files.length}개` : '';
  renderRunButton();
}

function updateRow(f) {
  const old = document.querySelector(`.file[data-path="${CSS.escape(f.path)}"]`);
  if (old) old.replaceWith(fileRow(f));
}

// ---------- 미리보기 ----------

let previewTimer = 0;
let previewSeq = 0;

function schedulePreview() {
  clearTimeout(previewTimer);
  previewTimer = setTimeout(runPreview, 120);
}

async function runPreview() {
  const f = selectedFile();
  const packing = settings.command === 'pack';
  const valid = state.files.filter((x) => !x.error);
  const seq = ++previewSeq;
  const origImg = $('orig-img');
  const resImg = $('result-img');
  const msg = $('result-msg');

  $('reveal-btn').disabled = !f;
  if (!f || (packing && !valid.length)) {
    $('preview-name').textContent = '미리보기';
    origImg.hidden = resImg.hidden = true;
    $('orig-meta').textContent = $('result-meta').textContent = '';
    setMsg(msg, '', '파일을 선택하면 처리 결과를 미리 볼 수 있습니다');
    $('result-colors').replaceChildren();
    return;
  }

  $('preview-name').textContent = packing ? `시트 미리보기 · ${valid.length}개 합침` : f.name;
  $('preview-name').title = f.path;
  const origSrc = localUrl(f);
  if (origImg.dataset.src !== origSrc) {
    origImg.dataset.src = origSrc;
    origImg.src = origSrc;
  }
  origImg.dataset.w = f.width;
  origImg.dataset.h = f.height;
  origImg.hidden = !f.width;
  $('orig-meta').textContent = f.width ? `${f.width}×${f.height}` : '';

  $('spinner').classList.add('on');
  let res;
  try {
    res = packing ? await PreviewPack(request(), valid.map((x) => x.path)) : await Preview(request(), f.path);
  } catch (err) {
    res = { status: 'failed', message: String(err) };
  }
  if (seq !== previewSeq) return; // 더 최신 요청이 있음
  $('spinner').classList.remove('on');

  if (res.status === 'done') {
    resImg.src = res.image;
    resImg.dataset.w = res.width;
    resImg.dataset.h = res.height;
    resImg.hidden = false;
    setMsg(msg, '', '');
    $('result-meta').textContent = `${res.width}×${res.height} · ${res.message} · ${res.millis}ms`;
  } else {
    resImg.hidden = true;
    $('result-meta').textContent = '';
    setMsg(msg, res.status, res.status === 'skipped' ? `건너뜀: ${res.message}` : res.message);
  }
  renderColors(res.colors);
  layoutImages();
}

function setMsg(box, cls, text) {
  box.className = `canvas-msg ${cls}`;
  box.replaceChildren();
  if (text) box.append(el('span', '', text));
}

function renderColors(colors) {
  const box = $('result-colors');
  box.replaceChildren();
  if (!colors || !colors.length) return;
  box.append(el('span', 'label', `결과 색상 ${colors.length}개 (투명 제외)`));
  colors.forEach((c) => {
    const s = el('span', 'sw');
    s.style.background = c;
    s.title = c;
    box.append(s);
  });
}

// 픽셀이 뭉개지지 않도록 가능한 한 정수 배율로 맞춥니다.
function fitScale(w, h, box) {
  const pad = 24;
  const s = Math.min((box.clientWidth - pad) / w, (box.clientHeight - pad) / h);
  if (!isFinite(s) || s <= 0) return 1;
  return s >= 1 ? Math.floor(s) : s;
}

// 두 영역은 크기가 같으므로, 같은 크기의 이미지는 맞춤 모드에서도 같은 배율로 표시됩니다.
function layoutImages() {
  for (const [img, box] of [[$('orig-img'), $('orig-canvas')], [$('result-img'), $('result-canvas')]]) {
    const w = Number(img.dataset.w), h = Number(img.dataset.h);
    if (!w || !h) continue;
    const s = settings.zoom === 'fit' ? fitScale(w, h, box) : Number(settings.zoom);
    img.style.width = `${w * s}px`;
    img.style.height = `${h * s}px`;
  }
}

// ---------- 실행 ----------

function renderRunButton() {
  const b = $('run-btn');
  if (state.running) {
    b.textContent = '취소';
    b.className = 'btn danger run';
    b.disabled = false;
    $('run-hint').textContent = 'Esc';
    return;
  }
  const n = state.files.filter((f) => !f.error).length;
  const label = cmdInfo().label;
  b.textContent = n ? `${label} 실행 · ${n}개` : `${label} 실행`;
  b.className = 'btn primary run';
  b.disabled = n === 0 || !!state.packing;
  $('run-hint').textContent = 'Ctrl+Enter';
}

async function startRun() {
  if (state.running) return;
  const targets = state.files.filter((f) => !f.error);
  if (!targets.length) return;
  if (settings.command === 'recolor' && !settings.palette) {
    toast('먼저 팔레트를 선택하세요', 'failed');
    return;
  }
  if (settings.command === 'pack') {
    await runPack(targets);
    return;
  }
  const mode = effMode();
  if (mode === 'dir' && !settings.exportDir) {
    toast('저장할 폴더를 먼저 선택하세요', 'failed');
    return;
  }
  if (mode === 'suffix' && !(settings.suffixes[settings.command] ?? '')) {
    toast('출력 접미사를 입력하세요', 'failed');
    return;
  }
  if (mode === 'inplace' &&
      !confirm(`원본 ${targets.length}개 파일을 덮어씁니다. 계속할까요?`)) return;

  targets.forEach((f) => { f.status = 'running'; f.message = ''; });
  renderFiles();
  try {
    await Run(request(), targets.map((f) => ({ path: f.path, root: f.root || '' })));
  } catch (err) {
    targets.forEach((f) => { f.status = ''; });
    renderFiles();
    toast(String(err), 'failed');
    return;
  }
  state.running = true;
  state.lastOutput = '';
  renderRunButton();
  setProgress(0, targets.length);
}

// 시트 합치기: 저장 위치를 고른 뒤 목록의 모든 파일을 한 장으로 합칩니다.
async function runPack(targets) {
  if (state.packing) return;
  state.packing = true;
  $('run-btn').disabled = true;
  $('status-text').textContent = '시트 저장 위치를 선택하세요…';
  try {
    const r = await RunPack(request(), targets.map((f) => f.path));
    if (r.canceled) {
      $('status-text').textContent = '취소됨';
    } else {
      $('status-text').textContent = r.message;
      toast(r.message, '', { label: '폴더 열기', run: () => Reveal(r.output) });
    }
  } catch (err) {
    $('status-text').textContent = '실패';
    toast(String(err), 'failed');
  } finally {
    state.packing = false;
    renderRunButton();
  }
}

function setProgress(done, total) {
  $('progress').classList.toggle('on', state.running);
  $('progress-bar').style.width = total ? `${(done / total) * 100}%` : '0';
  if (state.running) $('status-text').textContent = `처리 중… ${done}/${total}`;
}

EventsOn('job:result', (r) => {
  const f = state.files.find((x) => x.path === r.input);
  if (f) {
    f.status = r.status;
    f.message = r.message;
    updateRow(f);
  }
  if (r.status === 'done') state.lastOutput = r.output || r.input;
  setProgress(r.completed, r.total);
});

EventsOn('job:done', async (s) => {
  state.running = false;
  state.files.filter((f) => f.status === 'running').forEach((f) => { f.status = ''; f.message = '취소됨'; });
  setProgress(0, 0);
  renderRunButton();

  // 덮어쓴 파일은 크기·썸네일을 새로 읽습니다.
  const mode = effMode();
  if (mode === 'inplace') {
    const done = state.files.filter((f) => f.status === 'done');
    const fresh = await Describe(done.map((f) => f.path));
    fresh.forEach((e, i) => Object.assign(done[i], e, { root: done[i].root, v: Date.now() }));
  }
  renderFiles();
  schedulePreview();

  const text = `${s.canceled ? '취소됨 · ' : ''}완료 ${s.done} · 건너뜀 ${s.skipped} · 실패 ${s.failed}`;
  $('status-text').textContent = text;
  $('status-right').textContent = `${s.seconds.toFixed(1)}초`;
  let action = null;
  if (mode === 'dir' && s.done && settings.exportDir) {
    action = { label: '폴더 열기', run: () => Reveal(settings.exportDir) };
  } else if (mode === 'suffix' && state.lastOutput) {
    action = { label: '결과 폴더 열기', run: () => Reveal(state.lastOutput) };
  } else if (settings.command === 'slice' && s.done) {
    action = { label: '폴더 열기', run: () => Reveal(settings.outDir || state.lastOutput) };
  }
  toast(text, s.failed ? 'failed' : '', action);

  // 팔레트 추출에서 이름을 주고 저장했다면 팔레트 목록을 새로 읽습니다.
  if (settings.command === 'extract' && s.done) {
    const saved = $('save-name').value.trim().toLowerCase();
    if (saved) await refreshPalettes(saved);
  }
});

// ---------- 공통 UI ----------

let toastTimer = 0;
function toast(text, cls = '', action = null) {
  const t = $('toast');
  t.className = `toast on ${cls}`;
  t.replaceChildren(el('span', '', text));
  if (action) {
    const b = el('button', 'btn small', action.label);
    b.onclick = () => { action.run(); t.className = 'toast'; };
    t.append(b);
  }
  clearTimeout(toastTimer);
  toastTimer = setTimeout(() => { t.className = 'toast'; }, action ? 7000 : 3500);
}

function initGlobal() {
  $('add-files').onclick = async () => addPaths(await PickFiles());
  $('add-folder').onclick = async () => { const d = await PickFolder('이미지 폴더 선택'); if (d) addPaths([d]); };
  $('clear-files').onclick = () => { state.files = []; state.selected = null; renderFiles(); schedulePreview(); };
  $('run-btn').onclick = () => (state.running ? Cancel() : startRun());

  // 파일 드롭 (Wails 가 실제 경로를 전달)
  const overlay = $('drop-overlay');
  let depth = 0;
  window.addEventListener('dragenter', (e) => { e.preventDefault(); depth++; overlay.classList.add('on'); });
  window.addEventListener('dragleave', () => { if (--depth <= 0) { depth = 0; overlay.classList.remove('on'); } });
  window.addEventListener('dragover', (e) => e.preventDefault());
  window.addEventListener('drop', (e) => { e.preventDefault(); depth = 0; overlay.classList.remove('on'); });
  OnFileDrop((_x, _y, paths) => { depth = 0; overlay.classList.remove('on'); addPaths(paths); }, false);

  window.addEventListener('keydown', (e) => {
    const typing = ['INPUT', 'SELECT', 'TEXTAREA'].includes(document.activeElement?.tagName);
    if (e.ctrlKey && e.key === 'Enter') { e.preventDefault(); startRun(); return; }
    if (e.key === 'Escape' && state.running) { Cancel(); return; }
    if (e.altKey && /^[1-9]$/.test(e.key) && Number(e.key) <= COMMANDS.length) { e.preventDefault(); setCommand(COMMANDS[Number(e.key) - 1].id); return; }
    if (typing) return;
    const i = state.files.findIndex((f) => f.path === state.selected);
    if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
      e.preventDefault();
      const next = state.files[Math.max(0, Math.min(state.files.length - 1, i + (e.key === 'ArrowDown' ? 1 : -1)))];
      if (next) select(next.path, true);
    } else if (e.key === 'Delete' && state.selected) {
      removeFile(state.selected);
    }
  });

  new ResizeObserver(layoutImages).observe($('orig-canvas'));
  $('orig-img').onload = layoutImages;
}

// ---------- 시작 ----------

initOptions();
initGlobal();
if (import.meta.env.DEV) window.__addPaths = addPaths; // 개발 모드(브라우저)에서 드롭 대신 사용
renderFiles();
setCommand(COMMANDS.some((c) => c.id === settings.command) ? settings.command : 'reduce');
