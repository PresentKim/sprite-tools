// The editor's file strip: shows the same files as the batch list as thumbnails, and opens one on click.
// The list itself is owned by the batch screen (main.js); this only reads and draws it (received through hooks).
// Only the rows near the viewport exist in the DOM (virtual list), so thousands of files stay cheap and only visible
// rows request their thumbnail.
import { t as tr } from '../i18n/index.js';
import { createVirtual } from '../virtual.js';
import { el, localized } from './ui.js';

export const normPath = (p) => (p || '').toLowerCase().split('\\').join('/');

// Files shown in the strip: excluding error files, and only those matching the name filter (lowercase).
export function visibleList(files, filter) {
  const f = (filter || '').trim().toLowerCase();
  return files.filter((x) => !x.error && (!f || x.name.toLowerCase().includes(f)));
}

const localUrl = (f) => `/local?path=${encodeURIComponent(f.path)}&v=${f.v || 0}`;

// Height of one strip row in px (must match .ed-file in editor.css).
const ROW = 44;

// o: {
//   files(), filter(), setFilter(text), stateOf(path) -> {open, dirty, current}, current() -> path of the shown document,
//   open(path, pin), addFiles(), addFolder(), remove(path),
//   get(key), set(key, value)   // autoSave | keepZoom | collapsed
// }
export function createStrip(o) {
  const root = el('aside', 'ed-strip');

  // Thin bar in the collapsed state
  const rail = el('div', 'ed-strip-rail');
  const expand = el('button', 'ed-strip-toggle', '▸');
  expand.type = 'button';
  localized(() => { expand.title = tr('editor.strip.expand'); });
  expand.onclick = () => { o.set('collapsed', false); sync(); expand.blur(); };
  const railCount = el('span', 'ed-strip-railcount', '');
  rail.append(expand, railCount);

  // Expanded state
  const full = el('div', 'ed-strip-full');
  const head = el('div', 'ed-strip-head');
  const title = el('span', 'ed-strip-title ellipsis');
  const fold = el('button', 'ed-strip-toggle', '◂');
  fold.type = 'button';
  localized(() => { fold.title = tr('editor.strip.collapse'); });
  fold.onclick = () => { o.set('collapsed', true); sync(); fold.blur(); };
  head.append(title, fold);

  const adds = el('div', 'ed-strip-adds');
  const addF = el('button', 'btn small');
  const addD = el('button', 'btn small');
  for (const b of [addF, addD]) b.type = 'button';
  localized(() => {
    addF.textContent = tr('editor.strip.add_files');
    addD.textContent = tr('editor.strip.add_folder');
    addF.title = tr('editor.strip.add_files.title');
    addD.title = tr('editor.strip.add_folder.title');
  });
  addF.onclick = () => { addF.blur(); o.addFiles(); };
  addD.onclick = () => { addD.blur(); o.addFolder(); };
  adds.append(addF, addD);

  const filter = el('input', 'filter');
  filter.type = 'text';
  filter.spellcheck = false;
  filter.autocomplete = 'off';
  localized(() => {
    filter.placeholder = tr('editor.strip.filter');
    filter.title = tr('editor.strip.filter.title');
  });
  filter.oninput = () => o.setFilter(filter.value);
  filter.onkeydown = (e) => {
    if (e.key === 'Escape' || e.key === 'Enter') {
      if (e.key === 'Escape' && filter.value) { filter.value = ''; o.setFilter(''); }
      else filter.blur();
      e.stopPropagation();
    }
  };

  const list = el('div', 'ed-strip-list');
  const empty = el('div', 'ed-strip-empty muted small');

  const foot = el('div', 'ed-strip-foot');
  // labelKey / tipKey: catalog keys of the label and of its tooltip
  const opt = (key, labelKey, tipKey) => {
    const l = el('label', 'check small');
    const i = el('input');
    i.type = 'checkbox';
    i.checked = !!o.get(key);
    i.onchange = () => { o.set(key, i.checked); i.blur(); };
    const text = el('span');
    localized(() => {
      l.title = tr(tipKey);
      text.textContent = tr(labelKey);
    });
    l.append(i, ' ', text);
    return l;
  };
  foot.append(
    opt('autoSave', 'editor.strip.auto_save', 'editor.strip.auto_save.title'),
    opt('keepZoom', 'editor.strip.keep_zoom', 'editor.strip.keep_zoom.title'),
  );
  full.append(head, adds, filter, list, empty, foot);
  root.append(rail, full);

  let shown = []; // the files the list currently shows

  function makeRow(f) {
    const row = el('div', 'ed-file');
    const thumb = el('div', 'ed-file-thumb');
    const img = el('img');
    img.loading = 'lazy';
    img.draggable = false;
    img.alt = '';
    img.onerror = () => img.remove();
    img.src = localUrl(f);
    thumb.append(img);
    const main = el('div', 'ed-file-main');
    const name = el('div', 'ed-file-name ellipsis');
    const sub = el('div', 'ed-file-sub ellipsis');
    main.append(name, sub);
    const dot = el('span', 'ed-file-dot', '●');
    const rm = el('button', 'ed-file-x', '×');
    rm.type = 'button';
    rm.onclick = (e) => { e.stopPropagation(); o.remove(row.dataset.path); };
    row.append(thumb, main, dot, rm);
    row.onclick = () => o.open(row.dataset.path, false);
    row.ondblclick = () => o.open(row.dataset.path, true);
    row._parts = { name, sub, dot, rm };
    return row;
  }

  // Brings an existing row up to date with its file (called for every visible row on every sync).
  function patchRow(row, i) {
    const f = shown[i];
    if (!f) return;
    const p = row._parts;
    row.dataset.path = f.path;
    if (p.name.textContent !== f.name) p.name.textContent = f.name;
    const sub = f.width ? `${f.width}×${f.height}` : '';
    if (p.sub.textContent !== sub) p.sub.textContent = sub;
    row.title = f.path;
    p.dot.title = tr('editor.unsaved');
    p.rm.title = tr('editor.strip.remove');
    const st = o.stateOf(f.path);
    row.classList.toggle('current', !!st.current);
    row.classList.toggle('open', !!st.open);
    row.classList.toggle('dirty', !!st.dirty);
  }

  // A row is reused while its file and version are unchanged (a changed version reloads the thumbnail).
  const virt = createVirtual(list, {
    size: ROW,
    pad: 2,
    count: () => shown.length,
    keyOf: (i) => `${normPath(shown[i].path)}|${shown[i].v || 0}`,
    make: (i) => makeRow(shown[i]),
    patch: patchRow,
  });

  function sync() {
    const collapsed = !!o.get('collapsed');
    root.classList.toggle('collapsed', collapsed);
    rail.hidden = !collapsed;
    full.hidden = collapsed;
  }

  function render() {
    sync();
    const all = o.files();
    shown = visibleList(all, o.filter());
    const total = all.reduce((n, f) => n + (f.error ? 0 : 1), 0);
    const text = o.filter().trim() ? `${shown.length} / ${total}` : String(total);
    title.textContent = total ? tr('editor.strip.title_n', { text }) : tr('editor.strip.title');
    railCount.textContent = String(total || '');
    if (document.activeElement !== filter && filter.value !== o.filter()) filter.value = o.filter();
    virt.refresh();
    empty.hidden = shown.length > 0;
    empty.textContent = tr(all.length && !shown.length ? 'editor.strip.no_match' : 'editor.strip.empty');
  }

  function scrollToCurrent() {
    const cur = normPath(o.current());
    const i = cur ? shown.findIndex((f) => normPath(f.path) === cur) : -1;
    if (i >= 0) virt.scrollTo(i);
  }

  function syncOptions() {
    for (const i of foot.querySelectorAll('input')) i.checked = !!o.get(i.dataset.key || '');
  }
  foot.querySelectorAll('input').forEach((i, n) => { i.dataset.key = ['autoSave', 'keepZoom'][n]; });

  sync();
  return { el: root, render, scrollToCurrent, syncOptions, rendered: () => virt.rendered() };
}
