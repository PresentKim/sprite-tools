// 실행 취소/다시 실행 기록.
// 한 단계 = 하나의 편집이며 패치는 두 종류입니다:
//   픽셀 패치: { idx: Uint32Array, before: Uint32Array, after: Uint32Array }  (idx = 픽셀 번호 y*W+x, 값은 RGBA uint32)
//   스냅샷 패치: { kind: 'snap', w0, h0, pix0, w1, h1, pix1 }  (크기가 바뀌는 편집 — 문서 전체의 RGBA 바이트 복사본)
// 기록은 패치를 쌓고 꺼내기만 하며, 실제로 문서에 적용하는 일은 편집기가 합니다.
// 상한: 단계 수와 메모리(픽셀 패치는 픽셀 1개 = 12바이트, 스냅샷은 바이트 수)를 모두 넘지 않도록 오래된 것부터 버립니다.

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

  static size(p) { return p.kind === 'snap' ? p.pix0.length + p.pix1.length : p.idx.length * 12; }

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

  // 되돌릴 패치를 꺼내 반환합니다 (호출자가 before 쪽을 적용).
  undo() {
    const p = this.undoStack.pop();
    if (!p) return null;
    this.redoStack.push(p);
    return p;
  }

  // 다시 실행할 패치를 꺼내 반환합니다 (호출자가 after 쪽을 적용).
  redo() {
    const p = this.redoStack.pop();
    if (!p) return null;
    this.undoStack.push(p);
    return p;
  }
}
