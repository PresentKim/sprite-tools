// 문서 탭 막대. 상태는 편집기가 갖고, 여기서는 그려 주고 클릭만 알립니다.
// 탭: 이름 + 저장 안 한 변경 점(●) + 닫기(×). 미리보기 탭(탐색으로 연 탭)은 이름이 기울어집니다.
import { t as tr } from '../i18n/index.js';
import { el, localized } from './ui.js';

// cb: { activate(id), close(id), pin(id), closeClean() }
export function createTabs(cb) {
  const root = el('div', 'ed-tabbar');
  const list = el('div', 'ed-tabs');
  list.setAttribute('role', 'tablist');
  const clean = el('button', 'ed-tabs-clean');
  clean.type = 'button';
  localized(() => {
    clean.textContent = tr('editor.tabs.clean');
    clean.title = tr('editor.tabs.clean.title');
  });
  clean.onclick = () => { cb.closeClean(); clean.blur(); };
  root.append(list, clean);

  // 휠로 가로 스크롤
  list.addEventListener('wheel', (e) => {
    if (list.scrollWidth <= list.clientWidth) return;
    e.preventDefault();
    list.scrollLeft += e.deltaX || e.deltaY;
  }, { passive: false });

  const nodes = new Map(); // id -> 탭 요소

  function make(id) {
    const tab = el('div', 'ed-tab');
    tab.setAttribute('role', 'tab');
    tab.dataset.id = String(id);
    const name = el('span', 'ed-tab-name ellipsis');
    const dot = el('span', 'ed-tab-dot', '●');
    dot.title = tr('editor.unsaved');
    const x = el('button', 'ed-tab-x', '×');
    x.type = 'button';
    x.title = tr('editor.tabs.close.title');
    x.setAttribute('aria-label', tr('editor.tabs.close.aria'));
    x.onclick = (e) => { e.stopPropagation(); cb.close(id); };
    x.onpointerdown = (e) => e.stopPropagation();
    tab.append(name, dot, x);
    tab.onpointerdown = (e) => {
      if (e.button === 1) { e.preventDefault(); return; } // 가운데 버튼 자동 스크롤 방지
      if (e.button === 0) cb.activate(id);
    };
    tab.onauxclick = (e) => { if (e.button === 1) { e.preventDefault(); cb.close(id); } };
    tab.ondblclick = () => cb.pin(id);
    nodes.set(id, tab);
    return tab;
  }

  // docs: [{id, name, path, preview}], dirty: (doc) => bool
  function render(docs, activeId, dirty) {
    const keep = new Set(docs.map((d) => d.id));
    for (const [id, n] of nodes) if (!keep.has(id)) { n.remove(); nodes.delete(id); }
    const ordered = docs.map((d) => {
      const tab = nodes.get(d.id) || make(d.id);
      const isDirty = dirty(d);
      tab.classList.toggle('active', d.id === activeId);
      tab.classList.toggle('preview', !!d.preview);
      tab.classList.toggle('dirty', isDirty);
      tab.setAttribute('aria-selected', String(d.id === activeId));
      tab.title = `${d.path || d.name}${d.preview ? `\n${tr('editor.tabs.preview')}` : ''}${isDirty ? `\n${tr('editor.unsaved_exist')}` : ''}`;
      // the static texts of a tab are created once, so they are refreshed here (render runs again on language change)
      tab.querySelector('.ed-tab-dot').title = tr('editor.unsaved');
      const close = tab.querySelector('.ed-tab-x');
      close.title = tr('editor.tabs.close.title');
      close.setAttribute('aria-label', tr('editor.tabs.close.aria'));
      const name = tab.firstChild;
      if (name.textContent !== d.name) name.textContent = d.name;
      return tab;
    });
    // 순서가 같으면 DOM 을 건드리지 않습니다 (클릭 도중 요소가 바뀌지 않도록)
    const same = ordered.length === list.children.length && ordered.every((t, i) => list.children[i] === t);
    if (!same) list.replaceChildren(...ordered);
    root.hidden = docs.length === 0;
    clean.disabled = !docs.some((d) => !dirty(d));
    scrollToActive(activeId);
  }

  function scrollToActive(id) {
    const t = nodes.get(id);
    if (!t) return;
    const l = list.getBoundingClientRect(), r = t.getBoundingClientRect();
    if (r.left < l.left) list.scrollLeft += r.left - l.left - 4;
    else if (r.right > l.right) list.scrollLeft += r.right - l.right + 4;
  }

  return { el: root, render };
}
