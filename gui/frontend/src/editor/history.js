// 실행 취소/다시 실행 기록.
// 한 단계 = 하나의 편집이며 패치는 두 종류입니다:
//   픽셀 패치: { idx: Uint32Array, before: Uint32Array, after: Uint32Array }  (idx = 픽셀 번호 y*W+x, 값은 RGBA uint32)
//   스냅샷 패치: { kind: 'snap', w0, h0, pix0, w1, h1, pix1 }  (크기가 바뀌는 편집 — 문서 전체의 RGBA 바이트 복사본)
// 기록은 패치를 쌓고 꺼내기만 하며, 실제로 문서에 적용하는 일은 편집기가 합니다.
// 상한: 문서마다 단계 수(maxSteps)가 있고, 메모리(픽셀 패치는 픽셀 1개 = 12바이트, 스냅샷은 바이트 수)는
// 열려 있는 모든 문서가 HistoryBudget 하나를 나눠 씁니다. 예산을 넘으면 가장 오래 쓰지 않은 문서의 오래된 단계부터 버립니다.

// 여러 문서의 기록이 함께 쓰는 메모리 예산.
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

  // 예산을 넘었으면 줄입니다. keep(방금 쓴 기록)은 마지막까지 건드리지 않고, 그 안에서도 가장 최근 한 단계는 남깁니다.
  enforce(keep) {
    if (this.total <= this.maxBytes) return;
    const order = [...this.items].filter((h) => h !== keep).sort((a, b) => a.touched - b.touched);
    if (keep) order.push(keep);
    // 1) 오래 안 쓴 문서의 오래된 실행 취소 단계부터 (keep 은 최근 한 단계를 남김)
    for (const h of order) {
      const floor = h === keep ? 1 : 0;
      while (h.undoStack.length > floor && this.total > this.maxBytes) h.dropOldest();
      if (this.total <= this.maxBytes) return;
    }
    // 2) 그래도 넘으면 쓰지 않는 문서의 다시 실행 단계를 버립니다.
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
    this.bytes = 0; // 실행 취소 + 다시 실행 단계가 차지하는 바이트
    this.savedAt = 0; // 저장된 시점의 undoStack 길이. 도달할 수 없으면 -1
  }

  get canUndo() { return this.undoStack.length > 0; }
  get canRedo() { return this.redoStack.length > 0; }
  get dirty() { return this.undoStack.length !== this.savedAt; }

  markSaved() { this.savedAt = this.undoStack.length; }

  // 문서를 닫을 때 예산에서 빼고 비웁니다.
  dispose() {
    this.budget?.remove(this);
    this.clear();
  }

  static size(p) { return p.kind === 'snap' ? p.pix0.length + p.pix1.length : p.idx.length * 12; }

  dropOldest() {
    const p = this.undoStack.shift();
    if (!p) return;
    this.bytes -= History.size(p);
    if (this.savedAt >= 0) this.savedAt--; // 저장 시점이 버려진 구간이면 -1 이 되어 계속 "변경됨"으로 남음
    if (this.savedAt < 0) this.savedAt = -1;
  }

  dropRedo() {
    for (const p of this.redoStack) this.bytes -= History.size(p);
    this.redoStack = [];
  }

  push(patch) {
    // 새 변경은 다시 실행 기록을 버립니다. 저장된 시점이 거기에 있었다면 더는 돌아갈 수 없습니다.
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

  // 되돌릴 패치를 꺼내 반환합니다 (호출자가 before 쪽을 적용).
  undo() {
    const p = this.undoStack.pop();
    if (!p) return null;
    this.redoStack.push(p);
    this.budget?.touch(this);
    return p;
  }

  // 다시 실행할 패치를 꺼내 반환합니다 (호출자가 after 쪽을 적용).
  redo() {
    const p = this.redoStack.pop();
    if (!p) return null;
    this.undoStack.push(p);
    this.budget?.touch(this);
    return p;
  }
}
