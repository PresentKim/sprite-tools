// Review mode controller: rapid triage of many images, one at a time.
//
// The pixel editor (editor/editor.js, created with mode 'review') shows exactly one document; this controller decides which
// one, saves it when it was edited, keeps the progress, and deletes files safely:
//   - navigation: next marks the image as reviewed (saving it first when modified), previous does not change the status
//   - images are decoded ahead of time (a small LRU of the current image +-2) so stepping is instant
//   - delete moves the file to <folder>/.sprite-tools-trash (instant, reversible); the trash is handed to the Recycle Bin on request
//   - progress (which images were reviewed or edited, and the position) is remembered per folder in localStorage
import { EmptyTrashToRecycleBin, LoadImageRGBA, RestoreTrashed, TrashFile, TrashSummary } from '../wailsjs/go/main/App';
import { fromBase64 } from './editor/color.js';
import { editorPrefs } from './editor/editor.js';
import { el, localized, modal } from './editor/ui.js';
import { getLanguage, t, tn } from './i18n/index.js';
import { createVirtual } from './virtual.js';

const STORE_PREFIX = 'sprite-tools-review:'; // + common folder of the files -> { cur, s: { relPath: 1|2 }, del }
const OPTS_KEY = 'sprite-tools-review-opts';
const TRASH_KEY = 'sprite-tools-review-trash';
const FILM_SIZE = 56; // width of a filmstrip cell in px (see .rv-f in editor.css)
const PREFETCH = 2; // images decoded ahead in each direction
const PREFETCH_BYTES = 96 * 1024 * 1024; // decoded pixels kept for neighbours
const STATUS_OK = 1;
const STATUS_EDITED = 2;

const norm = (p) => (p || '').toLowerCase().split('\\').join('/');
const dirOf = (p) => p.replace(/[\\/][^\\/]*$/, '');
const fmtN = (n) => n.toLocaleString(getLanguage());
const localUrl = (f) => `/local?path=${encodeURIComponent(f.path)}&v=${f.v || 0}`;

function readJSON(key, fallback) {
  try {
    const v = JSON.parse(localStorage.getItem(key) || 'null');
    return v == null ? fallback : v;
  } catch {
    return fallback;
  }
}
function writeJSON(key, value) {
  try { localStorage.setItem(key, JSON.stringify(value)); } catch { /* storage may be full or unavailable: progress is simply not kept */ }
}

// o: {
//   ed,                         // the review-mode editor
//   toast(text, cls, actions),
//   entries() -> the batch list entries to review (error-free),
//   removeFile(path), insertFile(entry, afterPath), replaceFiles(paths) -> Promise<number>, addPaths(paths) -> Promise<paths[]>,
//   listChanged() (the batch list changed without a delete), pickFolder() -> Promise<string>, getBackup(), setBackup(on), reveal(path)
// }
export function createReview(o) {
  const ed = o.ed;
  const opts = { unreviewedOnly: false, ...readJSON(OPTS_KEY, {}) };
  const S = {
    active: false,
    order: [], // entries (the objects of the batch list) being reviewed, in order
    cur: -1, // index of the shown entry in order
    vis: [], // what the filmstrip shows (= order, or only unreviewed images plus the current one)
    status: new Map(), // normalized path -> STATUS_OK | STATUS_EDITED
    deleted: 0, // files deleted in this folder (remembered with the progress)
    key: null, // persistence key: the common folder of the files
    base: '', // that folder, normalized
    loadSeq: 0,
    stack: [], // deletions that can still be undone, newest last
    undoFlag: false, // the last action was a deletion (Ctrl+Z restores it when nothing was edited since)
    trashDirs: new Set(readJSON(TRASH_KEY, [])), // .sprite-tools-trash folders that may hold files
    trashInfo: { count: 0, bytes: 0 },
    trashPending: 0, // files trashed since the last time the user was asked about the trash
    stats: [], // navigation timings (ms) for verification
  };
  const kp = (e) => e._k || (e._k = norm(e.path));
  const statusOf = (e) => S.status.get(kp(e)) || 0;
  const current = () => S.order[S.cur] || null;

  // ---------- Persistence ----------

  let saveTimer = 0;
  function persistNow() {
    clearTimeout(saveTimer);
    saveTimer = 0;
    if (S.key == null) return;
    const prefix = S.base ? `${S.base}/` : '';
    const s = {};
    for (const [k, v] of S.status) if (k.startsWith(prefix)) s[k.slice(prefix.length)] = v;
    const e = current();
    writeJSON(STORE_PREFIX + S.key, { v: 1, cur: e ? kp(e).slice(prefix.length) : '', s, del: S.deleted, at: Date.now() });
  }
  function persistSoon() {
    if (!saveTimer) saveTimer = setTimeout(persistNow, 700);
  }
  window.addEventListener('pagehide', persistNow);
  const saveTrashDirs = () => writeJSON(TRASH_KEY, [...S.trashDirs]);

  // The common folder of the files: progress is remembered per folder, so reopening it later picks up where you left off.
  function commonFolder(entries) {
    let parts = null;
    const seen = new Set();
    for (const e of entries) {
      const d = norm(e.dir);
      if (seen.has(d)) continue;
      seen.add(d);
      const p = d.split('/');
      if (!parts) { parts = p; continue; }
      let i = 0;
      while (i < parts.length && i < p.length && parts[i] === p[i]) i++;
      parts.length = i;
    }
    return parts ? parts.join('/') : '';
  }

  // Loads the remembered progress of the folder. Returns the remembered position (a normalized path) or ''.
  function loadProgress(base) {
    S.status = new Map();
    S.deleted = 0;
    const saved = readJSON(STORE_PREFIX + (base || '*'), null);
    if (!saved || typeof saved !== 'object') return '';
    const prefix = base ? `${base}/` : '';
    for (const [rel, v] of Object.entries(saved.s || {})) if (v === STATUS_OK || v === STATUS_EDITED) S.status.set(prefix + rel, v);
    S.deleted = Number.isInteger(saved.del) ? saved.del : 0;
    return saved.cur ? prefix + saved.cur : '';
  }

  // ---------- Images: decoded ahead of time ----------

  const cache = new Map(); // `${path}|${version}` -> Promise<image>
  const cacheKey = (e) => `${e.path}|${e.v || 0}`;
  const decode = (r) => ({
    path: r.path, width: r.width, height: r.height, indexed: r.indexed, colors: r.colors,
    pix: fromBase64(r.pixels, new Uint8ClampedArray(r.width * r.height * 4)),
  });
  function fetchImage(e) {
    const k = cacheKey(e);
    let p = cache.get(k);
    if (!p) {
      p = LoadImageRGBA(e.path).then((r) => { const img = decode(r); p.ready = true; return img; });
      cache.set(k, p);
      p.catch(() => { if (cache.get(k) === p) cache.delete(k); });
    }
    return p;
  }

  // Is entry i part of the walk? Only unreviewed images when the filter is on.
  const walkable = (i) => !opts.unreviewedOnly || !statusOf(S.order[i]);
  // First walkable index at or after i going in direction dir; -1 when none.
  function findFrom(i, dir) {
    for (; i >= 0 && i < S.order.length; i += dir) if (walkable(i)) return i;
    return -1;
  }
  const findNext = (from, dir) => findFrom(from + dir, dir);

  let prefetchSeq = 0;
  async function prefetchAround(idx) {
    const seq = ++prefetchSeq;
    if (!S.order[idx]) return;
    const wanted = [];
    for (const dir of [1, -1]) {
      let at = idx;
      for (let n = 0; n < PREFETCH; n++) {
        at = findNext(at, dir);
        if (at < 0) break;
        wanted.push(S.order[at]);
      }
    }
    const keep = new Set([cacheKey(S.order[idx]), ...wanted.map(cacheKey)]);
    for (const k of cache.keys()) if (!keep.has(k)) cache.delete(k);
    let bytes = 0;
    for (const e of wanted) {
      bytes += (e.width || 0) * (e.height || 0) * 4;
      if (bytes > PREFETCH_BYTES) break;
      if (seq !== prefetchSeq) return; // the user already moved on
      try { await fetchImage(e); } catch { /* an unreadable neighbour is reported when it is actually shown */ }
    }
  }

  // ---------- Showing an image ----------

  // Shows the image at idx. A file that cannot be read is skipped (marked as an error in the batch list).
  async function show(idx, dir = 1) {
    const seq = ++S.loadSeq;
    for (let tries = 0; idx >= 0 && idx < S.order.length && tries < 50; tries++) {
      const e = S.order[idx];
      let img;
      try {
        img = await fetchImage(e);
      } catch (err) {
        if (seq !== S.loadSeq) return false;
        o.toast(t('review.toast.load_failed', { name: e.name, error: String(err) }), 'failed');
        e.error = String(err);
        e.status = 'failed';
        e.message = String(err);
        S.order.splice(idx, 1);
        if (idx < S.cur) S.cur--;
        o.listChanged();
        if (dir < 0) idx--;
        continue;
      }
      if (seq !== S.loadSeq) return false;
      ed.showImage({ path: img.path, width: img.width, height: img.height, indexed: img.indexed, colors: img.colors, pix: img.pix.slice() });
      S.cur = idx;
      afterShow(img);
      return true;
    }
    if (S.order.length) S.cur = Math.min(Math.max(S.cur, 0), S.order.length - 1);
    else { S.cur = -1; ed.clearImage(); }
    renderAll();
    return false;
  }

  function afterShow() {
    renderAll();
    persistSoon();
    prefetchAround(S.cur);
  }

  // ---------- Actions (run one after another) ----------

  let chain = Promise.resolve();
  let waiting = 0;
  // Navigation presses that pile up (key repeat) are dropped beyond two waiting; deletions and the like never are.
  function run(fn, droppable = false) {
    if (droppable && waiting >= 2) return;
    waiting++;
    chain = chain.then(fn).catch((err) => o.toast(String(err), 'failed')).finally(() => { waiting--; });
  }

  function mark(code) {
    const e = current();
    if (!e) return;
    const k = kp(e);
    const old = S.status.get(k) || 0;
    if (code > old) S.status.set(k, code);
  }

  // Saves the shown image when it was modified. false (with a message) when saving failed: the caller must stay on the image.
  async function settle() {
    const r = await ed.saveCurrent();
    if (!r.ok) {
      o.toast(t('review.toast.save_failed', { name: current()?.name || '', error: r.error }), 'failed');
      return false;
    }
    if (r.saved) mark(STATUS_EDITED);
    return true;
  }

  // Moves to a target (a function of the current state, evaluated once the previous image is settled).
  // withMark: the image being left counts as reviewed.
  function go(pick, { withMark = false, droppable = true, dir = 1, edge = false } = {}) {
    const t0 = performance.now();
    run(async () => {
      if (ed.busy()) return;
      if (!S.order.length) return;
      if (!(await settle())) return;
      if (withMark) mark(STATUS_OK);
      const to = pick();
      if (to < 0) {
        renderAll();
        persistSoon();
        if (withMark) endOfList();
        else if (edge) o.toast(t(dir > 0 ? 'toast.last_file' : 'toast.first_file'));
        return;
      }
      S.undoFlag = false;
      const warm = cache.get(cacheKey(S.order[to]))?.ready === true;
      if (await show(to, dir)) {
        // Time from the key press until the image is handed to the editor and the bars are updated (painting follows on the next frame).
        S.stats.push({ ms: performance.now() - t0, cached: warm });
        if (S.stats.length > 500) S.stats.shift();
      }
    }, droppable);
  }

  function endOfList() {
    const left = S.order.reduce((n, e) => n + (statusOf(e) ? 0 : 1), 0);
    if (!left) { o.toast(t('review.toast.all_done')); return; }
    const first = S.order.findIndex((e) => !statusOf(e));
    o.toast(tn('review.toast.end_left', left, { left: fmtN(left) }), '', [
      { label: t('review.jump_unreviewed'), run: () => go(() => first, { droppable: false }) },
    ]);
  }

  const next = () => go(() => findNext(S.cur, 1), { withMark: true });
  const prev = () => go(() => findNext(S.cur, -1), { dir: -1, edge: true });
  const first = () => go(() => findFrom(0, 1), { dir: -1, droppable: false });
  const last = () => go(() => findFrom(S.order.length - 1, -1), { droppable: false });
  const goIndex = (i) => go(() => (i >= 0 && i < S.order.length ? i : -1), { droppable: false, dir: i < S.cur ? -1 : 1 });

  async function promptGoto() {
    if (!S.order.length) return;
    let input;
    const r = await modal({
      title: t('review.goto.title'),
      build: (b) => {
        input = el('input');
        input.type = 'number';
        input.min = 1;
        input.max = S.order.length;
        input.value = S.cur + 1;
        b.append(el('p', '', t('review.goto.body', { max: fmtN(S.order.length) })), input);
        setTimeout(() => input.select(), 0); // typing replaces the current number
      },
      buttons: [
        { label: t('dialog.cancel'), value: null },
        {
          label: t('review.goto.go'), value: 'ok', primary: true,
          validate: () => { const n = Number(input.value); return Number.isInteger(n) && n >= 1 && n <= S.order.length; },
        },
      ],
    });
    if (r === 'ok') goIndex(Number(input.value) - 1);
  }

  // ---------- Deleting (to the trash folder) and undoing ----------

  function trashCurrent() {
    run(async () => {
      const e = current();
      if (!e || ed.busy()) return;
      let r;
      try {
        r = await TrashFile(e.path);
      } catch (err) {
        o.toast(String(err), 'failed');
        return;
      }
      const idx = S.cur;
      const item = { trash: r, entry: e, idx, after: S.order[idx - 1]?.path || '', status: statusOf(e), name: e.name };
      S.stack.push(item);
      S.status.delete(kp(e));
      S.deleted++;
      S.trashPending++;
      S.trashDirs.add(dirOf(r.trashed));
      saveTrashDirs();
      S.order.splice(idx, 1);
      S.cur = Math.min(idx, S.order.length - 1);
      o.removeFile(e.path);
      S.undoFlag = true;
      o.toast(t('review.toast.deleted', { name: e.name }), '', [
        { label: t('review.undo_delete'), run: () => run(() => restoreItem(item)) },
      ]);
      refreshTrash();
      persistSoon();
      let to = findFrom(idx, 1);
      if (to < 0) to = findFrom(idx - 1, -1);
      if (to < 0) { S.cur = -1; ed.clearImage(); renderAll(); return; }
      await show(to, 1);
    });
  }

  async function restoreItem(item) {
    if (!S.stack.includes(item) || ed.busy()) return;
    if (!(await settle())) return;
    try {
      await RestoreTrashed(item.trash.trashed, item.trash.original);
    } catch (err) {
      o.toast(String(err), 'failed');
      return;
    }
    S.stack.splice(S.stack.indexOf(item), 1);
    let at = item.after ? S.order.findIndex((x) => norm(x.path) === norm(item.after)) + 1 : 0;
    if (item.after && at === 0) at = Math.min(item.idx, S.order.length); // the neighbour is gone: fall back to the old position
    S.order.splice(at, 0, item.entry);
    o.insertFile(item.entry, item.after);
    if (item.status) S.status.set(kp(item.entry), item.status);
    S.deleted = Math.max(0, S.deleted - 1);
    S.trashPending = Math.max(0, S.trashPending - 1);
    S.undoFlag = S.stack.length > 0; // more Ctrl+Z steps back through earlier deletions
    refreshTrash();
    o.toast(t('review.toast.restored', { name: item.name }));
    await show(at, 1);
  }

  // Ctrl+Z: restores the last deletion when that was the last action. Returns false when it does not apply (pixel undo goes on).
  function undoDelete() {
    if (!S.undoFlag || !S.stack.length) return false;
    const item = S.stack[S.stack.length - 1];
    run(() => restoreItem(item));
    return true;
  }

  // ---------- The trash folders ----------

  let trashTimer = 0;
  function refreshTrash() {
    clearTimeout(trashTimer);
    trashTimer = setTimeout(async () => {
      try {
        S.trashInfo = S.trashDirs.size ? await TrashSummary([...S.trashDirs]) : { count: 0, bytes: 0 };
      } catch {
        S.trashInfo = { count: 0, bytes: 0 };
      }
      if (!S.trashInfo.count && S.trashDirs.size) { S.trashDirs.clear(); saveTrashDirs(); } // nothing left anywhere
      renderTrash();
    }, 150);
  }

  async function recycle() {
    const dirs = [...S.trashDirs];
    if (!dirs.length) return;
    try {
      const n = await EmptyTrashToRecycleBin(dirs);
      S.stack = []; // restored copies are gone from the trash folders now
      S.undoFlag = false;
      S.trashPending = 0;
      o.toast(tn('review.toast.recycled', n, { n: fmtN(n) }));
    } catch (err) {
      o.toast(t('review.toast.recycle_failed', { error: String(err) }), 'failed');
    }
    refreshTrash(); // recounts; folders that are empty now are forgotten
  }

  // The trash prompt shown when leaving Review or switching folders: the files stay where they are until the user decides.
  function promptTrash() {
    if (!S.trashPending) return;
    const n = Math.max(S.trashPending, S.trashInfo.count);
    S.trashPending = 0;
    const dir = [...S.trashDirs][0];
    o.toast(tn('review.toast.trash_pending', n, { n: fmtN(n) }), '', [
      { label: t('review.recycle_now'), run: () => recycle() },
      { label: t('review.keep'), run: () => {} },
      ...(dir ? [{ label: t('review.show'), run: () => o.reveal(dir) }] : []),
    ]);
  }

  // ---------- Session ----------

  // (Re)builds the review order from the batch list. Returns the index of `keepPath` in it (or -1).
  function rebuildOrder(keepPath) {
    const entries = o.entries().slice();
    const base = commonFolder(entries);
    let resume = '';
    if (S.key !== (base || '*')) {
      persistNow();
      S.key = base || '*';
      S.base = base;
      resume = loadProgress(base);
    }
    S.order = entries;
    const at = keepPath ? S.order.findIndex((e) => kp(e) === keepPath) : -1;
    return { at, resume };
  }

  // Starts (or resumes) reviewing the current list.
  async function begin(keepPath = '') {
    const { at, resume } = rebuildOrder(keepPath);
    S.cur = -1;
    S.stack = [];
    S.undoFlag = false;
    renderAll();
    if (!S.order.length) { ed.clearImage(); return; }
    let target = at;
    let resumed = false;
    if (target < 0 && resume) {
      target = S.order.findIndex((e) => kp(e) === resume);
      resumed = target > 0;
    }
    if (target < 0) target = findFrom(0, 1) >= 0 ? findFrom(0, 1) : 0;
    if (await show(target, 1) && resumed) {
      o.toast(t('review.toast.resumed', { pos: fmtN(target + 1), total: fmtN(S.order.length) }), '', [
        { label: t('review.start_over'), run: () => goIndex(0) },
      ]);
    }
  }

  async function enter() {
    S.active = true;
    ed.activate(true);
    refreshTrash();
    const e = current();
    run(() => begin(e && S.key != null ? kp(e) : ''));
  }

  // Leaving Review: the modified image is saved first (false = it could not be saved, stay), then the trash prompt.
  async function leave() {
    await chain;
    if (!(await settle())) return false;
    persistNow();
    ed.activate(false);
    S.active = false;
    S.loadSeq++;
    prefetchSeq++;
    cache.clear();
    promptTrash();
    return true;
  }

  async function openFolder() {
    const dir = await o.pickFolder();
    if (!dir) return;
    run(async () => {
      if (!(await settle())) return;
      persistNow();
      promptTrash();
      const n = await o.replaceFiles([dir]);
      if (!n) o.toast(t('toast.no_png'), 'failed');
      cache.clear();
      S.key = null; // force a fresh session even for the same folder
      await begin();
    });
  }

  // Files dropped onto the window in Review mode: add them to the list and the review order.
  async function drop(paths) {
    const added = await o.addPaths(paths);
    if (!added.length) { o.toast(t('toast.no_png'), 'failed'); return; }
    run(async () => {
      const e = current();
      const keep = e ? kp(e) : '';
      if (!e) { await begin(); return; }
      S.order = o.entries().slice();
      S.cur = S.order.findIndex((x) => kp(x) === keep);
      renderAll();
    });
  }

  function resetProgress() {
    run(async () => {
      const r = await modal({
        title: t('review.reset.title'),
        build: (b) => b.append(el('p', '', t('review.reset.body'))),
        buttons: [{ label: t('dialog.cancel'), value: null }, { label: t('review.reset.confirm'), value: 'ok', primary: true }],
      });
      if (r !== 'ok') return;
      S.status = new Map();
      S.deleted = 0;
      try { localStorage.removeItem(STORE_PREFIX + (S.key ?? '*')); } catch { /* ignore */ }
      persistSoon();
      renderAll();
      o.toast(t('review.toast.reset'));
    });
  }

  // ---------- UI ----------

  const top = ed.slots.top;
  const bottom = ed.slots.bottom;
  const emptyBox = ed.slots.empty;

  // Top: file name, folder, position, progress.
  const nameEl = el('button', 'rv-name ellipsis');
  nameEl.type = 'button';
  nameEl.onclick = () => { const e = current(); if (e) o.reveal(e.path); nameEl.blur(); };
  const dirEl = el('span', 'rv-dir muted ellipsis');
  const stateEl = el('span', 'rv-state');
  const posEl = el('span', 'rv-pos');
  const openBtn = el('button', 'btn small');
  openBtn.type = 'button';
  openBtn.onclick = () => { openBtn.blur(); openFolder(); };
  const row1 = el('div', 'rv-row');
  row1.append(nameEl, stateEl, dirEl, posEl, openBtn);

  const barEl = el('div', 'rv-bar');
  const barFill = el('i');
  barEl.append(barFill);
  const countsEl = el('span', 'rv-counts muted');
  const resetBtn = el('button', 'btn small ghost');
  resetBtn.type = 'button';
  resetBtn.onclick = () => { resetBtn.blur(); resetProgress(); };
  const row2 = el('div', 'rv-row');
  row2.append(barEl, countsEl, resetBtn);
  top.append(row1, row2);

  // Bottom: filmstrip, options, key hints.
  const filmEl = el('div', 'rv-film');
  const film = createVirtual(filmEl, {
    size: FILM_SIZE,
    horizontal: true,
    pad: 4,
    overscan: 8,
    count: () => S.vis.length,
    keyOf: (i) => `${S.vis[i].path}|${S.vis[i].v || 0}`,
    make: (i) => {
      const cell = el('div', 'rv-f');
      const img = el('img');
      img.loading = 'lazy';
      img.draggable = false;
      img.alt = '';
      img.onerror = () => img.remove();
      img.src = localUrl(S.vis[i]);
      cell.append(img);
      cell.onclick = () => {
        const at = S.order.findIndex((x) => x.path === cell.dataset.path);
        if (at >= 0 && at !== S.cur) goIndex(at);
      };
      return cell;
    },
    patch: (cell, i) => {
      const e = S.vis[i];
      const st = statusOf(e);
      cell.dataset.path = e.path;
      cell.classList.toggle('current', e === current());
      cell.classList.toggle('ok', st === STATUS_OK);
      cell.classList.toggle('edited', st === STATUS_EDITED);
      cell.title = `${e.name}${st ? ` · ${t(st === STATUS_EDITED ? 'review.state.edited' : 'review.state.ok')}` : ''}`;
    },
  });

  const checks = {};
  function check(id, onchange) {
    const l = el('label', 'check small');
    const i = el('input');
    i.type = 'checkbox';
    i.onchange = () => { onchange(i.checked); i.blur(); };
    const text = el('span');
    l.append(i, ' ', text);
    checks[id] = { l, i, text };
    return l;
  }
  const recycleBtn = el('button', 'btn small');
  recycleBtn.type = 'button';
  recycleBtn.onclick = () => { recycleBtn.blur(); recycle(); };
  const controls = el('div', 'rv-controls');
  controls.append(
    check('unreviewed', (on) => { opts.unreviewedOnly = on; writeJSON(OPTS_KEY, opts); renderAll(); prefetchAround(Math.max(S.cur, 0)); }),
    check('keepZoom', (on) => editorPrefs.set('keepZoom', on)),
    check('backup', (on) => o.setBackup(on)),
    el('span', 'spacer'),
    recycleBtn,
  );

  const hints = el('div', 'rv-hints muted');
  const hintParts = [];
  // keys: list of key caps; textKey: catalog key of the action
  function hint(keys, textKey) {
    const h = el('span', 'rv-hint');
    keys.forEach((k, i) => {
      if (i) h.append(' ');
      h.append(el('kbd', '', k));
    });
    const text = el('span');
    h.append(' ', text);
    hintParts.push(() => { text.textContent = t(textKey); });
    hints.append(h);
  }
  hint(['→', 'Enter'], 'review.hint.next');
  hint(['←', 'Backspace'], 'review.hint.prev');
  hint(['Delete'], 'review.hint.delete');
  hint(['Ctrl+Z'], 'review.hint.undo');
  hint(['Home', 'End'], 'review.hint.ends');
  hint(['Ctrl+G'], 'review.hint.goto');
  bottom.append(filmEl, controls, hints);

  // Empty state (nothing to review)
  const emptyTitle = el('p');
  const emptyHint = el('p', 'muted small');
  const emptyBtn = el('button', 'btn');
  emptyBtn.type = 'button';
  emptyBtn.onclick = () => { emptyBtn.blur(); openFolder(); };
  const emptyRow = el('div', 'row');
  emptyRow.append(emptyBtn);
  emptyBox.append(el('div', 'empty-art'), emptyTitle, emptyHint, emptyRow);

  localized(() => {
    openBtn.textContent = t('review.open_folder');
    openBtn.title = t('review.open_folder.title');
    emptyBtn.textContent = t('review.open_folder');
    resetBtn.textContent = t('review.reset');
    resetBtn.title = t('review.reset.title');
    nameEl.title = t('review.reveal.title');
    checks.unreviewed.text.textContent = t('review.unreviewed_only');
    checks.unreviewed.l.title = t('review.unreviewed_only.title');
    checks.keepZoom.text.textContent = t('editor.strip.keep_zoom');
    checks.keepZoom.l.title = t('editor.strip.keep_zoom.title');
    checks.backup.text.textContent = t('review.backup');
    checks.backup.l.title = t('editor.backup.title');
    for (const fn of hintParts) fn();
  });

  function renderTrash() {
    const n = S.trashInfo.count;
    recycleBtn.hidden = !n;
    recycleBtn.textContent = t('review.recycle', { n: fmtN(n) });
    recycleBtn.title = tn('review.recycle.title', n, { n: fmtN(n), mb: (S.trashInfo.bytes / 1048576).toFixed(1) });
  }

  function syncOptions() {
    checks.unreviewed.i.checked = !!opts.unreviewedOnly;
    checks.keepZoom.i.checked = !!editorPrefs.get('keepZoom');
    checks.backup.i.checked = !!o.getBackup();
  }

  // Everything that depends on the position or the statuses.
  function renderAll() {
    const e = current();
    const total = S.order.length;
    let reviewed = 0;
    let edited = 0;
    for (const x of S.order) {
      const st = statusOf(x);
      if (st) reviewed++;
      if (st === STATUS_EDITED) edited++;
    }
    S.vis = opts.unreviewedOnly ? S.order.filter((x) => x === e || !statusOf(x)) : S.order;

    nameEl.textContent = e ? e.name : '';
    dirEl.textContent = e ? e.dir : '';
    dirEl.title = e ? e.dir : '';
    const st = e ? statusOf(e) : 0;
    stateEl.textContent = st ? t(st === STATUS_EDITED ? 'review.state.edited' : 'review.state.ok') : '';
    stateEl.className = `rv-state${st === STATUS_EDITED ? ' edited' : st ? ' ok' : ''}`;
    if (!total) posEl.textContent = '';
    else if (opts.unreviewedOnly) posEl.textContent = t('review.position_filtered', { pos: fmtN(S.cur + 1), total: fmtN(total), left: fmtN(total - reviewed) });
    else posEl.textContent = t('review.position', { pos: fmtN(S.cur + 1), total: fmtN(total) });
    barFill.style.width = total ? `${(reviewed / total) * 100}%` : '0';
    countsEl.textContent = t('review.counts', { reviewed: fmtN(reviewed), edited: fmtN(edited), deleted: fmtN(S.deleted) });
    resetBtn.disabled = !reviewed && !S.deleted;

    emptyTitle.textContent = total ? '' : t('review.empty.title');
    emptyHint.textContent = total ? '' : t('review.empty.hint');
    emptyRow.hidden = !!total;

    syncOptions();
    renderTrash();
    film.refresh();
    const at = e ? S.vis.indexOf(e) : -1;
    if (at >= 0) film.scrollTo(at, 'center');
  }

  return {
    enter, leave, next, prev, first, last, promptGoto, trashCurrent, undoDelete, openFolder, drop, syncOptions,
    relocalize() { film.reset(); renderAll(); },
    // For verification from the dev console.
    stats() {
      const all = S.stats.map((x) => x.ms);
      const warm = S.stats.filter((x) => x.cached).map((x) => x.ms);
      const avg = (a) => (a.length ? a.reduce((s, v) => s + v, 0) / a.length : 0);
      const p95 = (a) => (a.length ? [...a].sort((x, y) => x - y)[Math.floor(a.length * 0.95)] : 0);
      return { n: all.length, avg: avg(all), p95: p95(all), warmN: warm.length, warmAvg: avg(warm), warmP95: p95(warm) };
    },
    resetStats() { S.stats = []; },
    state: () => ({ cur: S.cur, total: S.order.length, deleted: S.deleted, key: S.key, vis: S.vis.length, filmRendered: film.rendered(), stack: S.stack.length, trash: S.trashInfo }),
  };
}
