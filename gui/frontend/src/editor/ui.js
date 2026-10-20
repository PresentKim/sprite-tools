// 편집기에서 쓰는 작은 UI 도구: 요소 생성, 모달 대화상자.

// Labels of static widgets (built once) register a function with localized(): it runs now and again after every
// language change (relabel()). Dynamic content (menus, option panels, rows) is simply re-rendered with t().
const labelFns = [];
export function localized(fn) {
  labelFns.push(fn);
  fn();
}
export function relabel() {
  for (const fn of labelFns) fn();
}

export const el = (tag, cls, text) => {
  const e = document.createElement(tag);
  if (cls) e.className = cls;
  if (text != null) e.textContent = text;
  return e;
};

let openCount = 0;
export const isModalOpen = () => openCount > 0;

// 모달 대화상자. build(body) 로 본문을 채우고, 눌린 버튼의 value 로 resolve 합니다 (Esc/바깥 클릭 = null).
// buttons: [{label, value, primary, validate}]. validate() 가 false 를 반환하면 닫지 않습니다.
export function modal({ title, build, buttons }) {
  return new Promise((resolve) => {
    const back = el('div', 'overlay');
    const dlg = el('div', 'dialog ed-dialog');
    dlg.setAttribute('role', 'dialog');
    dlg.setAttribute('aria-modal', 'true');
    const head = el('div', 'dialog-head');
    head.append(el('h2', '', title));
    const body = el('div', 'ed-dialog-body');
    if (build) build(body);
    const foot = el('div', 'ed-dialog-foot');
    let primary = null;
    const done = (v) => {
      openCount--;
      window.removeEventListener('keydown', onKey, true);
      back.remove();
      resolve(v);
    };
    for (const b of buttons) {
      const btn = el('button', `btn${b.primary ? ' primary' : ''}`, b.label);
      btn.onclick = () => { if (b.validate && b.validate() === false) return; done(b.value); };
      if (b.primary) primary = btn;
      foot.append(btn);
    }
    function onKey(e) {
      if (e.key === 'Escape') { e.preventDefault(); e.stopPropagation(); done(null); }
      else if (e.key === 'Enter' && primary && e.target.tagName !== 'BUTTON') { e.preventDefault(); e.stopPropagation(); primary.click(); }
      else if (e.key.length === 1 || e.key === 'Backspace') e.stopPropagation(); // 대화상자 입력이 단축키로 번지지 않게
    }
    window.addEventListener('keydown', onKey, true);
    back.addEventListener('mousedown', (e) => { if (e.target === back) done(null); });
    dlg.append(head, body, foot);
    back.append(dlg);
    document.getElementById('app').append(back);
    openCount++;
    (body.querySelector('input') || primary)?.focus();
  });
}
