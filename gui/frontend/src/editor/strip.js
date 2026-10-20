// The editor's file strip: shows the same files as the batch list as thumbnails, and opens one on click.
// The list itself is owned by the batch screen (main.js); this only reads and draws it (received through hooks).
import { t as tr } from '../i18n/index.js';
import { el, localized } from './ui.js';

export const normPath = (p) => (p || '').toLowerCase().split('\\').join('/');

// Files shown in the strip: excluding error files, and only those matching the name filter (lowercase).
export function visibleList(files, filter) {
  const f = (filter || '').trim().toLowerCase();
  return files.filter((x) => !x.error && (!f || x.name.toLowerCase().includes(f)));
}

const localUrl = (f) => `/local?path=${encodeURIComponent(f.path)}&v=${f.v || 0}`;

// o: {
//   files(), filter(), setFilter(text), stateOf(path) -> {open, dirty, current},
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

  const rows = new Map(); // normPath -> {el, img, v, ...}

  function makeRow(f) {
    const row = el('div', 'ed-file');
    const thumb = el('div', 'ed-file-thumb');
    const img = el('img');
    img.loading = 'lazy';
    img.draggable = false;
    img.alt = '';
    img.onerror = () => img.remove();
    thumb.append(img);
    const main = el('div', 'ed-file-main');
    const name = el('div', 'ed-file-name ellipsis');
    const sub = el('div', 'ed-file-sub ellipsis');
    main.append(name, sub);
    const dot = el('span', 'ed-file-dot', '●');
    dot.title = tr('editor.unsaved');
    const rm = el('button', 'ed-file-x', '×');
    rm.type = 'button';
    rm.title = tr('editor.strip.remove');
    rm.onclick = (e) => { e.stopPropagation(); o.remove(row.dataset.path); };
    row.append(thumb, main, dot, rm);
    row.onclick = () => o.open(row.dataset.path, false);
    row.ondblclick = () => o.open(row.dataset.path, true);
    return { el: row, thumb, img, name, sub, v: null };
  }

  function sync() {
    const collapsed = !!o.get('collapsed');
    root.classList.toggle('collapsed', collapsed);
    rail.hidden = !collapsed;
    full.hidden = collapsed;
  }

  function render() {
    sync();
    const all = o.files();
    const shown = visibleList(all, o.filter());
    const total = all.filter((f) => !f.error).length;
    const text = o.filter().trim() ? `${shown.length} / ${total}` : String(total);
    title.textContent = total ? tr('editor.strip.title_n', { text }) : tr('editor.strip.title');
    railCount.textContent = String(total || '');
    if (document.activeElement !== filter && filter.value !== o.filter()) filter.value = o.filter();

    const keep = new Set();
    const ordered = shown.map((f) => {
      const key = normPath(f.path);
      keep.add(key);
      let r = rows.get(key);
      if (!r) { r = makeRow(f); rows.set(key, r); }
      r.el.dataset.path = f.path;
      if (r.name.textContent !== f.name) r.name.textContent = f.name;
      const sub = f.width ? `${f.width}×${f.height}` : '';
      if (r.sub.textContent !== sub) r.sub.textContent = sub;
      r.el.title = f.path;
      const v = f.v || 0;
      if (r.v !== v) { // thumbnails are reloaded only when the file has changed
        r.v = v;
        if (!r.img.isConnected) r.thumb.append(r.img);
        r.img.src = localUrl(f);
      }
      const st = o.stateOf(f.path);
      r.el.classList.toggle('current', !!st.current);
      r.el.classList.toggle('open', !!st.open);
      r.el.classList.toggle('dirty', !!st.dirty);
      return r.el;
    });
    for (const [key, r] of rows) if (!keep.has(key)) { r.el.remove(); rows.delete(key); }
    const same = ordered.length === list.children.length && ordered.every((n, i) => list.children[i] === n);
    if (!same) list.replaceChildren(...ordered);
    empty.hidden = shown.length > 0;
    empty.textContent = tr(all.length && !shown.length ? 'editor.strip.no_match' : 'editor.strip.empty');
    // static texts of existing rows follow the current language
    for (const r of rows.values()) {
      r.el.querySelector('.ed-file-dot').title = tr('editor.unsaved');
      r.el.querySelector('.ed-file-x').title = tr('editor.strip.remove');
    }
  }

  function scrollToCurrent() {
    list.querySelector('.ed-file.current')?.scrollIntoView({ block: 'nearest' });
  }

  function syncOptions() {
    for (const i of foot.querySelectorAll('input')) i.checked = !!o.get(i.dataset.key || '');
  }
  foot.querySelectorAll('input').forEach((i, n) => { i.dataset.key = ['autoSave', 'keepZoom'][n]; });

  sync();
  return { el: root, render, scrollToCurrent, syncOptions };
}
