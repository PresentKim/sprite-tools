// 편집기의 파일 띠: 일괄 처리 목록과 같은 파일들을 썸네일로 보여 주고 클릭하면 엽니다.
// 목록 자체는 일괄 처리 화면(main.js)이 갖고 있고, 여기서는 읽어서 그리기만 합니다 (hooks 로 받음).
import { el } from './ui.js';

export const normPath = (p) => (p || '').toLowerCase().split('\\').join('/');

// 띠에 보일 파일들: 오류 파일을 빼고, 이름 필터(소문자)에 맞는 것만.
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

  // 접힌 상태의 얇은 막대
  const rail = el('div', 'ed-strip-rail');
  const expand = el('button', 'ed-strip-toggle', '▸');
  expand.type = 'button';
  expand.title = '파일 띠 펼치기';
  expand.onclick = () => { o.set('collapsed', false); sync(); expand.blur(); };
  const railCount = el('span', 'ed-strip-railcount', '');
  rail.append(expand, railCount);

  // 펼친 상태
  const full = el('div', 'ed-strip-full');
  const head = el('div', 'ed-strip-head');
  const title = el('span', 'ed-strip-title ellipsis', '파일');
  const fold = el('button', 'ed-strip-toggle', '◂');
  fold.type = 'button';
  fold.title = '파일 띠 접기';
  fold.onclick = () => { o.set('collapsed', true); sync(); fold.blur(); };
  head.append(title, fold);

  const adds = el('div', 'ed-strip-adds');
  const addF = el('button', 'btn small', '＋ 파일');
  const addD = el('button', 'btn small', '＋ 폴더');
  for (const b of [addF, addD]) b.type = 'button';
  addF.title = 'PNG 파일을 목록에 추가하고 첫 파일을 엽니다 (일괄 처리 목록과 공유)';
  addD.title = '폴더의 PNG 를 목록에 추가하고 첫 파일을 엽니다';
  addF.onclick = () => { addF.blur(); o.addFiles(); };
  addD.onclick = () => { addD.blur(); o.addFolder(); };
  adds.append(addF, addD);

  const filter = el('input', 'filter');
  filter.type = 'text';
  filter.placeholder = '이름 필터';
  filter.spellcheck = false;
  filter.autocomplete = 'off';
  filter.title = '일괄 처리 목록의 필터와 같습니다';
  filter.oninput = () => o.setFilter(filter.value);
  filter.onkeydown = (e) => {
    if (e.key === 'Escape' || e.key === 'Enter') {
      if (e.key === 'Escape' && filter.value) { filter.value = ''; o.setFilter(''); }
      else filter.blur();
      e.stopPropagation();
    }
  };

  const list = el('div', 'ed-strip-list');
  const empty = el('div', 'ed-strip-empty muted small', '목록이 비어 있습니다. ＋ 버튼을 누르거나 PNG 파일·폴더를 끌어다 놓으세요.');

  const foot = el('div', 'ed-strip-foot');
  const opt = (key, label, tip) => {
    const l = el('label', 'check small');
    const i = el('input');
    i.type = 'checkbox';
    i.checked = !!o.get(key);
    i.onchange = () => { o.set(key, i.checked); i.blur(); };
    l.title = tip;
    l.append(i, ` ${label}`);
    return l;
  };
  foot.append(
    opt('autoSave', '넘어갈 때 자동 저장', 'PageDown/PageUp(Alt+←/→)으로 다음·이전 파일로 넘어갈 때 고친 파일을 먼저 저장합니다 (백업 설정을 따름)'),
    opt('keepZoom', '확대 유지', '다음·이전 파일을 열 때 이전 파일의 확대 배율과 위치를 그대로 씁니다 (끄면 화면에 맞춤)'),
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
    dot.title = '저장하지 않은 변경 사항';
    const rm = el('button', 'ed-file-x', '×');
    rm.type = 'button';
    rm.title = '목록에서 제거 (열려 있는 문서는 그대로)';
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
    title.textContent = total ? `파일 ${text}` : '파일';
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
      if (r.v !== v) { // 썸네일은 파일이 바뀌었을 때만 다시 불러옵니다
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
    empty.textContent = all.length && !shown.length ? '이름이 맞는 파일이 없습니다' : '목록이 비어 있습니다. ＋ 버튼을 누르거나 PNG 파일·폴더를 끌어다 놓으세요.';
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
