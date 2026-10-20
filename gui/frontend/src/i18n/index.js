// Frontend i18n: flat catalogs in ./locales/<code>.json, English is the default and the fallback.
// Adding a language = dropping a new JSON file into ./locales (see docs/TRANSLATING.md).
//
//   t('toast.saved', { name })        -> text with {name} placeholders filled in
//   tn('batch.files.count', 3)        -> plural variant: key + '.one' | '.other' (Intl.PluralRules); {n} is filled in
//   lt('key', params)                 -> () => t(key, params), for labels that are re-applied on language change
//   applyI18n(root)                   -> fills data-i18n / data-i18n-html / data-i18n-title /
//                                        data-i18n-placeholder / data-i18n-aria-label elements below root
const modules = import.meta.glob('./locales/*.json', { eager: true });

const catalogs = {};
for (const [path, mod] of Object.entries(modules)) {
  const code = path.replace(/^.*\//, '').replace(/\.json$/, '');
  catalogs[code] = mod.default || mod;
}

export const DEFAULT_LANGUAGE = 'en';
const STORAGE_KEY = 'sprite-tools-lang';

const listeners = [];
let current = DEFAULT_LANGUAGE;

// Accepts "ko", "ko-KR", "ko_KR.UTF-8"; returns a language code that has a catalog, or ''.
export function normalize(code) {
  const base = String(code || '').toLowerCase().split(/[-_.@]/)[0];
  return catalogs[base] ? base : '';
}

function readSaved() {
  try { return normalize(localStorage.getItem(STORAGE_KEY)); } catch { return ''; }
}

// The saved language wins; the OS/browser locale is deliberately not consulted (first run = English).
current = readSaved() || DEFAULT_LANGUAGE;

export function getLanguage() { return current; }

// Languages with a frontend catalog: [{ code, name }] (name = the language's own name), English first.
export function languages() {
  return Object.keys(catalogs)
    .sort((a, b) => (a === DEFAULT_LANGUAGE ? -1 : b === DEFAULT_LANGUAGE ? 1 : a.localeCompare(b)))
    .map((code) => ({ code, name: catalogs[code]['_meta.name'] || code }));
}

// Switches the language, saves the choice and notifies onLanguageChange listeners. Returns the code in effect.
export function setLanguage(code) {
  const next = normalize(code);
  if (!next) throw new Error(`unsupported language: ${code}`);
  const changed = next !== current;
  current = next;
  try { localStorage.setItem(STORAGE_KEY, next); } catch { /* storage may be unavailable */ }
  if (changed) for (const cb of listeners.slice()) cb(next);
  return next;
}

// cb(code) runs after every language change. Returns an unsubscribe function.
export function onLanguageChange(cb) {
  listeners.push(cb);
  return () => { const i = listeners.indexOf(cb); if (i >= 0) listeners.splice(i, 1); };
}

function lookup(key) {
  const cur = catalogs[current];
  if (cur && typeof cur[key] === 'string') return cur[key];
  const en = catalogs[DEFAULT_LANGUAGE];
  if (en && typeof en[key] === 'string') return en[key];
  return key;
}

function fill(text, params) {
  if (!params) return text;
  return text.replace(/\{(\w+)\}/g, (m, name) => (Object.prototype.hasOwnProperty.call(params, name) ? String(params[name]) : m));
}

// Looks the key up in the current language, then English, then returns the key itself.
export function t(key, params) {
  return fill(lookup(key), params);
}

// Plural form: tries key + '.<category>' (Intl.PluralRules of the current language), then key + '.other'.
export function tn(key, n, params) {
  let cat = 'other';
  try { cat = new Intl.PluralRules(current).select(n); } catch { /* unknown locale */ }
  const k = `${key}.${cat}`;
  const cur = catalogs[current];
  const en = catalogs[DEFAULT_LANGUAGE];
  const has = (c, kk) => !!c && typeof c[kk] === 'string';
  const use = has(cur, k) || has(en, k) ? k : `${key}.other`;
  return fill(lookup(use), { n, ...(params || {}) });
}

// () => t(key, params): a label that is looked up again each time it is applied.
export const lt = (key, params) => () => t(key, params);

const ATTRS = [
  ['data-i18n', (e, v) => { e.textContent = v; }],
  ['data-i18n-html', (e, v) => { e.innerHTML = v; }], // catalog values may contain <kbd> tags (help overlay only)
  ['data-i18n-title', (e, v) => { e.title = v; }],
  ['data-i18n-placeholder', (e, v) => { e.placeholder = v; }],
  ['data-i18n-aria-label', (e, v) => { e.setAttribute('aria-label', v); }],
];

export function applyI18n(root = document) {
  for (const [attr, set] of ATTRS) {
    const nodes = root.querySelectorAll(`[${attr}]`);
    for (const node of nodes) set(node, t(node.getAttribute(attr)));
    if (root.getAttribute && root.hasAttribute(attr)) set(root, t(root.getAttribute(attr)));
  }
  if (root === document || root.documentElement) document.documentElement.lang = current;
}
