// 실행 취소/다시 실행 기록.
// 한 단계 = 한 번의 획(또는 채우기·도형)에서 바뀐 픽셀의 패치입니다:
//   { idx: Uint32Array, before: Uint32Array, after: Uint32Array }  (idx = 픽셀 번호 y*W+x, 값은 RGBA uint32)
// 상한: 단계 수와 메모리(패치당 픽셀 1개 = 12바이트)를 모두 넘지 않도록 오래된 것부터 버립니다.

export class History {
  constructor({ maxSteps = 200, maxBytes = 64 * 1024 * 1024 } = {}) {
    this.maxSteps = maxSteps;
    this.maxBytes = maxBytes;
    this.clear();
  }

  clear() {
    this.undoStack = [];
    this.redoStack = [];
    this.bytes = 0;
    this.savedAt = 0; // 저장된 시점의 undoStack 길이. 도달할 수 없으면 -1
  }

  get canUndo() { return this.undoStack.length > 0; }
  get canRedo() { return this.redoStack.length > 0; }
  get dirty() { return this.undoStack.length !== this.savedAt; }

  markSaved() { this.savedAt = this.undoStack.length; }

  static size(p) { return p.idx.length * 12; }

  push(patch) {
    // 새 변경은 다시 실행 기록을 버립니다. 저장된 시점이 거기에 있었다면 더는 돌아갈 수 없습니다.
    if (this.redoStack.length && this.savedAt > this.undoStack.length) this.savedAt = -1;
    this.redoStack = [];
    this.undoStack.push(patch);
    this.bytes += History.size(patch);
    while (this.undoStack.length > 1 && (this.undoStack.length > this.maxSteps || this.bytes > this.maxBytes)) {
      this.bytes -= History.size(this.undoStack.shift());
      if (this.savedAt >= 0) this.savedAt--;
    }
    if (this.savedAt < 0) this.savedAt = -1;
  }

  // 되돌린 패치를 반환합니다 (호출자가 다시 그릴 영역을 알 수 있도록).
  undo(px32) {
    const p = this.undoStack.pop();
    if (!p) return null;
    for (let k = 0; k < p.idx.length; k++) px32[p.idx[k]] = p.before[k];
    this.redoStack.push(p);
    return p;
  }

  redo(px32) {
    const p = this.redoStack.pop();
    if (!p) return null;
    for (let k = 0; k < p.idx.length; k++) px32[p.idx[k]] = p.after[k];
    this.undoStack.push(p);
    return p;
  }
}
