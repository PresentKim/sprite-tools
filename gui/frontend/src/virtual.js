// Fixed-size virtual list: only the rows near the viewport exist in the DOM, so lists of thousands of files stay cheap.
// The scroller is the element that scrolls; a content element inside it gets the full scroll size, and rows are placed
// absolutely at index * size. Rows are cached by key, so a row that merely moved (after a delete or an insert) keeps its
// element, including an already loaded thumbnail.
//
// o: {
//   size,                 // row height (row width when horizontal) in px
//   horizontal,           // scroll sideways
//   overscan,             // extra rows rendered on each side
//   pad,                  // blank space before the first and after the last row
//   count(),              // number of items
//   keyOf(i),             // stable key of item i (default: the index)
//   make(i, key),         // creates the row element of item i
//   patch(el, i),         // optional: refreshes an existing row (selection marks, ...) on every sync
// }
export function createVirtual(scroller, o) {
  const horizontal = !!o.horizontal;
  const overscan = o.overscan ?? 6;
  const pad = o.pad ?? 0;
  const keyOf = o.keyOf || ((i) => i);
  const content = document.createElement('div');
  content.className = 'vlist-content';
  content.style.position = 'relative';
  if (horizontal) content.style.height = '100%';
  scroller.prepend(content);

  const rows = new Map(); // key -> element
  let total = 0;
  let raf = 0;
  let first = 0;
  let last = -1;

  const scrollPos = () => (horizontal ? scroller.scrollLeft : scroller.scrollTop);
  const viewSize = () => (horizontal ? scroller.clientWidth : scroller.clientHeight);

  function sync() {
    raf = 0;
    const n = o.count();
    const span = pad * 2 + n * o.size;
    if (n !== total) {
      total = n;
      content.style[horizontal ? 'width' : 'height'] = `${span}px`;
    }
    const pos = scrollPos();
    const view = viewSize() || 400;
    first = Math.max(0, Math.floor((pos - pad) / o.size) - overscan);
    last = Math.min(n - 1, Math.ceil((pos + view - pad) / o.size) + overscan);
    const keep = new Set();
    for (let i = first; i <= last; i++) {
      const key = keyOf(i);
      keep.add(key);
      let el = rows.get(key);
      if (!el) {
        el = o.make(i, key);
        el.style.position = 'absolute';
        if (horizontal) { el.style.top = '0'; el.style.bottom = '0'; el.style.width = `${o.size}px`; }
        else { el.style.left = '0'; el.style.right = '0'; el.style.height = `${o.size}px`; }
        rows.set(key, el);
        content.append(el);
      }
      el.style[horizontal ? 'left' : 'top'] = `${pad + i * o.size}px`;
      o.patch?.(el, i);
    }
    for (const [key, el] of rows) {
      if (!keep.has(key)) { el.remove(); rows.delete(key); }
    }
  }

  const schedule = () => { if (!raf) raf = requestAnimationFrame(sync); };
  scroller.addEventListener('scroll', schedule, { passive: true });
  new ResizeObserver(schedule).observe(scroller);

  return {
    // Re-reads the items: call after the list changed (rows with an unchanged key are reused).
    refresh() { if (raf) { cancelAnimationFrame(raf); raf = 0; } total = -1; sync(); },
    // Drops every cached row (e.g. after a language change, when static row texts must be rebuilt).
    reset() { for (const el of rows.values()) el.remove(); rows.clear(); this.refresh(); },
    // Brings item i into view ('nearest' only scrolls when needed; 'center' puts it in the middle).
    scrollTo(i, align = 'nearest') {
      const start = pad + i * o.size;
      const view = viewSize();
      const pos = scrollPos();
      let to = pos;
      if (align === 'center') to = start - (view - o.size) / 2;
      else if (start < pos) to = start - (i === 0 ? pad : 0);
      else if (start + o.size > pos + view) to = start + o.size - view + (i === total - 1 ? pad : 0);
      to = Math.max(0, to);
      if (to !== pos) scroller[horizontal ? 'scrollLeft' : 'scrollTop'] = to;
      sync();
    },
    // Number of rows currently in the DOM (for verification).
    rendered: () => rows.size,
    range: () => [first, last],
    el: content,
  };
}
