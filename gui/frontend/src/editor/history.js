// Undo/redo history.
// One step = one edit, and a patch is one of two kinds:
//   pixel patch: { idx: Uint32Array, before: Uint32Array, after: Uint32Array }  (idx = pixel index y*W+x, values are RGBA uint32)
//   snapshot patch: { kind: 'snap', w0, h0, pix0, w1, h1, pix1 }  (for edits that change the size - a copy of the whole document's RGBA bytes)
// The history only stacks and pops patches; applying them to the document is the editor's job.
// Limits: each document has a step count (maxSteps), and memory (a pixel patch costs 12 bytes per pixel, a snapshot its byte count)
// is shared by all open documents through a single HistoryBudget. Over budget, the oldest steps of the least recently used document are dropped first.

// Memory budget shared by the histories of several documents.
export class HistoryBudget {
  constructor(maxBytes = 256 * 1024 * 1024) {
    this.maxBytes = maxBytes;
    this.items = new Set();
    this.clock = 0;
  }

  add(h) { this.items.add(h); h.budget = this; h.touched = ++this.clock; }
  remove(h) { this.items.delete(h); if (h.budget === this) h.budget = null; }
  touch(h) { h.touched = ++this.clock; }

  get total() {
    let n = 0;
    for (const h of this.items) n += h.bytes;
    return n;
  }

  // If over budget, trim. keep (the history just written to) is touched last, and even then its most recent step is kept.
  enforce(keep) {
    if (this.total <= this.maxBytes) return;
    const order = [...this.items].filter((h) => h !== keep).sort((a, b) => a.touched - b.touched);
    if (keep) order.push(keep);
    // 1) Oldest undo steps of the least recently used documents first (keep retains its latest step)
    for (const h of order) {
      const floor = h === keep ? 1 : 0;
      while (h.undoStack.length > floor && this.total > this.maxBytes) h.dropOldest();
      if (this.total <= this.maxBytes) return;
    }
    // 2) If still over, drop the redo steps of documents not in use.
    for (const h of order) {
      if (h === keep) continue;
      if (h.redoStack.length) h.dropRedo();
      if (this.total <= this.maxBytes) return;
    }
  }
}

export class History {
  constructor({ maxSteps = 200, budget = null } = {}) {
    this.maxSteps = maxSteps;
    this.budget = null;
    this.touched = 0;
    this.clear();
    if (budget) budget.add(this);
  }

  clear() {
    this.undoStack = [];
    this.redoStack = [];
    this.bytes = 0; // bytes occupied by undo + redo steps
    this.savedAt = 0; // length of undoStack at the saved point; -1 if unreachable
  }

  get canUndo() { return this.undoStack.length > 0; }
  get canRedo() { return this.redoStack.length > 0; }
  get dirty() { return this.undoStack.length !== this.savedAt; }

  markSaved() { this.savedAt = this.undoStack.length; }

  // When a document is closed, subtract it from the budget and clear it.
  dispose() {
    this.budget?.remove(this);
    this.clear();
  }

  static size(p) { return p.kind === 'snap' ? p.pix0.length + p.pix1.length : p.idx.length * 12; }

  dropOldest() {
    const p = this.undoStack.shift();
    if (!p) return;
    this.bytes -= History.size(p);
    if (this.savedAt >= 0) this.savedAt--; // if the saved point fell in the dropped range this becomes -1 and stays "modified"
    if (this.savedAt < 0) this.savedAt = -1;
  }

  dropRedo() {
    for (const p of this.redoStack) this.bytes -= History.size(p);
    this.redoStack = [];
  }

  push(patch) {
    // A new change discards the redo history. If the saved point was there, it can no longer be returned to.
    if (this.redoStack.length && this.savedAt > this.undoStack.length) this.savedAt = -1;
    this.dropRedo();
    this.undoStack.push(patch);
    this.bytes += History.size(patch);
    while (this.undoStack.length > 1 && this.undoStack.length > this.maxSteps) this.dropOldest();
    if (this.savedAt < 0) this.savedAt = -1;
    if (this.budget) {
      this.budget.touch(this);
      this.budget.enforce(this);
    }
  }

  // Pop the patch to undo and return it (the caller applies the before side).
  undo() {
    const p = this.undoStack.pop();
    if (!p) return null;
    this.redoStack.push(p);
    this.budget?.touch(this);
    return p;
  }

  // Pop the patch to redo and return it (the caller applies the after side).
  redo() {
    const p = this.redoStack.pop();
    if (!p) return null;
    this.undoStack.push(p);
    this.budget?.touch(this);
    return p;
  }
}
