// Small UI helpers used by the editor: element creation and modal dialogs.

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

// Modal dialog. build(body) fills the body, and it resolves with the value of the pressed button (Esc/outside click = null).
// buttons: [{label, value, primary, validate}]. If validate() returns false, the dialog does not close.
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
      else if (e.key.length === 1 || e.key === 'Backspace') e.stopPropagation(); // keep dialog input from leaking into shortcuts
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
