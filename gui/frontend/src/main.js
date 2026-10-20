import './style.css';
import {
  Cancel, DefaultSuffix, Describe, ExpandPaths, ListPalettes, LoadPalette,
  PickFiles, PickFolder, PickPaletteFile, Preview, PreviewPack, RestoreBackups, Reveal, Run, RunPack, SetEditorDirtyCount,
  SetLanguage,
} from '../wailsjs/go/main/App';
import { BrowserOpenURL, EventsOn, OnFileDrop } from '../wailsjs/runtime/runtime';
import { createEditor } from './editor/editor.js';
import { DEFAULT_LANGUAGE, applyI18n, getLanguage, languages, onLanguageChange, setLanguage, t, tn } from './i18n/index.js';

// out: how the result is saved — file (suffix / other folder / overwrite), extract (no overwrite), slice (frame folder), pack (file chosen when saving)
// The order is the order of the Alt+1..9 shortcuts; a tab separator is inserted wherever group changes.
// Labels and descriptions live in the catalogs: batch.cmd.<id>.label / batch.cmd.<id>.desc
const COMMANDS = [
  { id: 'reduce', group: 0, out: 'file' },
  { id: 'recolor', group: 0, out: 'file' },
  { id: 'index', group: 0, out: 'file' },
  { id: 'extract', group: 0, out: 'extract' },
  { id: 'downscale', group: 1, out: 'file' },
  { id: 'upscale', group: 1, out: 'file' },
  { id: 'trim', group: 1, out: 'file' },
  { id: 'despeckle', group: 1, out: 'file' },
  { id: 'slice', group: 2, out: 'slice' },
  { id: 'pack', group: 2, out: 'pack' },
];
const cmdLabel = (c) => t(`batch.cmd.${c.id}.label`);
const cmdDesc = (c) => t(`batch.cmd.${c.id}.desc`);
const FRAME_PRESETS = ['8', '16', '24', '32', '48', '64', '16x32'];
const SCALES = [0, 2, 3, 4, 5, 6, 8, 10, 12, 16];
const UPSCALES = [2, 3, 4, 5, 6, 8, 10, 12, 16];
const ZOOMS = ['fit', 1, 2, 4, 8, 16];
const zoomLabel = (z) => (z === 'fit' ? t('batch.zoom.fit') : `${z}x`);
const statusLabel = (s) => t(`batch.file_status.${s}`);
const scaleLabel = (s) => (s === 0 ? t('common.auto_detect') : t('common.scale_option', { n: s }));
const paletteLabel = (p) => tn('common.palette_option', p.colors.length, { name: p.name.toLowerCase().endsWith('.png') ? p.name.split(/[\\/]/).pop() : p.name });

const $ = (id) => document.getElementById(id);
const el = (tag, cls, text) => {
  const e = document.createElement(tag);
  if (cls) e.className = cls;
  if (text != null) e.textContent = text;
  return e;
};

// ---------- State ----------

const defaults = {
  command: 'reduce',
  threshold: 0.05,
  reduceMode: 'threshold', // threshold | count
  maxColors: 16,
  frameSize: '16',
  keepEmpty: false,
  outDir: '', // folder to save frames to for frame slicing
  scale: 0,
  upscale: 2,
  pad: 0,
  minSize: 4, // despeckle: erase pieces smaller than this
  diagonal: true, // despeckle: treat diagonal neighbors as connected
  packCols: 0,
  packGap: 0,
  packAlign: 'topleft', // topleft | center
  palette: '',
  outMode: 'suffix', // suffix | dir | inplace
  exportDir: '', // folder when saving to a different folder
  backup: true, // back up the original before overwriting
  suffixes: {},
  threads: 0,
  recursive: true,
  zoom: 'fit',
  mode: 'batch', // batch | editor - the mode used last
};
const settings = loadSettings();
const state = {
  files: [], // {path,name,dir,width,height,bytes,error,status,message,v}
  selected: null,
  running: false,
  palettes: [],
  filter: '', // file name filter (lowercase)
  onlyShown: false, // when a filter is on, run only the visible files
  backups: [], // backups made by the last overwrite run {path, backup}
  backupDirs: [],
};

function loadSettings() {
  try {
    const stored = JSON.parse(localStorage.getItem('sprite-tools') || '{}');
    const s = { ...defaults, ...stored };
    // Migrate the old setting (inPlace boolean) to the save mode.
    if (!stored.outMode && stored.inPlace) s.outMode = 'inplace';
    return s;
  } catch {
    return { ...defaults };
  }
}
function saveSettings() {
  try { localStorage.setItem('sprite-tools', JSON.stringify(settings)); } catch { /* ignore save failures */ }
}

async function suffixFor(cmd) {
  if (settings.suffixes[cmd] == null) settings.suffixes[cmd] = await DefaultSuffix(cmd);
  return settings.suffixes[cmd];
}

function cmdInfo() {
  return COMMANDS.find((c) => c.id === settings.command) || COMMANDS[0];
}

// The save mode that actually applies to the current command. Slice/pack choose their own save location, and palette extraction does not support overwriting.
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
    backup: mode === 'inplace' && settings.backup,
    threads: Number(settings.threads) || 0,
    pad: Math.max(0, Math.round(Number(settings.pad)) || 0),
    minSize: Math.max(1, Math.round(Number(settings.minSize)) || 4),
    diagonal: !!settings.diagonal,
    saveName: cmd === 'extract' ? $('save-name').value.trim() : '',
    force: cmd === 'extract' && $('save-force').checked,
    cols: Math.max(0, Math.round(Number(settings.packCols)) || 0),
    gap: Math.max(0, Math.round(Number(settings.packGap)) || 0),
    align: settings.packAlign,
  };
}

// The status bar text is kept as a description (catalog key + params, or raw text) so that it can be re-rendered
// when the language changes. n selects the plural form (tn) when given.
let statusLeft = { key: 'status.ready' };
let statusRight = null;
function renderStatus() {
  const text = (s) => (!s ? '' : s.raw != null ? s.raw : s.n != null ? tn(s.key, s.n, s.params) : t(s.key, s.params));
  $('status-text').textContent = text(statusLeft);
  $('status-right').textContent = text(statusRight);
}
function setStatus(key, raw, params, n) {
  statusLeft = key ? { key, params, n } : { raw };
  renderStatus();
}

const localUrl = (f) => `/local?path=${encodeURIComponent(f.path)}&v=${f.v || 0}`;
const fmtBytes = (n) => (n < 1024 ? `${n} B` : n < 1048576 ? `${(n / 1024).toFixed(1)} KB` : `${(n / 1048576).toFixed(1)} MB`);

// ---------- Tabs and options ----------

function renderTabs() {
  const tabs = $('tabs');
  tabs.replaceChildren();
  COMMANDS.forEach((c, i) => {
    if (i > 0 && c.group !== COMMANDS[i - 1].group) tabs.append(el('span', 'tab-sep'));
    const b = el('button', 'tab');
    b.setAttribute('role', 'tab');
    b.setAttribute('aria-selected', String(c.id === settings.command));
    b.title = i < 10 ? `${cmdLabel(c)} (Alt+${(i + 1) % 10})` : cmdLabel(c);
    b.append(cmdLabel(c));
    b.onclick = () => setCommand(c.id);
    tabs.append(b);
  });
}

// Command tabs, title and description (all translated text of the selected command).
function renderCommandTexts() {
  renderTabs();
  const c = cmdInfo();
  $('cmd-title').textContent = cmdLabel(c);
  $('cmd-desc').textContent = cmdDesc(c);
}

async function setCommand(id) {
  settings.command = id;
  saveSettings();
  const c = COMMANDS.find((x) => x.id === id);
  renderCommandTexts();
  document.querySelectorAll('[data-cmd]').forEach((g) => { g.hidden = g.dataset.cmd !== id; });
  $('output-group').hidden = c.out === 'pack';
  $('output-file').hidden = c.out === 'slice' || c.out === 'pack';
  $('output-dir').hidden = c.out !== 'slice';
  $('inplace-radio').hidden = c.out === 'extract';
  $('suffix').value = await suffixFor(id);
  if (id === 'recolor') refreshPalettes(); // refresh every time so palettes saved from palette extraction show up right away
  renderOutput();
  renderRunButton();
  schedulePreview();
}

function renderOutput() {
  const mode = effMode();
  document.querySelectorAll('input[name="out-mode"]').forEach((r) => { r.checked = r.value === mode; });
  $('suffix-box').hidden = mode === 'inplace';
  $('export-dir-box').hidden = mode !== 'dir';
  $('inplace-box').hidden = mode !== 'inplace';
  $('backup').checked = settings.backup;
  $('inplace-warn').textContent = settings.backup ? t('batch.output.warn_backup') : t('batch.output.warn_nobackup');
  const sfx = settings.suffixes[settings.command] ?? '';
  if (mode === 'dir') {
    $('suffix-example').textContent = sfx ? t('batch.output.example_dir', { suffix: sfx }) : t('batch.output.example_dir_empty');
  } else {
    $('suffix-example').textContent = sfx ? t('batch.output.example_suffix', { suffix: sfx }) : t('batch.output.example_missing');
  }
  $('export-dir-label').textContent = settings.exportDir || t('batch.output.pick_folder');
  $('export-dir-label').title = settings.exportDir;
  $('out-dir-label').textContent = settings.outDir || t('batch.output.same_folder');
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
    if (!(v >= 1)) return; // ignore an empty value while typing
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
  const up = $('upscale-scale');
  renderScaleOptions();
  sc.onchange = () => { settings.scale = Number(sc.value); saveSettings(); schedulePreview(); };
  up.onchange = () => { settings.upscale = Number(up.value); saveSettings(); schedulePreview(); };

  const numberSetting = (id, key, max) => {
    const input = $(id);
    input.value = settings[key];
    input.oninput = () => {
      const v = Math.round(Number(input.value));
      if (!(v >= 0)) return; // ignore an empty value while typing
      settings[key] = Math.min(max, v);
      saveSettings();
      schedulePreview();
    };
  };
  numberSetting('trim-pad', 'pad', 256);
  numberSetting('despeckle-min', 'minSize', 100000);
  $('despeckle-min').oninput = () => {
    const v = Math.round(Number($('despeckle-min').value));
    if (!(v >= 1)) return; // ignore an empty value while typing and 0
    settings.minSize = Math.min(100000, v);
    saveSettings();
    schedulePreview();
  };
  $('despeckle-diag').checked = settings.diagonal;
  $('despeckle-diag').onchange = (e) => { settings.diagonal = e.target.checked; saveSettings(); schedulePreview(); };
  numberSetting('pack-cols', 'packCols', 256);
  numberSetting('pack-gap', 'packGap', 64);
  const showAlign = () => $('pack-align').querySelectorAll('button').forEach((b) => b.classList.toggle('on', b.dataset.align === settings.packAlign));
  showAlign();
  $('pack-align').querySelectorAll('button').forEach((b) => {
    b.onclick = () => { settings.packAlign = b.dataset.align; showAlign(); saveSettings(); schedulePreview(); };
  });

  // The palette save name/overwrite are not stored; they are read only when running.
  $('save-name').onkeydown = (e) => { if (e.key === 'Enter') startRun(); };

  document.querySelectorAll('input[name="out-mode"]').forEach((r) => {
    r.onchange = () => { if (r.checked) { settings.outMode = r.value; saveSettings(); renderOutput(); } };
  });
  $('export-dir-pick').onclick = async () => {
    const dir = await PickFolder(t('dialog.pick_export_dir'));
    if (dir) { settings.exportDir = dir; saveSettings(); renderOutput(); }
  };
  $('backup').onchange = (e) => { settings.backup = e.target.checked; saveSettings(); renderOutput(); };
  $('suffix').oninput = (e) => { settings.suffixes[settings.command] = e.target.value; saveSettings(); renderOutput(); };

  $('out-dir-pick').onclick = async () => {
    const dir = await PickFolder(t('dialog.pick_frame_dir'));
    if (dir) { settings.outDir = dir; saveSettings(); renderOutput(); }
  };
  $('out-dir-clear').onclick = () => { settings.outDir = ''; saveSettings(); renderOutput(); };

  $('threads').value = settings.threads;
  $('threads').oninput = (e) => { settings.threads = Math.max(0, Number(e.target.value) || 0); saveSettings(); };

  $('recursive').checked = settings.recursive;
  $('recursive').onchange = (e) => { settings.recursive = e.target.checked; saveSettings(); };

  // Palette
  $('palette-select').onchange = (e) => selectPalette(e.target.value);
  const fetchPalette = async () => {
    const name = $('palette-name').value.trim();
    if (!name) return;
    $('palette-fetch').disabled = true;
    $('palette-info').textContent = t('common.loading');
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

  // Zoom
  renderZoom();

  $('reveal-btn').onclick = () => { const f = selectedFile(); if (f) Reveal(f.path); };
  $('edit-btn').onclick = () => { const f = selectedFile(); if (f && !f.error) openInEditor(f.path); };
}

// The scale <select>s hold translated labels ("Auto-detect", "2x"), so they are rebuilt on language change.
function renderScaleOptions() {
  const fill = (select, list, value) => {
    select.replaceChildren(...list.map((s) => new Option(scaleLabel(s), s)));
    select.value = value;
  };
  fill($('scale'), SCALES, settings.scale);
  fill($('upscale-scale'), UPSCALES, settings.upscale);
}

function renderZoom() {
  const seg = $('zoom-seg');
  seg.replaceChildren(...ZOOMS.map((z) => {
    const b = el('button', '', zoomLabel(z));
    b.onclick = () => setZoom(z);
    b.dataset.zoom = z;
    b.classList.toggle('on', String(settings.zoom) === String(z));
    return b;
  }));
}

function setZoomMarks() {
  $('zoom-seg').querySelectorAll('button').forEach((b) => b.classList.toggle('on', String(settings.zoom) === b.dataset.zoom));
}

// ---------- Palettes ----------

async function refreshPalettes(select) {
  try {
    const list = await ListPalettes();
    const custom = state.palettes.filter((p) => p.name.toLowerCase().endsWith('.png'));
    // Also reload the palette picked as a PNG file last time.
    if (settings.palette.toLowerCase().endsWith('.png') && !custom.some((p) => p.name === settings.palette)) {
      try { custom.push(await LoadPalette(settings.palette)); } catch { /* ignore if the file is gone */ }
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
  if (!state.palettes.length) s.append(new Option(t('common.no_palettes'), ''));
  state.palettes.forEach((p) => s.append(new Option(paletteLabel(p), p.name)));
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
    info.textContent = t('batch.recolor.hint');
    return;
  }
  p.colors.forEach((c) => {
    const s = el('span');
    s.style.background = c;
    s.title = c;
    box.append(s);
  });
  info.textContent = tn('batch.recolor.info', p.colors.length, { path: p.path });
}

// ---------- File list ----------

// Returns: openable (error-free) file paths among those received this time (including ones already in the list, in order)
async function addPaths(paths, opt = {}) {
  if (!paths || !paths.length) return [];
  try {
    const entries = await ExpandPaths(paths, settings.recursive);
    const known = new Set(state.files.map((f) => f.path.toLowerCase()));
    const fresh = (entries || []).filter((e) => !known.has(e.path.toLowerCase()));
    state.files.push(...fresh.map((e) => ({ ...e, status: e.error ? 'failed' : '', message: e.error || '', v: Date.now() })));
    if (!fresh.length) {
      if (!opt.quiet || !(entries && entries.length)) toast(entries && entries.length ? t('batch.files.already') : t('toast.no_png'));
    } else if (!state.selected) state.selected = (fresh.find((f) => !f.error) || fresh[0]).path;
    renderFiles();
    schedulePreview();
    return (entries || []).filter((e) => !e.error).map((e) => e.path);
  } catch (err) {
    toast(String(err), 'failed');
    return [];
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

// Files that match the filter (visible on screen). All of them if there is no filter.
function visibleFiles() {
  return state.filter ? state.files.filter((f) => f.name.toLowerCase().includes(state.filter)) : state.files;
}

// Targets of run/pack: all error-free files. Narrowed to the visible files only when a filter is on and 'visible files only' is checked.
function runTargets() {
  const base = state.filter && state.onlyShown ? visibleFiles() : state.files;
  return base.filter((f) => !f.error);
}

function removeFile(path) {
  const i = state.files.findIndex((f) => f.path === path);
  if (i < 0) return;
  const vis = visibleFiles();
  const vi = vis.findIndex((f) => f.path === path);
  state.files.splice(i, 1);
  if (state.selected === path) {
    const rest = visibleFiles();
    state.selected = (rest.length ? rest[Math.min(Math.max(vi, 0), rest.length - 1)] : state.files[Math.min(i, state.files.length - 1)])?.path ?? null;
  }
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

  const badge = f.status ? el('span', `badge ${f.status}`, statusLabel(f.status)) : el('span');
  const rm = el('button', 'remove', '×');
  rm.title = t('batch.files.remove');
  rm.onclick = (e) => { e.stopPropagation(); removeFile(f.path); };

  row.append(thumb, main, badge, rm);
  row.onclick = () => select(f.path);
  row.ondblclick = () => { if (!f.error) openInEditor(f.path); };
  return row;
}

function renderFiles() {
  const list = $('file-list');
  const shown = visibleFiles();
  list.replaceChildren(...shown.map(fileRow));
  if (state.files.length && !shown.length) {
    list.append(el('div', 'no-match', t('batch.files.no_match', { filter: $('file-filter').value.trim() })));
  }
  $('empty').hidden = state.files.length > 0;
  $('file-count').textContent = !state.files.length ? ''
    : state.filter ? t('batch.files.count_filtered', { shown: shown.length, total: state.files.length }) : tn('batch.files.count', state.files.length);
  renderRunButton();
  editor?.filesChanged(); // the editor's file strip shows the same list
}

function setFilter(text) {
  if ($('file-filter').value !== text) $('file-filter').value = text; // also sync the input when it was changed from the editor's file strip
  const t = text.trim().toLowerCase();
  if (t === state.filter) return;
  state.filter = t;
  renderFiles();
  schedulePreview(); // the pack preview may have different target files
}

function updateRow(f) {
  const old = document.querySelector(`.file[data-path="${CSS.escape(f.path)}"]`);
  if (old) old.replaceWith(fileRow(f));
}

// ---------- Preview ----------

let previewTimer = 0;
let previewSeq = 0;

function schedulePreview() {
  clearTimeout(previewTimer);
  previewTimer = setTimeout(runPreview, 120);
}

async function runPreview() {
  const f = selectedFile();
  const packing = settings.command === 'pack';
  const valid = runTargets();
  const seq = ++previewSeq;
  const origImg = $('orig-img');
  const resImg = $('result-img');
  const msg = $('result-msg');

  $('reveal-btn').disabled = !f;
  $('edit-btn').disabled = !f || !!f.error;
  if (!f || (packing && !valid.length)) {
    $('preview-name').textContent = t('batch.preview.title');
    origImg.hidden = resImg.hidden = true;
    state.hasResult = false;
    applyCompare();
    $('orig-meta').textContent = $('result-meta').textContent = '';
    setMsg(msg, '', t('batch.preview.empty'));
    $('result-colors').replaceChildren();
    return;
  }

  $('preview-name').textContent = packing ? tn('batch.preview.pack_title', valid.length) : f.name;
  $('preview-name').title = f.path;
  const origSrc = localUrl(f);
  if (origImg.dataset.src !== origSrc) {
    origImg.dataset.src = origSrc;
    origImg.src = origSrc;
    $('compare-img').src = origSrc; // preload so it shows right away when held to compare
  }
  origImg.dataset.w = f.width;
  origImg.dataset.h = f.height;
  $('compare-img').dataset.w = f.width;
  $('compare-img').dataset.h = f.height;
  origImg.hidden = !f.width;
  $('orig-meta').textContent = f.width ? `${f.width}×${f.height}` : '';

  $('spinner').classList.add('on');
  let res;
  try {
    res = packing ? await PreviewPack(request(), valid.map((x) => x.path)) : await Preview(request(), f.path);
  } catch (err) {
    res = { status: 'failed', message: String(err) };
  }
  if (seq !== previewSeq) return; // a newer request exists
  $('spinner').classList.remove('on');

  state.hasResult = res.status === 'done';
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
    setMsg(msg, res.status, res.status === 'skipped' ? t('batch.preview.skipped', { message: res.message }) : res.message);
  }
  renderColors(res.colors);
  layoutImages();
  applyCompare();
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
  box.append(el('span', 'label', tn('batch.preview.colors', colors.length)));
  colors.forEach((c) => {
    const s = el('span', 'sw');
    s.style.background = c;
    s.title = c;
    box.append(s);
  });
}

// Fit at an integer zoom whenever possible so pixels are not smeared.
function fitScale(w, h, box) {
  const pad = 24;
  const s = Math.min((box.clientWidth - pad) / w, (box.clientHeight - pad) / h);
  if (!isFinite(s) || s <= 0) return 1;
  return s >= 1 ? Math.floor(s) : s;
}

// The two areas are the same size, so images of the same size are shown at the same zoom even in fit mode.
function layoutImages() {
  for (const [img, box] of [[$('orig-img'), $('orig-canvas')], [$('result-img'), $('result-canvas')], [$('compare-img'), $('result-canvas')]]) {
    const w = Number(img.dataset.w), h = Number(img.dataset.h);
    if (!w || !h) continue;
    const s = settings.zoom === 'fit' ? fitScale(w, h, box) : Number(settings.zoom);
    img.style.width = `${w * s}px`;
    img.style.height = `${h * s}px`;
  }
}

function setZoom(z) {
  settings.zoom = z;
  saveSettings();
  setZoomMarks();
  layoutImages();
}

// ---------- Preview interaction ----------

// Pixel inspection: draw the image once to an offscreen canvas and read colors from it. Redrawn when a new image is loaded.
const pixelCache = new WeakMap(); // img -> {w, h, data}
function pixelData(img) {
  let c = pixelCache.get(img);
  if (c) return c;
  if (!img.complete || !img.naturalWidth) return null;
  c = { w: 0, h: 0, data: null };
  try {
    const cv = document.createElement('canvas');
    cv.width = img.naturalWidth;
    cv.height = img.naturalHeight;
    const ctx = cv.getContext('2d', { willReadFrequently: true });
    ctx.drawImage(img, 0, 0);
    c = { w: cv.width, h: cv.height, data: ctx.getImageData(0, 0, cv.width, cv.height).data };
  } catch { /* if too large or unreadable, show only the coordinates */ }
  pixelCache.set(img, c);
  return c;
}

const hex2 = (n) => n.toString(16).padStart(2, '0');

function clearProbe(canvas) {
  const key = canvas.id === 'orig-canvas' ? 'orig' : 'result';
  $(`${key}-probe`).hidden = true;
  $(`${key}-meta`).hidden = false;
}

// Convert the mouse position over the canvas to image pixel coordinates and show coordinates, color and alpha in the caption.
function probe(canvas, clientX, clientY) {
  const key = canvas.id === 'orig-canvas' ? 'orig' : 'result';
  const img = [...canvas.querySelectorAll('img')].find((i) => !i.hidden && i.naturalWidth);
  if (!img) { clearProbe(canvas); return; }
  const r = img.getBoundingClientRect();
  if (clientX < r.left || clientX >= r.right || clientY < r.top || clientY >= r.bottom) { clearProbe(canvas); return; }
  const x = Math.min(img.naturalWidth - 1, Math.floor((clientX - r.left) * img.naturalWidth / r.width));
  const y = Math.min(img.naturalHeight - 1, Math.floor((clientY - r.top) * img.naturalHeight / r.height));
  const box = $(`${key}-probe`);
  box.replaceChildren();
  const px = pixelData(img);
  if (px && px.data) {
    const i = (y * px.w + x) * 4;
    const [R, G, B, A] = [px.data[i], px.data[i + 1], px.data[i + 2], px.data[i + 3]];
    const sw = el('i', 'probe-sw');
    sw.style.setProperty('--c', `rgba(${R},${G},${B},${A / 255})`);
    box.append(sw, `${x}, ${y}  #${hex2(R)}${hex2(G)}${hex2(B)}  α${A}`);
  } else {
    box.append(`${x}, ${y}`);
  }
  box.hidden = false;
  $(`${key}-meta`).hidden = true;
}

// Hold to compare: while the mouse (result pane) or Space is held, show the original in the result pane.
function applyCompare() {
  const on = (state.cmpMouse || state.cmpKey) && state.hasResult && !$('orig-img').hidden;
  $('compare-img').hidden = !on;
  $('result-img').hidden = on || !state.hasResult;
  $('result-title').textContent = on ? t('batch.preview.result_holding') : t('batch.preview.result');
  $('result-pane').classList.toggle('comparing', !!on);
  layoutImages();
}

function setCompare(kind, on) {
  if (!!state[kind] === on) return;
  state[kind] = on;
  applyCompare();
}

// When one side is scrolled while zoomed, make the other follow at the same ratio. Remember scrolls made by code so they do not come back and sync again.
let syncEcho = null;
function linkScroll(a, b) {
  a.addEventListener('scroll', () => {
    if (settings.zoom === 'fit') return;
    if (syncEcho && syncEcho.el === a && syncEcho.l === a.scrollLeft && syncEcho.t === a.scrollTop) { syncEcho = null; return; }
    const ax = a.scrollWidth - a.clientWidth, ay = a.scrollHeight - a.clientHeight;
    const bx = b.scrollWidth - b.clientWidth, by = b.scrollHeight - b.clientHeight;
    const l = ax > 0 && bx > 0 ? Math.round((a.scrollLeft / ax) * bx) : b.scrollLeft;
    const t = ay > 0 && by > 0 ? Math.round((a.scrollTop / ay) * by) : b.scrollTop;
    if (l === b.scrollLeft && t === b.scrollTop) return;
    b.scrollLeft = l;
    b.scrollTop = t;
    syncEcho = { el: b, l: b.scrollLeft, t: b.scrollTop };
  });
}

// Ctrl+wheel: change the zoom step (fit -> 1x ... 16x) one notch at a time. The center position being viewed is kept.
let wheelAt = 0;
function wheelZoom(e) {
  if (!e.ctrlKey) return;
  e.preventDefault();
  const now = performance.now();
  if (now - wheelAt < 90) return; // merge the flood of trackpad events into one step
  const i = ZOOMS.findIndex((z) => String(z) === String(settings.zoom));
  const j = Math.max(0, Math.min(ZOOMS.length - 1, i + (e.deltaY < 0 ? 1 : -1)));
  if (j === i) return;
  wheelAt = now;
  const pane = e.target.closest('.canvas') || $('orig-canvas');
  const cx = pane.scrollWidth ? (pane.scrollLeft + pane.clientWidth / 2) / pane.scrollWidth : 0.5;
  const cy = pane.scrollHeight ? (pane.scrollTop + pane.clientHeight / 2) / pane.scrollHeight : 0.5;
  setZoom(ZOOMS[j]);
  if (settings.zoom !== 'fit') {
    for (const c of [$('orig-canvas'), $('result-canvas')]) {
      c.scrollLeft = cx * c.scrollWidth - c.clientWidth / 2;
      c.scrollTop = cy * c.scrollHeight - c.clientHeight / 2;
    }
  }
}

function initPreviewInteraction() {
  const canvases = [$('orig-canvas'), $('result-canvas')];
  for (const canvas of canvases) {
    let last = null;
    canvas.addEventListener('mousemove', (e) => { last = [e.clientX, e.clientY]; probe(canvas, e.clientX, e.clientY); });
    canvas.addEventListener('mouseleave', () => { last = null; clearProbe(canvas); });
    canvas.addEventListener('scroll', () => { if (last) probe(canvas, last[0], last[1]); });
  }
  for (const img of [$('orig-img'), $('result-img'), $('compare-img')]) {
    img.addEventListener('load', () => { pixelCache.delete(img); });
  }
  $('orig-img').addEventListener('load', layoutImages);

  const rc = $('result-canvas');
  rc.addEventListener('mousedown', (e) => {
    if (e.button !== 0) return;
    const r = rc.getBoundingClientRect();
    if (e.clientX - r.left >= rc.clientWidth || e.clientY - r.top >= rc.clientHeight) return; // scrollbar
    setCompare('cmpMouse', true);
  });
  window.addEventListener('mouseup', () => setCompare('cmpMouse', false));
  window.addEventListener('blur', () => { setCompare('cmpMouse', false); setCompare('cmpKey', false); });

  linkScroll($('orig-canvas'), $('result-canvas'));
  linkScroll($('result-canvas'), $('orig-canvas'));
  document.querySelector('.compare').addEventListener('wheel', wheelZoom, { passive: false });
}

// ---------- Run ----------

function renderRunButton() {
  const b = $('run-btn');
  if (state.running) {
    b.textContent = t('batch.run.cancel');
    b.className = 'btn danger run';
    b.disabled = false;
    $('only-shown-box').hidden = true;
    $('run-hint').textContent = 'Esc';
    return;
  }
  // Even with a filter on, the default is to process the whole list. The button says 'All', and when checked only the visible files are processed.
  const filtered = !!state.filter;
  const total = state.files.filter((f) => !f.error).length;
  const shown = visibleFiles().filter((f) => !f.error).length;
  const n = runTargets().length;
  $('only-shown-box').hidden = !filtered;
  $('only-shown').checked = state.onlyShown;
  $('only-shown-label').textContent = tn('batch.only_shown', shown);
  const label = cmdLabel(cmdInfo());
  const all = filtered && !state.onlyShown;
  b.textContent = n ? tn(all ? 'batch.run.button_all' : 'batch.run.button_n', n, { label }) : t('batch.run.button', { label });
  b.className = 'btn primary run';
  b.disabled = n === 0 || !!state.packing;
  $('run-hint').textContent = all ? tn('batch.run.hint_all', total) : 'Ctrl+Enter';
}

async function startRun() {
  if (state.running) return;
  const targets = runTargets();
  if (!targets.length) return;
  if (settings.command === 'recolor' && !settings.palette) {
    toast(t('toast.pick_palette_first'), 'failed');
    return;
  }
  if (settings.command === 'pack') {
    await runPack(targets);
    return;
  }
  const mode = effMode();
  if (mode === 'dir' && !settings.exportDir) {
    toast(t('toast.pick_folder_first'), 'failed');
    return;
  }
  if (mode === 'suffix' && !(settings.suffixes[settings.command] ?? '')) {
    toast(t('toast.suffix_required'), 'failed');
    return;
  }
  if (mode === 'inplace' &&
      !confirm(tn('batch.run.confirm', targets.length, { note: t(settings.backup ? 'batch.run.confirm_backup' : 'batch.run.confirm_nobackup') }))) return;

  targets.forEach((f) => { f.status = 'running'; f.message = ''; });
  renderFiles();
  // Remember this run's settings so the result notice does not get out of sync if settings change during the run.
  state.run = { mode, cmd: settings.command, exportDir: settings.exportDir, outDir: settings.outDir, saveName: $('save-name').value.trim() };
  state.backups = [];
  state.backupDirs = [];
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

// Pack sheet: after choosing a save location, merge all files in the list into one sheet.
async function runPack(targets) {
  if (state.packing) return;
  state.packing = true;
  $('run-btn').disabled = true;
  setStatus('status.pack_pick');
  try {
    const r = await RunPack(request(), targets.map((f) => f.path));
    if (r.canceled) {
      setStatus('status.canceled');
    } else {
      setStatus(null, r.message);
      toast(r.message, '', [
        { label: t('toast.action.reveal'), run: () => Reveal(r.output) },
        { label: t('toast.action.add_result'), run: () => addPaths([r.output]) },
      ]);
    }
  } catch (err) {
    setStatus('status.failed');
    toast(String(err), 'failed');
  } finally {
    state.packing = false;
    renderRunButton();
  }
}

function setProgress(done, total) {
  $('progress').classList.toggle('on', state.running);
  $('progress-bar').style.width = total ? `${(done / total) * 100}%` : '0';
  if (state.running) setStatus('status.processing', null, { done, total });
}

EventsOn('job:result', (r) => {
  const f = state.files.find((x) => x.path === r.input);
  if (f) {
    f.status = r.status;
    f.message = r.message;
    updateRow(f);
  }
  if (r.status === 'done') state.lastOutput = r.output || r.input;
  if (r.backup) state.backups.push({ path: r.input, backup: r.backup });
  setProgress(r.completed, r.total);
});

EventsOn('job:done', async (s) => {
  state.running = false;
  state.files.filter((f) => f.status === 'running').forEach((f) => { f.status = ''; f.message = t('status.canceled'); });
  setProgress(0, 0);
  renderRunButton();

  // Overwritten files get their size/thumbnail re-read.
  const run = state.run || { mode: effMode(), cmd: settings.command, exportDir: settings.exportDir, outDir: settings.outDir, saveName: '' };
  const mode = run.mode;
  state.backupDirs = s.backupDirs || [];
  if (mode === 'inplace') {
    const done = state.files.filter((f) => f.status === 'done');
    const fresh = await Describe(done.map((f) => f.path));
    fresh.forEach((e, i) => Object.assign(done[i], e, { root: done[i].root, v: Date.now() }));
  }
  renderFiles();
  schedulePreview();

  const summary = { done: s.done, skipped: s.skipped, failed: s.failed };
  const text = t(s.canceled ? 'status.summary_canceled' : 'status.summary', summary);
  setStatus(s.canceled ? 'status.summary_canceled' : 'status.summary', null, summary);
  statusRight = { key: 'status.seconds', params: { s: s.seconds.toFixed(1) } };
  renderStatus();
  const actions = [];
  if (mode === 'inplace') {
    if (state.backups.length) {
      const pairs = state.backups.slice();
      actions.push({ label: t('toast.action.undo'), run: () => restoreBackups(pairs) });
      if (state.backupDirs.length) actions.push({ label: t('toast.action.open_backup'), run: () => Reveal(state.backupDirs[0]) });
    }
  } else {
    if (mode === 'dir' && s.done && run.exportDir) {
      actions.push({ label: t('toast.action.reveal'), run: () => Reveal(run.exportDir) });
    } else if (mode === 'suffix' && state.lastOutput) {
      actions.push({ label: t('toast.action.open_result'), run: () => Reveal(state.lastOutput) });
    } else if (run.cmd === 'slice' && s.done) {
      actions.push({ label: t('toast.action.reveal'), run: () => Reveal(run.outDir || state.lastOutput) });
    }
    if (s.outputs && s.outputs.length) {
      const outputs = s.outputs.slice();
      actions.push({ label: t('toast.action.add_result'), run: () => addPaths(outputs) });
    }
  }
  toast(text, s.failed ? 'failed' : '', actions);

  // If a palette was saved with a name from palette extraction, reload the palette list.
  if (run.cmd === 'extract' && s.done) {
    const saved = run.saveName.toLowerCase();
    if (saved) await refreshPalettes(saved);
  }
});

// ---------- Common UI ----------

// Put overwritten originals back from the backup, and re-read the size, thumbnail and preview in the list.
async function restoreBackups(pairs) {
  try {
    const rep = await RestoreBackups(pairs);
    const fresh = await Describe(pairs.map((p) => p.path));
    fresh.forEach((e, i) => {
      const f = state.files.find((x) => x.path === pairs[i].path);
      if (f) Object.assign(f, e, { root: f.root, v: Date.now(), status: '', message: '' });
    });
    if (rep.restored === pairs.length) state.backups = []; // so the same backup is not restored twice
    renderFiles();
    schedulePreview();
    const err = rep.errors && rep.errors.length ? ` ${t('status.restore_errors', { n: rep.errors.length, error: rep.errors[0] })}` : '';
    setStatus('status.restored', null, null, rep.restored);
    toast(`${tn('status.restored', rep.restored)}${err}`, err ? 'failed' : '');
  } catch (err) {
    toast(String(err), 'failed');
  }
}

// actions: one {label, run} or an array. Pressing a button closes the notice and runs the action. With buttons it is shown longer.
let toastTimer = 0;
function toast(text, cls = '', actions = null) {
  const list = !actions ? [] : Array.isArray(actions) ? actions : [actions];
  const t = $('toast');
  t.className = `toast on ${cls}`;
  t.replaceChildren(el('span', '', text));
  for (const action of list) {
    const b = el('button', 'btn small', action.label);
    b.onclick = () => { t.className = 'toast'; action.run(); };
    t.append(b);
  }
  clearTimeout(toastTimer);
  toastTimer = setTimeout(() => { t.className = 'toast'; }, list.length ? 10000 : 3500);
}

// ---------- Shortcut help ----------

function toggleHelp(open = $('help').hidden) {
  $('help').hidden = !open;
  if (open) $('help-close').focus();
}

function initGlobal() {
  $('add-files').onclick = async () => addPaths(await PickFiles());
  $('add-folder').onclick = async () => { const d = await PickFolder(t('dialog.pick_image_dir')); if (d) addPaths([d]); };
  $('clear-files').onclick = () => { state.files = []; state.selected = null; renderFiles(); schedulePreview(); };
  $('run-btn').onclick = () => (state.running ? Cancel() : startRun());
  $('only-shown').onchange = (e) => { state.onlyShown = e.target.checked; renderRunButton(); schedulePreview(); };

  const filter = $('file-filter');
  filter.oninput = () => setFilter(filter.value);
  filter.onkeydown = (e) => {
    if (e.key !== 'Escape') return;
    if (filter.value) { filter.value = ''; setFilter(''); e.stopPropagation(); } // so it does not leak into undo
    else filter.blur();
  };

  // The command tabs scroll sideways with the wheel when the window is too narrow for all of them (longer translations).
  $('tabs').addEventListener('wheel', (e) => {
    const bar = $('tabs');
    if (bar.scrollWidth <= bar.clientWidth) return;
    e.preventDefault();
    bar.scrollLeft += e.deltaX || e.deltaY;
  }, { passive: false });

  $('help-btn').onclick = () => toggleHelp();
  $('help-close').onclick = () => toggleHelp(false);
  $('help').addEventListener('mousedown', (e) => { if (e.target === $('help')) toggleHelp(false); });

  // File drop (Wails passes the real paths)
  const overlay = $('drop-overlay');
  let depth = 0;
  window.addEventListener('dragenter', (e) => { e.preventDefault(); depth++; overlay.classList.add('on'); });
  window.addEventListener('dragleave', () => { if (--depth <= 0) { depth = 0; overlay.classList.remove('on'); } });
  window.addEventListener('dragover', (e) => e.preventDefault());
  window.addEventListener('drop', (e) => { e.preventDefault(); depth = 0; overlay.classList.remove('on'); });
  OnFileDrop((_x, _y, paths) => {
    depth = 0;
    overlay.classList.remove('on');
    if (settings.mode !== 'editor') { addPaths(paths); return; }
    editor.dropPaths(paths || []); // add to the list and open the first file
  }, false);

  window.addEventListener('keydown', (e) => {
    const active = document.activeElement;
    const typing = ['INPUT', 'SELECT', 'TEXTAREA'].includes(active?.tagName);
    // Fields where text is typed (excluding checkboxes, radios and sliders)
    const entering = active?.tagName === 'SELECT' || active?.tagName === 'TEXTAREA' ||
      (active?.tagName === 'INPUT' && !['checkbox', 'radio', 'range', 'button'].includes(active.type));
    if (e.key === 'Escape' && !$('help').hidden) { toggleHelp(false); return; }
    if (e.key === 'F1' || (e.key === '?' && !entering)) { e.preventDefault(); toggleHelp(); return; }
    if (!$('help').hidden) return; // while the help is open, block other shortcuts
    if (e.ctrlKey && !e.altKey && (e.key === '1' || e.key === '2')) { e.preventDefault(); setMode(e.key === '1' ? 'batch' : 'editor'); return; }
    if (settings.mode === 'editor') return; // the editor handles its own shortcuts
    if (e.ctrlKey && e.key.toLowerCase() === 'f') { e.preventDefault(); $('file-filter').focus(); $('file-filter').select(); return; }
    if (e.ctrlKey && e.key === 'Enter') { e.preventDefault(); startRun(); return; }
    if (e.key === 'Escape' && state.running) { Cancel(); return; }
    if (e.code === 'Space' && !entering && !e.ctrlKey && !e.altKey && !e.metaKey) {
      e.preventDefault(); // prevent a button press or list scroll
      if (!e.repeat) setCompare('cmpKey', true);
      return;
    }
    if (e.altKey && /^[0-9]$/.test(e.key)) {
      const n = e.key === '0' ? 10 : Number(e.key);
      if (n <= COMMANDS.length) { e.preventDefault(); setCommand(COMMANDS[n - 1].id); return; }
    }
    if (typing) return;
    const vis = visibleFiles();
    const i = vis.findIndex((f) => f.path === state.selected);
    if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
      e.preventDefault();
      const next = vis[Math.max(0, Math.min(vis.length - 1, i + (e.key === 'ArrowDown' ? 1 : -1)))];
      if (next) select(next.path, true);
    } else if (e.key === 'Delete' && state.selected) {
      removeFile(state.selected);
    }
  });
  window.addEventListener('keyup', (e) => {
    if (e.code === 'Space' && state.cmpKey) { e.preventDefault(); setCompare('cmpKey', false); }
  });

  new ResizeObserver(layoutImages).observe($('orig-canvas'));
  initPreviewInteraction();
}

// ---------- Editor connection ----------

const normPath = (p) => p.toLowerCase().split('\\').join('/');

// The editor is created once the Go language has been set (see start()), because it calls Go while it is being built.
let editor = null;
let dirtyCount = 0;

function renderDirtyDot() {
  const dot = $('mode-dot');
  dot.hidden = !dirtyCount;
  dot.textContent = dirtyCount > 1 ? `●${dirtyCount}` : '●';
  dot.title = dirtyCount ? tn('app.unsaved_docs', dirtyCount) : '';
}

function buildEditor() {
  editor = createEditor($('editor'), {
    toast,
    isBlocked: () => !$('help').hidden,
    getBackup: () => settings.backup,
    setBackup: (on) => { settings.backup = on; saveSettings(); renderOutput(); },
    // The editor and batch share the same file list (state.files).
    files: () => state.files,
    getFilter: () => $('file-filter').value,
    setFilter: (text) => setFilter(text),
    addPaths: (paths) => addPaths(paths, { quiet: true }),
    removeFile: (path) => removeFile(path),
    // Also sync the file being viewed in the editor to the selection of the batch list (the preview is refreshed when returning to batch).
    onCurrentChange: (path) => {
      const f = path && state.files.find((x) => normPath(x.path) === normPath(path));
      if (!f || state.selected === f.path) return;
      state.selected = f.path;
      document.querySelectorAll('.file').forEach((r) => r.classList.toggle('selected', r.dataset.path === f.path));
    },
    // n: number of unsaved documents
    onDirtyChange: (n) => {
      dirtyCount = n;
      renderDirtyDot();
      SetEditorDirtyCount(n).catch(() => {});
    },
    // If the saved file is in the list, re-read its size, thumbnail and preview; otherwise let it be added to the list.
    onSaved: (path) => {
      const f = state.files.find((x) => normPath(x.path) === normPath(path));
      const actions = [{ label: t('toast.action.reveal'), run: () => Reveal(path) }];
      if (f) {
        Describe([path]).then(([e]) => {
          Object.assign(f, e, { root: f.root, v: Date.now(), status: '', message: '' });
          renderFiles();
          if (settings.mode !== 'editor') schedulePreview();
        });
      } else {
        actions.unshift({ label: t('toast.action.add_to_list'), run: () => addPaths([path]) });
      }
      return actions;
    },
  });
}

function applyMode() {
  const ed = settings.mode === 'editor';
  document.querySelector('.layout').hidden = ed;
  $('tabs').hidden = ed;
  $('tabs-spacer').hidden = !ed;
  $('editor').hidden = !ed;
  $('mode-seg').querySelectorAll('button').forEach((b) => b.classList.toggle('on', b.dataset.mode === settings.mode));
  editor.activate(ed);
  if (!ed) { // reflect a selection changed in the editor on the batch screen
    schedulePreview();
    document.querySelector('.file.selected')?.scrollIntoView({ block: 'nearest' });
  }
}

function setMode(mode) {
  if (settings.mode === mode) return;
  settings.mode = mode;
  saveSettings();
  applyMode();
}

async function openInEditor(path) {
  setMode('editor');
  await editor.open(path);
}

function initMode() {
  if (settings.mode !== 'editor') settings.mode = 'batch';
  $('mode-seg').querySelectorAll('button').forEach((b) => { b.onclick = () => { setMode(b.dataset.mode); b.blur(); }; });
  applyMode();
}

// ---------- Start ----------

// ---------- Language ----------
// The frontend language (src/i18n) is the source of truth: the selector lists the languages that have a frontend catalog.
// Go-side messages use the same code when the Go catalog has it; otherwise Go falls back to English.

// Sets the Go language to the current one; falls back to English when Go does not know it.
async function syncGoLanguage() {
  try {
    await SetLanguage(getLanguage());
  } catch {
    try { await SetLanguage(DEFAULT_LANGUAGE); } catch { /* keep whatever Go has */ }
  }
}

function renderLangSelect() {
  const sel = $('lang-select');
  sel.replaceChildren(...languages().map((l) => new Option(l.name, l.code)));
  sel.value = getLanguage();
}

// Re-renders every piece of dynamic UI after a language switch, then refreshes whatever Go produced text for.
let langSeq = 0;
async function onLanguageChanged() {
  const seq = ++langSeq;
  applyI18n(document);
  renderLangSelect();
  renderCommandTexts();
  renderScaleOptions();
  renderPaletteSelect();
  renderSwatches(state.palettes.find((p) => p.name === settings.palette));
  renderZoom();
  renderOutput();
  renderFiles();
  renderStatus();
  renderDirtyDot();
  applyCompare();
  await syncGoLanguage(); // Go must know the new language before it produces more text
  if (seq !== langSeq) return;
  editor.relocalize();
  schedulePreview();
}

async function start() {
  applyI18n(document); // frontend texts first, so nothing flashes empty while Go is being told
  renderLangSelect();
  await syncGoLanguage(); // must come before every other Go call
  buildEditor();
  initOptions();
  initGlobal();
  initMode();
  $('lang-select').onchange = (e) => { setLanguage(e.target.value); e.target.blur(); };
  onLanguageChange(() => { onLanguageChanged(); });
  if (import.meta.env.DEV) { window.__addPaths = addPaths; window.__editor = editor; } // in dev mode (browser), use instead of drop
  renderStatus();
  renderFiles();
  setCommand(COMMANDS.some((c) => c.id === settings.command) ? settings.command : 'reduce');
}

start();
